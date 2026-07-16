package parser

import (
	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

func (p *Parser) ParseBlock() (*BlockNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' to start block")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	var children []Node
	for !p.Match(tokeniser.TokenCloseCurly, tokeniser.TokenEof) {
		stmt, semiNeeded, err := p.ParseStatement()
		if err != nil {
			return nil, err
		}
		children = append(children, stmt)

		if p.Match(tokeniser.TokenCloseCurly) {
			break
		}

		if semiNeeded && !p.Match(tokeniser.TokenSemicolon, tokeniser.TokenNewline) {
			return nil, shared.NewError(p.CurrLoc(),
				"expected ';' or '\\n' to end statement")
		}

		for p.Match(tokeniser.TokenSemicolon, tokeniser.TokenNewline) {
			p.Inc()
		}
	}

	if !p.Expect(tokeniser.TokenCloseCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}' to close block")
	}

	return &BlockNode{
		Body: children,
		Loc:  beginLoc,
	}, nil
}

func (p *Parser) ParseFunctionDefinition() (*FunctionDefNode, error) {
	beginLoc := p.CurrLoc()

	extern := false

	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordExtern) {
		extern = true
		p.Inc()
	}

	if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordLet) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'let' keyword")
	}
	p.Inc()

	name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected function name")
	}

	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '('")
	}

	var args []*FunctionNodeArg
	variadic := false
	for !p.Match(tokeniser.TokenCloseParen) {
		if p.Match(tokeniser.Token3Dots) {
			p.Inc()
			variadic = true
			break
		}

		mutable := false
		if p.Match(tokeniser.TokenKeyword) {
			kw := p.Consume().Value
			if kw == string(tokeniser.KeywordMut) {
				mutable = true
			} else {
				return nil, shared.NewError(p.CurrLoc(),
					"expected either 'mut' or argument name")
			}
		}

		arg, err := p.ParseIdent()
		if err != nil {
			return nil, err
		}

		if !p.Expect(tokeniser.TokenColon) {
			return nil, shared.NewError(p.PrevLoc(), "expected ':'")
		}

		argType, err := p.ParseType()
		if err != nil {
			return nil, err
		}

		args = append(args, &FunctionNodeArg{
			Name:    arg.Name,
			Type:    argType,
			Mutable: mutable,
		})

		if !p.Match(tokeniser.TokenComma) {
			break
		}
		p.Consume()
	}

	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')'")
	}

	var retType TypeNode
	if p.Match(tokeniser.TokenColon) {
		p.Inc()
		argType, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		retType = argType
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	attrs := attributes.Attributes{}

	expectsBody := true

	for p.Match(tokeniser.TokenAt) {
		attrName, attrArgs, err := p.ParseAttribute()
		if err != nil {
			return nil, err
		}

		switch attributes.AttributeType(attrName) {
		case attributes.AttributeTypeInline:
			if len(attrArgs) > 0 {
				return nil, shared.NewError(p.PrevLoc(), "inline attribute does not take any arguments")
			}
			attrs = append(attrs, attributes.FunctionAttributeInline{})
		case attributes.AttributeTypeNoInline:
			if len(attrArgs) > 0 {
				return nil, shared.NewError(p.PrevLoc(), "noinline attribute does not take any arguments")
			}
			attrs = append(attrs, attributes.FunctionAttributeNoInline{})
		case attributes.AttributeTypeNoReturn:
			if len(attrArgs) > 0 {
				return nil, shared.NewError(p.PrevLoc(), "noreturn attribute does not take any arguments")
			}
			attrs = append(attrs, attributes.FunctionAttributeNoReturn{})
		case attributes.AttributeTypeForeign:
			fgnNameStr := name.Value
			if len(attrArgs) == 0 {
			} else if len(attrArgs) == 1 {
				nameNode, ok := attrArgs[0].(*StringLiteralNode)
				if !ok {
					return nil, shared.NewError(p.PrevLoc(), "foreign attribute argument must be a string literal")
				}
				fgnNameStr = nameNode.Value
			} else {
				return nil, shared.NewError(p.PrevLoc(), "foreign attribute takes at most one argument")
			}
			attrs = append(attrs, attributes.FunctionAttributeForeign{From: fgnNameStr})
			expectsBody = false
		default:
			return nil, shared.NewError(p.PrevLoc(), "unknown function attribute: %s", attrName)
		}
	}

	var body Node

	if expectsBody {
		if p.Match(tokeniser.TokenOpenCurly) {
			b, err := p.ParseBlock()
			if err != nil {
				return nil, err
			}
			body = b
		} else if p.Match(tokeniser.TokenEquals) {
			p.Inc() // consume '='

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			b, err := p.ParseExpression()
			if err != nil {
				return nil, err
			}
			body = b
		} else {
			return nil, shared.NewError(p.PrevLoc(), "expected function body as either block or expression after '='")
		}
	}

	return &FunctionDefNode{
		Name:        name.Value,
		Args:        args,
		RetTypeNode: retType,
		Body:        body,
		Extern:      extern,
		Loc:         beginLoc,
		Attributes:  attrs,
		HasVariadic: variadic,
	}, nil
}

