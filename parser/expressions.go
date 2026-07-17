package parser

import (
	"math/big"
	"strconv"

	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

func (p *Parser) ParseExpression() (ExpressionNode, error) {
	return p.ParseLogicalOr()
}

func (p *Parser) ParseSizeOfExpression() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordSizeof) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'sizeof' keyword")
	}
	p.Inc()

	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '(' after 'sizeof'")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	var node Node
	var err error

	p.PushPos()
	node, err = p.ParseType()
	if err == nil {
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
	}
	if err != nil || !p.Match(tokeniser.TokenCloseParen) {
		p.PopPos()
		node, err = p.ParseExpression()
		if err != nil {
			return nil, shared.NewError(p.PrevLoc(), "expected type or expression after 'sizeof'")
		}
	} else {
		p.CommitPos()
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')' after 'sizeof' operand")
	}

	switch node := node.(type) {
	case TypeNode:
		return &SizeOfNode{
			Operand: node,
			Loc:     beginLoc,
		}, nil
	case ExpressionNode:
		return &SizeOfExprNode{
			Operand: node,
			Loc:     beginLoc,
		}, nil
	}

	panic("unreachable")
}

func (p *Parser) ParseAlignOfExpression() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	p.Inc() // alignof
	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '(' after 'alignof'")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	var operand Node
	p.PushPos()
	operand, err := p.ParseType()
	if err == nil {
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
	}
	if err != nil || !p.Match(tokeniser.TokenCloseParen) {
		p.PopPos()
		operand, err = p.ParseExpression()
		if err != nil {
			return nil, shared.NewError(p.PrevLoc(), "expected type or expression after 'alignof('")
		}
	} else {
		p.CommitPos()
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')' after 'alignof' operand")
	}
	node := &AlignOfNode{Loc: beginLoc}
	switch operand := operand.(type) {
	case *NamedTypeNode:
		// A bare name can denote either a type or a value. Semantic resolution
		// chooses the visible binding without evaluating the value expression.
		node.Operand = operand
		node.Expression = &IdentifierNode{Name: operand.Name, Module: operand.ModName, Loc: operand.Loc}
	case TypeNode:
		node.Operand = operand
	case ExpressionNode:
		node.Expression = operand
	default:
		panic("unreachable")
	}
	return node, nil
}

func (p *Parser) ParseOffsetOfExpression() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	p.Inc() // offsetof
	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '(' after 'offsetof'")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	operand, err := p.ParseType()
	if err != nil {
		return nil, shared.NewError(p.PrevLoc(), "expected type after 'offsetof('")
	}
	if !p.Expect(tokeniser.TokenComma) {
		return nil, shared.NewError(p.PrevLoc(), "expected ',' after type in 'offsetof'")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	field, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected field name in 'offsetof'")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')' after 'offsetof' field")
	}
	return &OffsetOfNode{Operand: operand, Field: field.Value, Loc: beginLoc}, nil
}

func (p *Parser) ParseLogicalOr() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseLogicalAnd()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordOr) {
		p.Inc()

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		right, err := p.ParseLogicalAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{
			Op:       BinaryOpLogicalOr,
			Operand1: left,
			Operand2: right,
			Loc:      beginLoc,
		}
	}

	return left, nil
}

func (p *Parser) ParseLogicalAnd() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseLogicalNot()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordAnd) {
		p.Inc()

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		right, err := p.ParseLogicalNot()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{
			Op:       BinaryOpLogicalAnd,
			Operand1: left,
			Operand2: right,
			Loc:      beginLoc,
		}
	}

	return left, nil
}

func (p *Parser) ParseLogicalNot() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordNot) {
		p.Inc()

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		expr, err := p.ParseLogicalNot()
		if err != nil {
			return nil, err
		}

		return &UnaryOpNode{
			Op:      UnaryOpLogicalNot,
			Operand: expr,
			Loc:     beginLoc,
		}, nil
	}

	return p.ParseBitwiseOr()
}

