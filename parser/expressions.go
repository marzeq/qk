package parser

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

func (p *Parser) ParseExpression() (ExpressionNode, error) {
	return p.ParseLogicalOr()
}

func (p *Parser) ParseWholeExpression() (ExpressionNode, error) {
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
	if !p.Match(tokeniser.TokenEof) {
		return nil, shared.NewError(p.CurrLoc(), "unexpected token %s after expression", p.Peek())
	}
	return expr, nil
}

func (p *Parser) ParseSizeOfExpression() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	if !p.ConsumeBuiltin("sizeof") {
		return nil, shared.NewError(p.CurrLoc(), "expected '@sizeof' builtin")
	}

	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '(' after '@sizeof'")
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
			return nil, shared.NewError(p.PrevLoc(), "expected type or expression after '@sizeof'")
		}
	} else {
		p.CommitPos()
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')' after '@sizeof' operand")
	}

	switch node := node.(type) {
	case *NamedTypeNode:
		// A bare name can denote either a type or a value. Semantic resolution
		// chooses the visible binding without evaluating the value expression.
		return &SizeOfNode{
			Operand: node, Expression: &IdentifierNode{Name: node.Name, Module: node.ModName, Loc: node.Loc},
			Loc: p.SpanFrom(beginLoc),
		}, nil
	case TypeNode:
		return &SizeOfNode{
			Operand: node,
			Loc:     p.SpanFrom(beginLoc),
		}, nil
	case ExpressionNode:
		return &SizeOfExprNode{
			Operand: node,
			Loc:     p.SpanFrom(beginLoc),
		}, nil
	}

	panic("unreachable")
}

func (p *Parser) ParseAlignOfExpression() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	p.ConsumeBuiltin("alignof")
	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '(' after '@alignof'")
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
			return nil, shared.NewError(p.PrevLoc(), "expected type or expression after '@alignof('")
		}
	} else {
		p.CommitPos()
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')' after '@alignof' operand")
	}
	node := &AlignOfNode{Loc: p.SpanFrom(beginLoc)}
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
	p.ConsumeBuiltin("offsetof")
	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '(' after '@offsetof'")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	operand, err := p.ParseType()
	if err != nil {
		return nil, shared.NewError(p.PrevLoc(), "expected type after '@offsetof('")
	}
	if !p.Expect(tokeniser.TokenComma) {
		return nil, shared.NewError(p.PrevLoc(), "expected ',' after type in '@offsetof'")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	field, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected field name in '@offsetof'")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')' after '@offsetof' field")
	}
	return &OffsetOfNode{Operand: operand, Field: field.Value, Loc: p.SpanFrom(beginLoc)}, nil
}

func (p *Parser) ParseLogicalOr() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseLogicalAnd()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenLogicalOr) {
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
			Loc:      p.SpanFrom(beginLoc),
		}
	}

	return left, nil
}

func (p *Parser) ParseLogicalAnd() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseBitwiseOr()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenLogicalAnd) {
		p.Inc()

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		right, err := p.ParseBitwiseOr()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{
			Op:       BinaryOpLogicalAnd,
			Operand1: left,
			Operand2: right,
			Loc:      p.SpanFrom(beginLoc),
		}
	}

	return left, nil
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
		left = &BinaryOpNode{Op: op, Operand1: left, Operand2: right, Loc: p.SpanFrom(beginLoc)}
	}
	return left, nil
}

func (p *Parser) ParseUnary() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	if p.MatchAdjacentPair(tokeniser.TokenPlus) {
		return nil, shared.NewError(beginLoc, "use ... += 1 instead")
	}
	if p.MatchAdjacentPair(tokeniser.TokenMinus) {
		return nil, shared.NewError(beginLoc, "use ... -= 1 instead")
	}

	if p.Match(tokeniser.TokenExclam, tokeniser.TokenMinus, tokeniser.TokenTilde) {
		op := p.Consume()

		var val UnaryOpKind
		switch op.Type {
		case tokeniser.TokenExclam:
			val = UnaryOpLogicalNot
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
			Loc:     p.SpanFrom(beginLoc),
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
		case p.Match(tokeniser.TokenDot) && p.Next().Type == tokeniser.TokenOpenCurly:
			qualified, ok := expr.(*IdentifierNode)
			if !ok {
				qualified, ok = dottedIdentifier(expr)
			}
			if !ok {
				return expr, nil
			}
			p.Inc()
			return p.ParseStructLiteral(qualified)
		case p.Match(tokeniser.TokenOpenParen):
			// A type-producing binding uses ordinary call syntax. Before a
			// structural literal the following '.{' makes that interpretation
			// unambiguous, so retain the arguments on the type identifier.
			p.PushPos()
			typeArguments, typeErr := p.parseCallTypeArguments()
			if typeErr == nil && p.Match(tokeniser.TokenDot) && p.Next().Type == tokeniser.TokenOpenCurly {
				qualified, ok := expr.(*IdentifierNode)
				if !ok {
					qualified, ok = dottedIdentifier(expr)
				}
				if ok {
					p.CommitPos()
					qualified.TypeArguments = typeArguments
					qualified.Loc = p.SpanFrom(qualified.Loc)
					expr = qualified
					continue
				}
			}
			p.PopPos()
			call, err := p.ParseCall(expr)
			if err != nil {
				return nil, err
			}
			if identifier, ok := expr.(*IdentifierNode); ok {
				call.Name = identifier
			}
			expr = call
		case p.MatchAdjacentPair(tokeniser.TokenPlus):
			return nil, shared.NewError(p.CurrLoc(), "use ... += 1 instead")
		case p.MatchAdjacentPair(tokeniser.TokenMinus):
			return nil, shared.NewError(p.CurrLoc(), "use ... -= 1 instead")
		case p.Match(tokeniser.TokenOpenSquare):
			p.Inc()

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			var first ExpressionNode
			if !p.Match(tokeniser.TokenColon) {
				var err error
				first, err = p.ParseExpression()
				if err != nil {
					return nil, err
				}
			}

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			if p.Match(tokeniser.TokenColon) {
				p.Inc()
				for p.Match(tokeniser.TokenNewline) {
					p.Inc()
				}

				var end ExpressionNode
				if !p.Match(tokeniser.TokenCloseSquare) {
					var err error
					end, err = p.ParseExpression()
					if err != nil {
						return nil, err
					}
				}

				for p.Match(tokeniser.TokenNewline) {
					p.Inc()
				}
				if !p.Expect(tokeniser.TokenCloseSquare) {
					return nil, shared.NewError(p.PrevLoc(), "expected ']'")
				}
				expr = &SliceExprNode{
					Subject: expr,
					Start:   first,
					End:     end,
					Loc:     p.SpanFrom(beginLoc),
				}
				continue
			}

			if first == nil {
				return nil, shared.NewError(p.CurrLoc(), "expected index or ':'")
			}
			if !p.Expect(tokeniser.TokenCloseSquare) {
				return nil, shared.NewError(p.PrevLoc(), "expected ']'")
			}

			expr = &IndexExprNode{
				Subject: expr,
				Index:   first,
				Loc:     p.SpanFrom(beginLoc),
			}
		case p.Match(tokeniser.TokenDot):
			p.Inc()

			if p.Match(tokeniser.TokenAsterisk) {
				p.Inc()
				expr = &UnaryOpNode{
					Op:      UnaryOpDereference,
					Operand: expr,
					Loc:     p.SpanFrom(beginLoc),
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
					Loc:     p.SpanFrom(beginLoc),
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
					Loc:     p.SpanFrom(beginLoc),
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
				Loc:     p.SpanFrom(beginLoc),
			}
		default:
			return expr, nil
		}
	}

}

