package parser

import (
  "github.com/marzeq/quokka/shared"
  "github.com/marzeq/quokka/tokeniser"
)

func (p *Parser) ParseDeclaration() (*Node, error) {
  beginLoc := p.CurrLoc()

  kw, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_KEYWORD)
  if !ok || kw.Value != "let" && kw.Value != "var" {
    return nil, shared.NewError(p.PrevLoc(), "expected `let` or `var` keyword")
  }

  identLoc := p.CurrLoc()
  ident, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
  if !ok {
    return nil, shared.NewError(p.PrevLoc(), "expected name")
  }

  var tpe *Node
  if p.Match(tokeniser.TOKEN_TYPE_COLON) {
    p.Inc()

    ident, err := p.ParseIdent()
    if err != nil {
      return nil, err
    }
    tpe = ident
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
      Loc:   identLoc,
    },
    Right: expr,
    Loc:   beginLoc,
  }, nil
}

func (p *Parser) ParseAssignment() (*Node, error) {
  identLoc := p.CurrLoc()
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
      Loc:   identLoc,
    },
    Right: expr,
    Loc:   identLoc,
  }, err
}

func (p *Parser) ParsePointerAssignment() (*Node, error) {
	identLoc := p.CurrLoc()
	if (!p.Expect(tokeniser.TOKEN_TYPE_ASTERISK)) {
		return nil, shared.NewError(p.PrevLoc(), "expected '*'")
	}
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
      Type:  NODE_TYPE_UNARY_OP,
			Value: "*",
			Right: &Node{
				Type:  NODE_TYPE_IDENTIFIER,
				Value: ident.Value,
				Loc:   identLoc,
			},
			Loc: identLoc,
    },
		Right: expr,
    Loc: 	 identLoc,
  }, err
}

func (p *Parser) ParseAssignmentBy() (*Node, error) {
  identLoc := p.CurrLoc()
  ident, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
  if !ok {
    return nil, shared.NewError(p.PrevLoc(), "expected name")
  }

  if !p.Match(tokeniser.TOKEN_TYPE_INC_BY, tokeniser.TOKEN_TYPE_DEC_BY, tokeniser.TOKEN_TYPE_MUL_BY, tokeniser.TOKEN_TYPE_DIV_BY, tokeniser.TOKEN_TYPE_MOD_BY) {
    return nil, shared.NewError(p.PrevLoc(), "expected '+=', '-=', '*=', '/=' or '%%='")
  }

  opType := p.Consume()
  op := ""

  switch opType.Type {
  case tokeniser.TOKEN_TYPE_INC_BY:
    op = "+"
  case tokeniser.TOKEN_TYPE_DEC_BY:
    op = "-"
  case tokeniser.TOKEN_TYPE_MUL_BY:
    op = "*"
  case tokeniser.TOKEN_TYPE_DIV_BY:
    op = "/"
  case tokeniser.TOKEN_TYPE_MOD_BY:
    op = "%"
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
    Loc:   identLoc,
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
    Loc: identLoc,
  }, err
}

func (p *Parser) ParseBlock() (*Node, error) {
  beginLoc := p.CurrLoc()
  if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
    return nil, shared.NewError(p.PrevLoc(), "expected '{' to start block")
  }

  for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
    p.Inc()
  }

  var children []*Node
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
    Loc:      beginLoc,
  }, nil
}