func (p *Parser) ParseBitwiseOr() (ExpressionNode, error) {
	return p.parseLeftAssociative(p.ParseBitwiseXor, []tokeniser.TokenKind{tokeniser.TokenPipe}, map[tokeniser.TokenKind]BinaryOpKind{
		tokeniser.TokenPipe: BinaryOpBitwiseOr,
	})
}

func (p *Parser) ParseBitwiseXor() (ExpressionNode, error) {
	return p.parseLeftAssociative(p.ParseBitwiseAnd, []tokeniser.TokenKind{tokeniser.TokenCaret}, map[tokeniser.TokenKind]BinaryOpKind{
		tokeniser.TokenCaret: BinaryOpBitwiseXor,
	})
}

func (p *Parser) ParseBitwiseAnd() (ExpressionNode, error) {
	return p.parseLeftAssociative(p.ParseComparison, []tokeniser.TokenKind{tokeniser.TokenAmpersand}, map[tokeniser.TokenKind]BinaryOpKind{
		tokeniser.TokenAmpersand: BinaryOpBitwiseAnd,
	})
}

func (p *Parser) parseLeftAssociative(
	parseOperand func() (ExpressionNode, error),
	tokens []tokeniser.TokenKind,
	operators map[tokeniser.TokenKind]BinaryOpKind,
) (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	left, err := parseOperand()
	if err != nil {
		return nil, err
	}
	for p.Match(tokens...) {
		op := operators[p.Consume().Type]
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		right, err := parseOperand()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{Op: op, Operand1: left, Operand2: right, Loc: beginLoc}
	}
	return left, nil
}

func (p *Parser) ParseUnary() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	if p.Match(tokeniser.TokenIncrement) {
		return nil, shared.NewError(beginLoc, "use ... += 1 instead")
	}
	if p.Match(tokeniser.TokenDecrement) {
		return nil, shared.NewError(beginLoc, "use ... -= 1 instead")
	}

	if p.Match(tokeniser.TokenMinus, tokeniser.TokenTilde) {
		op := p.Consume()

		var val UnaryOpKind
		switch op.Type {
		case tokeniser.TokenMinus:
			val = UnaryOpNegate
		case tokeniser.TokenTilde:
			val = UnaryOpBitwiseNot
		}

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		expr, err := p.ParseUnary()
		if err != nil {
			return nil, err
		}

		return &UnaryOpNode{
			Op:      val,
			Operand: expr,
			Loc:     beginLoc,
		}, nil
	}

	return p.ParsePostfix()
}

func (p *Parser) ParsePostfix() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()

	expr, err := p.ParseTerm()

	if err != nil {
		return nil, err
	}

	for {
		switch {
		case p.Match(tokeniser.TokenOpenParen):
			call, err := p.ParseCall(expr)
			if err != nil {
				return nil, err
			}
			expr = call
		case p.Match(tokeniser.TokenIncrement):
			return nil, shared.NewError(p.CurrLoc(), "use ... += 1 instead")
		case p.Match(tokeniser.TokenDecrement):
			return nil, shared.NewError(p.CurrLoc(), "use ... -= 1 instead")
		case p.Match(tokeniser.TokenOpenSquare):
			p.Inc()

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			index, err := p.ParseExpression()
			if err != nil {
				return nil, err
			}

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			if !p.Expect(tokeniser.TokenCloseSquare) {
				return nil, shared.NewError(p.PrevLoc(), "expected ']'")
			}

			expr = &IndexExprNode{
				Subject: expr,
				Index:   index,
				Loc:     beginLoc,
			}
		case p.Match(tokeniser.TokenDot):
			p.Inc()

			if p.Match(tokeniser.TokenAsterisk) {
				p.Inc()
				expr = &UnaryOpNode{
					Op:      UnaryOpDereference,
					Operand: expr,
					Loc:     beginLoc,
				}
				continue
			}

			if p.Match(tokeniser.TokenAmpersand) {
				p.Inc()
				op := UnaryOpReference
				if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordMut) {
					p.Inc()
					op = UnaryOpMutableReference
				}
				expr = &UnaryOpNode{
					Op:      op,
					Operand: expr,
					Loc:     beginLoc,
				}
				continue
			}

			if p.Match(tokeniser.TokenOpenParen) {
				p.Inc()
				for p.Match(tokeniser.TokenNewline) {
					p.Inc()
				}
				typ, err := p.ParseType()
				if err != nil {
					return nil, err
				}
				for p.Match(tokeniser.TokenNewline) {
					p.Inc()
				}
				if !p.Expect(tokeniser.TokenCloseParen) {
					return nil, shared.NewError(p.PrevLoc(), "expected ')'")
				}
				expr = &CastNode{
					ToType:  typ,
					Operand: expr,
					Loc:     beginLoc,
				}
				continue
			}

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			field, err := p.ParseIdent()
			if err != nil {
				return nil, err
			}

			expr = &FieldAccessNode{
				Subject: expr,
				Field:   field,
				Loc:     beginLoc,
			}
		default:
			return expr, nil
		}
	}

}

