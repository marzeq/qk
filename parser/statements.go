package parser

import (
	"github.com/marzeq/quokka/shared"
	"github.com/marzeq/quokka/tokeniser"
)

func (p *Parser) ParseDeclaration() (*DeclarationNode, error) {
  beginLoc := p.CurrLoc()

  kw, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_KEYWORD)
  if !ok || kw.Value != "let" && kw.Value != "var" {
    return nil, shared.NewError(p.PrevLoc(), "expected `let` or `var` keyword")
  }

  ident, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_IDENT)
  if !ok {
    return nil, shared.NewError(p.PrevLoc(), "expected name")
  }

  var tpe *IdentifierNode
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

  return &DeclarationNode{
    Name: ident.Value,
    Type: tpe,
    Mutable: kw.Value == "var",
    Value: expr,
    Loc: beginLoc,
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
    Value: expr,
    Loc: ident.Loc,
  }, err
}

func (p *Parser) ParsePointerAssignment() (*AssignmentNode, error) {
  identLoc := p.CurrLoc()
  if (!p.Expect(tokeniser.TOKEN_TYPE_ASTERISK)) {
    return nil, shared.NewError(p.PrevLoc(), "expected '*'")
  }
  ident, err := p.ParseIdent()
  if err != nil {
    return nil, err
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

  return &AssignmentNode{
    Assignee: &UnaryOpNode{
      Op: UNARY_OP_DEREFERENCE,
      Operand: ident,
      Loc: identLoc,
    },
    Value: expr,
    Loc: identLoc,
  }, err
}

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
      return nil, shared.NewError(p.CurrLoc(), "expected ';' or '\\n' to end statement")
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
    Loc: beginLoc,
  }, nil
}

func (p *Parser) ParseFunctionDefinition() (*FunctionDefNode, error) {
  beginLoc := p.CurrLoc()
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
  for !p.Match(tokeniser.TOKEN_TYPE_CLOSE_PAREN) {
    mutable := false
    if p.Match(tokeniser.TOKEN_TYPE_KEYWORD) {
      kw := p.Consume().Value
      if kw == "var" {
        mutable = true
      } else {
        return nil, shared.NewError(p.CurrLoc(), "expected either 'var' or argument name")
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

    pointerLevel, argType, err := p.ParseType()
    if err != nil {
      return nil, err
    }

    args = append(args, FunctionNodeType{
      Name: arg.Name,
      Type: argType,
      PointerLevel: pointerLevel,
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
    pointerLevel, argType, err := p.ParseType()
    if err != nil {
      return nil, err
    }
    retType = FunctionNodeType{
      Type: argType,
      PointerLevel: pointerLevel,
    }
  }

  if !p.Expect(tokeniser.TOKEN_TYPE_EQUALS) {
    return nil, shared.NewError(p.PrevLoc(), "expected '='")
  }

  for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
    p.Inc()
  }

  var body Node

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

  return &FunctionDefNode{
    Name: name.Value,
    Args: args,
    RetType: retType,
    Body: body,
    Loc: beginLoc,
  }, nil
}

func (p *Parser) ParseExternalFunctionDefinition() (*FunctionDefNode, error) {
  beginLoc := p.CurrLoc()
  if !p.Expect(tokeniser.TOKEN_TYPE_KEYWORD) {
    return nil, shared.NewError(p.PrevLoc(), "expected 'declare' keyword")
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

    pointerLevel, argType, err := p.ParseType()
    if err != nil {
      return nil, err
    }

    args = append(args, FunctionNodeType{
      Name: arg.Name,
      Type: argType,
      PointerLevel: pointerLevel,
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

  pointerLevel, returnType, err := p.ParseType()
  if err != nil {
    return nil, err
  }

  return &FunctionDefNode{
    Name: name.Value,
    Args: args,
    RetType: FunctionNodeType{
      Type: returnType,
      PointerLevel: pointerLevel,
    },
    HasVariadic: variadic,
    Loc: beginLoc,
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

  if !p.Expect(tokeniser.TOKEN_TYPE_OPEN_CURLY) {
    return nil, shared.NewError(p.PrevLoc(), "expected '{'")
  }

  for p.Match(tokeniser.TOKEN_TYPE_NEWLINE) {
    p.Inc()
  }

  var fields []shared.Pair[string, *IdentifierNode]
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

    fieldType, err := p.ParseIdent()
    if err != nil {
      return nil, err
    }

    fields = append(fields, shared.Pair[string, *IdentifierNode]{
      L: fieldName.Name,
      R: fieldType,
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
    Name: name.Value,
    Fields: fields,
    Loc: beginLoc,
  }, nil
}

func (p *Parser) ParseImport() (*ImportNode, error) {
  beginLoc := p.CurrLoc()
  if kw, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_KEYWORD); !ok || kw.Value != "import" {
    return nil, shared.NewError(p.PrevLoc(), "expected 'import' keyword")
  }

  path, ok := p.ExpectGet(tokeniser.TOKEN_TYPE_STRING)
  if !ok {
    return nil, shared.NewError(p.PrevLoc(), "expected path")
  }

  return &ImportNode{
    Module: path.Value,
    Loc: beginLoc,
  }, nil
}

func (p *Parser) ParseStatement() (Node, bool, error) {
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
    ident, err := p.ParseIdent()
    if err != nil {
      return nil, false, err
    }

    if p.Match(tokeniser.TOKEN_TYPE_EQUALS) {
      node, err := p.ParseAssignment(ident)
      return node, true, err
    } else if p.Match(tokeniser.TOKEN_TYPE_OPEN_PAREN) {
      node, err := p.ParseFunctionCall(ident)
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
      Node: thenBlock,
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

      elseifBlock, err := p.ParseBlock()
      if err != nil {
        return nil, err
      }

      elseIfBranch := IfBranch{
        Condition: elseifCondition,
        Node: elseifBlock,
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
  kt, ok := KeywordTypeFromString(kw.Value)
  if !ok {
    return nil, shared.NewError(p.PrevLoc(), "expected 'return', 'break' or 'continue'")
  }

  var expr ExpressionNode
  if kw.Value == "return" && !p.Match(tokeniser.TOKEN_TYPE_NEWLINE, tokeniser.TOKEN_TYPE_SEMICOLON) {
    got, err := p.ParseExpression()
    if err != nil {
      return nil, err
    }
    expr = got
  }

  return &ControlKeywordNode{
    Keyword: kt,
    ReturnValue: expr,
    Loc: loc,
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

  return &ForNode{
    ExprsOrStmts: exprsOrStmts,
    Body: body,
    Loc: beginLoc,
  }, nil
}
