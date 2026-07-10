package parser

import (
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
	if err != nil {
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
		return nil, shared.NewError(p.PrevLoc(), "expected ')' after 'sizeof' type")
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

	return p.ParseComparison()
}

func (p *Parser) ParseUnary() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()

	if p.Match(tokeniser.TokenMinus) {
		op := p.Consume()

		var val UnaryOpKind
		switch op.Type {
		case tokeniser.TokenMinus:
			val = UnaryOpNegate
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
				expr = &UnaryOpNode{
					Op:      UnaryOpReference,
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
	left, err := p.ParseAddSub()
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

		right, err := p.ParseAddSub()
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

			modAN := &ModuleAccessNode{
				ModName: ident.Name,
				Ident:   modIdent,
				Loc:     ident.Loc,
			}

			if p.Match(tokeniser.TokenOpenCurly) {
				return p.ParseStructLiteral(modAN)
			}

			if p.Match(tokeniser.TokenOpenParen) {
				return p.ParseFunctionCall(modAN)
			}

			return modAN, nil
		}

		if p.Match(tokeniser.TokenOpenParen) {
			modAN := &ModuleAccessNode{
				ModName: "",
				Ident:   ident,
				Loc:     ident.Loc,
			}
			return p.ParseFunctionCall(modAN)
		}

		if p.Match(tokeniser.TokenOpenCurly) {
			modAN := &ModuleAccessNode{
				ModName: "",
				Ident:   ident,
				Loc:     ident.Loc,
			}
			return p.ParseStructLiteral(modAN)
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

	return nil, shared.NewError(p.CurrLoc(), "unexpected token %s", p.Peek())
}

func (p *Parser) ParseFunctionCall(name *ModuleAccessNode) (*FunctionCallNode, error) {
	var args []ExpressionNode

	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '('")
	}

	if !p.Match(tokeniser.TokenCloseParen) {
		for {
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

	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')'")
	}

	return &FunctionCallNode{
		Name: name,
		Args: args,
		Loc:  name.Loc,
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

	condition, err := p.ParseExpression()
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

			elseifCondition, err := p.ParseExpression()
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

func (p *Parser) ParseStructLiteral(name *ModuleAccessNode) (*StructLiteralNode, error) {
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
	var elements []ExpressionNode
	for {
		if p.Match(tokeniser.TokenCloseCurly) {
			break
		}
		elem, err := p.ParseExpression()
		if err != nil {
			return nil, err
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

	return p.ParseNamedType()
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

	tpe, err := p.ParseType()
	if err != nil {
		return nil, err
	}

	return &PointerTypeNode{
		BaseType: tpe,
		Loc:      beginLoc,
	}, nil
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