func (p *Parser) ParseComparison() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseShift()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenEqualsEquals, tokeniser.TokenNotEquals,
		tokeniser.TokenLess, tokeniser.TokenGreater, tokeniser.TokenLessEquals, tokeniser.TokenGreaterEquals) {
		op := p.Consume()
		var val BinaryOpKind
		switch op.Type {
		case tokeniser.TokenEqualsEquals:
			val = BinaryOpEqual
		case tokeniser.TokenNotEquals:
			val = BinaryOpNotEqual
		case tokeniser.TokenLess:
			val = BinaryOpLess
		case tokeniser.TokenGreater:
			val = BinaryOpGreater
		case tokeniser.TokenLessEquals:
			val = BinaryOpLessEqual
		case tokeniser.TokenGreaterEquals:
			val = BinaryOpGreaterEqual
		}
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		right, err := p.ParseShift()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{
			Op:       val,
			Operand1: left,
			Operand2: right,
			Loc:      beginLoc,
		}
	}

	return left, nil
}

func (p *Parser) ParseShift() (ExpressionNode, error) {
	return p.parseLeftAssociative(p.ParseAddSub,
		[]tokeniser.TokenKind{tokeniser.TokenShiftLeft, tokeniser.TokenShiftRight},
		map[tokeniser.TokenKind]BinaryOpKind{
			tokeniser.TokenShiftLeft:  BinaryOpShiftLeft,
			tokeniser.TokenShiftRight: BinaryOpShiftRight,
		})
}

func (p *Parser) ParseAddSub() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseMulDiv()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenPlus, tokeniser.TokenMinus) {
		op := p.Consume()
		var val BinaryOpKind
		if op.Type == tokeniser.TokenPlus {
			val = BinaryOpAdd
		} else {
			val = BinaryOpSubtract
		}

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		right, err := p.ParseMulDiv()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{
			Op:       val,
			Operand1: left,
			Operand2: right,
			Loc:      beginLoc,
		}
	}

	return left, nil
}

func (p *Parser) ParseMulDiv() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseUnary()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenAsterisk, tokeniser.TokenSlash, tokeniser.TokenPercent) {
		op := p.Consume()
		var val BinaryOpKind
		switch op.Type {
		case tokeniser.TokenAsterisk:
			val = BinaryOpMultiply
		case tokeniser.TokenSlash:
			val = BinaryOpDivide
		case tokeniser.TokenPercent:
			val = BinaryOpModulo
		default:
			return nil, shared.NewError(p.PrevLoc(), "unexpected operator %s", op)
		}
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		right, err := p.ParseUnary()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{
			Op:       val,
			Operand1: left,
			Operand2: right,
			Loc:      beginLoc,
		}
	}

	return left, nil
}

