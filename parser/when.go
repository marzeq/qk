package parser

import (
	"errors"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

func (p *Parser) parseLinkItems(close tokeniser.TokenKind) ([]LinkItemNode, bool, error) {
	var items []LinkItemNode
	conditional := false
	for {
		for p.Match(tokeniser.TokenNewline, tokeniser.TokenComma) {
			p.Inc()
		}
		if p.Match(close) {
			return items, conditional, nil
		}
		if p.Match(tokeniser.TokenEof) {
			return nil, false, shared.NewError(p.CurrLoc(), "expected closing delimiter in @link")
		}
		if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordWhen) {
			when, err := p.parseLinkWhen()
			if err != nil {
				return nil, false, err
			}
			items = append(items, LinkItemNode{When: when})
			conditional = true
			continue
		}
		if p.MatchBuiltin("compiler_error") {
			directive, err := p.parseCompilerDirective()
			if err != nil {
				return nil, false, err
			}
			items = append(items, LinkItemNode{Directive: directive})
			conditional = true
			continue
		}
		kindToken, ok := p.ExpectGet(tokeniser.TokenIdentifier)
		if !ok {
			return nil, false, shared.NewError(p.PrevLoc(), "expected complete link entry or 'when' in @link")
		}
		var kind attributes.LinkKind
		switch kindToken.Value {
		case "system":
			kind = attributes.LinkSystem
		case "path":
			kind = attributes.LinkPath
		case "search":
			kind = attributes.LinkSearchPath
		case "framework":
			kind = attributes.LinkFramework
		default:
			return nil, false, shared.NewError(kindToken.Loc, "unknown @link entry kind %q", kindToken.Value)
		}
		value, ok := p.ExpectGet(tokeniser.TokenString)
		if !ok {
			return nil, false, shared.NewError(p.PrevLoc(), "expected string after %s in @link", kindToken.Value)
		}
		if value.Value == "" {
			return nil, false, shared.NewError(value.Loc, "@link values cannot be empty")
		}
		link := attributes.Link{Kind: kind, Value: value.Value}
		items = append(items, LinkItemNode{Link: &link})
	}
}

func (p *Parser) parseLinkWhen() (*LinkWhenNode, error) {
	begin := p.CurrLoc()
	node := &LinkWhenNode{}
	for {
		if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordWhen) {
			return nil, shared.NewError(p.CurrLoc(), "expected 'when' in @link")
		}
		p.Inc()
		condition, err := p.parseWhenCondition()
		if err != nil {
			return nil, err
		}
		if !p.Expect(tokeniser.TokenOpenCurly) {
			return nil, shared.NewError(p.PrevLoc(), "expected '{' after @link condition")
		}
		items, _, err := p.parseLinkItems(tokeniser.TokenCloseCurly)
		if err != nil {
			return nil, err
		}
		p.Inc()
		node.Branches = append(node.Branches, LinkWhenBranchNode{Condition: condition, Items: items})
		branchEnd := p.pos
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordElse) {
			p.pos = branchEnd
			break
		}
		p.Inc()
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordWhen) {
			continue
		}
		if !p.Expect(tokeniser.TokenOpenCurly) {
			return nil, shared.NewError(p.PrevLoc(), "expected '{' after else in @link")
		}
		node.ElseItems, _, err = p.parseLinkItems(tokeniser.TokenCloseCurly)
		if err != nil {
			return nil, err
		}
		p.Inc()
		break
	}
	node.Loc = p.SpanFrom(begin)
	return node, nil
}

func (p *Parser) parseWhen(context WhenContext) (*WhenNode, error) {
	begin := p.CurrLoc()
	node := &WhenNode{Context: context}
	for {
		if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordWhen) {
			return nil, shared.NewError(p.CurrLoc(), "expected 'when'")
		}
		branchBegin := p.CurrLoc()
		p.Inc()
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		condition, err := p.parseWhenCondition()
		if err != nil {
			return nil, err
		}
		body, value, err := p.parseWhenBody(context)
		if err != nil {
			return nil, err
		}
		node.Branches = append(node.Branches, WhenBranchNode{
			Condition: condition, Body: body, Value: value, Loc: branchBegin.WithEnd(p.PrevLoc()),
		})
		branchEnd := p.pos
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordElse) {
			p.pos = branchEnd
			break
		}
		p.Inc()
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordWhen) {
			continue
		}
		body, value, err = p.parseWhenBody(context)
		if err != nil {
			return nil, err
		}
		node.ElseBody, node.ElseValue = body, value
		break
	}
	node.Loc = p.SpanFrom(begin)
	return node, nil
}

