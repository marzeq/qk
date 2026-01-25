package parser

import (
	"github.com/marzeq/quokka/shared"
	"github.com/marzeq/quokka/tokeniser"
)

func (p *Parser) ParseBlock() (*BlockNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' to start block")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	var children []Node
	for !p.Match(tokeniser.TOKEN_TYPE_CLOSE_CURLY, tokeniser.TOKEN_TYPE_EOF) {
		stmt, semiNeeded, err := p.ParseStatement()
		if err != nil {
			return nil, err
		}
		children = append(children, stmt)

		if p.Match(tokeniser.TOKEN_TYPE_CLOSE_CURLY) {
			break
		}

		if semiNeeded && !p.Match(tokeniser.TOKEN_TYPE_SEMICOLON, tokeniser.TOKEN_TYPE_NEWLINE) {
			return nil, shared.NewError(p.CurrLoc(),
				"expected ';' or '\\n' to end statement")
		}

		for p.Match(tokeniser.TOKEN_TYPE_SEMICOLON, tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_CLOSE_CURLY) {
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

	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == string(tokeniser.KEYWORD_EXTERN) {
		extern = true
		p.Inc()
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_KEYWORD) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'let' keyword")
	}

	name, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected function name")
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
		return nil, shared.NewError(p.PrevLoc(), "expected '('")
	}

	var args []FunctionNodeType
	variadic := false
	for !p.Match(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
		if p.Match(tokeniser.TOKEN_TYPE_3DOTS) {
			p.Inc()
			variadic = true
			break
		}

		mutable := false
		if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) {
			kw := p.Consume().Value
			if kw == string(tokeniser.KEYWORD_VAR) {
				mutable = true
			} else {
				return nil, shared.NewError(p.CurrLoc(),
					"expected either 'var' or argument name")
			}
		}

		arg, err := p.ParseIdent()
		if err != nil {
			return nil, err
		}
		if arg.Next != nil {
			return nil, shared.NewError(arg.Next.Loc, "argument names cannot be qualified")
		}

		if !p.Expect(tokeniser.TOKEN_TYPE_COLON) {
			return nil, shared.NewError(p.PrevLoc(), "expected ':'")
		}

		argType, err := p.ParseType()
		if err != nil {
			return nil, err
		}

		args = append(args, FunctionNodeType{
			Name:    arg.Name,
			Type:    argType,
			Mutable: mutable,
		})

		if !p.Match(tokeniser.TOKEN_TYPE_COMMA) {
			break
		}
		p.Consume()
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')'")
	}

	retType := FunctionNodeType{
		Type: nil,
	}
	if p.Match(tokeniser.TOKEN_TYPE_COLON) {
		p.Inc()
		argType, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		retType = FunctionNodeType{
			Type: argType,
		}
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_EQUALS) {
		return nil, shared.NewError(p.PrevLoc(), "expected '='")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	var body Node

	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == string(tokeniser.KEYWORD_EXTERN) {
		p.Inc()
		if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
			return nil, shared.NewError(p.PrevLoc(), "expected '(' after 'extern'")
		}
		externNameTok, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_STRING)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(),
				"expected string literal for extern function name")
		}
		if !p.Expect(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
			return nil, shared.NewError(p.PrevLoc(),
				"expected ')' after extern function name")
		}
		return &FunctionDefNode{
			Name:        name.Value,
			Args:        args,
			RetType:     retType,
			ExternFrom:  externNameTok.Value,
			HasVariadic: variadic,
			Loc:         beginLoc,
		}, nil
	}

	if p.Match(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
		b, err := p.ParseBlock()
		if err != nil {
			return nil, err
		}
		body = b
	} else {
		b, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}
		body = b
	}

	if variadic {
		return nil, shared.NewError(beginLoc, "variadics are only supported for extern functions for now")
	}

	return &FunctionDefNode{
		Name:    name.Value,
		Args:    args,
		RetType: retType,
		Body:    body,
		Extern:  extern,
		Loc:     beginLoc,
	}, nil
}

func (p *Parser) ParseStructDefinition() (*StructDefNode, error) {
	beginLoc := p.CurrLoc()

	if !p.Expect(tokeniser.TOKEN_TYPE_KEYWORD) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'struct' keyword")
	}

	name, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected struct name")
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_EQUALS) {
		return nil, shared.NewError(p.PrevLoc(), "expected '='")
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{'")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	var fields []StructField
	for !p.Match(tokeniser.TOKEN_TYPE_CLOSE_CURLY) {
		fieldName, err := p.ParseIdent()
		if err != nil {
			return nil, err
		}
		if fieldName.Next != nil {
			return nil, shared.NewError(fieldName.Next.Loc, "field names cannot be qualified")
		}

		if !p.Expect(tokeniser.TOKEN_TYPE_COLON) {
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

		if !p.Match(tokeniser.TOKEN_TYPE_COMMA, tokeniser.TOKEN_TYPE_NEWLINE) {
			break
		}
		p.Inc()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_CLOSE_CURLY) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}'")
	}

	return &StructDefNode{
		Name:   name.Value,
		Fields: fields,
		Loc:    beginLoc,
	}, nil
}