func (p *Parser) ParseTerm() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	if p.Match(tokeniser.TokenDot) && p.Next().Type == tokeniser.TokenIdentifier {
		p.Inc()
		variant := p.Consume()
		return &EnumLiteralNode{Variant: variant.Value, Loc: beginLoc}, nil
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordIf) {
		expr, err := p.ParseIfExpression()
		if err != nil {
			return nil, err
		}

		return expr, nil
	}

	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordGiven) {
		expr, err := p.ParseGivenExpression()
		if err != nil {
			return nil, err
		}

		return expr, nil
	}

	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordSizeof) {
		expr, err := p.ParseSizeOfExpression()
		if err != nil {
			return nil, err
		}
		return expr, nil
	}

	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordAlignof) {
		return p.ParseAlignOfExpression()
	}

	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordOffsetof) {
		return p.ParseOffsetOfExpression()
	}

	if p.Match(tokeniser.TokenOpenParen) {
		p.Consume()

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		expr, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		if !p.Expect(tokeniser.TokenCloseParen) {
			return nil, shared.NewError(p.PrevLoc(), "expected ')'")
		}

		return expr, nil
	}

	if p.Match(tokeniser.TokenIdentifier) {
		ident, err := p.ParseIdent()
		if err != nil {
			return nil, err
		}

		if p.Match(tokeniser.TokenColon) {
			p.Inc()

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			modIdent, err := p.ParseIdent()
			if err != nil {
				return nil, err
			}

			qualifiedIdent := &IdentifierNode{
				Module: ident.Name,
				Name:   modIdent.Name,
				Loc:    ident.Loc,
			}

			if p.Match(tokeniser.TokenOpenCurly) &&
				(!p.disambiguateTrailingBlock || p.trailingBraceStartsStructLiteral()) {
				return p.ParseStructLiteral(qualifiedIdent)
			}

			if p.Match(tokeniser.TokenOpenParen) {
				return p.ParseFunctionCall(qualifiedIdent)
			}

			return qualifiedIdent, nil
		}

		if p.Match(tokeniser.TokenOpenParen) {
			return p.ParseFunctionCall(ident)
		}

		if p.Match(tokeniser.TokenOpenCurly) &&
			(!p.disambiguateTrailingBlock || p.trailingBraceStartsStructLiteral()) {
			return p.ParseStructLiteral(ident)
		}

		return ident, nil
	}

	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordLen) {
		p.Inc()

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		if !p.Expect(tokeniser.TokenOpenParen) {
			return nil, shared.NewError(p.PrevLoc(), "expected '(' after 'len'")
		}

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		expr, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		if !p.Expect(tokeniser.TokenCloseParen) {
			return nil, shared.NewError(p.PrevLoc(), "expected ')' after 'len' expression")
		}

		return &UnaryOpNode{
			Op:      UnaryOpSliceLen,
			Operand: expr,
			Loc:     beginLoc,
		}, nil
	}

	if p.Match(tokeniser.TokenOpenCurly) {
		return p.ParseStructLiteral(nil)
	}

	if p.Match(tokeniser.TokenOpenSquare) {
		return p.ParseSliceLiteral()
	}

	if p.Match(tokeniser.TokenKeyword) {
		val := p.Peek().Value

		switch val {
		case string(tokeniser.KeywordTrue), string(tokeniser.KeywordFalse):
			bLit := p.Consume()
			return &BoolLiteralNode{
				Value: bLit.Value,
				Loc:   beginLoc,
			}, nil
		case string(tokeniser.KeywordNil):
			p.Inc()
			return &NilLiteralNode{
				Loc: beginLoc,
			}, nil
		}
	}

	if p.Match(tokeniser.TokenNumber) {
		nLit := p.Consume()
		if !p.Match(tokeniser.TokenDot) {
			return &IntegerLiteralNode{
				Value: nLit.Value,
				Loc:   beginLoc,
			}, nil
		}
		p.Inc()
		if !p.Match(tokeniser.TokenNumber) {
			if p.Match(tokeniser.TokenOpenParen) {
				p.Dec()
				return &IntegerLiteralNode{
					Value: nLit.Value,
					Loc:   beginLoc,
				}, nil
			}
			return &FloatLiteralNode{
				Value: nLit.Value + ".0",
				Loc:   beginLoc,
			}, nil
		}
		n2Lit := p.Consume()
		return &FloatLiteralNode{
			Value: nLit.Value + "." + n2Lit.Value,
			Loc:   beginLoc,
		}, nil
	}

	if p.Match(tokeniser.TokenDot) {
		p.Inc()
		if !p.Match(tokeniser.TokenNumber) {
			return nil, shared.NewError(p.PrevLoc(), "expected number after decimal point")
		}
		n2Lit := p.Consume()
		return &FloatLiteralNode{
			Value: "0." + n2Lit.Value,
			Loc:   beginLoc,
		}, nil
	}

	if p.Match(tokeniser.TokenChar) {
		cLit := p.Consume()
		return &CharLiteralNode{
			Value: []byte(cLit.Value)[0],
			Loc:   beginLoc,
		}, nil
	}

	if p.Match(tokeniser.TokenString) {
		sLit := p.Consume()
		return &StringLiteralNode{
			Value: sLit.Value,
			Loc:   beginLoc,
		}, nil
	}

	if p.Match(tokeniser.TokenCString) {
		literal := p.Consume()
		return &CStringLiteralNode{Value: literal.Value, Loc: beginLoc}, nil
	}

	return nil, shared.NewError(p.CurrLoc(), "unexpected token %s", p.Peek())
}

