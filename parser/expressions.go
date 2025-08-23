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
    p.Inc()

    for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
      p.Inc()
    }

    right, err := p.ParseLogicalAnd()
    if err != nil {
      return nil, err
    }
    left = &Node{
      Type:  NODE_TYPE_BINARY_OP,
      Value: "or",
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
    p.Inc()

    for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
      p.Inc()
    }

    right, err := p.ParseLogicalNot()
    if err != nil {
      return nil, err
    }
    left = &Node{
      Type:  NODE_TYPE_BINARY_OP,
      Value: "and",
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
    p.Inc()

    for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
      p.Inc()
    }

    expr, err := p.ParseLogicalNot()
    if err != nil {
      return nil, err
    }

    return &Node{
      Type:  NODE_TYPE_UNARY_OP,
      Value: "not",
      Right: expr,
      Loc:   beginLoc,
    }, nil
  }

  return p.ParseComparison()
}

func (p *Parser) ParseUnary() (*Node, error) {
  beginLoc := p.CurrLoc()
  if p.Match(tokeniser.TOKEN_TYPE_MINUS, tokeniser.TOKEN_TYPE_ASTERISK, tokeniser.TOKEN_TYPE_AMPERSAND) {
    op := p.Consume()
    val := ""
    switch op.Type {
    case tokeniser.TOKEN_TYPE_MINUS:
      val = "-"
    case tokeniser.TOKEN_TYPE_ASTERISK:
      val = "*"
    case tokeniser.TOKEN_TYPE_AMPERSAND:
      val = "&"
    }
    for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
      p.Inc()
    }
    expr, err := p.ParseUnary()
    if err != nil {
      return nil, err
    }
    return &Node{
      Type:  NODE_TYPE_UNARY_OP,
      Value: val,
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
    val := ""
    switch op.Type {
    case tokeniser.TOKEN_TYPE_EQUALS_EQUALS:
      val = "=="
    case tokeniser.TOKEN_TYPE_NOT_EQUALS:
      val = "!="
    case tokeniser.TOKEN_TYPE_LESS:
      val = "<"
    case tokeniser.TOKEN_TYPE_GREATER:
      val = ">"
    case tokeniser.TOKEN_TYPE_LESS_EQUALS:
      val = "<="
    case tokeniser.TOKEN_TYPE_GREATER_EQUALS:
      val = ">="
    }
    for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
      p.Inc()
    }

    right, err := p.ParseAddSub()
    if err != nil {
      return nil, err
    }
    left = &Node{
      Type:  NODE_TYPE_BINARY_OP,
      Value: val,
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
    val := ""
    if op.Type == tokeniser.TOKEN_TYPE_PLUS {
      val = "+"
    } else {
      val = "-"
    }

    for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
      p.Inc()
    }

    right, err := p.ParseMulDiv()
    if err != nil {
      return nil, err
    }
    left = &Node{
      Type:  NODE_TYPE_BINARY_OP,
      Value: val,
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

  for p.Match(tokeniser.TOKEN_TYPE_ASTERISK, tokeniser.TOKEN_TYPE_SLASH, tokeniser.TOKEN_TYPE_PERCENT) {
    op := p.Consume()
    val := ""
    switch op.Type {
    case tokeniser.TOKEN_TYPE_ASTERISK:
      val = "*"
    case tokeniser.TOKEN_TYPE_SLASH:
      val = "/"
    default:
      val = "%"
    }
    for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
      p.Inc()
    }

    right, err := p.ParseUnary()
    if err != nil {
      return nil, err
    }
    left = &Node{
      Type:  NODE_TYPE_BINARY_OP,
      Value: val,
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
    p.Inc()
    if p.Match(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
      p.Dec()
      return p.ParseFunctionCall()
    }

    if p.Match(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
      p.Dec()
      return p.ParseStructLiteral()
    }

    p.Dec()
    return p.ParseIdent()
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

  if p.Match(tokeniser.TOKEN_TYPE_CHAR) {
    cLit := p.Consume()
    return &Node{
      Type:  NODE_TYPE_CHAR_LITERAL,
      Value: []byte(cLit.Value)[0],
      Loc:   beginLoc,
    }, nil
  }

  if p.Match(tokeniser.TOKEN_TYPE_STRING) {
    sLit := p.Consume()
    return &Node{
      Type:  NODE_TYPE_STRING_LITERAL,
      Value: sLit.Value,
      Loc:   beginLoc,
    }, nil
  }

  return nil, shared.NewError(p.CurrLoc(), "unexpected token %s", p.Peek())
}

func (p *Parser) ParseCast() (*Node, error) {
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

  tpe, err := p.ParseIdent()
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
    return nil, shared.NewError(p.PrevLoc(), "expected ')")
  }

  return &Node{
    Type:  NODE_TYPE_CAST,
    Left:  &Node{Type: NODE_TYPE_IDENTIFIER, Value: tpe.Value},
    Right: expr,
    Loc:   beginLoc,
  }, nil
}

func (p *Parser) ParseFunctionCall() (*Node, error) {
  beginLoc := p.CurrLoc()
  name, err := p.ParseIdent()
  if err != nil {
    return nil, err
  }
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

func (p *Parser) ParseStructLiteral() (*Node, error) {
  beginLoc := p.CurrLoc()
  name, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
  if !ok {
    return nil, shared.NewError(p.PrevLoc(), "expected struct name")
  }

  if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
    return nil, shared.NewError(p.PrevLoc(), "expected '{' to start struct literal")
  }

  for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
    p.Inc()
  }

  value := &StructLiteralValue{}
  for {
    if p.Match(tokeniser.TOKEN_TYPE_CLOSE_CURLY) {
      p.Inc()
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

    value.Fields = append(value.Fields, shared.Pair[*Node, *Node]{L: &Node{Type: NODE_TYPE_IDENTIFIER, Value: fieldName.Value}, R: fieldValue})

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

  return &Node{
    Type:  NODE_TYPE_STRUCT_LITERAL,
    Value: value,
    Left:  &Node{Type: NODE_TYPE_IDENTIFIER, Value: name.Value},
    Loc:   beginLoc,
  }, nil
}

func (p *Parser) ParseIdent() (*Node, error) {
  beginLoc := p.CurrLoc()
  firstIdent, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
  if !ok {
    return nil, shared.NewError(p.PrevLoc(), "expected identifier")
  }
  node := &Node{
    Type:  NODE_TYPE_IDENTIFIER,
    Value: firstIdent.Value,
    Loc:   beginLoc,
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
    currnode.Right = &Node{
      Type:  NODE_TYPE_IDENTIFIER,
      Value: nextIdent.Value,
      Loc:   loc,
    }
    currnode = currnode.Right
  }

  return node, nil
}