func dottedIdentifier(expr ExpressionNode) (*IdentifierNode, bool) {
	var parts []string
	var typeArguments []TypeNode
	for {
		switch n := expr.(type) {
		case *FieldAccessNode:
			if len(parts) == 0 {
				typeArguments = n.Field.TypeArguments
			}
			parts = append([]string{n.Field.Name}, parts...)
			expr = n.Subject
		case *IdentifierNode:
			parts = append([]string{n.Name}, parts...)
			if len(parts) < 2 {
				return nil, false
			}
			return &IdentifierNode{
				Name: parts[len(parts)-1], Module: strings.Join(parts[:len(parts)-1], "."),
				TypeArguments: typeArguments, Loc: n.Loc,
			}, true
		default:
			return nil, false
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
			Loc:      p.SpanFrom(beginLoc),
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
			Loc:      p.SpanFrom(beginLoc),
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
			Loc:      p.SpanFrom(beginLoc),
		}
	}

	return left, nil
}

func (p *Parser) ParseTerm() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	if p.Match(tokeniser.TokenPipe, tokeniser.TokenLogicalOr) {
		return p.ParseLambdaExpression()
	}
	if p.Match(tokeniser.TokenDot) && p.Next().Type == tokeniser.TokenOpenCurly {
		p.Inc()
		return p.ParseStructLiteral(nil)
	}
	if p.Match(tokeniser.TokenDot) && p.Next().Type == tokeniser.TokenIdentifier {
		p.Inc()
		variant := p.Consume()
		return &EnumLiteralNode{Variant: variant.Value, Loc: p.SpanFrom(beginLoc)}, nil
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordIf) {
		expr, err := p.ParseIfExpression()
		if err != nil {
			return nil, err
		}

		return expr, nil
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordWhen) {
		node, err := p.parseWhen(WhenExpression)
		if err != nil {
			return nil, err
		}
		return node, nil
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordMatch) {
		return p.ParseMatch(true)
	}

	if p.Match(tokeniser.TokenOpenCurly) {
		expr, err := p.ParseBlockExpression()
		if err != nil {
			return nil, err
		}
		return expr, nil
	}

	if p.MatchBuiltin("sizeof") {
		expr, err := p.ParseSizeOfExpression()
		if err != nil {
			return nil, err
		}
		return expr, nil
	}

	if p.MatchBuiltin("alignof") {
		return p.ParseAlignOfExpression()
	}

	if p.MatchBuiltin("offsetof") {
		return p.ParseOffsetOfExpression()
	}
	if p.MatchBuiltin("embed") {
		return p.ParseEmbedExpression()
	}
	if p.MatchBuiltin("asm") {
		return p.ParseInlineAsmExpression()
	}
	if p.MatchBuiltin("repr") {
		begin := p.CurrLoc()
		p.ConsumeBuiltin("repr")
		if !p.Expect(tokeniser.TokenOpenParen) {
			return nil, shared.NewError(p.PrevLoc(), "expected '(' after '@repr'")
		}
		operand, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}
		if !p.Expect(tokeniser.TokenCloseParen) {
			return nil, shared.NewError(p.PrevLoc(), "expected ')' after '@repr' operand")
		}
		return &ReprNode{Operand: operand, Loc: p.SpanFrom(begin)}, nil
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

	if p.Match(tokeniser.TokenNoInitializer) {
		p.Inc()
		return &NoInitializerNode{Loc: p.SpanFrom(beginLoc)}, nil
	}

	if p.Match(tokeniser.TokenIdentifier) {
		ident, err := p.ParseIdent()
		if err != nil {
			return nil, err
		}
		return ident, nil
	}

	if p.MatchBuiltin("len") {
		p.ConsumeBuiltin("len")

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		if !p.Expect(tokeniser.TokenOpenParen) {
			return nil, shared.NewError(p.PrevLoc(), "expected '(' after '@len'")
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
			return nil, shared.NewError(p.PrevLoc(), "expected ')' after '@len' expression")
		}

		return &UnaryOpNode{
			Op:      UnaryOpSliceLen,
			Operand: expr,
			Loc:     p.SpanFrom(beginLoc),
		}, nil
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
				Loc:   p.SpanFrom(beginLoc),
			}, nil
		case string(tokeniser.KeywordNil):
			p.Inc()
			return &NilLiteralNode{
				Loc: p.SpanFrom(beginLoc),
			}, nil
		}
	}

	if p.Match(tokeniser.TokenNumber) {
		nLit := p.Consume()
		return &IntegerLiteralNode{
			Value: nLit.Value,
			Loc:   p.SpanFrom(beginLoc),
		}, nil
	}

	if p.Match(tokeniser.TokenFloat) {
		nLit := p.Consume()
		return &FloatLiteralNode{
			Value: nLit.Value,
			Loc:   p.SpanFrom(beginLoc),
		}, nil
	}

	if p.Match(tokeniser.TokenChar) {
		cLit := p.Consume()
		return &CharLiteralNode{
			Value: []byte(cLit.Value)[0],
			Loc:   p.SpanFrom(beginLoc),
		}, nil
	}

	if p.Match(tokeniser.TokenString) {
		sLit := p.Consume()
		return &StringLiteralNode{
			Value: sLit.Value,
			Loc:   p.SpanFrom(beginLoc),
		}, nil
	}

	if p.Match(tokeniser.TokenCString) {
		literal := p.Consume()
		return &CStringLiteralNode{Value: literal.Value, Loc: p.SpanFrom(beginLoc)}, nil
	}

	return nil, shared.NewError(p.CurrLoc(), "unexpected token %s", p.Peek())
}

