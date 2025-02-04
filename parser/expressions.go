package parser

import (
	"github.com/marzeq/quokka/shared"
	"github.com/marzeq/quokka/tokeniser"
)

func (p *Parser) ParseExpression() (*Node, error) {
	return p.ParseLogicalOr()
}

func (p *Parser) ParseLogicalOr() (*Node, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseLogicalAnd()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "or" {
		op := p.Consume()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		right, err := p.ParseLogicalAnd()
		if err != nil {
			return nil, err
		}
		left = &Node{
			Type:  NODE_TYPE_BINARY_OP,
			Value: op.Value,
			Left:  left,
			Right: right,
			Loc:   beginLoc,
		}
	}

	return left, nil
}

func (p *Parser) ParseLogicalAnd() (*Node, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseLogicalNot()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "and" {
		op := p.Consume()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		right, err := p.ParseLogicalNot()
		if err != nil {
			return nil, err
		}
		left = &Node{
			Type:  NODE_TYPE_BINARY_OP,
			Value: op.Value,
			Left:  left,
			Right: right,
			Loc:   beginLoc,
		}
	}

	return left, nil
}

func (p *Parser) ParseLogicalNot() (*Node, error) {
	beginLoc := p.CurrLoc()
	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "not" {
		op := p.Consume()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		expr, err := p.ParseLogicalNot()
		if err != nil {
			return nil, err
		}

		return &Node{
			Type:  NODE_TYPE_UNARY_OP,
			Value: op.Value,
			Right: expr,
			Loc:   beginLoc,
		}, nil
	}

	return p.ParseComparison()
}

func (p *Parser) ParseUnary() (*Node, error) {
	beginLoc := p.CurrLoc()
	if p.Match(tokeniser.TOKEN_TYPE_MINUS) {
		op := p.Consume()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		expr, err := p.ParseUnary()
		if err != nil {
			return nil, err
		}

		return &Node{
			Type:  NODE_TYPE_UNARY_OP,
			Value: op.Type,
			Right: expr,
			Loc:   beginLoc,
		}, nil
	}

	return p.ParseTerm()
}

func (p *Parser) ParseComparison() (*Node, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseAddSub()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TOKEN_TYPE_EQUALS_EQUALS, tokeniser.TOKEN_TYPE_NOT_EQUALS, tokeniser.TOKEN_TYPE_LESS, tokeniser.TOKEN_TYPE_GREATER, tokeniser.TOKEN_TYPE_LESS_EQUALS, tokeniser.TOKEN_TYPE_GREATER_EQUALS) {
		op := p.Consume()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		right, err := p.ParseAddSub()
		if err != nil {
			return nil, err
		}
		left = &Node{
			Type:  NODE_TYPE_BINARY_OP,
			Value: op.Type,
			Left:  left,
			Right: right,
			Loc:   beginLoc,
		}
	}

	return left, nil
}

func (p *Parser) ParseAddSub() (*Node, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseMulDiv()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TOKEN_TYPE_PLUS, tokeniser.TOKEN_TYPE_MINUS) {
		op := p.Consume()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		right, err := p.ParseMulDiv()
		if err != nil {
			return nil, err
		}
		left = &Node{
			Type:  NODE_TYPE_BINARY_OP,
			Value: op.Type,
			Left:  left,
			Right: right,
			Loc:   beginLoc,
		}
	}

	return left, nil
}

func (p *Parser) ParseMulDiv() (*Node, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseUnary()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TOKEN_TYPE_ASTERISK, tokeniser.TOKEN_TYPE_SLASH) {
		op := p.Consume()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		right, err := p.ParseUnary()
		if err != nil {
			return nil, err
		}
		left = &Node{
			Type:  NODE_TYPE_BINARY_OP,
			Value: op.Type,
			Left:  left,
			Right: right,
			Loc:   beginLoc,
		}
	}

	return left, nil
}

func (p *Parser) ParseTerm() (*Node, error) {
	beginLoc := p.CurrLoc()
	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "if" {
		expr, err := p.ParseIfExpression()
		if err != nil {
			return nil, err
		}

		return expr, nil
	}

	if p.Match(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
		p.Consume()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		expr, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		if !p.Expect(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
			return nil, shared.NewError(p.PrevLoc(), "expected ')'")
		}

		return expr, nil
	}

	if p.Match(tokeniser.TOKEN_TYPE_IDENT) {
		id := p.Consume()
		if p.Match(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
			return p.ParseFunctionCall(id)
		}

		return &Node{
			Type:  NODE_TYPE_IDENTIFIER,
			Value: id.Value,
			Loc:   beginLoc,
		}, nil
	}

	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "true" || p.Peek().Value == "false" {
		bLit := p.Consume()
		return &Node{
			Type:  NODE_TYPE_BOOL_LITERAL,
			Value: bLit.Value,
			Loc:   beginLoc,
		}, nil
	}

	if p.Match(tokeniser.TOKEN_TYPE_NUMBER) {
		nLit := p.Consume()
		return &Node{
			Type:  NODE_TYPE_NUMBER_LITERAL,
			Value: nLit.Value,
			Loc:   beginLoc,
		}, nil
	}

	return nil, shared.NewError(p.CurrLoc(), "unexpected token %s", p.Peek())
}

func (p *Parser) ParseFunctionCall(name tokeniser.Token) (*Node, error) {
	beginLoc := p.CurrLoc()
	var args []*Node

	p.Consume()

	if !p.Match(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
		for {
			arg, err := p.ParseExpression()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)

			if !p.Match(tokeniser.TOKEN_TYPE_COMMA) {
				break
			}
			p.Inc()

			for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
				p.Inc()
			}
		}
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
		return nil, shared.NewError(p.PrevLoc(), "expected '('")
	}

	return &Node{
		Type:     NODE_TYPE_FUNCTION_CALL,
		Value:    &Node{Type: NODE_TYPE_IDENTIFIER, Value: name.Value},
		Children: args,
		Loc:      beginLoc,
	}, nil
}

func (p *Parser) ParseIfExpression() (*Node, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TOKEN_TYPE_KEYWORD) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'if' keyword")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	condition, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	thenBlock, err := p.ParseBlockExpression()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	ifBranch := &IfBranch{
		Condition: condition,
		Node:      thenBlock,
	}

	ifNodeValue := &IfNodeValue{
		IfBranch:       ifBranch,
		ElseIfBranches: []*IfBranch{},
	}

	for p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "else" {
		p.Consume()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "if" {
			p.Consume()

			for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
				p.Inc()
			}

			elseifCondition, err := p.ParseExpression()
			if err != nil {
				return nil, err
			}

			for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
				p.Inc()
			}

			elseifBlock, err := p.ParseBlockExpression()
			if err != nil {
				return nil, err
			}

			elseIfBranch := &IfBranch{
				Condition: elseifCondition,
				Node:      elseifBlock,
			}
			ifNodeValue.ElseIfBranches = append(ifNodeValue.ElseIfBranches, elseIfBranch)
		} else {
			elseBlock, err := p.ParseBlockExpression()
			if err != nil {
				return nil, err
			}

			ifNodeValue.ElseBranch = &IfBranch{
				Condition: nil,
				Node:      elseBlock,
			}
			break
		}
	}

	return &Node{
		Type:  NODE_TYPE_IF_EXPR,
		Value: ifNodeValue,
		Loc:   beginLoc,
	}, nil
}

func (p *Parser) ParseBlockExpression() (*Node, error) {
	if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' to start block")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	blockExpression, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_CLOSE_CURLY) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}' to end block")
	}

	return blockExpression, err
}
