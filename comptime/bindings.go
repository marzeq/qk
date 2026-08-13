package comptime

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

// BindingsFingerprint identifies every resolved value that can affect a when
// expansion, including non-exported bindings that do not appear in a semantic
// module interface.
func BindingsFingerprint(bindings map[string]Value) string {
	keys := make([]string, 0, len(bindings))
	for key := range bindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	for _, key := range keys {
		value := bindings[key]
		hash.Write([]byte(key))
		hash.Write([]byte{0, byte(value.kind)})
		switch value.kind {
		case valueBool:
			if value.boolean {
				hash.Write([]byte{1})
			} else {
				hash.Write([]byte{0})
			}
		case valueInteger:
			hash.Write([]byte(value.integer.String()))
		}
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// PackageBindingsFingerprint identifies only the module-level values visible
// while expanding one package: its own declarations and declarations from
// directly imported packages. Unrelated application bindings must not
// invalidate reusable dependency or standard-library modules.
func PackageBindingsFingerprint(bindings map[string]Value, packagePath string, imports []string) string {
	visible := make(map[string]Value)
	addPackage := func(path string) {
		prefix := path + "."
		for key, value := range bindings {
			name, ok := strings.CutPrefix(key, prefix)
			if ok && !strings.Contains(name, ".") {
				visible[key] = value
			}
		}
	}
	addPackage(packagePath)
	for _, imported := range imports {
		addPackage(imported)
	}
	return BindingsFingerprint(visible)
}

type moduleBindingDeclaration struct {
	module string
	name   string
	expr   parser.ExpressionNode
	loc    shared.Location
	scope  *moduleBindingScope
}

type moduleBindingScope struct {
	imports map[string]string
	aliases map[string]string
}

// ResolveModuleBindings resolves file-scope compile-time declarations in
// their canonical module namespaces before individual files are expanded.
func ResolveModuleBindings(sources map[string]string, config Config) (map[string]Value, error) {
	return ResolvePackageBindings(sources, nil, config)
}

// ResolvePackageBindings resolves module-level compile-time bindings under
// canonical package paths rather than local module declarations.
func ResolvePackageBindings(sources map[string]string, packagePaths map[string]string, config Config) (map[string]Value, error) {
	return ResolvePackageBindingsWithImports(sources, packagePaths, nil, config)
}

// ResolvePackageBindingsWithImports resolves compile-time bindings while
// preserving the source-visible import paths used by project source mounts.
func ResolvePackageBindingsWithImports(sources map[string]string, packagePaths map[string]string, importResolutions map[string]map[string]string, config Config) (map[string]Value, error) {
	origins := make([]string, 0, len(sources))
	for origin := range sources {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	declarations := map[string]moduleBindingDeclaration{}
	moduleScopes := make(map[string]*moduleBindingScope)
	for _, origin := range origins {
		tokens, err := tokeniser.NewTokeniser(sources[origin], origin).Tokenise()
		if err != nil {
			return nil, err
		}
		module := packagePaths[origin]
		if module == "" {
			module = tokenModule(tokens)
		}
		scope := moduleScopes[module]
		if scope == nil {
			scope = &moduleBindingScope{imports: make(map[string]string), aliases: make(map[string]string)}
			moduleScopes[module] = scope
		}
		if header, scanErr := parser.ScanSourceHeader(tokens); scanErr == nil {
			for i, imported := range header.Imports {
				resolved := imported
				if target := importResolutions[module][imported]; target != "" {
					resolved = target
				}
				scope.imports[imported] = resolved
				if i < len(header.Aliases) && header.Aliases[i] != "" {
					scope.aliases[header.Aliases[i]] = resolved
				}
			}
		}
		for _, declaration := range topLevelCompileTimeDeclarations(tokens, module) {
			key := module + "." + declaration.name
			if previous, exists := declarations[key]; exists {
				return nil, shared.NewError(declaration.loc, "duplicate compile-time binding %q; previously declared at %s", key, previous.loc)
			}
			declaration.scope = scope
			declarations[key] = declaration
		}
	}
	values := make(map[string]Value, len(declarations))
	target := targetFromConfig(config)
	target.releaseMode = config.ReleaseMode
	state := map[string]uint8{}
	var resolve func(string, shared.Location) (Value, error)
	resolve = func(key string, loc shared.Location) (Value, error) {
		declaration, exists := declarations[key]
		if !exists {
			return Value{}, shared.NewError(loc, "unknown compile-time value %q", key)
		}
		if state[key] == 1 {
			return Value{}, shared.NewError(loc, "cyclic compile-time binding involving %q", key)
		}
		if state[key] == 2 {
			return values[key], nil
		}
		state[key] = 1
		value, err := evaluate(declaration.expr, target, func(name string, refLoc shared.Location) (Value, error) {
			if !strings.Contains(name, ".") {
				name = declaration.module + "." + name
			} else {
				resolved := ""
				if strings.HasPrefix(name, declaration.module+".") && !strings.Contains(strings.TrimPrefix(name, declaration.module+"."), ".") {
					resolved = name
				}
				for imported, target := range declaration.scope.imports {
					if strings.HasPrefix(name, imported+".") && !strings.Contains(strings.TrimPrefix(name, imported+"."), ".") {
						resolved = target + strings.TrimPrefix(name, imported)
						break
					}
				}
				if resolved == "" {
					if qualifier, binding, ok := strings.Cut(name, "."); ok {
						if imported := declaration.scope.aliases[qualifier]; imported != "" && !strings.Contains(binding, ".") {
							resolved = imported + "." + binding
						}
					}
				}
				if resolved == "" {
					return Value{}, shared.NewError(refLoc, "compile-time value %q is not in scope", name)
				}
				name = resolved
			}
			return resolve(name, refLoc)
		})
		if err != nil {
			return Value{}, err
		}
		if value.kind != valueBool && value.kind != valueInteger {
			return Value{}, shared.NewError(declaration.loc, "compile-time binding %q must resolve to a boolean or integer", key)
		}
		values[key], state[key] = value, 2
		return value, nil
	}
	keys := make([]string, 0, len(declarations))
	for key := range declarations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := resolve(key, declarations[key].loc); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func tokenModule(tokens []tokeniser.Token) string {
	for index, tok := range tokens {
		if tok.Type != tokeniser.TokenKeyword || tok.Value != string(tokeniser.KeywordModule) {
			continue
		}
		var parts []string
		for index++; index < len(tokens) && tokens[index].Type != tokeniser.TokenNewline && tokens[index].Type != tokeniser.TokenEof; index++ {
			if tokens[index].Type == tokeniser.TokenIdentifier {
				parts = append(parts, tokens[index].Value)
			}
		}
		return strings.Join(parts, ".")
	}
	return ""
}

func topLevelCompileTimeDeclarations(tokens []tokeniser.Token, module string) []moduleBindingDeclaration {
	var declarations []moduleBindingDeclaration
	depth := 0
	for index := range tokens {
		switch tokens[index].Type {
		case tokeniser.TokenOpenCurly:
			depth++
		case tokeniser.TokenCloseCurly:
			depth--
		}
		if depth != 0 || tokens[index].Type != tokeniser.TokenKeyword || tokens[index].Value != string(tokeniser.KeywordLet) {
			continue
		}
		namePos := index + 1
		if namePos < len(tokens) && tokens[namePos].Type == tokeniser.TokenKeyword && tokens[namePos].Value == string(tokeniser.KeywordMut) {
			namePos++
		}
		if namePos >= len(tokens) || tokens[namePos].Type != tokeniser.TokenIdentifier {
			continue
		}
		equals := namePos + 1
		for equals < len(tokens) && tokens[equals].Type != tokeniser.TokenEquals && tokens[equals].Type != tokeniser.TokenNewline {
			if tokens[equals].Type == tokeniser.TokenLess {
				equals = len(tokens)
				break
			}
			equals++
		}
		if equals >= len(tokens) {
			continue
		}
		if equals+1 >= len(tokens) || tokens[equals+1].Type != tokeniser.TokenIdentifier || tokens[equals+1].Value != "comptime" {
			continue
		}
		end, parens, squares := equals+2, 0, 0
		for end < len(tokens) {
			tok := tokens[end]
			switch tok.Type {
			case tokeniser.TokenOpenParen:
				parens++
			case tokeniser.TokenCloseParen:
				parens--
			case tokeniser.TokenOpenSquare:
				squares++
			case tokeniser.TokenCloseSquare:
				squares--
			}
			if parens == 0 && squares == 0 && (tok.Type == tokeniser.TokenNewline || tok.Type == tokeniser.TokenSemicolon || tok.Type == tokeniser.TokenEof) {
				break
			}
			end++
		}
		if end == equals+2 {
			continue
		}
		expr, err := parseCondition(tokens[equals+2:end], tokens[namePos].Loc)
		if err != nil {
			continue
		}
		declarations = append(declarations, moduleBindingDeclaration{module: module, name: tokens[namePos].Value, expr: expr, loc: tokens[namePos].Loc})
	}
	return declarations
}

func literalToken(value Value, loc shared.Location) (tokeniser.Token, error) {
	switch value.kind {
	case valueBool:
		text := string(tokeniser.KeywordFalse)
		if value.boolean {
			text = string(tokeniser.KeywordTrue)
		}
		return tokeniser.Token{Type: tokeniser.TokenKeyword, Value: text, Loc: loc}, nil
	case valueInteger:
		return tokeniser.Token{Type: tokeniser.TokenNumber, Value: value.integer.String(), Loc: loc}, nil
	default:
		return tokeniser.Token{}, shared.NewError(loc, "compile-time target values may only be bound after converting them to a boolean or integer")
	}
}