func (p *Parser) ParseFunctionDefinition() (*Node, error) {
  beginLoc := p.CurrLoc()
  if !p.Expect(tokeniser.TOKEN_TYPE_KEYWORD) {
    return nil, shared.NewError(p.PrevLoc(), "expected 'let' keyword")
  }

  nameLoc := p.CurrLoc()
  name, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
  if !ok {
    return nil, shared.NewError(p.PrevLoc(), "expected function name")
  }

  if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
    return nil, shared.NewError(p.PrevLoc(), "expected '('")
  }

  var args []shared.Pair[*Node, *Node]
  for !p.Match(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
    argNameLoc := p.CurrLoc()
    argName, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
    if !ok {
      return nil, shared.NewError(p.PrevLoc(), "expected argument name")
    }

    if !p.Expect(tokeniser.TOKEN_TYPE_COLON) {
      return nil, shared.NewError(p.PrevLoc(), "expected ':'")
    }

    arg, err := p.ParseIdent()
    if err != nil {
      return nil, err
    }

    args = append(args, shared.Pair[*Node, *Node]{
      L: &Node{Type: NODE_TYPE_IDENTIFIER, Value: argName.Value, Loc: argNameLoc},
      R: arg,
    })

    if !p.Match(tokeniser.TOKEN_TYPE_COMMA) {
      break
    }
    p.Consume()
  }

  if !p.Expect(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
    return nil, shared.NewError(p.PrevLoc(), "expected ')'")
  }

	var retType *Node
  if p.Match(tokeniser.TOKEN_TYPE_COLON) {
		p.Inc()
		r, err := p.ParseIdent()
		if err != nil {
			return nil, err
		}
		retType = r
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
      Name:        &Node{Type: NODE_TYPE_IDENTIFIER, Value: name.Value, Loc: nameLoc},
      Args:        args,
      RetType:     retType,
      HasVariadic: false,
    },
    Children: []*Node{body},
    Loc:      beginLoc,
  }, nil
}