func (p *Parser) ParseLambdaExpression() (*LambdaNode, error) {
	beginLoc := p.CurrLoc()
	var args []*FunctionNodeArg
	typedVariadic := false

	if p.Match(tokeniser.TokenLogicalOr) {
		p.Inc()
	} else {
		p.Inc() // opening '|'
		for {
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			if p.Match(tokeniser.TokenPipe) {
				p.Inc()
				break
			}
			name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
			if !ok {
				return nil, shared.NewError(p.PrevLoc(), "expected lambda parameter name or '|'")
			}
			group := []*FunctionNodeArg{{Name: name.Value, Loc: name.Loc}}
			if p.Match(tokeniser.TokenEquals) {
				return nil, shared.NewError(p.CurrLoc(), "lambda parameters cannot have default values")
			}
			for !p.Match(tokeniser.TokenColon, tokeniser.TokenPipe) {
				if !p.Expect(tokeniser.TokenComma) {
					return nil, shared.NewError(p.PrevLoc(), "expected ':', ',' or '|' after lambda parameter")
				}
				for p.Match(tokeniser.TokenNewline) {
					p.Inc()
				}
				if p.Match(tokeniser.TokenPipe) {
					break
				}
				name, ok = p.ExpectGet(tokeniser.TokenIdentifier)
				if !ok {
					return nil, shared.NewError(p.PrevLoc(), "expected lambda parameter name")
				}
				group = append(group, &FunctionNodeArg{Name: name.Value, Loc: name.Loc})
				if p.Match(tokeniser.TokenEquals) {
					return nil, shared.NewError(p.CurrLoc(), "lambda parameters cannot have default values")
				}
			}

			if p.Match(tokeniser.TokenColon) {
				p.Inc()
				isTypedVariadic := p.Match(tokeniser.Token3Dots)
				if isTypedVariadic {
					if len(group) != 1 {
						return nil, shared.NewError(p.CurrLoc(), "typed variadic parameter cannot use a grouped declaration")
					}
					p.Inc()
				}
				typeNode, err := p.ParseType()
				if err != nil {
					return nil, err
				}
				if isTypedVariadic {
					typeNode = &SliceTypeNode{ElementType: typeNode, Loc: typeNode.GetLoc()}
					typedVariadic = true
				}
				for _, arg := range group {
					arg.Type = typeNode
				}
			}
			if p.Match(tokeniser.TokenEquals) {
				return nil, shared.NewError(p.CurrLoc(), "lambda parameters cannot have default values")
			}
			args = append(args, group...)
			if typedVariadic {
				if p.Match(tokeniser.TokenComma) {
					return nil, shared.NewError(p.CurrLoc(), "typed variadic parameter must be last")
				}
				if !p.Expect(tokeniser.TokenPipe) {
					return nil, shared.NewError(p.PrevLoc(), "expected '|' after lambda parameters")
				}
				break
			}
			if p.Match(tokeniser.TokenComma) {
				p.Inc()
				continue
			}
			if !p.Expect(tokeniser.TokenPipe) {
				return nil, shared.NewError(p.PrevLoc(), "expected ',' or '|' after lambda parameter")
			}
			break
		}
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if !p.Expect(tokeniser.TokenFatArrow) {
		return nil, shared.NewError(p.PrevLoc(), "expected '=>' after lambda parameters")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	var body ExpressionNode
	var err error
	if p.Match(tokeniser.TokenOpenCurly) {
		body, err = p.ParseBlockExpression()
	} else {
		body, err = p.ParseExpression()
	}
	if err != nil {
		return nil, err
	}
	return &LambdaNode{Args: args, Body: body, TypedVariadic: typedVariadic, Loc: p.SpanFrom(beginLoc)}, nil
}

func (p *Parser) ParseEmbedExpression() (*EmbedNode, error) {
	begin := p.CurrLoc()
	p.ConsumeBuiltin("embed")
	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '(' after '@embed'")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if !p.Match(tokeniser.TokenString) {
		return nil, shared.NewError(p.CurrLoc(), "expected file path string in '@embed'")
	}
	path := p.Consume().Value
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')' after '@embed' path")
	}
	return &EmbedNode{Path: path, Loc: p.SpanFrom(begin)}, nil
}

// ParseInlineAsmExpression parses:
//
//	@asm("template",
//	  out u64 "=r",
//	  in value "r",
//	  clobber "cc",
//	  volatile)
//
// Outputs are intentionally listed before inputs because their positions form
// the leading operands in LLVM's inline-assembly constraint string.
func (p *Parser) ParseInlineAsmExpression() (*InlineAsmNode, error) {
	begin := p.CurrLoc()
	p.ConsumeBuiltin("asm")
	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '(' after '@asm'")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	template, ok := p.ExpectGet(tokeniser.TokenString)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "@asm requires a string literal template")
	}
	node := &InlineAsmNode{Template: template.Value}
	seenInput := false
	seenClobber := false
	for {
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenCloseParen) {
			p.Inc()
			break
		}
		if !p.Expect(tokeniser.TokenComma) {
			return nil, shared.NewError(p.PrevLoc(), "expected ',' or ')' in @asm")
		}
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenCloseParen) {
			p.Inc()
			break
		}
		entryLoc := p.CurrLoc()
		if p.Match(tokeniser.TokenIdentifier) && p.Peek().Value == "out" {
			if seenInput || seenClobber {
				return nil, shared.NewError(entryLoc, "@asm outputs must precede inputs and clobbers")
			}
			p.Inc()
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			typeNode, err := p.ParseType()
			if err != nil {
				return nil, err
			}
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			constraint, ok := p.ExpectGet(tokeniser.TokenString)
			if !ok {
				return nil, shared.NewError(p.PrevLoc(), "expected output constraint string after @asm output type")
			}
			node.Outputs = append(node.Outputs, InlineAsmOutput{TypeNode: typeNode, Constraint: constraint.Value, Loc: p.SpanFrom(entryLoc)})
			continue
		}
		if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordIn) {
			if seenClobber {
				return nil, shared.NewError(entryLoc, "@asm inputs must precede clobbers")
			}
			seenInput = true
			p.Inc()
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			value, err := p.ParseExpression()
			if err != nil {
				return nil, err
			}
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			constraint, ok := p.ExpectGet(tokeniser.TokenString)
			if !ok {
				return nil, shared.NewError(p.PrevLoc(), "expected input constraint string after @asm input value")
			}
			node.Inputs = append(node.Inputs, InlineAsmInput{Value: value, Constraint: constraint.Value, Loc: p.SpanFrom(entryLoc)})
			continue
		}
		if p.Match(tokeniser.TokenIdentifier) && p.Peek().Value == "clobber" {
			seenClobber = true
			p.Inc()
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			name, ok := p.ExpectGet(tokeniser.TokenString)
			if !ok {
				return nil, shared.NewError(p.PrevLoc(), "expected clobber name string")
			}
			node.Clobbers = append(node.Clobbers, name.Value)
			continue
		}
		if p.Match(tokeniser.TokenIdentifier) && p.Peek().Value == "volatile" {
			p.Inc()
			node.Volatile = true
			continue
		}
		return nil, shared.NewError(entryLoc, "expected 'out', 'in', 'clobber', or 'volatile' in @asm")
	}
	node.Loc = p.SpanFrom(begin)
	return node, nil
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
	expanded := false

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
			if p.Match(tokeniser.TokenCloseParen) {
				break
			}

			arg, err := p.ParseExpression()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if p.Match(tokeniser.Token3Dots) {
				p.Inc()
				expanded = true
				for p.Match(tokeniser.TokenNewline) {
					p.Inc()
				}
				if !p.Match(tokeniser.TokenCloseParen) {
					return nil, shared.NewError(p.CurrLoc(), "expanded slice must be the final call argument")
				}
				break
			}

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
		Callee:            callee,
		Args:              args,
		VariadicExpansion: expanded,
		Loc:               callee.GetLoc().WithEnd(p.PrevLoc()),
	}, nil
}