func (p *Parser) ParseFunctionCall(name *IdentifierNode) (*FunctionCallNode, error) {
	call, err := p.ParseCall(name)
	if err != nil {
		return nil, err
	}
	call.Name = name
	return call, nil
}

func (p *Parser) ParseCall(callee ExpressionNode) (*FunctionCallNode, error) {
	var args []ExpressionNode

	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '('")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	if !p.Match(tokeniser.TokenCloseParen) {
		for {
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			arg, err := p.ParseExpression()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)

			if !p.Match(tokeniser.TokenComma) {
				break
			}
			p.Inc()

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
		}
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')'")
	}

	return &FunctionCallNode{
		Callee: callee,
		Args:   args,
		Loc:    callee.GetLoc(),
	}, nil
}

func (p *Parser) ParseIfExpression() (*IfExprNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TokenKeyword) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'if' keyword")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	wasDisambiguatingTrailingBlock := p.disambiguateTrailingBlock
	p.disambiguateTrailingBlock = true
	condition, err := p.ParseExpression()
	p.disambiguateTrailingBlock = wasDisambiguatingTrailingBlock
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	thenBlock, err := p.ParseBlockExpression()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	node := &IfExprNode{
		Loc: beginLoc,
		IfBranch: IfExprBranch{
			Condition: condition,
			Node:      thenBlock,
		},
	}

	for p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordElse) {
		p.Consume()

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		if p.Match(tokeniser.TokenKeyword) &&
			p.Peek().Value == string(tokeniser.KeywordIf) {
			p.Consume()

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			wasDisambiguatingTrailingBlock := p.disambiguateTrailingBlock
			p.disambiguateTrailingBlock = true
			elseifCondition, err := p.ParseExpression()
			p.disambiguateTrailingBlock = wasDisambiguatingTrailingBlock
			if err != nil {
				return nil, err
			}

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			elseifBlock, err := p.ParseBlockExpression()
			if err != nil {
				return nil, err
			}

			elseIfBranch := IfExprBranch{
				Condition: elseifCondition,
				Node:      elseifBlock,
			}
			node.ElseIfBranches = append(node.ElseIfBranches, elseIfBranch)

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
		} else {
			elseBlock, err := p.ParseBlockExpression()
			if err != nil {
				return nil, err
			}

			node.ElseBranch = elseBlock
			break
		}
	}

	return node, nil
}

func (p *Parser) ParseGivenExpression() (*GivenExprNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TokenKeyword) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'given' keyword")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	block, err := p.ParseBlock()
	if err != nil {
		return nil, err
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if !p.Expect(tokeniser.TokenArrow) {
		return nil, shared.NewError(p.PrevLoc(), "expected '->'")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	expr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}
	return &GivenExprNode{
		Block:     block,
		FinalExpr: expr,
		Loc:       beginLoc,
	}, nil
}