func (p *Parser) ParseImport() (*ImportNode, error) {
	beginLoc := p.CurrLoc()
	if kw, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_KEYWORD); !ok ||
		kw.Value != string(tokeniser.KEYWORD_IMPORT) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'import' keyword")
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
		return nil, shared.NewError(p.PrevLoc(), "expected '('")
	}

	modules := []string{}
	for {
		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		strTok, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_STRING)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected string literal")
		}
		modules = append(modules, strTok.Value)

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		if p.Match(tokeniser.TOKEN_TYPE_COMMA) {
			p.Inc()
			for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
				p.Inc()
			}
			continue
		}

		if p.Match(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
			p.Inc()
			break
		}
		return nil, shared.NewError(p.PrevLoc(), "expected ',' or ')'")
	}

	return &ImportNode{
		Modules: modules,
		Loc:     beginLoc,
	}, nil
}

func (p *Parser) ParseModule() (*ModuleNode, error) {
	beginLoc := p.CurrLoc()
	if kw, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_KEYWORD); !ok ||
		kw.Value != string(tokeniser.KEYWORD_MODULE) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'module' keyword")
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_EQUALS) {
		return nil, shared.NewError(p.PrevLoc(), "expected '='")
	}
	nameTok, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_STRING)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected module name as a string")
	}
	name := nameTok.Value

	return &ModuleNode{
		Name: name,
		Loc:  beginLoc,
	}, nil
}

func (p *Parser) ParseStatement() (Node, bool, error) {
	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) {
		kw := p.Peek().Value
		switch kw {
		case string(tokeniser.KEYWORD_LET):
			p.Inc()
			if !p.Expect(tokeniser.TOKEN_TYPE_IDENT) {
				return nil, false, shared.NewError(p.PrevLoc(), "expected name")
			}
			p.Dec()
			isParen := p.Next().Type == tokeniser.TOKEN_TYPE_OPEN_PAREN
			p.Dec()
			if isParen {
				node, err := p.ParseFunctionDefinition()
				return node, true, err
			}
			node, err := p.ParseDeclaration()
			return node, true, err
		case string(tokeniser.KEYWORD_VAR):
			node, err := p.ParseDeclaration()
			return node, true, err
		case
			string(tokeniser.KEYWORD_RETURN),
			string(tokeniser.KEYWORD_BREAK),
			string(tokeniser.KEYWORD_CONTINUE):
			node, err := p.ParseControlKeyword()
			return node, true, err
		case string(tokeniser.KEYWORD_IF):
			node, err := p.ParseIfStatement()
			return node, false, err
		case string(tokeniser.KEYWORD_FOR):
			node, err := p.ParseForLoop()
			return node, false, err
		default:
			return nil, false, shared.NewError(p.CurrLoc(), "unexpected keyword: %s", kw)
		}
	}

	if p.Match(tokeniser.TOKEN_TYPE_IDENT) {
		ident, err := p.ParseIdent()
		if err != nil {
			return nil, false, err
		}

		if p.Match(tokeniser.TOKEN_TYPE_COLON) {
			p.Inc()

			if ident.Next != nil {
				return nil, true, shared.NewError(ident.Loc,
					"module name cannot be a qualified identifier")
			}

			for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
				p.Inc()
			}

			modIdent, err := p.ParseIdent()
			if err != nil {
				return nil, true, err
			}

			modAN := &ModuleAccessNode{
				ModName: ident.Name,
				Ident:   modIdent,
				Loc:     ident.Loc,
			}

			if p.Match(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
				node, err := p.ParseFunctionCall(modAN)
				return node, true, err
			}

			return modAN, true, nil
		}

		if p.Match(tokeniser.TOKEN_TYPE_EQUALS) {
			node, err := p.ParseAssignment(ident)
			return node, true, err
		} else if p.Match(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
			modAN := &ModuleAccessNode{
				ModName: "",
				Ident:   ident,
				Loc:     ident.Loc,
			}
			node, err := p.ParseFunctionCall(modAN)
			return node, true, err
		} else if p.Match(tokeniser.TOKEN_TYPE_OPEN_SQUARE) {
			node, err := p.ParseArrayAssignment(ident)
			return node, true, err
		}

		return nil, false, shared.NewError(p.CurrLoc(),
			"unexpected token after identifier %s", p.Peek())
	}

	if p.Match(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
		node, err := p.ParseBlock()
		return node, true, err
	}

	if p.Match(tokeniser.TOKEN_TYPE_ASTERISK) {
		node, err := p.ParsePointerAssignment()
		return node, true, err
	}

	return nil, false, shared.NewError(p.CurrLoc(), "unexpected token %s", p.Peek())
}

