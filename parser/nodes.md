Node types:

```
{
  Type: ROOT
  Children: [](FUNCTION_DEF | IMPORT)
}

{
  Type: DECLARATION
  Value: {
    Mutable: bool
    Type: IDENTIFIER Node | nil # nil means it has to be inferred
  }
  Left: IDENTIFIER Node
  Right: expression
}

{
  Type: ASSIGNMENT
  Left: IDENTIFIER Node
  Right: expression 
}

{
  Type: CHAR_LITERAL 
  Value: byte
}

{
  Type: BLOCK
  Children: STATEMENT Node[]
}

{
  Type: FUNCTION_DEF
  Value: {
    Name: IDENTIFIER Node
    Args: {L: IDENTIFIER Node (argname), R: IDENTIFIER Node (argtype)}[]
    RetType: IDENTIFIER Node
  }
  Children: []*Node{body}
}

{
  Type: IMPORT
  Right: IDENTIFIER Node
}

{
  Type: IF
  Value: {
    IfBranch: {
      Condition: expression
      Node: BLOCK Node
    }
    ElseIfBranches: {
      Condition: expression
      Node: BLOCK Node
    }[]
    ElseBranch: {
      Condition: nil
      Node: BLOCK Node
    }
  }
}

{
  Type: CONTROL_KEYWORD
  Value: "return" | "break" | "continue"
  Right: expression # if return had a value associated with it
}

{
  Type: FOR
  Value: {
    ExprsOrStmts: [](expression or statement),
    Body:         BLOCK Node,
  }
}

{
  Type:  UNARY_OP
  Value: "not" | "-"
  Right: expression
}

{
  Type:  BINARY_OP
  Value: "==" | "!=" | ">" | ">=" | "<" | "<=" | "+" | "-" | "*" | "/" | "%" | "and" | "or"
  Left:  expression
  Right: expression
}

{
  Type: CAST
  Left: IDENTIFIER Node
  Right: expression
}

{
  Type:  IDENTIFIER
  Value: string
}

{
  Type:  NUMBER_LITERAL
  Value: string
}

{
  Type:     FUNCTION_CALL
  Value:    IDENTIFIER Node
  Children: []expression
}

{
  Type: IF_EXPRESSION
  Value: {
    IfBranch: {
      Condition: expression
      Node: expression
    }
    ElseIfBranches: {
      Condition: expression
      Node: expression
    }[]
    ElseBranch: {
      Condition: nil
      Node: expression
    }
  }
}
```