func (p *Parser) ParseAttribute() (string, []Node, error) {
	if !p.Expect(tokeniser.TokenAt) {
		return "", nil, shared.NewError(p.PrevLoc(), "expected '@' for attribute")
	}
	nameIdent, err := p.ParseIdent()
	if err != nil {
		return "", nil, err
	}
	if !p.Match(tokeniser.TokenOpenParen) {
		return nameIdent.Name, nil, nil
	}
	p.Inc()

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	var args []Node
	for !p.Match(tokeniser.TokenCloseParen) {
		arg, err := p.ParseExpression()
		if err != nil {
			return "", nil, err
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
	p.Inc() // consume ')'

	return nameIdent.Name, args, nil
}

func (p *Parser) ParseTypeAlias() (*TypeAliasNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordLet) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'let' keyword")
	}
	p.Inc()

	name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected type alias name")
	}

	if !p.Expect(tokeniser.TokenEquals) {
		return nil, shared.NewError(p.PrevLoc(), "expected '='")
	}

	if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordType) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'type' keyword")
	}
	p.Inc()

	tpe, err := p.ParseType()
	if err != nil {
		return nil, err
	}

	return &TypeAliasNode{
		Name: name.Value,
		Type: tpe,
		Loc:  beginLoc,
	}, nil
}

func (p *Parser) ParseImport() (*ImportNode, error) {
	beginLoc := p.CurrLoc()
	if kw, ok := p.ExpectGet(tokeniser.TokenKeyword); !ok ||
		kw.Value != string(tokeniser.KeywordImport) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'import' keyword")
	}

	modules := []string{}

	if p.Match(tokeniser.TokenOpenParen) {
		p.Inc()

		for {
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			if p.Match(tokeniser.TokenCloseParen) {
				p.Inc()
				break
			}

			name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
			if !ok {
				return nil, shared.NewError(p.PrevLoc(), "expected module name")
			}
			modules = append(modules, name.Value)

			if p.Match(tokeniser.TokenComma) {
				p.Inc()

				for p.Match(tokeniser.TokenNewline) {
					p.Inc()
				}
				continue
			}

			if p.Match(tokeniser.TokenNewline) {
				p.Inc()
				continue
			}

			if p.Match(tokeniser.TokenCloseParen) {
				p.Inc()
				break
			}

			return nil, shared.NewError(p.PrevLoc(), "expected ',', newline, or ')'")
		}
	} else {
		name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected module name")
		}
		modules = append(modules, name.Value)
	}

	return &ImportNode{
		Modules: modules,
		Loc:     beginLoc,
	}, nil
}

func (p *Parser) ParseModule() (*ModuleNode, error) {
	beginLoc := p.CurrLoc()
	if kw, ok := p.ExpectGet(tokeniser.TokenKeyword); !ok ||
		kw.Value != string(tokeniser.KeywordModule) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'module' keyword")
	}

	nameTok, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected module name")
	}
	name := nameTok.Value

	return &ModuleNode{
		Name: name,
		Loc:  beginLoc,
	}, nil
}

