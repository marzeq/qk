package parser

import (
	"errors"
	"fmt"
	"slices"

	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

type Parser struct {
	pos              int
	tokens           []tokeniser.Token
	posStack         []int
	allowTypeCapture bool
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

func (p *Parser) SpanFrom(start shared.Location) shared.Location {
	if p.pos == 0 {
		return start
	}
	return start.WithEnd(p.tokens[p.pos-1].Loc)
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

func (p *Parser) MatchAdjacentPair(ttype tokeniser.TokenKind) bool {
	current, next := p.Peek(), p.Next()
	return current.Type == ttype && next.Type == ttype && current.Loc.EndOffset == next.Loc.Offset
}

func (p *Parser) MatchBuiltin(name string) bool {
	return p.Match(tokeniser.TokenAt) && p.Next().Type == tokeniser.TokenIdentifier && p.Next().Value == name
}

func (p *Parser) ConsumeBuiltin(name string) bool {
	if !p.MatchBuiltin(name) {
		return false
	}
	p.Inc().Inc()
	return true
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
			FilePath:   eofTok.Loc.FilePath,
			SourceText: eofTok.Loc.SourceText,
			EndLC:      eofTok.Loc.LC,
			EndOffset:  eofTok.Loc.Offset,
		},
	}
	var parseErrors []error
	for !p.Match(tokeniser.TokenEof) {
		for p.Match(tokeniser.TokenNewline, tokeniser.TokenSemicolon) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenEof) {
			break
		}

		stmt, err := p.parseTopLevel()
		if err != nil {
			parseErrors = append(parseErrors, err)
			p.synchroniseTopLevel()
			continue
		}
		rootNode.Body = append(rootNode.Body, stmt)

		if p.Match(tokeniser.TokenEof) {
			break
		}

		if !p.Match(tokeniser.TokenNewline, tokeniser.TokenSemicolon) {
			parseErrors = append(parseErrors, shared.NewError(p.CurrLoc(), "expected ';' or '\\n'"))
			p.synchroniseTopLevel()
			continue
		}

		for p.Match(tokeniser.TokenNewline, tokeniser.TokenSemicolon) {
			p.Inc()
		}
	}

	return rootNode, errors.Join(parseErrors...)
}

func (p *Parser) parseTopLevel() (Node, error) {
	startLoc := p.CurrLoc()
	e := shared.NewError(startLoc, "expected function definition, constant definition, type alias or import statement")
	if p.MatchBuiltin("compiler_error") || p.MatchBuiltin("compiler_assert") {
		return p.parseCompilerDirective()
	}
	if !p.Match(tokeniser.TokenKeyword) {
		return nil, e
	}

	isPublic := p.Peek().Value == string(tokeniser.KeywordPub)
	if isPublic {
		p.Inc()
		if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordLet) {
			return nil, e
		}
	}

	var node Node
	var err error
	switch p.Peek().Value {
	case string(tokeniser.KeywordLet):
		node, _, err = p.ParseStatement()
	case string(tokeniser.KeywordImport):
		node, err = p.ParseImport()
	case string(tokeniser.KeywordModule):
		node, err = p.ParseModule()
	case string(tokeniser.KeywordWhen):
		node, err = p.parseWhen(WhenDeclarations)
	default:
		return nil, e
	}
	if err != nil {
		return nil, err
	}

	switch n := node.(type) {
	case *FunctionDefNode:
		n.Pub = isPublic
		if isPublic {
			n.Loc = startLoc.WithEnd(n.Loc)
		}
	case *DeclarationNode:
		n.Pub = isPublic
		if isPublic {
			n.Loc = startLoc.WithEnd(n.Loc)
		}
	case *TypeAliasNode:
		n.Pub = isPublic
		if isPublic {
			n.Loc = startLoc.WithEnd(n.Loc)
		}
	default:
		if isPublic {
			return nil, e
		}
	}
	return node, nil
}

func isTopLevelStart(tok tokeniser.Token) bool {
	if tok.Type != tokeniser.TokenKeyword {
		return false
	}
	switch tok.Value {
	case string(tokeniser.KeywordPub), string(tokeniser.KeywordLet),
		string(tokeniser.KeywordImport), string(tokeniser.KeywordModule):
		return true
	default:
		return false
	}
}

func (p *Parser) synchroniseTopLevel() {
	// Failed speculative parses must not affect the next declaration.
	p.posStack = nil
	depth := 0
	atBoundary := p.pos == 0 || p.tokens[p.pos-1].Type == tokeniser.TokenNewline || p.tokens[p.pos-1].Type == tokeniser.TokenSemicolon
	for !p.Match(tokeniser.TokenEof) {
		if depth == 0 && atBoundary && isTopLevelStart(p.Peek()) {
			return
		}
		tok := p.Consume()
		switch tok.Type {
		case tokeniser.TokenOpenCurly:
			depth++
		case tokeniser.TokenCloseCurly:
			if depth > 0 {
				depth--
			}
		}
		atBoundary = tok.Type == tokeniser.TokenNewline || tok.Type == tokeniser.TokenSemicolon
	}
}