func (p *Parser) ParseStructLiteral(name *IdentifierNode) (*StructLiteralNode, error) {
	beginLoc := p.CurrLoc()
	if name != nil {
		beginLoc = name.GetLoc()
	}

	if !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' to start struct literal")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	node := &StructLiteralNode{
		Name: name,
		Loc:  p.SpanFrom(beginLoc),
	}
	if p.Match(tokeniser.TokenDot) {
		for {
			if !p.Expect(tokeniser.TokenDot) {
				return nil, shared.NewError(p.PrevLoc(), "expected '.' before flag name")
			}
			member, ok := p.ExpectGet(tokeniser.TokenIdentifier)
			if !ok {
				return nil, shared.NewError(p.PrevLoc(), "expected flag name")
			}
			node.FlagMembers = append(node.FlagMembers, member.Value)
			if !p.Match(tokeniser.TokenComma, tokeniser.TokenNewline) {
				break
			}
			p.Inc()
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			if p.Match(tokeniser.TokenCloseCurly) {
				break
			}
		}
		if !p.Expect(tokeniser.TokenCloseCurly) {
			return nil, shared.NewError(p.PrevLoc(), "expected '}' to end flags literal")
		}
		node.Loc = p.SpanFrom(beginLoc)
		return node, nil
	}
	for {
		if p.Match(tokeniser.TokenCloseCurly) {
			break
		}
		if p.Match(tokeniser.TokenNoInitializer) {
			node.NoInitRemaining = true
			p.Inc()
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			if p.Match(tokeniser.TokenComma) {
				p.Inc()
				for p.Match(tokeniser.TokenNewline) {
					p.Inc()
				}
			}
			if !p.Match(tokeniser.TokenCloseCurly) {
				return nil, shared.NewError(p.CurrLoc(), "'---' must be the final struct initializer entry")
			}
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

	node.Loc = p.SpanFrom(beginLoc)
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
				Loc:          p.SpanFrom(beginLoc),
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
		Loc:      p.SpanFrom(beginLoc),
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
		Loc:  p.SpanFrom(beginLoc),
	}

	return node, nil
}

func (p *Parser) ParseType() (TypeNode, error) {
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordWhen) {
		return p.parseWhen(WhenType)
	}
	if p.MatchBuiltin("reprof") {
		begin := p.CurrLoc()
		p.ConsumeBuiltin("reprof")
		if !p.Expect(tokeniser.TokenOpenParen) {
			return nil, shared.NewError(p.PrevLoc(), "expected '(' after '@reprof'")
		}
		operand, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		if !p.Expect(tokeniser.TokenCloseParen) {
			return nil, shared.NewError(p.PrevLoc(), "expected ')' after '@reprof' operand")
		}
		return &ReprTypeNode{Operand: operand, Loc: p.SpanFrom(begin)}, nil
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordDyn) {
		return nil, shared.NewError(p.CurrLoc(), "dynamic trait pointer type must start with '*'")
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordMut) &&
		p.Next().Type == tokeniser.TokenKeyword && p.Next().Value == string(tokeniser.KeywordDyn) {
		return nil, shared.NewError(p.CurrLoc(), "mutable dynamic trait pointer type must start with '*mut'")
	}
	if p.Match(tokeniser.TokenAsterisk) {
		return p.ParsePointerType()
	}

	if p.Match(tokeniser.TokenOpenSquare) {
		return p.ParseArrayOrSliceType()
	}

	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordStruct) {
		return p.ParseStructType()
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordEnum) {
		return p.ParseEnumType()
	}
	if p.Match(tokeniser.TokenIdentifier) && p.Peek().Value == "flags" {
		if p.Next().Type == tokeniser.TokenOpenParen {
			return p.ParseFlagsType()
		}
		if p.Next().Type == tokeniser.TokenOpenCurly {
			return nil, shared.NewError(p.Next().Loc, "expected backing type in parentheses after 'flags'")
		}
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordUnion) {
		return p.ParseUnionType()
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordOpaque) {
		loc := p.CurrLoc()
		p.Inc()
		return &OpaqueTypeNode{Loc: loc}, nil
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordTrait) {
		return p.ParseTraitType()
	}

	return p.ParseNamedType()
}

func (p *Parser) ParseDynType(begin shared.Location, mutable bool) (*DynTypeNode, error) {
	if !p.Expect(tokeniser.TokenKeyword) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'dyn' to start dynamic trait type")
	}
	trait, err := p.ParseNamedType()
	if err != nil {
		return nil, err
	}
	return &DynTypeNode{TraitType: trait, Mutable: mutable, Loc: p.SpanFrom(begin)}, nil
}

