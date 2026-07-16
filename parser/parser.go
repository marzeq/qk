package parser

import (
	"fmt"
	"slices"

	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

type Parser struct {
	pos                    int
	tokens                 []tokeniser.Token
	posStack               []int
	parsingForEachIterable bool
}

func NewParser(tokens []tokeniser.Token) *Parser {
	return &Parser{
		pos:    0,
		tokens: tokens,
	}
}

func (p *Parser) PushPos() {
	p.posStack = append(p.posStack, p.pos)
}

func (p *Parser) PopPos() {
	if len(p.posStack) == 0 {
		return
	}

	last := len(p.posStack) - 1
	p.pos = p.posStack[last]
	p.posStack = p.posStack[:last]
}

func (p *Parser) CommitPos() {
	if len(p.posStack) == 0 {
		return
	}

	p.posStack = p.posStack[:len(p.posStack)-1]
}

func (p *Parser) Peek() tokeniser.Token {
	if p.pos >= len(p.tokens) || p.pos < 0 {
		return tokeniser.Token{Type: tokeniser.TokenEof}
	}

	return p.tokens[p.pos]
}

func (p *Parser) Next() tokeniser.Token {
	pos := p.pos + 1

	if pos >= len(p.tokens) || pos < 0 {
		return tokeniser.Token{Type: tokeniser.TokenEof}
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

func (p *Parser) Expect(expected tokeniser.TokenKind) bool {
	tok := p.Consume()

	ret := tok.Type == expected

	return ret
}

func (p *Parser) ExpectGet(expected tokeniser.TokenKind) (*tokeniser.Token, bool) {
	tok := p.Consume()

	if tok.Type != expected {
		return nil, false
	}

	return &tok, true
}

func (p *Parser) Match(ttypes ...tokeniser.TokenKind) bool {
	ptype := p.Peek().Type

	return slices.Contains(ttypes, ptype)
}

func (p *Parser) DumpCurrent() {
	fmt.Printf("Current token: %v\n", p.Peek())
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
	for !p.Match(tokeniser.TokenEof) {
		for p.Match(tokeniser.TokenNewline, tokeniser.TokenSemicolon) {
			p.Inc()
		}
		e := shared.NewError(p.CurrLoc(), "expected function definition, constant definition, type alias or import statement")
		if !p.Match(tokeniser.TokenKeyword) {
			return nil, e
		}

		switch p.Peek().Value {
		case string(tokeniser.KeywordPub):
			p.Inc()
			if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordLet) && p.Peek().Value != string(tokeniser.KeywordExtern) {
				return nil, e
			}
			stmt, _, err := p.ParseStatement()
			if err != nil {
				return nil, err
			}

			switch s := stmt.(type) {
			case *FunctionDefNode:
				s.Pub = true
			case *DeclarationNode:
				s.Pub = true
			case *TypeAliasNode:
				s.Pub = true
			default:
				return nil, e
			}

			rootNode.Body = append(rootNode.Body, stmt)
		case string(tokeniser.KeywordLet):
			stmt, _, err := p.ParseStatement()
			if err != nil {
				return nil, err
			}
			switch stmt.(type) {
			case *FunctionDefNode, *DeclarationNode, *TypeAliasNode:
				rootNode.Body = append(rootNode.Body, stmt)
			default:
				return nil, e
			}
		case string(tokeniser.KeywordExtern):
			stmt, _, err := p.ParseStatement()
			if err != nil {
				return nil, err
			}
			rootNode.Body = append(rootNode.Body, stmt)

		case string(tokeniser.KeywordImport):
			imp, err := p.ParseImport()
			if err != nil {
				return nil, err
			}
			rootNode.Body = append(rootNode.Body, imp)
		case string(tokeniser.KeywordModule):
			mod, err := p.ParseModule()
			if err != nil {
				return nil, err
			}
			rootNode.Body = append(rootNode.Body, mod)
		default:
			return nil, e
		}

		if p.Match(tokeniser.TokenEof) {
			break
		}

		if !p.Match(tokeniser.TokenNewline, tokeniser.TokenSemicolon) {
			return nil, shared.NewError(p.CurrLoc(), "expected ';' or '\\n'")
		}

		for p.Match(tokeniser.TokenNewline, tokeniser.TokenSemicolon) {
			p.Inc()
		}
	}

	return rootNode, nil
}
