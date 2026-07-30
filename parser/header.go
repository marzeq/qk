package parser

import (
	"strings"

	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

// SourceHeader contains the dependency information that can be read before
// compile-time expansion. Imports are collected lexically, so imports inside a
// compile-time branch are available while resolving that branch.
type SourceHeader struct {
	Module  string
	Imports []string
	Aliases []string
}

func ScanSourceHeader(tokens []tokeniser.Token) (SourceHeader, error) {
	var header SourceHeader
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.Type != tokeniser.TokenKeyword {
			continue
		}
		switch tok.Value {
		case string(tokeniser.KeywordModule):
			if header.Module != "" {
				continue
			}
			name, _, err := scanHeaderModulePath(tokens, i+1)
			if err != nil {
				return SourceHeader{}, err
			}
			header.Module = name
		case string(tokeniser.KeywordImport):
			imports, aliases, end, err := scanHeaderImport(tokens, i+1)
			if err != nil {
				return SourceHeader{}, err
			}
			header.Imports = append(header.Imports, imports...)
			header.Aliases = append(header.Aliases, aliases...)
			i = end - 1
		}
	}
	return header, nil
}

func scanHeaderModulePath(tokens []tokeniser.Token, start int) (string, int, error) {
	if start >= len(tokens) || tokens[start].Type != tokeniser.TokenIdentifier {
		loc := shared.Location{}
		if start < len(tokens) {
			loc = tokens[start].Loc
		}
		return "", start, shared.NewError(loc, "expected module name")
	}
	parts := []string{tokens[start].Value}
	i := start + 1
	for i < len(tokens) && tokens[i].Type == tokeniser.TokenDot {
		i++
		if i >= len(tokens) || tokens[i].Type != tokeniser.TokenIdentifier {
			return "", i, shared.NewError(tokens[i-1].Loc, "expected module name after '.'")
		}
		parts = append(parts, tokens[i].Value)
		i++
	}
	return strings.Join(parts, "."), i, nil
}

func scanHeaderImport(tokens []tokeniser.Token, start int) ([]string, []string, int, error) {
	i := start
	for i < len(tokens) && tokens[i].Type == tokeniser.TokenNewline {
		i++
	}
	grouped := i < len(tokens) && tokens[i].Type == tokeniser.TokenOpenParen
	if grouped {
		i++
	}
	var imports, aliases []string
	for i < len(tokens) {
		for i < len(tokens) && (tokens[i].Type == tokeniser.TokenNewline || tokens[i].Type == tokeniser.TokenComma) {
			i++
		}
		if grouped && i < len(tokens) && tokens[i].Type == tokeniser.TokenCloseParen {
			return imports, aliases, i + 1, nil
		}
		name, end, err := scanHeaderModulePath(tokens, i)
		if err != nil {
			return nil, nil, i, err
		}
		i = end
		alias := ""
		if i < len(tokens) && tokens[i].Type == tokeniser.TokenIdentifier {
			alias = tokens[i].Value
			i++
		}
		imports = append(imports, name)
		aliases = append(aliases, alias)
		if !grouped {
			return imports, aliases, i, nil
		}
		for i < len(tokens) && tokens[i].Type != tokeniser.TokenComma && tokens[i].Type != tokeniser.TokenNewline && tokens[i].Type != tokeniser.TokenCloseParen {
			i++
		}
	}
	return imports, aliases, i, nil
}