func (p *Parser) ParseExternalFunctionDefinition() (*Node, error) {
  beginLoc := p.CurrLoc()
  if !p.Expect(tokeniser.TOKEN_TYPE_KEYWORD) {
    return nil, shared.NewError(p.PrevLoc(), "expected 'let' keyword")
  }

  nameLoc := p.CurrLoc()
  name, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
  if !ok {
    return nil, shared.NewError(p.PrevLoc(), "expected function name")
  }

  if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
    return nil, shared.NewError(p.PrevLoc(), "expected '('")
  }

  var args []shared.Pair[*Node, *Node]
  variadic := false
  for !p.Match(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
    if p.Match(tokeniser.TOKEN_TYPE_3DOTS) {
      p.Inc()
      variadic = true
      break
    }

    argNameLoc := p.CurrLoc()
    argName, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
    if !ok {
      return nil, shared.NewError(p.PrevLoc(), "expected argument name")
    }

    if !p.Expect(tokeniser.TOKEN_TYPE_COLON) {
      return nil, shared.NewError(p.PrevLoc(), "expected ':'")
    }

    argTypeLoc := p.CurrLoc()
    argType, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
    if !ok {
      return nil, shared.NewError(p.PrevLoc(), "expected argument type")
    }

    args = append(args, shared.Pair[*Node, *Node]{
      L: &Node{Type: NODE_TYPE_IDENTIFIER, Value: argName.Value, Loc: argNameLoc},
      R: &Node{Type: NODE_TYPE_IDENTIFIER, Value: argType.Value, Loc: argTypeLoc},
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

  retTypeLoc := p.CurrLoc()
  returnType, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
  if !ok {
    return nil, shared.NewError(p.PrevLoc(), "expected return type")
  }

  return &Node{
    Type: NODE_TYPE_FUNCTION_DEF,
    Value: &FunctionValue{
      Name:        &Node{Type: NODE_TYPE_IDENTIFIER, Value: name.Value, Loc: nameLoc},
      Args:        args,
      RetType:     &Node{Type: NODE_TYPE_IDENTIFIER, Value: returnType.Value, Loc: retTypeLoc},
      HasVariadic: variadic,
    },
    Loc: beginLoc,
  }, nil
}

func (p *Parser) ParseStructDefinition() (*Node, error) {
  beginLoc := p.CurrLoc()
  if !p.Expect(tokeniser.TOKEN_TYPE_KEYWORD) {
    return nil, shared.NewError(p.PrevLoc(), "expected 'struct' keyword")
  }

  nameLoc := p.CurrLoc()
  name, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
  if !ok {
    return nil, shared.NewError(p.PrevLoc(), "expected struct name")
  }

  if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
    return nil, shared.NewError(p.PrevLoc(), "expected '{'")
  }

  for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
    p.Inc()
  }

  var props []shared.Pair[*Node, *Node]
  for !p.Match(tokeniser.TOKEN_TYPE_CLOSE_CURLY) {
    propNameLoc := p.CurrLoc()
    propName, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
    if !ok {
      return nil, shared.NewError(p.PrevLoc(), "expected property name")
    }

    if !p.Expect(tokeniser.TOKEN_TYPE_COLON) {
      return nil, shared.NewError(p.PrevLoc(), "expected ':'")
    }

    propTypeLoc := p.CurrLoc()
    propType, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
    if !ok {
      return nil, shared.NewError(p.PrevLoc(), "expected property type")
    }

    props = append(props, shared.Pair[*Node, *Node]{
      L: &Node{Type: NODE_TYPE_IDENTIFIER, Value: propName.Value, Loc: propNameLoc},
      R: &Node{Type: NODE_TYPE_IDENTIFIER, Value: propType.Value, Loc: propTypeLoc},
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

  return &Node{
    Type: NODE_TYPE_STRUCT_DEF,
    Left: &Node{Type: NODE_TYPE_IDENTIFIER, Value: name.Value, Loc: nameLoc},
    Value: &StructValue{
      Fields: props,
    },
    Loc: beginLoc,
  }, nil
}

func (p *Parser) ParseImport() (*Node, error) {
  beginLoc := p.CurrLoc()
  if kw, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_KEYWORD); !ok || kw.Value != "import" {
    return nil, shared.NewError(p.PrevLoc(), "expected 'import' keyword")
  }

  strLoc := p.CurrLoc()
  path, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_STRING)
  if !ok {
    return nil, shared.NewError(p.PrevLoc(), "expected path")
  }

  return &Node{
    Type:  NODE_TYPE_IMPORT,
    Right: &Node{Type: NODE_TYPE_STRING_LITERAL, Value: path.Value, Loc: strLoc},
    Loc:   beginLoc,
  }, nil
}

func (p *Parser) ParseStatement() (*Node, bool, error) {
  if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) {
    kw := p.Peek().Value
    switch kw {
    case "let":
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
    case "declare":
      node, err := p.ParseExternalFunctionDefinition()
      return node, true, err
    case "var":
      node, err := p.ParseDeclaration()
      return node, true, err
    case "return", "naked_return", "break", "continue":
      node, err := p.ParseControlKeyword()
      return node, true, err
    case "if":
      node, err := p.ParseIfStatement()
      return node, false, err
    case "for":
      node, err := p.ParseForLoop()
      return node, false, err
    default:
      return nil, false, shared.NewError(p.CurrLoc(), "unexpected keyword: %s", kw)
    }
  }

  if p.Match(tokeniser.TOKEN_TYPE_IDENT) {
    p.Inc()
    if p.Match(tokeniser.TOKEN_TYPE_EQUALS) {
      p.Dec()
      node, err := p.ParseAssignment()
      return node, true, err
    } else if p.Match(tokeniser.TOKEN_TYPE_INC_BY, tokeniser.TOKEN_TYPE_DEC_BY, tokeniser.TOKEN_TYPE_MUL_BY, tokeniser.TOKEN_TYPE_DIV_BY) {
      p.Dec()
      node, err := p.ParseAssignmentBy()
      return node, true, err
    } else if p.Match(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
      p.Dec()
      node, err := p.ParseFunctionCall()
      return node, true, err
    }

    return nil, false, shared.NewError(p.CurrLoc(), "unexpected token after identifier %s", p.Peek())
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

func (p *Parser) ParseIfStatement() (*Node, error) {
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
    Loc:   beginLoc,
  }, nil
}

func (p *Parser) ParseControlKeyword() (*Node, error) {
  loc := p.CurrLoc()
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
    Loc:   loc,
  }, nil
}

func (p *Parser) ParseForLoop() (*Node, error) {
  beginLoc := p.CurrLoc()
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
    Loc: beginLoc,
  }, nil
}