func (p *Parser) ParseTraitType() (*TraitTypeNode, error) {
	begin := p.CurrLoc()
	p.Inc()
	if !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' after trait")
	}
	methods := []TraitMethodNode{}
	seen := map[string]struct{}{}
	for {
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenCloseCurly) {
			p.Inc()
			break
		}
		loc := p.CurrLoc()
		kw, ok := p.ExpectGet(tokeniser.TokenKeyword)
		if !ok || kw.Value != string(tokeniser.KeywordLet) {
			return nil, shared.NewError(p.PrevLoc(), "expected trait method declaration")
		}
		name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected trait method name")
		}
		if _, duplicate := seen[name.Value]; duplicate {
			return nil, shared.NewError(name.Loc, "duplicate trait method %q", name.Value)
		}
		seen[name.Value] = struct{}{}
		genericParameters := []GenericParameterNode{}
		if !p.Expect(tokeniser.TokenOpenParen) {
			return nil, shared.NewError(p.PrevLoc(), "expected '('")
		}
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		receiver := MethodReceiverNone
		if p.Match(tokeniser.TokenIdentifier) && p.Peek().Value == "self" {
			p.Inc()
			receiver = MethodReceiverValue
		} else if p.Match(tokeniser.TokenAsterisk) {
			p.Inc()
			receiver = MethodReceiverPointer
			if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordMut) {
				p.Inc()
				receiver = MethodReceiverMutablePointer
			}
			self, ok := p.ExpectGet(tokeniser.TokenIdentifier)
			if !ok || self.Value != "self" {
				return nil, shared.NewError(p.PrevLoc(), "trait method receiver must be self, *self, or *mut self")
			}
		} else {
			return nil, shared.NewError(p.CurrLoc(), "trait method must have a self, *self, or *mut self receiver")
		}
		args := []*FunctionNodeArg{}
		if p.Match(tokeniser.TokenComma) {
			p.Inc()
		}
		for {
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			if p.Match(tokeniser.TokenCloseParen) {
				break
			}
			if parameter, matched, parseErr := p.parseTypeParameter(); matched || parseErr != nil {
				if parseErr != nil {
					return nil, parseErr
				}
				genericParameters = append(genericParameters, parameter)
				if p.Match(tokeniser.TokenComma) {
					p.Inc()
				}
				continue
			}
			arg, err := p.ParseIdent()
			if err != nil {
				return nil, err
			}

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			if !p.Expect(tokeniser.TokenColon) {
				return nil, shared.NewError(
					p.PrevLoc(),
					"expected ':' after trait method parameter",
				)
			}

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			t, err := p.ParseType()
			if err != nil {
				return nil, err
			}

			args = append(args, &FunctionNodeArg{
				Name: arg.Name,
				Type: t,
				Loc:  arg.Loc,
			})

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			if !p.Match(tokeniser.TokenComma) {
				break
			}

			p.Inc()
		}

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		if !p.Expect(tokeniser.TokenCloseParen) {
			return nil, shared.NewError(p.PrevLoc(), "expected ')'")
		}
		var ret TypeNode = &NamedTypeNode{Name: "void", Loc: loc}
		if p.Match(tokeniser.TokenColon) {
			p.Inc()
			var err error
			ret, err = p.parseFunctionReturnType()
			if err != nil {
				return nil, err
			}
		}
		var body Node
		var err error
		expressionBody := false
		if p.Match(tokeniser.TokenOpenCurly) {
			body, err = p.ParseBlock()
			if err != nil {
				return nil, err
			}
		} else if p.Match(tokeniser.TokenEquals) {
			p.Inc()
			expressionBody = true
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			if p.Match(tokeniser.TokenOpenCurly) {
				body, err = p.ParseBlockExpression()
			} else {
				body, err = p.ParseExpression()
			}
			if err != nil {
				return nil, err
			}
		}
		methods = append(methods, TraitMethodNode{
			Name: name.Value, GenericParameters: genericParameters, Receiver: receiver,
			Args: args, ReturnType: ret, Body: body, ExpressionBody: expressionBody,
			Loc: p.SpanFrom(loc),
		})
		if p.Match(tokeniser.TokenComma, tokeniser.TokenSemicolon) {
			p.Inc()
		}
	}
	return &TraitTypeNode{Methods: methods, Loc: p.SpanFrom(begin)}, nil
}

func (p *Parser) ParseUnionType() (*UnionTypeNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordUnion) {
		return nil, shared.NewError(p.CurrLoc(), "expected 'union'")
	}
	p.Inc()
	var tagType TypeNode
	autoTag := false
	if p.Match(tokeniser.TokenOpenParen) {
		p.Inc()
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenAt) {
			p.Inc()
			attribute, err := p.ParseIdent()
			if err != nil {
				return nil, err
			}
			if attribute.Name != "auto" {
				return nil, shared.NewError(attribute.Loc, "unknown tagged union attribute @%s; expected @auto", attribute.Name)
			}
			autoTag = true
		} else {
			var err error
			tagType, err = p.ParseType()
			if err != nil {
				return nil, err
			}
		}
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if !p.Expect(tokeniser.TokenCloseParen) {
			return nil, shared.NewError(p.PrevLoc(), "expected ')' after tagged union tag type")
		}
	}
	if !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' after union")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	fields := []StructField{}
	variants := []TaggedUnionVariantNode{}
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
		if tagType != nil || autoTag {
			variant := TaggedUnionVariantNode{Name: name.Name, Loc: name.Loc}
			if p.Match(tokeniser.TokenOpenParen) {
				p.Inc()
				for p.Match(tokeniser.TokenNewline) {
					p.Inc()
				}
				fieldNames := map[string]struct{}{}
				for !p.Match(tokeniser.TokenCloseParen) {
					fieldName := ""
					fieldLoc := p.CurrLoc()
					if p.Match(tokeniser.TokenIdentifier) && p.Next().Type == tokeniser.TokenColon {
						fieldName = p.Consume().Value
						if _, duplicate := fieldNames[fieldName]; duplicate {
							return nil, shared.NewError(fieldLoc, "duplicate tagged union payload field %q", fieldName)
						}
						fieldNames[fieldName] = struct{}{}
						p.Inc()
					}
					fieldType, err := p.ParseType()
					if err != nil {
						return nil, err
					}
					variant.Fields = append(variant.Fields, StructField{Name: fieldName, Type: fieldType})
					if p.Match(tokeniser.TokenCloseParen) {
						break
					}
					if !p.Expect(tokeniser.TokenComma) {
						return nil, shared.NewError(p.PrevLoc(), "expected ',' after tagged union payload field")
					}
					for p.Match(tokeniser.TokenNewline) {
						p.Inc()
					}
				}
				if !p.Expect(tokeniser.TokenCloseParen) {
					return nil, shared.NewError(p.PrevLoc(), "expected ')' after tagged union payload")
				}
				variant.Loc = name.Loc.WithEnd(p.PrevLoc())
			}
			variants = append(variants, variant)
			if p.Match(tokeniser.TokenCloseCurly) {
				break
			}
			if !p.Expect(tokeniser.TokenComma) {
				return nil, shared.NewError(p.PrevLoc(), "expected ',' after tagged union variant")
			}
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			continue
		}
		if !p.Expect(tokeniser.TokenColon) {
			return nil, shared.NewError(p.PrevLoc(), "expected ':' after union field")
		}
		fieldType, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		fields = append(fields, StructField{Name: name.Name, Type: fieldType})
		if p.Match(tokeniser.TokenCloseCurly) {
			break
		}
		if !p.Expect(tokeniser.TokenComma) {
			return nil, shared.NewError(p.PrevLoc(), "expected ',' after union field")
		}
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
	}
	if !p.Expect(tokeniser.TokenCloseCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}' after union")
	}
	if tagType == nil && !autoTag && len(fields) == 0 {
		return nil, shared.NewError(beginLoc, "union must declare at least one field")
	}
	if autoTag && len(variants) == 0 {
		return nil, shared.NewError(beginLoc, "auto-tagged union must declare at least one variant")
	}
	return &UnionTypeNode{TagType: tagType, AutoTag: autoTag, Fields: fields, Variants: variants, Loc: p.SpanFrom(beginLoc)}, nil
}