func (p *Parser) ParseBlockExpression() (ExpressionNode, error) {
	if !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' to start block")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	blockExpression, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	if !p.Expect(tokeniser.TokenCloseCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}' to end block")
	}

	return blockExpression, err
}

func (p *Parser) ParseStructLiteral(name *IdentifierNode) (*StructLiteralNode, error) {
	beginLoc := p.CurrLoc()

	if !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' to start struct literal")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	node := &StructLiteralNode{
		Name: name,
		Loc:  beginLoc,
	}
	for {
		if p.Match(tokeniser.TokenCloseCurly) {
			break
		}

		fieldName, ok := p.ExpectGet(tokeniser.TokenIdentifier)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected field name")
		}

		if !p.Expect(tokeniser.TokenEquals) {
			return nil, shared.NewError(p.PrevLoc(), "expected '='")
		}

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		fieldValue, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}

		node.Fields = append(node.Fields, shared.Pair[string, ExpressionNode]{L: fieldName.Value, R: fieldValue})

		if !p.Match(tokeniser.TokenComma, tokeniser.TokenNewline) {
			break
		}
		p.Inc()

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
	}

	if !p.Expect(tokeniser.TokenCloseCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}' to end struct literal")
	}

	return node, nil
}

func (p *Parser) ParseSliceLiteral() (*SliceLiteralNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TokenOpenSquare) {
		return nil, shared.NewError(p.PrevLoc(), "expected '[' to start slice literal")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	var elements []ExpressionNode
	for {
		if p.Match(tokeniser.TokenCloseSquare) {
			break
		}
		elem, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}
		if len(elements) == 0 && p.Match(tokeniser.TokenSemicolon) {
			p.Inc()
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			amount, err := p.ParseExpression()
			if err != nil {
				return nil, err
			}
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			if !p.Expect(tokeniser.TokenCloseSquare) {
				return nil, shared.NewError(p.PrevLoc(), "expected ']' to end repeated slice literal")
			}
			return &SliceLiteralNode{
				RepeatValue:  elem,
				RepeatAmount: amount,
				Loc:          beginLoc,
			}, nil
		}
		elements = append(elements, elem)
		if !p.Match(tokeniser.TokenComma, tokeniser.TokenNewline) {
			break
		}
		p.Inc()
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
	}
	if !p.Expect(tokeniser.TokenCloseSquare) {
		return nil, shared.NewError(p.PrevLoc(), "expected ']' to end slice literal")
	}
	return &SliceLiteralNode{
		Elements: elements,
		Loc:      beginLoc,
	}, nil
}

func (p *Parser) ParseIdent() (*IdentifierNode, error) {
	beginLoc := p.CurrLoc()
	firstIdent, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected identifier")
	}
	node := &IdentifierNode{
		Name: firstIdent.Value,
		Loc:  beginLoc,
	}

	return node, nil
}

func (p *Parser) ParseType() (TypeNode, error) {
	if p.Match(tokeniser.TokenAsterisk) {
		return p.ParsePointerType()
	}

	if p.Match(tokeniser.TokenOpenSquare) {
		return p.ParseSliceType()
	}

	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordStruct) {
		return p.ParseStructType()
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordEnum) {
		return p.ParseEnumType()
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordUnion) {
		return p.ParseUnionType()
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordOpaque) {
		loc := p.CurrLoc()
		p.Inc()
		return &OpaqueTypeNode{Loc: loc}, nil
	}

	return p.ParseNamedType()
}

