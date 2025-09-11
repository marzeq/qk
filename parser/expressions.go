package parser

import (
	"github.com/marzeq/quokka/shared"
	"github.com/marzeq/quokka/tokeniser"
)

func (p *Parser) ParseExpression() (ExpressionNode, error) {
	return p.ParseLogicalOr()
}

func (p *Parser) ParseLogicalOr() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseLogicalAnd()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "or" {
		p.Inc()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		right, err := p.ParseLogicalAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{
			Op:       BINARY_OP_LOGICAL_OR,
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

	for p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "and" {
		p.Inc()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		right, err := p.ParseLogicalNot()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{
			Op:       BINARY_OP_LOGICAL_AND,
			Operand1: left,
			Operand2: right,
			Loc:      beginLoc,
		}
	}

	return left, nil
}

func (p *Parser) ParseLogicalNot() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "not" {
		p.Inc()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		expr, err := p.ParseLogicalNot()
		if err != nil {
			return nil, err
		}

		return &UnaryOpNode{
			Op:      UNARY_OP_LOGICAL_NOT,
			Operand: expr,
			Loc:     beginLoc,
		}, nil
	}

	return p.ParseComparison()
}

func (p *Parser) ParseUnary() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	if p.Match(tokeniser.TOKEN_TYPE_MINUS, tokeniser.TOKEN_TYPE_ASTERISK, tokeniser.TOKEN_TYPE_AMPERSAND) {
		op := p.Consume()
		var val UnaryOpType
		switch op.Type {
		case tokeniser.TOKEN_TYPE_MINUS:
			val = UNARY_OP_NEGATE
		case tokeniser.TOKEN_TYPE_ASTERISK:
			val = UNARY_OP_DEREFERENCE
		case tokeniser.TOKEN_TYPE_AMPERSAND:
			val = UNARY_OP_REFERENCE
		}
		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
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

	return p.ParseTerm()
}

func (p *Parser) ParseComparison() (ExpressionNode, error) {
	beginLoc := p.CurrLoc()
	left, err := p.ParseAddSub()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TOKEN_TYPE_EQUALS_EQUALS, tokeniser.TOKEN_TYPE_NOT_EQUALS, tokeniser.TOKEN_TYPE_LESS, tokeniser.TOKEN_TYPE_GREATER, tokeniser.TOKEN_TYPE_LESS_EQUALS, tokeniser.TOKEN_TYPE_GREATER_EQUALS) {
		op := p.Consume()
		var val BinaryOpType
		switch op.Type {
		case tokeniser.TOKEN_TYPE_EQUALS_EQUALS:
			val = BINARY_OP_EQUAL
		case tokeniser.TOKEN_TYPE_NOT_EQUALS:
			val = BINARY_OP_NOT_EQUAL
		case tokeniser.TOKEN_TYPE_LESS:
			val = BINARY_OP_LESS
		case tokeniser.TOKEN_TYPE_GREATER:
			val = BINARY_OP_GREATER
		case tokeniser.TOKEN_TYPE_LESS_EQUALS:
			val = BINARY_OP_LESS_EQUAL
		case tokeniser.TOKEN_TYPE_GREATER_EQUALS:
			val = BINARY_OP_GREATER_EQUAL
		}
		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
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

	for p.Match(tokeniser.TOKEN_TYPE_PLUS, tokeniser.TOKEN_TYPE_MINUS) {
		op := p.Consume()
		var val BinaryOpType
		if op.Type == tokeniser.TOKEN_TYPE_PLUS {
			val = BINARY_OP_ADD
		} else {
			val = BINARY_OP_SUBTRACT
		}

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
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

	for p.Match(tokeniser.TOKEN_TYPE_ASTERISK, tokeniser.TOKEN_TYPE_SLASH, tokeniser.TOKEN_TYPE_PERCENT) {
		op := p.Consume()
		var val BinaryOpType
		switch op.Type {
		case tokeniser.TOKEN_TYPE_ASTERISK:
			val = BINARY_OP_MULTIPLY
		case tokeniser.TOKEN_TYPE_SLASH:
			val = BINARY_OP_DIVIDE
		case tokeniser.TOKEN_TYPE_PERCENT:
			val = BINARY_OP_MODULO
		default:
			return nil, shared.NewError(p.PrevLoc(), "unexpected operator %s", op)
		}
		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
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
	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "if" {
		expr, err := p.ParseIfExpression()
		if err != nil {
			return nil, err
		}

		return expr, nil
	}

	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "given" {
		expr, err := p.ParseGivenExpression()
		if err != nil {
			return nil, err
		}

		return expr, nil
	}

	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == "cast" {
		expr, err := p.ParseCast()
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
		ident, err := p.ParseIdent()
		if err != nil {
			return nil, err
		}

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		if p.Match(tokeniser.TOKEN_TYPE_COLON) {
			p.Inc()

			if ident.Next != nil {
				return nil, shared.NewError(ident.Loc, "module name cannot be a qualified identifier")
			}

			for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
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

			if p.Match(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
				return p.ParseStructLiteral(modAN)
			}

			if p.Match(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
				return p.ParseFunctionCall(modAN)
			}

			return modAN, nil
		}

		if p.Match(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
			modAN := &ModuleAccessNode{
				ModName: "",
				Ident:   ident,
				Loc:     ident.Loc,
			}
			return p.ParseFunctionCall(modAN)
		}

		if p.Match(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
			modAN := &ModuleAccessNode{
				ModName: "",
				Ident:   ident,
				Loc:     ident.Loc,
			}
			return p.ParseStructLiteral(modAN)
		}

		return ident, nil
	}

	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) {
		val := p.Peek().Value

		switch val {
		case "true", "false":
			bLit := p.Consume()
			return &BoolLiteralNode{
				Value: bLit.Value,
				Loc:   beginLoc,
			}, nil
		case "nil":
			p.Inc()
			return &NilLiteralNode{
				Loc: beginLoc,
			}, nil
		}
	}

	if p.Match(tokeniser.TOKEN_TYPE_NUMBER) {
		nLit := p.Consume()
		return &NumberLiteralNode{
			Value: nLit.Value,
			Loc:   beginLoc,
		}, nil
	}

	if p.Match(tokeniser.TOKEN_TYPE_CHAR) {
		cLit := p.Consume()
		return &CharLiteralNode{
			Value: []byte(cLit.Value)[0],
			Loc:   beginLoc,
		}, nil
	}

	if p.Match(tokeniser.TOKEN_TYPE_STRING) {
		sLit := p.Consume()
		return &StringLiteralNode{
			Value: sLit.Value,
			Loc:   beginLoc,
		}, nil
	}

	return nil, shared.NewError(p.CurrLoc(), "unexpected token %s", p.Peek())
}

func (p *Parser) ParseCast() (*CastNode, error) {
	beginLoc := p.CurrLoc()

	if t, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_KEYWORD); !ok || t.Value != "cast" {
		return nil, shared.NewError(beginLoc, "expected 'cast' keyword")
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
		return nil, shared.NewError(p.PrevLoc(), "expected '(")
	}
	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	tpe, err := p.ParseType()
	if err != nil {
		return nil, err
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_COMMA) {
		return nil, shared.NewError(p.PrevLoc(), "expected ','")
	}
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

	return &CastNode{
		ToType:  tpe,
		Operand: expr,
		Loc:     beginLoc,
	}, nil
}