func (p *Parser) parseWhenCondition() (ExpressionNode, error) {
	start := p.pos
	parenDepth, squareDepth := 0, 0
	for !p.Match(tokeniser.TokenEof) {
		switch p.Peek().Type {
		case tokeniser.TokenOpenParen:
			parenDepth++
		case tokeniser.TokenCloseParen:
			parenDepth--
		case tokeniser.TokenOpenSquare:
			squareDepth++
		case tokeniser.TokenCloseSquare:
			squareDepth--
		case tokeniser.TokenOpenCurly:
			if parenDepth == 0 && squareDepth == 0 {
				tokens := make([]tokeniser.Token, 0, p.pos-start+1)
				for _, token := range p.tokens[start:p.pos] {
					if token.Type != tokeniser.TokenNewline {
						tokens = append(tokens, token)
					}
				}
				if len(tokens) == 0 {
					return nil, shared.NewError(p.CurrLoc(), "expected condition after 'when'")
				}
				tokens = append(tokens, tokeniser.Token{Type: tokeniser.TokenEof, Loc: tokens[len(tokens)-1].Loc})
				return NewParser(tokens).ParseWholeExpression()
			}
		}
		p.Inc()
	}
	return nil, shared.NewError(p.CurrLoc(), "expected '{' after compile-time condition")
}

func (p *Parser) parseWhenBody(context WhenContext) ([]Node, Node, error) {
	switch context {
	case WhenDeclarations:
		body, err := p.parseWhenDeclarationBody()
		return body, nil, err
	case WhenStatements:
		block, err := p.ParseBlock()
		if err != nil {
			return nil, nil, err
		}
		return block.Body, nil, nil
	case WhenExpression:
		block, err := p.ParseBlockExpression()
		return nil, block, err
	case WhenType:
		if !p.Expect(tokeniser.TokenOpenCurly) {
			return nil, nil, shared.NewError(p.PrevLoc(), "expected '{' after compile-time condition")
		}
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.MatchBuiltin("compiler_error") || p.MatchBuiltin("compiler_assert") {
			directive, err := p.parseCompilerDirective()
			if err != nil {
				return nil, nil, err
			}
			for p.Match(tokeniser.TokenNewline, tokeniser.TokenSemicolon) {
				p.Inc()
			}
			if !p.Expect(tokeniser.TokenCloseCurly) {
				return nil, nil, shared.NewError(p.PrevLoc(), "expected '}' after compiler directive")
			}
			return nil, directive, nil
		}
		typeNode, err := p.ParseType()
		if err != nil {
			return nil, nil, err
		}
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if !p.Expect(tokeniser.TokenCloseCurly) {
			return nil, nil, shared.NewError(p.PrevLoc(), "expected '}' after selected type")
		}
		return nil, typeNode, nil
	default:
		panic("unknown when context")
	}
}

func (p *Parser) parseWhenDeclarationBody() ([]Node, error) {
	if !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' after compile-time condition")
	}
	var body []Node
	var parseErrors []error
	for !p.Match(tokeniser.TokenCloseCurly, tokeniser.TokenEof) {
		for p.Match(tokeniser.TokenNewline, tokeniser.TokenSemicolon) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenCloseCurly) {
			break
		}
		node, err := p.parseTopLevel()
		if err != nil {
			parseErrors = append(parseErrors, err)
			p.synchroniseTopLevel()
			continue
		}
		body = append(body, node)
		if !p.Match(tokeniser.TokenCloseCurly, tokeniser.TokenNewline, tokeniser.TokenSemicolon) {
			parseErrors = append(parseErrors, shared.NewError(p.CurrLoc(), "expected declaration separator in when block"))
			p.synchroniseTopLevel()
		}
	}
	if !p.Expect(tokeniser.TokenCloseCurly) {
		parseErrors = append(parseErrors, shared.NewError(p.PrevLoc(), "expected '}' after when declarations"))
	}
	return body, errors.Join(parseErrors...)
}

func (p *Parser) parseCompilerDirective() (*CompilerDirectiveNode, error) {
	begin := p.CurrLoc()
	name := "compiler_error"
	if p.MatchBuiltin("compiler_assert") {
		name = "compiler_assert"
	}
	if !p.ConsumeBuiltin(name) || !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '(' after @%s", name)
	}
	nameEnd := p.tokens[p.pos-2].Loc
	node := &CompilerDirectiveNode{Name: name, NameLoc: begin.WithEnd(nameEnd)}
	if name == "compiler_assert" {
		condition, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}
		node.Condition = condition
		if !p.Expect(tokeniser.TokenComma) {
			return nil, shared.NewError(p.PrevLoc(), "expected ',' after @compiler_assert condition")
		}
	}
	message, ok := p.ExpectGet(tokeniser.TokenString)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected string message in @%s", name)
	}
	node.Message = message.Value
	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')' after @%s", name)
	}
	node.Loc = p.SpanFrom(begin)
	return node, nil
}