func (p *Parser) ParseUnionType() (*UnionTypeNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordUnion) {
		return nil, shared.NewError(p.CurrLoc(), "expected 'union'")
	}
	p.Inc()
	if !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' after union")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	fields := []StructField{}
	seen := map[string]struct{}{}
	for !p.Match(tokeniser.TokenCloseCurly) {
		name, err := p.ParseIdent()
		if err != nil {
			return nil, err
		}
		if _, exists := seen[name.Name]; exists {
			return nil, shared.NewError(name.Loc, "duplicate union field %q", name.Name)
		}
		seen[name.Name] = struct{}{}
		if !p.Expect(tokeniser.TokenColon) {
			return nil, shared.NewError(p.PrevLoc(), "expected ':' after union field")
		}
		fieldType, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		fields = append(fields, StructField{Name: name.Name, Type: fieldType})
		if !p.Match(tokeniser.TokenComma, tokeniser.TokenNewline) {
			break
		}
		p.Inc()
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
	}
	if !p.Expect(tokeniser.TokenCloseCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}' after union")
	}
	if len(fields) == 0 {
		return nil, shared.NewError(beginLoc, "union must declare at least one field")
	}
	return &UnionTypeNode{Fields: fields, Loc: beginLoc}, nil
}

func (p *Parser) ParseEnumType() (*EnumTypeNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordEnum) {
		return nil, shared.NewError(p.CurrLoc(), "expected 'enum'")
	}
	p.Inc()
	if !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' after enum")
	}
	variants := []string{}
	values := []string{}
	seen := map[string]struct{}{}
	valueMode := -1 // 0 is implicit, 1 is explicit.
	for {
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenCloseCurly) {
			p.Inc()
			break
		}
		variant, ok := p.ExpectGet(tokeniser.TokenIdentifier)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected enum variant")
		}
		if _, exists := seen[variant.Value]; exists {
			return nil, shared.NewError(variant.Loc, "duplicate enum variant %q", variant.Value)
		}
		seen[variant.Value] = struct{}{}
		variants = append(variants, variant.Value)
		hasExplicitValue := p.Match(tokeniser.TokenEquals)
		mode := 0
		if hasExplicitValue {
			mode = 1
		}
		if valueMode != -1 && valueMode != mode {
			return nil, shared.NewError(variant.Loc, "cannot mix implicit and explicit enum values")
		}
		valueMode = mode
		if hasExplicitValue {
			p.Inc()
			value, ok := p.ExpectGet(tokeniser.TokenNumber)
			if !ok {
				return nil, shared.NewError(p.PrevLoc(), "expected integer literal after '=' in enum variant")
			}
			if !enumValueFits32Bits(value.Value) {
				return nil, shared.NewError(value.Loc, "enum value %s does not fit in 32 bits", value.Value)
			}
			values = append(values, value.Value)
		} else {
			values = append(values, strconv.Itoa(len(variants)-1))
		}
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenComma) {
			p.Inc()
			continue
		}
		if !p.Match(tokeniser.TokenCloseCurly) {
			return nil, shared.NewError(p.CurrLoc(), "expected ',' or '}' after enum variant")
		}
	}
	if len(variants) == 0 {
		return nil, shared.NewError(beginLoc, "enum must declare at least one variant")
	}
	return &EnumTypeNode{Variants: variants, Values: values, Loc: beginLoc}, nil
}

func enumValueFits32Bits(value string) bool {
	n, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return false
	}
	min := big.NewInt(-1 << 31)
	max := new(big.Int).SetUint64(1<<32 - 1)
	return n.Cmp(min) >= 0 && n.Cmp(max) <= 0
}

func (p *Parser) ParseStructType() (*StructTypeNode, error) {
	beginLoc := p.CurrLoc()

	if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordStruct) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'struct' keyword")
	}
	p.Inc()

	if !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{'")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	var fields []StructField
	for !p.Match(tokeniser.TokenCloseCurly) {
		if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordUnion) {
			fieldType, err := p.ParseUnionType()
			if err != nil {
				return nil, err
			}
			fields = append(fields, StructField{Type: fieldType})
			if !p.Match(tokeniser.TokenComma, tokeniser.TokenNewline) {
				break
			}
			p.Inc()
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			continue
		}
		fieldName, err := p.ParseIdent()
		if err != nil {
			return nil, err
		}

		if !p.Expect(tokeniser.TokenColon) {
			return nil, shared.NewError(p.PrevLoc(), "expected ':'")
		}

		fieldType, err := p.ParseType()
		if err != nil {
			return nil, err
		}

		fields = append(fields, StructField{
			Name: fieldName.Name,
			Type: fieldType,
		})

		if !p.Match(tokeniser.TokenComma, tokeniser.TokenNewline) {
			break
		}
		p.Inc()

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
	}

	if !p.Expect(tokeniser.TokenCloseCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}'")
	}

	return &StructTypeNode{
		Fields: fields,
		Loc:    beginLoc,
	}, nil
}