func (p *Parser) ParseFunctionCall(name *ModuleAccessNode) (*FunctionCallNode, error) {
	var args []ExpressionNode

	if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
		return nil, shared.NewError(p.PrevLoc(), "expected '('")
	}

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

	node := &IfExprNode{
		Loc: beginLoc,
		IfBranch: IfExprBranch{
			Condition: condition,
			Node:      thenBlock,
		},
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
	if !p.Expect(tokeniser.TOKEN_TYPE_KEYWORD) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'given' keyword")
	}
	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}
	block, err := p.ParseBlock()
	if err != nil {
		return nil, err
	}
	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}
	if !p.Expect(tokeniser.TOKEN_TYPE_ARROW) {
		return nil, shared.NewError(p.PrevLoc(), "expected '->'")
	}
	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
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

func (p *Parser) ParseStructLiteral(name *ModuleAccessNode) (*StructLiteralNode, error) {
	beginLoc := p.CurrLoc()

	if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' to start struct literal")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	node := &StructLiteralNode{
		Name: name,
		Loc:  beginLoc,
	}
	for {
		if p.Match(tokeniser.TOKEN_TYPE_CLOSE_CURLY) {
			break
		}

		fieldName, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected field name")
		}

		if !p.Expect(tokeniser.TOKEN_TYPE_EQUALS) {
			return nil, shared.NewError(p.PrevLoc(), "expected '='")
		}

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		fieldValue, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}

		node.Fields = append(node.Fields, shared.Pair[string, ExpressionNode]{L: fieldName.Value, R: fieldValue})

		if !p.Match(tokeniser.TOKEN_TYPE_COMMA, tokeniser.TOKEN_TYPE_NEWLINE) {
			break
		}
		p.Inc()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_CLOSE_CURLY) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}' to end struct literal")
	}

	return node, nil
}

func (p *Parser) ParseIdent() (*IdentifierNode, error) {
	beginLoc := p.CurrLoc()
	firstIdent, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected identifier")
	}
	node := &IdentifierNode{
		Name: firstIdent.Value,
		Loc:  beginLoc,
	}
	currnode := node

	for p.Match(tokeniser.TOKEN_TYPE_DOT) {
		p.Inc()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		loc := p.CurrLoc()
		nextIdent, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected identifier")
		}
		currnode.Next = &IdentifierNode{
			Name: nextIdent.Value,
			Loc:  loc,
		}
		currnode = currnode.Next
	}

	return node, nil
}

func (p *Parser) ParseType() (*TypeNode, error) {
	beginLoc := p.CurrLoc()

	pointerLevel := 0
	for p.Match(tokeniser.TOKEN_TYPE_ASTERISK) {
		p.Inc()
		pointerLevel++
	}

	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) {
		kw := p.Consume().Value
		return &TypeNode{
			Name:         kw,
			PointerLevel: pointerLevel,
			Loc:          beginLoc,
		}, nil
	}

	firstIdent, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected type name, module name or asterisk")
	}
	modName := ""
	if p.Match(tokeniser.TOKEN_TYPE_COLON) {
		p.Inc()
		modName = firstIdent.Value
		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}
		tn, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		if pointerLevel > 0 && tn.PointerLevel > 0 {
			return nil, shared.NewError(beginLoc, "you must either put all asterisks before or after the module name")
		}
		tn.ModName = modName
		tn.PointerLevel += pointerLevel
		return tn, nil
	}

	node := &TypeNode{
		Name:         firstIdent.Value,
		PointerLevel: pointerLevel,
		Loc:          beginLoc,
	}

	return node, nil
}