func (p *Parser) ParseMatch(expression bool) (*MatchNode, error) {
	begin := p.CurrLoc()
	kw, ok := p.ExpectGet(tokeniser.TokenKeyword)
	if !ok || kw.Value != string(tokeniser.KeywordMatch) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'match'")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	var subjects []ExpressionNode
	for {
		subject, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}
		subjects = append(subjects, subject)
		if !p.Match(tokeniser.TokenComma) {
			break
		}
		p.Inc()
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
	}
	node := &MatchNode{Subjects: subjects, Expression: expression}
	var err error
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordAs) {
		if len(subjects) != 1 {
			return nil, shared.NewError(p.CurrLoc(), "a multi-subject match cannot use an 'as' binding")
		}
		p.Inc()
		binding, ok := p.ExpectGet(tokeniser.TokenIdentifier)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected binding name after 'as'")
		}
		node.BindingName, node.BindingLoc = binding.Value, binding.Loc
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' after match subject")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	for !p.Match(tokeniser.TokenCloseCurly) {
		armBegin := p.CurrLoc()
		var patterns []*MatchPatternNode
		for {
			pattern, err := p.parseMatchPattern()
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, pattern)
			if !p.Match(tokeniser.TokenComma) {
				break
			}
			p.Inc()
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
		}
		var guard ExpressionNode
		if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordIf) {
			p.Inc()
			guard, err = p.ParseExpression()
			if err != nil {
				return nil, err
			}
		}
		if !p.Expect(tokeniser.TokenFatArrow) {
			return nil, shared.NewError(p.PrevLoc(), "expected '=>' after match pattern")
		}
		if len(patterns) != len(subjects) && !(len(patterns) == 1 && patterns[0].Kind == MatchPatternWildcard) {
			return nil, shared.NewError(armBegin, "match arm has %d patterns for %d subjects", len(patterns), len(subjects))
		}
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		bodyStartsWithBlock := p.Match(tokeniser.TokenOpenCurly)
		var body ExpressionNode
		if !expression && p.Match(tokeniser.TokenOpenCurly) {
			body, err = p.ParseBlock()
		} else {
			body, err = p.ParseExpression()
		}
		if err != nil {
			return nil, err
		}
		node.Arms = append(node.Arms, MatchArmNode{
			Patterns: patterns, Guard: guard, Body: body, Loc: armBegin.WithEnd(body.GetLoc()),
		})
		if p.Match(tokeniser.TokenCloseCurly) {
			break
		}
		_, blockBody := body.(*BlockNode)
		blockBody = blockBody && bodyStartsWithBlock
		if p.Match(tokeniser.TokenComma) {
			p.Inc()
		} else if !blockBody {
			return nil, shared.NewError(p.PrevLoc(), "expected ',' after match arm")
		}
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
	}
	if len(node.Arms) == 0 {
		return nil, shared.NewError(begin, "match must declare at least one arm")
	}
	if !p.Expect(tokeniser.TokenCloseCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}' after match arms")
	}
	node.Loc = p.SpanFrom(begin)
	return node, nil
}

func (p *Parser) parseMatchPattern() (*MatchPatternNode, error) {
	begin := p.CurrLoc()
	first, err := p.parseMatchPatternTerm()
	if err != nil {
		return nil, err
	}
	alternatives := []*MatchPatternNode{first}
	for p.Match(tokeniser.TokenPipe) {
		p.Inc()
		alternative, err := p.parseMatchPatternTerm()
		if err != nil {
			return nil, err
		}
		alternatives = append(alternatives, alternative)
	}
	if len(alternatives) == 1 {
		return first, nil
	}
	return &MatchPatternNode{Kind: MatchPatternAlternative, Alternatives: alternatives, Loc: begin.WithEnd(alternatives[len(alternatives)-1].Loc)}, nil
}

func (p *Parser) parseMatchPatternTerm() (*MatchPatternNode, error) {
	begin := p.CurrLoc()
	pattern, err := p.parseMatchPatternAtom()
	if err != nil {
		return nil, err
	}
	if !p.Match(tokeniser.Token2Dots) {
		return pattern, nil
	}
	if pattern.Kind != MatchPatternLiteral {
		return nil, shared.NewError(pattern.Loc, "range pattern must start with a literal")
	}
	p.Inc()
	end, err := p.parseMatchPatternLiteral()
	if err != nil {
		return nil, shared.NewError(p.CurrLoc(), "expected literal after '..' in match range")
	}
	return &MatchPatternNode{Kind: MatchPatternRange, Start: pattern.Literal, End: end, Loc: begin.WithEnd(end.GetLoc())}, nil
}