func (p *Parser) ParseSliceType() (*SliceTypeNode, error) {
	beginLoc := p.CurrLoc()

	if !p.Expect(tokeniser.TokenOpenSquare) {
		return nil, shared.NewError(p.PrevLoc(), "expected '[' to start slice type")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	elementType, err := p.ParseType()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	if !p.Match(tokeniser.TokenComma) {
		if !p.Expect(tokeniser.TokenCloseSquare) {
			return nil, shared.NewError(p.PrevLoc(), "expected ']' to end slice type")
		}
		return &SliceTypeNode{
			ElementType: elementType,
			Size:        -1,
			Loc:         beginLoc,
		}, nil
	}
	p.Inc()

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	sizeToken, ok := p.ExpectGet(tokeniser.TokenNumber)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected slice size")
	}

	size, err := strconv.Atoi(sizeToken.Value)
	if err != nil {
		return nil, shared.NewError(sizeToken.Loc, "invalid slice size: %s", sizeToken.Value)
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	if !p.Expect(tokeniser.TokenCloseSquare) {
		return nil, shared.NewError(p.PrevLoc(), "expected ']' to end slice type")
	}

	return &SliceTypeNode{
		ElementType: elementType,
		Size:        size,
		Loc:         beginLoc,
	}, nil
}

func (p *Parser) ParsePointerType() (*PointerTypeNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TokenAsterisk) {
		return nil, shared.NewError(p.PrevLoc(), "expected '*' to start pointer type")
	}

	mutable := false
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordMut) {
		p.Inc()
		mutable = true
	}

	var tpe TypeNode
	var err error
	if p.Match(tokeniser.TokenOpenParen) {
		tpe, err = p.ParseFunctionType()
	} else {
		tpe, err = p.ParseType()
	}
	if err != nil {
		return nil, err
	}
	if mutable {
		if _, ok := tpe.(*FunctionTypeNode); ok {
			return nil, shared.NewError(beginLoc, "function pointers cannot be mutable pointers")
		}
	}

	return &PointerTypeNode{
		BaseType: tpe,
		Mutable:  mutable,
		Loc:      beginLoc,
	}, nil
}

func (p *Parser) ParseFunctionType() (*FunctionTypeNode, error) {
	beginLoc := p.CurrLoc()
	p.Inc() // (
	var params []TypeNode
	for !p.Match(tokeniser.TokenCloseParen) {
		param, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		params = append(params, param)
		if !p.Match(tokeniser.TokenComma) {
			break
		}
		p.Inc()
	}
	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')' in function pointer type")
	}
	if !p.Expect(tokeniser.TokenColon) {
		return nil, shared.NewError(p.PrevLoc(), "expected ':' and return type in function pointer type")
	}
	ret, err := p.ParseType()
	if err != nil {
		return nil, err
	}
	return &FunctionTypeNode{Parameters: params, ReturnType: ret, Loc: beginLoc}, nil
}

func (p *Parser) ParseNamedType() (*NamedTypeNode, error) {
	beginLoc := p.CurrLoc()
	ident, err := p.ParseIdent()
	if err != nil {
		return nil, err
	}

	if !p.Match(tokeniser.TokenColon) {
		return &NamedTypeNode{
			ModName: "",
			Name:    ident.Name,
			Loc:     beginLoc,
		}, nil
	}
	p.Inc()

	realIdent, err := p.ParseIdent()
	if err != nil {
		return nil, err
	}

	return &NamedTypeNode{
		ModName: ident.Name,
		Name:    realIdent.Name,
		Loc:     beginLoc,
	}, nil
}