func (p *Parser) ParseDeclaration() (*DeclarationNode, error) {
	beginLoc := p.CurrLoc()

	kw, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_KEYWORD)
	if !ok || (kw.Value != string(tokeniser.KEYWORD_LET) &&
		kw.Value != string(tokeniser.KEYWORD_VAR)) {
		return nil, shared.NewError(p.PrevLoc(), "expected `let` or `var` keyword")
	}

	ident, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected name")
	}

	var tpe *TypeNode
	if p.Match(tokeniser.TOKEN_TYPE_COLON) {
		p.Inc()

		t, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		tpe = t
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_EQUALS) {
		return nil, shared.NewError(p.PrevLoc(), "expected '='")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	expr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	return &DeclarationNode{
		Name:    ident.Value,
		Type:    tpe,
		Mutable: kw.Value == string(tokeniser.KEYWORD_VAR),
		Value:   expr,
		Loc:     beginLoc,
	}, nil
}

func (p *Parser) ParseAssignment(ident *IdentifierNode) (*AssignmentNode, error) {
	if !p.Expect(tokeniser.TOKEN_TYPE_EQUALS) {
		return nil, shared.NewError(p.PrevLoc(), "expected '='")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	expr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	return &AssignmentNode{
		Assignee: ident,
		Value:    expr,
		Loc:      ident.Loc,
	}, err
}

func (p *Parser) ParsePointerAssignment() (*AssignmentNode, error) {
	identLoc := p.CurrLoc()
	if !p.Match(tokeniser.TOKEN_TYPE_ASTERISK) {
		return nil, shared.NewError(p.PrevLoc(), "expected '*'")
	}
	expr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_EQUALS) {
		return nil, shared.NewError(p.PrevLoc(), "expected '='")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	valExpr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	return &AssignmentNode{
		Assignee: expr,
		Value:    valExpr,
		Loc:      identLoc,
	}, err
}

func (p *Parser) ParseArrayAssignment(ident *IdentifierNode) (*ArrayAssignmentNode, error) {
	if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_SQUARE) {
		return nil, shared.NewError(p.PrevLoc(), "expected '['")
	}
	indexExpr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}
	if !p.Expect(tokeniser.TOKEN_TYPE_CLOSE_SQUARE) {
		return nil, shared.NewError(p.PrevLoc(), "expected ']'")
	}
	if !p.Expect(tokeniser.TOKEN_TYPE_EQUALS) {
		return nil, shared.NewError(p.PrevLoc(), "expected '='")
	}
	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}
	valueExpr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}
	return &ArrayAssignmentNode{
		Assignee: ident,
		Index:    indexExpr,
		Value:    valueExpr,
		Loc:      ident.Loc,
	}, nil
}

func (p *Parser) ParseIfStatement() (*IfNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TOKEN_TYPE_KEYWORD) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'if'")
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

	thenBlock, err := p.ParseBlock()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	node := &IfNode{
		Loc: beginLoc,
		IfBranch: IfBranch{
			Condition: condition,
			Node:      thenBlock,
		},
	}

	for p.Match(tokeniser.TOKEN_TYPE_KEYWORD) && p.Peek().Value == string(tokeniser.KEYWORD_ELSE) {
		p.Consume()

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}

		if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) &&
			p.Peek().Value == string(tokeniser.KEYWORD_IF) {
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
	kw, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_KEYWORD)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected 'return', 'break' or 'continue'")
	}
	if kw.Value != string(tokeniser.KEYWORD_RETURN) &&
		kw.Value != string(tokeniser.KEYWORD_BREAK) &&
		kw.Value != string(tokeniser.KEYWORD_CONTINUE) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'return', 'break' or 'continue'")
	}

	var expr ExpressionNode
	if kw.Value == string(tokeniser.KEYWORD_RETURN) && !p.Match(tokeniser.TOKEN_TYPE_NEWLINE, tokeniser.TOKEN_TYPE_SEMICOLON) {
		got, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}
		expr = got
	}

	return &ControlKeywordNode{
		Keyword:     tokeniser.KeywordType(kw.Value),
		ReturnValue: expr,
		Loc:         loc,
	}, nil
}

func (p *Parser) ParseForLoop() (*ForNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TOKEN_TYPE_KEYWORD) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'for'")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	var err error

	exprsOrStmts := []Node{}

	for !p.Match(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
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

		if p.Match(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
			break
		}

		if !p.Expect(tokeniser.TOKEN_TYPE_SEMICOLON) {
			return nil, shared.NewError(p.PrevLoc(),
				"expected ';' to end statement or expression")
		}

		for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
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