func (p *Parser) ParseStatement() (Node, bool, error) {
	if p.Match(tokeniser.TokenKeyword) {
		kw := p.Peek().Value
		switch kw {
		case string(tokeniser.KeywordLet):
			p.PushPos()
			p.Inc() // consume `let`

			if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordMut) {
				p.PopPos()
				node, err := p.ParseDeclaration()
				return node, true, err
			}

			if !p.Expect(tokeniser.TokenIdentifier) {
				return nil, false, shared.NewError(p.PrevLoc(), "expected name")
			}

			switch {
			case p.Match(tokeniser.TokenOpenParen):
				// let fn(...) = ...
				p.PopPos()
				node, err := p.ParseFunctionDefinition()
				return node, true, err

			case p.Match(tokeniser.TokenColon):
				// let x: T = ...
				p.PopPos()
				node, err := p.ParseDeclaration()
				return node, true, err

			case p.Match(tokeniser.TokenEquals):
				p.Inc() // look past '='

				if p.Match(tokeniser.TokenKeyword) &&
					p.Peek().Value == string(tokeniser.KeywordType) {

					// let A = type ...
					p.PopPos()
					node, err := p.ParseTypeAlias()
					return node, true, err
				}

				// let x = ...
				p.PopPos()
				node, err := p.ParseDeclaration()
				return node, true, err
			}

			return nil, false, shared.NewError(p.PrevLoc(), "invalid let statement")
		case string(tokeniser.KeywordExtern):
			node, err := p.ParseFunctionDefinition()
			return node, true, err
		case
			string(tokeniser.KeywordReturn),
			string(tokeniser.KeywordBreak),
			string(tokeniser.KeywordContinue):
			node, err := p.ParseControlKeyword()
			return node, true, err
		case string(tokeniser.KeywordIf):
			node, err := p.ParseIfStatement()
			return node, false, err
		case string(tokeniser.KeywordFor):
			node, err := p.ParseForLoop()
			return node, false, err
		default:
			return nil, false, shared.NewError(p.CurrLoc(), "unexpected keyword: %s", kw)
		}
	}

	if p.Match(tokeniser.TokenOpenCurly) {
		node, err := p.ParseBlock()
		return node, true, err
	}

	expr, err := p.ParseExpression()
	if err != nil {
		return nil, false, err
	}

	if p.Match(tokeniser.TokenEquals) {
		p.Inc()
		parsed, err := p.ParseAssignment(expr)
		if err != nil {
			return nil, false, err
		}
		return parsed, true, nil
	}

	if p.Match(tokeniser.TokenIncBy, tokeniser.TokenDecBy, tokeniser.TokenMulBy, tokeniser.TokenDivBy, tokeniser.TokenModBy) {
		parsed, err := p.ParseCompoundAssignment(p.Consume(), expr)
		if err != nil {
			return nil, false, err
		}
		return parsed, true, nil
	}

	switch expr.(type) {
	case *FunctionCallNode:
		return expr, true, nil
	default:
		return nil, false, shared.NewError(p.CurrLoc(), "expected a valid statement")
	}
}

func (p *Parser) ParseDeclaration() (*DeclarationNode, error) {
	beginLoc := p.CurrLoc()

	kw, ok := p.ExpectGet(tokeniser.TokenKeyword)
	if !ok || kw.Value != string(tokeniser.KeywordLet) {
		return nil, shared.NewError(p.PrevLoc(), "expected `let` keyword")
	}

	mutable := false
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordMut) {
		p.Inc()
		mutable = true
	}

	ident, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected name")
	}

	var tpe TypeNode
	if p.Match(tokeniser.TokenColon) {
		p.Inc()

		t, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		tpe = t
	}

	if !p.Expect(tokeniser.TokenEquals) {
		return nil, shared.NewError(p.PrevLoc(), "expected '='")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	expr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	return &DeclarationNode{
		Name:     ident.Value,
		TypeNode: tpe,
		Mutable:  mutable,
		Value:    expr,
		Loc:      beginLoc,
	}, nil
}

func (p *Parser) ParseAssignment(subj ExpressionNode) (*AssignmentNode, error) {
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	expr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	return &AssignmentNode{
		Assignee: subj,
		Value:    expr,
		Loc:      subj.GetLoc(),
	}, err
}

func (p *Parser) ParseCompoundAssignment(opTok tokeniser.Token, subj ExpressionNode) (*AssignmentNode, error) {
	var op BinaryOpKind

	switch opTok.Type {
	case tokeniser.TokenIncBy:
		op = BinaryOpAdd
	case tokeniser.TokenDecBy:
		op = BinaryOpSubtract
	case tokeniser.TokenMulBy:
		op = BinaryOpMultiply
	case tokeniser.TokenDivBy:
		op = BinaryOpDivide
	case tokeniser.TokenModBy:
		op = BinaryOpModulo
	default:
		return nil, shared.NewError(opTok.Loc, "unexpected compound assignment operator %s", opTok)
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	expr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	return &AssignmentNode{
		Assignee: subj,
		Value: &BinaryOpNode{
			Op:       op,
			Operand1: subj,
			Operand2: expr,
			Loc:      expr.GetLoc(),
		},
		Loc: subj.GetLoc(),
	}, nil
}

func (p *Parser) ParseIfStatement() (*IfNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TokenKeyword) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'if'")
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

	thenBlock, err := p.ParseBlock()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	node := &IfNode{
		Loc: beginLoc,
		IfBranch: IfBranch{
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

			elseifBlock, err := p.ParseBlock()
			if err != nil {
				return nil, err
			}

			elseIfBranch := IfBranch{
				Condition: elseifCondition,
				Node:      elseifBlock,
			}
			node.ElseIfBranches = append(node.ElseIfBranches, elseIfBranch)
		} else {
			elseBlock, err := p.ParseBlock()
			if err != nil {
				return nil, err
			}

			node.ElseBranch = elseBlock
			break
		}
	}

	return node, nil
}

