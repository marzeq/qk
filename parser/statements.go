package parser

import (
	"github.com/marzeq/quokka/shared"
	"github.com/marzeq/quokka/tokeniser"
)

func (p *Parser) ParseDeclaration() (*Node, error) {
	kw, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_KEYWORD)
	if !ok || kw.Value != "let" && kw.Value != "var" {
		return nil, shared.NewError(p.PrevLoc(), "expected `let` or `var` keyword")
	}

	ident, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected name")
	}

	var tpe *Node
	if p.Match(tokeniser.TOKEN_TYPE_COLON) {
		p.Inc()

		ident, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected type name")
		}
		tpe = &Node{Type: NODE_TYPE_IDENTIFIER, Value: ident.Value}
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

	return &Node{
		Type: NODE_TYPE_DECLARATION,
		Value: &DeclarationValue{
			Mutable: kw.Value == "var",
			Type:    tpe,
		},
		Left: &Node{
			Type:  NODE_TYPE_IDENTIFIER,
			Value: ident.Value,
		},
		Right: expr,
	}, nil
}

func (p *Parser) ParseAssignment() (*Node, error) {
	ident, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected name")
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

	return &Node{
		Type: NODE_TYPE_ASSIGNMENT,
		Left: &Node{
			Type:  NODE_TYPE_IDENTIFIER,
			Value: ident.Value,
		},
		Right: expr,
	}, err
}

func (p *Parser) ParseAssignmentBy() (*Node, error) {
	ident, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected name")
	}

	if !p.Match(tokeniser.TOKEN_TYPE_INC_BY, tokeniser.TOKEN_TYPE_DEC_BY, tokeniser.TOKEN_TYPE_MUL_BY, tokeniser.TOKEN_TYPE_DIV_BY) {
		return nil, shared.NewError(p.PrevLoc(), "expected '+=', '-=', '*=' or '/='")
	}

	opType := p.Consume()
	var op tokeniser.TokenType

	switch opType.Type {
	case tokeniser.TOKEN_TYPE_INC_BY:
		op = tokeniser.TOKEN_TYPE_PLUS
	case tokeniser.TOKEN_TYPE_DEC_BY:
		op = tokeniser.TOKEN_TYPE_MINUS
	case tokeniser.TOKEN_TYPE_MUL_BY:
		op = tokeniser.TOKEN_TYPE_ASTERISK
	case tokeniser.TOKEN_TYPE_DIV_BY:
		op = tokeniser.TOKEN_TYPE_SLASH
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	expr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	identNode := &Node{
		Type:  NODE_TYPE_IDENTIFIER,
		Value: ident.Value,
	}

	return &Node{
		Type: NODE_TYPE_ASSIGNMENT,
		Left: identNode,
		Right: &Node{
			Type:  NODE_TYPE_BINARY_OP,
			Value: op,
			Left:  identNode,
			Right: expr,
		},
	}, err
}

func (p *Parser) ParseBlock() (*Node, error) {
	if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' to start block")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	var children []*Node
	for !p.Match(tokeniser.TOKEN_TYPE_CLOSE_CURLY, tokeniser.TOKEN_TYPE_EOF) {
		stmt, err := p.ParseStatement()
		if err != nil {
			return nil, err
		}
		children = append(children, stmt)

		if p.Match(tokeniser.TOKEN_TYPE_CLOSE_CURLY) {
			break
		}

		if !p.Match(tokeniser.TOKEN_TYPE_SEMICOLON, tokeniser.TOKEN_TYPE_NEWLINE) {
			return nil, shared.NewError(p.CurrLoc(), "expected ';' or '\\n' to end statement")
		}

		for p.Match(tokeniser.TOKEN_TYPE_SEMICOLON, tokeniser.TOKEN_TYPE_NEWLINE) {
			p.Inc()
		}
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_CLOSE_CURLY) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}' to close block")
	}

	return &Node{
		Type:     NODE_TYPE_BLOCK,
		Children: children,
	}, nil
}

func (p *Parser) ParseFunctionDefinition() (*Node, error) {
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

	var args []shared.Pair[*Node, *Node]
	for !p.Match(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
		argName, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected argument name")
		}

		if !p.Expect(tokeniser.TOKEN_TYPE_COLON) {
			return nil, shared.NewError(p.PrevLoc(), "expected ':'")
		}

		argType, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected argument type")
		}

		args = append(args, shared.Pair[*Node, *Node]{
			L: &Node{Type: NODE_TYPE_IDENTIFIER, Value: argName.Value},
			R: &Node{Type: NODE_TYPE_IDENTIFIER, Value: argType.Value},
		})

		if !p.Match(tokeniser.TOKEN_TYPE_COMMA) {
			break
		}
		p.Consume()
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')'")
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_COLON) {
		return nil, shared.NewError(p.PrevLoc(), "expected ':'")
	}

	returnType, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected return type")
	}

	if !p.Expect(tokeniser.TOKEN_TYPE_EQUALS) {
		return nil, shared.NewError(p.PrevLoc(), "expected '='")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	var body *Node

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

	return &Node{
		Type: NODE_TYPE_FUNCTION_DEF,
		Value: &FunctionValue{
			Name:    &Node{Type: NODE_TYPE_IDENTIFIER, Value: name.Value},
			Args:    args,
			RetType: &Node{Type: NODE_TYPE_IDENTIFIER, Value: returnType.Value},
		},
		Children: []*Node{body},
	}, nil
}

