package parser

import (
	"slices"

	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

type Parser struct {
	pos    int
	tokens []tokeniser.Token
}

func NewParser(tokens []tokeniser.Token) *Parser {
	return &Parser{
		pos:    0,
		tokens: tokens,
	}
}

func (p *Parser) Peek() tokeniser.Token {
	if p.pos >= len(p.tokens) || p.pos < 0 {
		return tokeniser.Token{Type: tokeniser.TOKEN_TYPE_EOF}
	}

	return p.tokens[p.pos]
}

func (p *Parser) Next() tokeniser.Token {
	pos := p.pos + 1

	if pos >= len(p.tokens) || pos < 0 {
		return tokeniser.Token{Type: tokeniser.TOKEN_TYPE_EOF}
	}

	return p.tokens[pos]
}

func (p *Parser) Inc() *Parser {
	p.pos++

	return p
}

func (p *Parser) Dec() *Parser {
	p.pos--

	return p
}

func (p *Parser) PrevLoc() shared.Location {
	l := p.Dec().Peek().Loc
	p.Inc()
	return l
}

func (p *Parser) CurrLoc() shared.Location {
	return p.Peek().Loc
}

func (p *Parser) Consume() tokeniser.Token {
	c := p.Peek()
	p.Inc()

	return c
}

func (p *Parser) Expect(expected tokeniser.TokenType) bool {
	tok := p.Consume()

	ret := tok.Type == expected

	return ret
}

func (p *Parser) ExpectGet(expected tokeniser.TokenType) (*tokeniser.Token, bool) {
	tok := p.Consume()

	if tok.Type != expected {
		return nil, false
	}

	return &tok, true
}

func (p *Parser) Match(ttypes ...tokeniser.TokenType) bool {
	ptype := p.Peek().Type

	return slices.Contains(ttypes, ptype)
}

func (p *Parser) Parse() (*RootNode, error) {
	eofTok := p.tokens[len(p.tokens)-1]
	rootNode := &RootNode{
		Loc: shared.Location{
			LC: shared.LineCol{
				Line: 1,
				Col:  1,
			},
			FilePath: eofTok.Loc.FilePath,
		},
	}
	for !p.Match(tokeniser.TOKEN_TYPE_EOF) {
		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE, tokeniser.TOKEN_TYPE_SEMICOLON) {
			p.Inc()
		}
		e := shared.NewError(p.CurrLoc(), "expected function definition, struct definition or import statement")
		if !p.Match(tokeniser.TOKEN_TYPE_KEYWORD) {
			return nil, e
		}

		switch p.Peek().Value {
		case string(tokeniser.KEYWORD_PUB):
			p.Inc()
			if p.Peek().Type != tokeniser.TOKEN_TYPE_KEYWORD {
				return nil, e
			}
			switch p.Peek().Value {
			case
				string(tokeniser.KEYWORD_LET),
				string(tokeniser.KEYWORD_EXTERN):
				fnDef, err := p.ParseFunctionDefinition()
				if err != nil {
					return nil, err
				}
				fnDef.Pub = true
				rootNode.Body = append(rootNode.Body, fnDef)
			case string(tokeniser.KEYWORD_STRUCT):
				str, err := p.ParseStructDefinition()
				if err != nil {
					return nil, err
				}
				str.Pub = true
				rootNode.Body = append(rootNode.Body, str)
			default:
				return nil, e
			}
		case
			string(tokeniser.KEYWORD_LET),
			string(tokeniser.KEYWORD_EXTERN):
			fnDef, err := p.ParseFunctionDefinition()
			if err != nil {
				return nil, err
			}
			rootNode.Body = append(rootNode.Body, fnDef)
		case string(tokeniser.KEYWORD_STRUCT):
			str, err := p.ParseStructDefinition()
			if err != nil {
				return nil, err
			}
			rootNode.Body = append(rootNode.Body, str)
		case string(tokeniser.KEYWORD_IMPORT):
			imp, err := p.ParseImport()
			if err != nil {
				return nil, err
			}
			rootNode.Body = append(rootNode.Body, imp)
		case string(tokeniser.KEYWORD_MODULE):
			mod, err := p.ParseModule()
			if err != nil {
				return nil, err
			}
			rootNode.Body = append(rootNode.Body, mod)
		default:
			return nil, e
		}

		if p.Match(tokeniser.TOKEN_TYPE_EOF) {
			break
		}

		if !p.Match(tokeniser.TOKEN_TYPE_NEWLINE, tokeniser.TOKEN_TYPE_SEMICOLON) {
			return nil, shared.NewError(p.CurrLoc(), "expected ';' or '\\n'")
		}

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE, tokeniser.TOKEN_TYPE_SEMICOLON) {
			p.Inc()
		}
	}

	return rootNode, nil
}