func (p *Parser) ParseControlKeyword() (*ControlKeywordNode, error) {
	loc := p.CurrLoc()
	kw, ok := p.ExpectGet(tokeniser.TokenKeyword)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected 'return', 'break' or 'continue'")
	}
	if kw.Value != string(tokeniser.KeywordReturn) &&
		kw.Value != string(tokeniser.KeywordBreak) &&
		kw.Value != string(tokeniser.KeywordContinue) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'return', 'break' or 'continue'")
	}

	var expr ExpressionNode
	if kw.Value == string(tokeniser.KeywordReturn) && !p.Match(tokeniser.TokenNewline, tokeniser.TokenSemicolon) {
		got, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}
		expr = got
	}

	return &ControlKeywordNode{
		Keyword:     tokeniser.KeywordKind(kw.Value),
		ReturnValue: expr,
		Loc:         loc,
	}, nil
}

func (p *Parser) ParseForLoop() (Node, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TokenKeyword) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'for'")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	if p.Match(tokeniser.TokenIdentifier) &&
		p.Next().Type == tokeniser.TokenKeyword &&
		p.Next().Value == string(tokeniser.KeywordIn) {
		return p.parseRangeOrForEach(beginLoc)
	}

	var err error

	exprsOrStmts := []Node{}

	for !p.Match(tokeniser.TokenOpenCurly) {
		ogLoc := p.CurrLoc()
		ogPos := p.pos
		exOrSt, _, err := p.ParseStatement()
		if err != nil {
			p.pos = ogPos
			exOrSt, err = p.ParseExpression()
			if err != nil {
				return nil, shared.NewError(ogLoc, "expected a valid statement or expression")
			}
		}

		exprsOrStmts = append(exprsOrStmts, exOrSt)

		if p.Match(tokeniser.TokenOpenCurly) {
			break
		}

		if !p.Expect(tokeniser.TokenSemicolon) {
			return nil, shared.NewError(p.PrevLoc(),
				"expected ';' to end statement or expression")
		}

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	body, err := p.ParseBlock()
	if err != nil {
		return nil, err
	}

	return &ForNode{
		ExprsOrStmts: exprsOrStmts,
		Body:         body,
		Loc:          beginLoc,
	}, nil
}

func (p *Parser) parseRangeOrForEach(beginLoc shared.Location) (Node, error) {
	name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected loop variable name")
	}
	if kw, ok := p.ExpectGet(tokeniser.TokenKeyword); !ok || kw.Value != string(tokeniser.KeywordIn) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'in' in for loop")
	}

	p.parsingForEachIterable = true
	iterable, err := p.ParseExpression()
	p.parsingForEachIterable = false
	if err != nil {
		return nil, err
	}

	if p.Match(tokeniser.Token2Dots) {
		p.Inc()
		inclusive := p.Match(tokeniser.TokenEquals)
		if inclusive {
			p.Inc()
		}

		// Suppress struct-literal parsing for the range bound as well: the
		// following loop body starts with '{', which would otherwise be consumed
		// as a struct literal after an identifier bound.
		p.parsingForEachIterable = true
		end, err := p.ParseExpression()
		p.parsingForEachIterable = false
		if err != nil {
			return nil, err
		}
		body, err := p.ParseBlock()
		if err != nil {
			return nil, err
		}
		return &RangeForNode{
			Name:      name.Value,
			Start:     iterable,
			End:       end,
			Inclusive: inclusive,
			Body:      body,
			Loc:       beginLoc,
		}, nil
	}

	body, err := p.ParseBlock()
	if err != nil {
		return nil, err
	}
	return &ForEachNode{
		Name:     name.Value,
		Iterable: iterable,
		Body:     body,
		Loc:      beginLoc,
	}, nil
}