func (p *Parser) ParseImport() (*Node, error) {
	if kw, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_KEYWORD); !ok || kw.Value != "import" {
		return nil, shared.NewError(p.PrevLoc(), "expected 'import' keyword")
	}

	ident, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected name")
	}

	return &Node{
		Type:  NODE_TYPE_IMPORT,
		Right: &Node{Type: NODE_TYPE_IDENTIFIER, Value: ident.Value},
	}, nil
}

func (p *Parser) ParseStatement() (*Node, error) {
	if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) {
		kw := p.Peek().Value
		switch kw {
		case "let":
			p.Inc()
			if !p.Expect(tokeniser.TOKEN_TYPE_IDENT) {
				return nil, shared.NewError(p.PrevLoc(), "expected name")
			}
			p.Dec()
			isParen := p.Next().Type == tokeniser.TOKEN_TYPE_OPEN_PAREN
			p.Dec()
			if isParen {
				return p.ParseFunctionDefinition()
			}
			return p.ParseDeclaration()
		case "var":
			return p.ParseDeclaration()
		case "return", "naked_return", "break", "continue":
			return p.ParseControlKeyword()
		case "if":
			return p.ParseIfStatement()
		case "for":
			return p.ParseForLoop()
		default:
			return nil, shared.NewError(p.CurrLoc(), "unexpected keyword: %s", kw)
		}
	}

	if p.Match(tokeniser.TOKEN_TYPE_IDENT) {
		ident := p.Consume()

		if p.Match(tokeniser.TOKEN_TYPE_EQUALS) {
			p.Dec()
			return p.ParseAssignment()
		} else if p.Match(tokeniser.TOKEN_TYPE_INC_BY, tokeniser.TOKEN_TYPE_DEC_BY, tokeniser.TOKEN_TYPE_MUL_BY, tokeniser.TOKEN_TYPE_DIV_BY) {
			p.Dec()
			return p.ParseAssignmentBy()
		} else if p.Match(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
			return p.ParseFunctionCall(ident)
		}

		return nil, shared.NewError(p.CurrLoc(), "unexpected token after identifier %s", p.Peek())
	}

	if p.Match(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
		return p.ParseBlock()
	}

	return nil, shared.NewError(p.CurrLoc(), "unexpected token %s", p.Peek())
}

func (p *Parser) ParseIfStatement() (*Node, error) {
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

			elseifBlock, err := p.ParseBlock()
			if err != nil {
				return nil, err
			}

			elseIfBranch := &IfBranch{
				Condition: elseifCondition,
				Node:      elseifBlock,
			}
			ifNodeValue.ElseIfBranches = append(ifNodeValue.ElseIfBranches, elseIfBranch)
		} else {
			elseBlock, err := p.ParseBlock()
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
		Type:  NODE_TYPE_IF,
		Value: ifNodeValue,
	}, nil
}

func (p *Parser) ParseControlKeyword() (*Node, error) {
	kw, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_KEYWORD)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected 'return', 'break' or 'continue'")
	}

	var expr *Node
	if kw.Value == "return" && !p.Match(tokeniser.TOKEN_TYPE_NEWLINE, tokeniser.TOKEN_TYPE_SEMICOLON) {
		got, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}
		expr = got
	}

	return &Node{
		Type:  NODE_TYPE_CONTROL_KEYWORD,
		Value: kw.Value,
		Right: expr,
	}, nil
}

func (p *Parser) ParseForLoop() (*Node, error) {
	if !p.Expect(tokeniser.TOKEN_TYPE_KEYWORD) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'for'")
	}

	for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
		p.Inc()
	}

	var err error

	exprsOrStmts := []*Node{}

	for !p.Match(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
		ogLoc := p.CurrLoc()
		ogPos := p.pos
		exOrSt, err := p.ParseStatement()
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
			return nil, shared.NewError(p.PrevLoc(), "expected ';' to end statement or expression")
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

	return &Node{
		Type: NODE_TYPE_FOR,
		Value: &ForLoopValue{
			ExprsOrStmts: exprsOrStmts,
			Body:         body,
		},
	}, nil
}
