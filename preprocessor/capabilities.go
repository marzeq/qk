package preprocessor

import (
	"sort"
	"strings"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

type capabilityDeclaration struct {
	name string
	expr parser.ExpressionNode
	loc  shared.Location
}

// ResolveCapabilities collects and evaluates trusted standard-library
// compile-time capability declarations. When disabled, known capabilities are
// retained but forced to false for -nostdlib preprocessing.
func ResolveCapabilities(sources map[string]string, config Config, disabled bool) (map[string]bool, error) {
	origins := make([]string, 0, len(sources))
	for origin := range sources {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	declarations := map[string]capabilityDeclaration{}
	for _, origin := range origins {
		tokens, err := tokeniser.NewTokeniser(sources[origin], origin).Tokenise()
		if err != nil {
			return nil, err
		}
		found, err := collectCapabilityDeclarations(tokens)
		if err != nil {
			return nil, err
		}
		for _, declaration := range found {
			if previous, exists := declarations[declaration.name]; exists {
				return nil, shared.NewError(declaration.loc, "duplicate compile-time capability %q; previously declared at %s", declaration.name, previous.loc)
			}
			declarations[declaration.name] = declaration
		}
	}

	values := make(map[string]bool, len(declarations))
	if disabled {
		for name := range declarations {
			values[name] = false
		}
		return values, nil
	}
	target := targetFromTriple(config.TargetTriple)
	target.noLibc, target.noStdlib = config.NoLibc, config.NoStdlib
	state := map[string]uint8{}
	var resolve func(string, shared.Location) (bool, error)
	resolve = func(name string, loc shared.Location) (bool, error) {
		declaration, exists := declarations[name]
		if !exists {
			return false, shared.NewError(loc, "unknown compile-time value %q", name)
		}
		switch state[name] {
		case 1:
			return false, shared.NewError(loc, "cyclic compile-time capability involving %q", name)
		case 2:
			return values[name], nil
		}
		state[name] = 1
		value, err := evaluate(declaration.expr, target, resolve)
		if err != nil {
			return false, err
		}
		if value.kind != valueBool {
			return false, shared.NewError(declaration.loc, "compile-time capability %q must be boolean", name)
		}
		values[name] = value.boolean
		state[name] = 2
		return value.boolean, nil
	}
	names := make([]string, 0, len(declarations))
	for name := range declarations {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := resolve(name, declarations[name].loc); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func stripCapabilityDeclarations(tokens []tokeniser.Token, trusted bool) ([]tokeniser.Token, error) {
	declarations, ranges, err := scanCapabilityDeclarations(tokens)
	if err != nil {
		return nil, err
	}
	if len(declarations) != 0 && !trusted {
		return nil, shared.NewError(declarations[0].loc, "compile-time capabilities may only be declared by the trusted standard library")
	}
	if len(ranges) == 0 {
		return tokens, nil
	}
	result := make([]tokeniser.Token, 0, len(tokens))
	nextRange := 0
	for index := 0; index < len(tokens); {
		if nextRange < len(ranges) && index == ranges[nextRange][0] {
			index = ranges[nextRange][1]
			nextRange++
			continue
		}
		result = append(result, tokens[index])
		index++
	}
	return result, nil
}

func collectCapabilityDeclarations(tokens []tokeniser.Token) ([]capabilityDeclaration, error) {
	declarations, _, err := scanCapabilityDeclarations(tokens)
	return declarations, err
}

func scanCapabilityDeclarations(tokens []tokeniser.Token) ([]capabilityDeclaration, [][2]int, error) {
	var declarations []capabilityDeclaration
	var ranges [][2]int
	braceDepth := 0
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if token.Type == tokeniser.TokenOpenCurly {
			braceDepth++
			continue
		}
		if token.Type == tokeniser.TokenCloseCurly {
			braceDepth--
			continue
		}
		if braceDepth != 0 || token.Type != tokeniser.TokenKeyword || token.Value != string(tokeniser.KeywordLet) {
			continue
		}
		if index+3 >= len(tokens) || tokens[index+1].Type != tokeniser.TokenIdentifier ||
			tokens[index+2].Type != tokeniser.TokenEquals || tokens[index+3].Type != tokeniser.TokenIdentifier ||
			tokens[index+3].Value != "compile_time" {
			continue
		}
		nameToken := tokens[index+1]
		if !strings.HasPrefix(nameToken.Value, "Has") || len(nameToken.Value) == 3 {
			return nil, nil, shared.NewError(nameToken.Loc, "compile-time capability name %q must begin with Has", nameToken.Value)
		}
		end, parens, squares := index+4, 0, 0
		for end < len(tokens) {
			t := tokens[end]
			switch t.Type {
			case tokeniser.TokenOpenParen:
				parens++
			case tokeniser.TokenCloseParen:
				parens--
			case tokeniser.TokenOpenSquare:
				squares++
			case tokeniser.TokenCloseSquare:
				squares--
			}
			if parens == 0 && squares == 0 && (t.Type == tokeniser.TokenNewline || t.Type == tokeniser.TokenSemicolon || t.Type == tokeniser.TokenEof) {
				break
			}
			end++
		}
		if end == index+4 {
			return nil, nil, shared.NewError(nameToken.Loc, "expected expression after compile_time")
		}
		expr, err := parseCondition(tokens[index+4:end], nameToken.Loc)
		if err != nil {
			return nil, nil, err
		}
		declarations = append(declarations, capabilityDeclaration{name: nameToken.Value, expr: expr, loc: nameToken.Loc})
		ranges = append(ranges, [2]int{index, end})
		index = end - 1
	}
	return declarations, ranges, nil
}