func (p *Parser) parseMatchPatternAtom() (*MatchPatternNode, error) {
	begin := p.CurrLoc()
	if p.Match(tokeniser.TokenIdentifier) && p.Peek().Value == "_" {
		p.Inc()
		return &MatchPatternNode{Kind: MatchPatternWildcard, Loc: p.SpanFrom(begin)}, nil
	}
	if p.Match(tokeniser.TokenDot) {
		p.Inc()
		variant, ok := p.ExpectGet(tokeniser.TokenIdentifier)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected variant name after '.'")
		}
		pattern := &MatchPatternNode{Kind: MatchPatternVariant, Variant: variant.Value, Loc: p.SpanFrom(begin)}
		if p.Match(tokeniser.TokenOpenParen) {
			pattern.Payload = true
			p.Inc()
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
			for !p.Match(tokeniser.TokenCloseParen) {
				binding, ok := p.ExpectGet(tokeniser.TokenIdentifier)
				if !ok {
					return nil, shared.NewError(p.PrevLoc(), "expected payload binding in variant pattern")
				}
				entry := MatchBinding{Name: binding.Value, Loc: binding.Loc}
				if p.Match(tokeniser.TokenEquals) {
					p.Inc()
					entry.Field = entry.Name
					bound, ok := p.ExpectGet(tokeniser.TokenIdentifier)
					if !ok {
						return nil, shared.NewError(p.PrevLoc(), "expected binding name after '=' in variant pattern")
					}
					entry.Name, entry.Loc = bound.Value, bound.Loc
				}
				pattern.Bindings = append(pattern.Bindings, entry)
				if p.Match(tokeniser.TokenCloseParen) {
					break
				}
				if !p.Expect(tokeniser.TokenComma) {
					return nil, shared.NewError(p.PrevLoc(), "expected ',' after variant pattern binding")
				}
				for p.Match(tokeniser.TokenNewline) {
					p.Inc()
				}
			}
			if !p.Expect(tokeniser.TokenCloseParen) {
				return nil, shared.NewError(p.PrevLoc(), "expected ')' after variant pattern")
			}
			pattern.Loc = p.SpanFrom(begin)
		}
		return pattern, nil
	}
	literal, err := p.parseMatchPatternLiteral()
	if err != nil {
		return nil, err
	}
	return &MatchPatternNode{Kind: MatchPatternLiteral, Literal: literal, Loc: literal.GetLoc()}, nil
}

func (p *Parser) parseMatchPatternLiteral() (ExpressionNode, error) {
	begin := p.CurrLoc()
	switch {
	case p.Match(tokeniser.TokenNumber):
		tok := p.Consume()
		if strings.Contains(tok.Value, ".") {
			return &FloatLiteralNode{Value: tok.Value, Loc: p.SpanFrom(begin)}, nil
		}
		return &IntegerLiteralNode{Value: tok.Value, Loc: p.SpanFrom(begin)}, nil
	case p.Match(tokeniser.TokenChar):
		tok := p.Consume()
		return &CharLiteralNode{Value: tok.Value[0], Loc: p.SpanFrom(begin)}, nil
	case p.Match(tokeniser.TokenString):
		tok := p.Consume()
		return &StringLiteralNode{Value: tok.Value, Loc: p.SpanFrom(begin)}, nil
	case p.Match(tokeniser.TokenCString):
		tok := p.Consume()
		return &CStringLiteralNode{Value: tok.Value, Loc: p.SpanFrom(begin)}, nil
	case p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordTrue):
		p.Inc()
		return &BoolLiteralNode{Value: string(tokeniser.KeywordTrue), Loc: p.SpanFrom(begin)}, nil
	case p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordFalse):
		p.Inc()
		return &BoolLiteralNode{Value: string(tokeniser.KeywordFalse), Loc: p.SpanFrom(begin)}, nil
	default:
		return nil, shared.NewError(begin, "expected match pattern")
	}
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
	return &EnumTypeNode{Variants: variants, Values: values, Loc: p.SpanFrom(beginLoc)}, nil
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

func (p *Parser) ParseFlagsType() (*FlagsTypeNode, error) {
	beginLoc := p.CurrLoc()
	p.Inc()
	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '(' after flags")
	}
	underlying, err := p.ParseNamedType()
	if err != nil {
		return nil, err
	}
	if !p.Expect(tokeniser.TokenCloseParen) || !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' after flags underlying type")
	}
	known := map[string]*big.Int{}
	var variants, values []string
	valueMode := -1 // 0 is implicit, 1 is explicit.
	for {
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenCloseCurly) {
			p.Inc()
			break
		}
		name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected flag name")
		}
		if _, exists := known[name.Value]; exists {
			return nil, shared.NewError(name.Loc, "duplicate flag %q", name.Value)
		}
		hasExplicitValue := p.Match(tokeniser.TokenEquals)
		mode := 0
		if hasExplicitValue {
			mode = 1
		}
		if valueMode != -1 && valueMode != mode {
			return nil, shared.NewError(name.Loc, "cannot mix implicit and explicit flag values")
		}
		valueMode = mode
		var value *big.Int
		if hasExplicitValue {
			p.Inc()
			var err error
			value, err = p.parseFlagValue(known)
			if err != nil {
				return nil, err
			}
		} else {
			value = new(big.Int).Lsh(big.NewInt(1), uint(len(variants)))
		}
		known[name.Value] = value
		variants, values = append(variants, name.Value), append(values, value.String())
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenComma) {
			p.Inc()
			continue
		}
		if !p.Match(tokeniser.TokenCloseCurly) {
			return nil, shared.NewError(p.CurrLoc(), "expected ',' or '}' after flag")
		}
	}
	if len(variants) == 0 {
		return nil, shared.NewError(beginLoc, "flags must declare at least one member")
	}
	return &FlagsTypeNode{Underlying: underlying, Variants: variants, Values: values, Loc: p.SpanFrom(beginLoc)}, nil
}

func (p *Parser) parseFlagValue(known map[string]*big.Int) (*big.Int, error) {
	result := new(big.Int)
	for {
		var term *big.Int
		if p.Match(tokeniser.TokenIdentifier) {
			tok := p.Consume()
			value, ok := known[tok.Value]
			if !ok {
				return nil, shared.NewError(tok.Loc, "unknown earlier flag %q", tok.Value)
			}
			term = new(big.Int).Set(value)
		} else if p.Match(tokeniser.TokenNumber) {
			tok := p.Consume()
			if tok.NumberBase == 16 {
				term, _ = new(big.Int).SetString(tok.Value, 10)
			} else if tok.Value == "1" && p.Match(tokeniser.TokenShiftLeft) {
				p.Inc()
				shift, ok := p.ExpectGet(tokeniser.TokenNumber)
				if !ok || shift.NumberBase != 10 {
					return nil, shared.NewError(p.PrevLoc(), "expected decimal bit position after '1 <<'")
				}
				bit, ok := new(big.Int).SetString(shift.Value, 10)
				if !ok || !bit.IsUint64() {
					return nil, shared.NewError(shift.Loc, "invalid flag bit position")
				}
				term = new(big.Int).Lsh(big.NewInt(1), uint(bit.Uint64()))
			} else {
				return nil, shared.NewError(tok.Loc, "flag values must use hexadecimal, '1 << bit', or earlier flag names")
			}
		} else {
			return nil, shared.NewError(p.CurrLoc(), "expected hexadecimal value, '1 << bit', or earlier flag name")
		}
		result.Or(result, term)
		if !p.Match(tokeniser.TokenPipe) {
			break
		}
		p.Inc()
	}
	return result, nil
}

