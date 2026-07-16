package parser

import (
	"errors"
	"fmt"
	"slices"

	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

type Parser struct {
	pos      int
	tokens   []tokeniser.Token
	posStack []int
	// disambiguateTrailingBlock marks expressions followed by a block, such as
	// if conditions and for iterables. In that context a brace after an
	// identifier is a struct literal only when it starts with field syntax.
	disambiguateTrailingBlock bool
}

func (p *Parser) trailingBraceStartsStructLiteral() bool {
	if !p.Match(tokeniser.TokenOpenCurly) {
		return false
	}

	pos := p.pos + 1
	for pos < len(p.tokens) && p.tokens[pos].Type == tokeniser.TokenNewline {
		pos++
	}
	if pos >= len(p.tokens) {
		return false
	}
	if p.tokens[pos].Type != tokeniser.TokenCloseCurly {
		if p.tokens[pos].Type != tokeniser.TokenIdentifier ||
			pos+1 >= len(p.tokens) || p.tokens[pos+1].Type != tokeniser.TokenEquals {
			return false
		}
	}

	depth := 0
	for pos = p.pos; pos < len(p.tokens); pos++ {
		switch p.tokens[pos].Type {
		case tokeniser.TokenOpenCurly:
			depth++
		case tokeniser.TokenCloseCurly:
			depth--
		}
		if depth == 0 {
			pos++
			break
		}
	}
	if depth != 0 {
		return false
	}

	for pos < len(p.tokens) && p.tokens[pos].Type == tokeniser.TokenNewline {
		pos++
	}
	if pos >= len(p.tokens) {
		return false
	}

	next := p.tokens[pos]
	switch next.Type {
	case tokeniser.TokenOpenCurly, tokeniser.TokenOpenSquare, tokeniser.TokenDot,
		tokeniser.TokenEqualsEquals, tokeniser.TokenNotEquals,
		tokeniser.TokenLess, tokeniser.TokenLessEquals,
		tokeniser.TokenGreater, tokeniser.TokenGreaterEquals,
		tokeniser.TokenPlus, tokeniser.TokenMinus, tokeniser.TokenAsterisk,
		tokeniser.TokenSlash, tokeniser.TokenPercent, tokeniser.TokenAmpersand,
		tokeniser.TokenPipe, tokeniser.TokenCaret, tokeniser.TokenShiftLeft, tokeniser.TokenShiftRight:
		return true
	case tokeniser.TokenKeyword:
		return next.Value == string(tokeniser.KeywordAnd) ||
			next.Value == string(tokeniser.KeywordOr)
	default:
		return false
	}
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
	e := shared.NewError(p.CurrLoc(), "expected function definition, constant definition, type alias or import statement")
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
	default:
		return nil, e
	}
	if err != nil {
		return nil, err
	}

	switch n := node.(type) {
	case *FunctionDefNode:
		n.Pub = isPublic
	case *DeclarationNode:
		n.Pub = isPublic
	case *TypeAliasNode:
		n.Pub = isPublic
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