func (p *Parser) ParseStructType() (*StructTypeNode, error) {
	beginLoc := p.CurrLoc()

	if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordStruct) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'struct' keyword")
	}
	p.Inc()

	attrs, err := p.parseAttributes("")
	if err != nil {
		return nil, err
	}

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
			if p.Match(tokeniser.TokenCloseCurly) {
				break
			}
			if !p.Expect(tokeniser.TokenComma) {
				return nil, shared.NewError(p.PrevLoc(), "expected ',' after struct field")
			}
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

		if p.Match(tokeniser.TokenCloseCurly) {
			break
		}
		if !p.Expect(tokeniser.TokenComma) {
			return nil, shared.NewError(p.PrevLoc(), "expected ',' after struct field")
		}

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
	}

	if !p.Expect(tokeniser.TokenCloseCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}'")
	}

	return &StructTypeNode{
		Fields:     fields,
		Loc:        p.SpanFrom(beginLoc),
		Attributes: attrs,
	}, nil
}

func (p *Parser) ParseArrayOrSliceType() (TypeNode, error) {
	beginLoc := p.CurrLoc()

	if !p.Expect(tokeniser.TokenOpenSquare) {
		return nil, shared.NewError(p.PrevLoc(), "expected '[' to start slice type")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	if p.Match(tokeniser.TokenCloseSquare) {
		p.Inc()
		mutable := false
		if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordMut) {
			mutable = true
			p.Inc()
		}
		elementType, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		return &SliceTypeNode{ElementType: elementType, Mutable: mutable, Loc: p.SpanFrom(beginLoc)}, nil
	}

	length, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if !p.Expect(tokeniser.TokenCloseSquare) {
		return nil, shared.NewError(p.PrevLoc(), "expected ']' after array length")
	}
	elementType, err := p.ParseType()
	if err != nil {
		return nil, err
	}
	return &ArrayTypeNode{ElementType: elementType, Length: length, Loc: p.SpanFrom(beginLoc)}, nil
}

func (p *Parser) ParsePointerType() (TypeNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TokenAsterisk) {
		return nil, shared.NewError(p.PrevLoc(), "expected '*' to start pointer type")
	}

	mutable := false
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordMut) {
		p.Inc()
		mutable = true
	}
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordDyn) {
		return p.ParseDynType(beginLoc, mutable)
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
		Loc:      p.SpanFrom(beginLoc),
	}, nil
}

func (p *Parser) ParseFunctionType() (*FunctionTypeNode, error) {
	beginLoc := p.CurrLoc()
	p.Inc() // (
	var params []TypeNode
	typedVariadic := false
	for {
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenCloseParen) {
			break
		}
		isTypedVariadic := p.Match(tokeniser.Token3Dots)
		if isTypedVariadic {
			p.Inc()
		}
		param, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		if isTypedVariadic {
			param = &SliceTypeNode{ElementType: param, Loc: param.GetLoc()}
			typedVariadic = true
		}
		params = append(params, param)
		if isTypedVariadic {
			if p.Match(tokeniser.TokenComma) {
				return nil, shared.NewError(p.CurrLoc(), "typed variadic parameter must be last")
			}
			break
		}
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
	return &FunctionTypeNode{Parameters: params, ReturnType: ret, TypedVariadic: typedVariadic, Loc: p.SpanFrom(beginLoc)}, nil
}

func (p *Parser) ParseNamedType() (*NamedTypeNode, error) {
	beginLoc := p.CurrLoc()
	capture := false
	if p.Match(tokeniser.TokenDollar) {
		if !p.allowTypeCapture {
			return nil, shared.NewError(p.CurrLoc(), "type captures are only valid in method owner patterns")
		}
		capture = true
		p.Inc()
	}
	ident, err := p.ParseIdent()
	if err != nil {
		return nil, err
	}

	if !p.Match(tokeniser.TokenDot) {
		node := &NamedTypeNode{
			ModName: "",
			Name:    ident.Name,
			Capture: capture,
			Loc:     p.SpanFrom(beginLoc),
		}
		if p.Match(tokeniser.TokenOpenParen) {
			if capture {
				return nil, shared.NewError(node.Loc, "captured type parameter %q cannot accept type arguments", node.Name)
			}
			arguments, err := p.parseCallTypeArguments()
			if err != nil {
				return nil, err
			}
			node.TypeArguments = arguments
			node.Loc = p.SpanFrom(beginLoc)
		}
		if capture && p.Match(tokeniser.TokenColon) {
			p.Inc()
			constraint, err := p.ParseType()
			if err != nil {
				return nil, err
			}
			node.CaptureConstraint = constraint
			node.Loc = p.SpanFrom(beginLoc)
		}
		return node, nil
	}
	if capture {
		return nil, shared.NewError(beginLoc, "captured type parameter cannot be module-qualified")
	}
	parts := []string{ident.Name}
	for p.Match(tokeniser.TokenDot) {
		p.Inc()
		realIdent, err := p.ParseIdent()
		if err != nil {
			return nil, err
		}
		parts = append(parts, realIdent.Name)
	}

	node := &NamedTypeNode{
		ModName: strings.Join(parts[:len(parts)-1], "."),
		Name:    parts[len(parts)-1],
		Loc:     p.SpanFrom(beginLoc),
	}
	if p.Match(tokeniser.TokenOpenParen) {
		arguments, err := p.parseCallTypeArguments()
		if err != nil {
			return nil, err
		}
		node.TypeArguments = arguments
		node.Loc = p.SpanFrom(beginLoc)
	}
	return node, nil
}

func (p *Parser) parseCallTypeArguments() ([]TypeNode, error) {
	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '('")
	}
	var arguments []TypeNode
	for {
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenCloseParen) {
			p.Inc()
			return arguments, nil
		}
		argument, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, argument)
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenCloseParen) {
			p.Inc()
			return arguments, nil
		}
		if !p.Expect(tokeniser.TokenComma) {
			return nil, shared.NewError(p.PrevLoc(), "expected ',' or ')' in type argument list")
		}
	}
}
