package typechecker

import (
	"strings"

	"github.com/marzeq/quokka/parser"
	"github.com/marzeq/quokka/shared"
)

type VarSig struct {
  Type shared.Type
  Mutable bool
}

type FunctionSig struct {
  Name string
  ArgTypes []shared.Pair[string, shared.Type]
  HasVariadic bool
  RetType shared.Type
  ImplicitReturn bool
}

type (
  VarTable = *shared.SymbolTable[*VarSig]
  FuncTable = *shared.SymbolTable[*FunctionSig]
  TypeTable = shared.TypeTable
)

type TypeChecker struct {
  VarTable VarTable
  FuncTable FuncTable
  TypeTable TypeTable
}

func NewTypeChecker() *TypeChecker {
  return &TypeChecker{
    VarTable: shared.NewSymbolTable[*VarSig](),
    FuncTable: shared.NewSymbolTable[*FunctionSig](),
    TypeTable: shared.NewTypeTable(),
  }
}

func (tc *TypeChecker) TypeCheck(root *parser.RootNode) (*parser.RootNode, map[string]*FunctionSig, TypeTable, error) {
  for _, n := range root.Body{
    switch node := n.(type){
    case *parser.FunctionDefNode:
      fsig, err := tc.ExtractFunctionSig(node)
      if err != nil {
        return nil, nil, nil, err
      }

      if ok := tc.FuncTable.Define(fsig.Name, fsig); !ok {
        return nil, nil, nil, shared.NewError(node.Loc, "function '%s' is already defined", fsig.Name)
      }

      if fsig.Name == "main" && fsig.RetType != shared.PRIMITIVE_I32 {
        return nil, nil, nil, shared.NewError(node.Loc, "'main' function must be of '%s' return type", shared.PRIMITIVE_I32)
      }
    case *parser.StructDefNode:
      if _, ok := tc.TypeTable.Lookup(node.Name); ok {
        return nil, nil, nil, shared.NewError(node.Loc, "struct '%s' is already defined", node.Name)
      }

      fields := make([]shared.Pair[string, shared.Type], len(node.Fields))

      for i, field := range node.Fields {
        tpe := field.R.Name
        resolved, ok := tc.TypeTable.Lookup(tpe)
        if !ok {
          return nil, nil, nil, shared.NewError(field.R.Loc, "field '%s' has undefined type '%s'", field.L, tpe)
        }
        fields[i] = shared.Pair[string, shared.Type]{L: field.L, R: resolved}
      }

      tc.TypeTable.Define(node.Name, shared.Struct{
        Fields: fields,
      })
    }
  }

  for _, n := range root.Body {
    switch node := n.(type) {
    case *parser.FunctionDefNode:
    sig, _ := tc.FuncTable.Lookup(node.Name)

    tc.enterScope()
    if err := tc.typeCheckFunction(node, sig); err != nil {
      return nil, nil, nil, err
    }
    tc.exitScope()
    }
  }

  return root, tc.FuncTable.GetScope(), tc.TypeTable, nil
}

func (tc *TypeChecker) enterScope() {
  tc.VarTable.EnterScope()
  tc.FuncTable.EnterScope()
}

func (tc *TypeChecker) exitScope() {
  tc.VarTable.ExitScope()
  tc.FuncTable.ExitScope()
}

func (tc *TypeChecker) typeCheckFunction(funcNode *parser.FunctionDefNode, sig *FunctionSig) error {
  for _, arg := range sig.ArgTypes {
    tc.VarTable.Define(arg.L, &VarSig{
      Type: arg.R,
      Mutable: false,
    })
  }

  
  if funcNode.Body == nil {
    return nil
  }
  switch body := funcNode.Body.(type) {
  case *parser.BlockNode:
    _, err := tc.typeCheckBlock(body, sig, false, true)
    return err
  default:
    if expr, ok := body.(parser.ExpressionNode); ok {
      exprType, err := tc.typeCheckExpression(expr, sig.RetType)
      if err != nil {
        return err
      }

      if !shared.CanCoerceTo(exprType, sig.RetType) {
        return shared.NewError(funcNode.RetType.Loc, "function '%s' expects return type '%s' but returns '%s'",
          funcNode.Name, sig.RetType, exprType)
      }

      switch b := body.(type) {
      case *parser.NumberLiteralNode:
        if b.ExprType == shared.PRIMITIVE_UNTYPED_INT && sig.RetType != shared.PRIMITIVE_VOID {
          b.ExprType = sig.RetType
        }
      }
      return nil
    }
    return shared.NewError(body.GetLoc(), "function body must be a block or an expression")
  }
}

func (tc *TypeChecker) typeCheckBlock(blockNode *parser.BlockNode, sig *FunctionSig, isLoop bool, isMainBody bool) (bool, error) {
  foundReturn := false

  for i, n := range blockNode.Body {
    switch node := n.(type) {
    case *parser.DeclarationNode:
      varName, varSig, err := tc.typeCheckDeclaration(node)
      if err != nil {
        return false, err
      }
      if ok := tc.VarTable.Define(varName, varSig); !ok {
        return false, shared.NewError(node.Loc, "variable '%s' is already declared in this scope", varName)
      }

    case *parser.FunctionDefNode:
      fsig, err := tc.ExtractFunctionSig(node)
      if err != nil {
        return false, err
      }
      if ok := tc.FuncTable.Define(fsig.Name, fsig); !ok {
        return false, shared.NewError(node.Loc, "function '%s' is already defined", fsig.Name)
      }

      tc.enterScope()
      if err := tc.typeCheckFunction(node, fsig); err != nil {
        return false, err
      }
      tc.exitScope()

    case *parser.AssignmentNode:
      if err := tc.typeCheckAssignment(node); err != nil {
        return false, err
      }

    case *parser.FunctionCallNode:
      if _, err := tc.typeCheckFunctionCall(node); err != nil {
        return false, err
      }

    case *parser.ControlKeywordNode:
      switch node.Keyword {
      case parser.KEYWORD_TYPE_RETURN:
        var retType shared.Type = shared.PRIMITIVE_VOID
        if node.ReturnValue != nil {
          var err error
          retType, err = tc.typeCheckExpression(node.ReturnValue, sig.RetType)
          if err != nil {
            return false, err
          }

          if !shared.CanCoerceTo(retType, sig.RetType) {
            return false, shared.NewError(node.Loc, "wrong return type for function, expected '%s' got '%s'", sig.RetType, retType)
          }
          if retVal, ok := node.ReturnValue.(*parser.NumberLiteralNode); ok && shared.IsNumericType(sig.RetType) {
            retVal.ExprType = sig.RetType
          }
        } else if sig.RetType != shared.PRIMITIVE_VOID {
          return false, shared.NewError(node.Loc, "wrong return type for function, expected '%s' got void", sig.RetType)
        }
        foundReturn = true

        if i != len(blockNode.Body)-1 {
          return false, shared.NewError(blockNode.Body[i+1].GetLoc(), "dead code following return statement")
        }

      case parser.KEYWORD_TYPE_BREAK, parser.KEYWORD_TYPE_CONTINUE:
        if !isLoop {
          return false, shared.NewError(node.Loc, "'%s' statement outside a loop", node.Keyword)
        }
      }

    case *parser.BlockNode:
      tc.enterScope()
      returns, err := tc.typeCheckBlock(node, sig, isLoop, false)
      if err != nil {
        return false, err
      }
      tc.exitScope()

      if i == len(blockNode.Body)-1 && !foundReturn {
        foundReturn = returns
      }

    case *parser.ForNode:
      if err := tc.typeCheckForLoop(node, sig); err != nil {
        return false, err
      }

    case *parser.IfNode:
      tc.enterScope()
      returns, err := tc.typeCheckIfStatement(node, sig, isLoop)
      if err != nil {
        return false, err
      }
      tc.exitScope()

      if i == len(blockNode.Body)-1 && !foundReturn {
        foundReturn = returns
      }

    default:
      return false, shared.NewError(node.GetLoc(), "unexpected node in block when type checking")
    }
  }

  if isMainBody {
    if !foundReturn && sig.RetType != shared.PRIMITIVE_VOID {
      return false, shared.NewError(blockNode.Loc, "function with return type '%s' is missing a return statement", sig.RetType)
    }

    sig.ImplicitReturn = !foundReturn
  }

  return foundReturn, nil
}

func (tc *TypeChecker) typeCheckExpression(en parser.ExpressionNode, expectedType shared.Type) (shared.Type, error) {
  switch exprNode := en.(type) {
  case *parser.IdentifierNode:
    varSig, ok := tc.VarTable.Lookup(exprNode.Name)
    if !ok {
      return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "undefined variable '%s'", exprNode.Name)
    }
    gotType, err := ResolveFieldChain(exprNode, varSig.Type)
    if err != nil {
      return shared.PRIMITIVE_VOID, err
    }
    exprNode.ExprType = gotType
    return gotType, nil

  case *parser.NumberLiteralNode:
    exprType := shared.PRIMITIVE_UNTYPED_INT
    if expectedType != shared.PRIMITIVE_VOID && shared.CanCoerceTo(exprType, expectedType) {
      exprNode.ExprType = expectedType
      return expectedType, nil
    }
    exprNode.ExprType = exprType
    return exprType, nil

  case *parser.CharLiteralNode:
    exprNode.ExprType = shared.PRIMITIVE_CHAR
    return shared.PRIMITIVE_CHAR, nil

  case *parser.StringLiteralNode:
    exprNode.ExprType = shared.PRIMITIVE_CSTRING
    return shared.PRIMITIVE_CSTRING, nil

  case *parser.BoolLiteralNode:
    exprNode.ExprType = shared.PRIMITIVE_BOOL
    return shared.PRIMITIVE_BOOL, nil

  case *parser.FunctionCallNode:
    return tc.typeCheckFunctionCall(exprNode)

  case *parser.UnaryOpNode:
    operandType, err := tc.typeCheckExpression(exprNode.Operand, shared.PRIMITIVE_VOID)
    if err != nil {
      return shared.PRIMITIVE_VOID, err
    }

    if nlNode, ok := exprNode.Operand.(parser.NumberLiteralNode);
      ok && expectedType != shared.PRIMITIVE_VOID && operandType == shared.PRIMITIVE_UNTYPED_INT && shared.IsNumericType(expectedType) {
      nlNode.ExprType = expectedType
      operandType = expectedType
    }

    switch exprNode.Op {
    case parser.UNARY_OP_LOGICAL_NOT:
      if operandType != shared.PRIMITIVE_BOOL {
        return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Operand.GetLoc(), "unary operator 'not' expects a boolean operand, found '%s'", operandType)
      }
      exprNode.ExprType = shared.PRIMITIVE_BOOL
      return shared.PRIMITIVE_BOOL, nil
    case parser.UNARY_OP_NEGATE:
      if !shared.IsNumericType(operandType) {
        return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Operand.GetLoc(), "unary operator '-' requires numeric operand, found '%s'", operandType)
      }
      exprNode.ExprType = operandType
      return operandType, nil
    case parser.UNARY_OP_DEREFERENCE:
      if !operandType.IsPointer() {
        return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Operand.GetLoc(), "unary operator '*' requires a pointer operand, found '%s'", operandType)
      }
      t := operandType.(shared.Pointer).To
      exprNode.ExprType = t
      return t, nil
    case parser.UNARY_OP_REFERENCE:
      identifier, ok := exprNode.Operand.(*parser.IdentifierNode)
      if !ok {
        return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Operand.GetLoc(), "unary operator '&' requires an identifier operand")
      }
      varName := identifier.Name
      varSig, ok := tc.VarTable.Lookup(varName)
      if !ok {
        return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Operand.GetLoc(), "undefined variable '%s'", varName)
      }
      t := shared.Pointer{
        To: operandType,
        Const: !varSig.Mutable,
      }
      exprNode.ExprType = t
      return t, nil
    default:
      return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "unknown operator")
    }

  case *parser.BinaryOpNode:
    leftType, err := tc.typeCheckExpression(exprNode.Operand1, shared.PRIMITIVE_VOID)
    if err != nil {
      return shared.PRIMITIVE_VOID, err
    }
    rightType, err := tc.typeCheckExpression(exprNode.Operand2, shared.PRIMITIVE_VOID)
    if err != nil {
      return shared.PRIMITIVE_VOID, err
    }

    if expectedType != shared.PRIMITIVE_VOID {
      if leftType == shared.PRIMITIVE_UNTYPED_INT && shared.IsNumericType(expectedType) {
        exprNode.Operand1.SetType(expectedType)
        leftType = expectedType
      }
      if rightType == shared.PRIMITIVE_UNTYPED_INT && shared.IsNumericType(expectedType) {
        exprNode.Operand2.SetType(expectedType)
        rightType = expectedType
      }
    }

    switch exprNode.Op {
    case parser.BINARY_OP_LOGICAL_AND, parser.BINARY_OP_LOGICAL_OR:
      if leftType != shared.PRIMITIVE_BOOL || rightType != shared.PRIMITIVE_BOOL {
        return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "operator '%s' expects boolean operands, found '%s' and '%s'", exprNode.Op, leftType, rightType)
      }
      exprNode.ExprType = shared.PRIMITIVE_BOOL
      return shared.PRIMITIVE_BOOL, nil

    case parser.BINARY_OP_EQUAL, parser.BINARY_OP_NOT_EQUAL,
      parser.BINARY_OP_LESS, parser.BINARY_OP_LESS_EQUAL,
      parser.BINARY_OP_GREATER, parser.BINARY_OP_GREATER_EQUAL:
      if !shared.IsNumericType(leftType) || !shared.IsNumericType(rightType) {
        return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "operator '%s' cannot be applied to operands of type '%s' and '%s'", exprNode.Op, leftType, rightType)
      }
      exprNode.ExprType = shared.PRIMITIVE_BOOL
      return shared.PRIMITIVE_BOOL, nil

    case parser.BINARY_OP_ADD, parser.BINARY_OP_SUBTRACT,
      parser.BINARY_OP_MULTIPLY, parser.BINARY_OP_DIVIDE,
      parser.BINARY_OP_MODULO:
      if !shared.IsNumericType(leftType) || !shared.IsNumericType(rightType) {
        return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "operator '%s' requires numeric operands, found '%s' and '%s'", exprNode.Op, leftType, rightType)
      }
      commonType := shared.BiggerNumericType(leftType, rightType)
      exprNode.Operand1.SetType(commonType)
      exprNode.Operand2.SetType(commonType)
      exprNode.ExprType = commonType
      return commonType, nil

    default:
      return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "unknown operator")
    }

  case *parser.CastNode:
    tpeName := exprNode.ToType.Name
    tpe, ok := tc.TypeTable.Lookup(tpeName)
    if !ok {
      return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "no such type '%s'", tpeName)
    }
    exTpe, err := tc.typeCheckExpression(exprNode.Operand, shared.PRIMITIVE_VOID)
    if err != nil {
      return shared.PRIMITIVE_VOID, err
    }

    if exTpe == shared.PRIMITIVE_UNTYPED_INT {
      exprNode.Operand.SetType(tpe)
    }

    if shared.CanCastTo(exTpe, tpe) {
      exprNode.ExprType = tpe
      return tpe, nil
    } else {
      return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "cannot cast type '%s' to '%s'", exTpe, tpe)
    }

  case *parser.IfExprNode:
    condType, err := tc.typeCheckExpression(exprNode.IfBranch.Condition, shared.PRIMITIVE_BOOL)
    if err != nil {
      return shared.PRIMITIVE_VOID, err
    }
    if condType != shared.PRIMITIVE_BOOL {
      return shared.PRIMITIVE_VOID, shared.NewError(exprNode.IfBranch.Condition.GetLoc(), "if condition must be boolean, found '%s'", condType)
    }

    ifType, err := tc.typeCheckExpression(exprNode.IfBranch.Node, expectedType)
    if err != nil {
      return shared.PRIMITIVE_VOID, err
    }
    commonType := ifType

    for _, elseIf := range exprNode.ElseIfBranches {
      elseIfCondType, err := tc.typeCheckExpression(elseIf.Condition, shared.PRIMITIVE_BOOL)
      if err != nil {
        return shared.PRIMITIVE_VOID, err
      }
      if elseIfCondType != shared.PRIMITIVE_BOOL {
        return shared.PRIMITIVE_VOID, shared.NewError(elseIf.Condition.GetLoc(), "else-if condition must be boolean, found '%s'", elseIfCondType)
      }

      elseIfType, err := tc.typeCheckExpression(elseIf.Node, expectedType)
      if err != nil {
        return shared.PRIMITIVE_VOID, err
      }
      if elseIfType != commonType {
        return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "all branches must return same type, expected '%s' but found '%s'", commonType, elseIfType)
      }
    }

    elseType, err := tc.typeCheckExpression(exprNode.ElseBranch, expectedType)
    if err != nil {
      return shared.PRIMITIVE_VOID, err
    }
    if elseType != commonType {
      return shared.PRIMITIVE_VOID, shared.NewError(exprNode.ElseBranch.GetLoc(), "else branch must match type '%s', found '%s'", commonType, elseType)
    }

    exprNode.ExprType = commonType
    return commonType, nil

  case *parser.StructLiteralNode:
    name := exprNode.Name.Name
    structType, ok := tc.TypeTable.Lookup(name)
    if !ok {
      return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "no such struct type '%s'", name)
    }
    st, ok := structType.(shared.Struct)
    if !ok {
      return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "'%s' is not a struct type", name)
    }
    if len(st.Fields) != len(exprNode.Fields) {
      return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "struct '%s' expects %d fields, got %d (zero values are not allowed)", name, len(st.Fields), len(exprNode.Fields))
    }
    for _, field := range exprNode.Fields {
      var fieldType shared.Type = nil
      for _, f := range st.Fields {
        if f.L == field.L {
          fieldType = f.R
        }
      }
      if fieldType == nil {
        return shared.PRIMITIVE_VOID, shared.NewError(field.R.GetLoc(), "struct '%s' has no field named '%s'", name, field.L)
      }
      exprType, err := tc.typeCheckExpression(field.R, fieldType)
      if err != nil {
        return shared.PRIMITIVE_VOID, err
      }
      if exprType != fieldType {
        return shared.PRIMITIVE_VOID, shared.NewError(field.R.GetLoc(), "field '%s' of struct '%s' expects type '%s', got '%s'", field.L, name, fieldType, exprType)
      }
      field.R.SetType(fieldType)
    }
    exprNode.ExprType = structType
    return structType, nil

  default:
    return shared.PRIMITIVE_VOID, shared.NewError(exprNode.GetLoc(), "unsupported expression type")
  }
}

func (tc *TypeChecker) typeCheckFunctionCall(funccallNode *parser.FunctionCallNode) (shared.Type, error) {
  fname := funccallNode.Name.Name

  fsig, ok := tc.FuncTable.Lookup(fname)
  if !ok {
    return shared.PRIMITIVE_VOID, shared.NewError(funccallNode.Name.Loc, "undefined function '%s'", fname)
  }

  if (len(funccallNode.Args) > len(fsig.ArgTypes) && !fsig.HasVariadic) || len(funccallNode.Args) < len(fsig.ArgTypes) {
    return shared.PRIMITIVE_VOID, shared.NewError(funccallNode.Loc, "argument count mismatch, expected at least %d, got %d", len(fsig.ArgTypes), len(funccallNode.Args))
  }

  for i, arg := range funccallNode.Args {
    var fsigArgType shared.Type = shared.PRIMITIVE_VOID
    if i < len(fsig.ArgTypes) {
      fsigArgType = fsig.ArgTypes[i].R
    }
    argType, err := tc.typeCheckExpression(arg, fsigArgType)
    if err != nil {
      return shared.PRIMITIVE_VOID, err
    }
    if fsigArgType != argType && fsigArgType != shared.PRIMITIVE_VOID {
      return shared.PRIMITIVE_VOID, shared.NewError(arg.GetLoc(),
        "argument %d of function '%s' has type '%s' but expected '%s'",
        i+1, fname, argType, fsigArgType,
      )
    }
  }

  funccallNode.ExprType = fsig.RetType
  return fsig.RetType, nil
}

func (tc *TypeChecker) typeCheckDeclaration(declNode *parser.DeclarationNode) (string, *VarSig, error) {
  if strings.HasPrefix(declNode.Name, "___") {
    return "", nil, shared.NewError(declNode.Loc, "variables starting with '___' are reserved for the compiler")
  }
  mutable := declNode.Mutable
  var varType shared.Type = shared.PRIMITIVE_VOID

  if declNode.Type != nil {
    varTypeStr := declNode.Type.Name
    vt, ok := tc.TypeTable.Lookup(varTypeStr)
    if !ok {
      return "", nil, shared.NewError(declNode.Type.Loc, "variable '%s' has undefined type '%s'", declNode.Name, varTypeStr)
    }
    varType = vt
  }

  exprType, err := tc.typeCheckExpression(declNode.Value, varType)
  if err != nil {
    return "", nil, err
  }

  if varType == shared.PRIMITIVE_VOID {
    varType = exprType
  } else if !shared.CanCoerceTo(exprType, varType) {
    return "", nil, shared.NewError(declNode.Loc,
      "cannot assign value of type '%s' to variable '%s' of type '%s'",
      exprType, declNode.Name, varType,
    )
  }

  if varType == shared.PRIMITIVE_UNTYPED_INT {
    varType = shared.PRIMITIVE_I32
    exprType = shared.PRIMITIVE_I32
  }
  if varType == shared.PRIMITIVE_VOID {
    return "", nil, shared.NewError(declNode.Loc, "a variable cannot be of type void")
  }

  declNode.Value.SetType(varType)

  return declNode.Name, &VarSig{
    Type: varType,
    Mutable: mutable,
  }, nil
}

func (tc *TypeChecker) typeCheckAssignment(asNode *parser.AssignmentNode) error {
  switch assignee := asNode.Assignee.(type) {
  case *parser.IdentifierNode:
    return tc.typeCheckIdentifierAssignment(asNode, assignee)
  case *parser.UnaryOpNode:
    if assignee.Op == parser.UNARY_OP_DEREFERENCE {
      return tc.typeCheckPointerAssignment(asNode, assignee)
    }
  }

  return shared.NewError(asNode.Assignee.GetLoc(), "left side of assignment must be a variable or dereferenced pointer")
}

func (tc *TypeChecker) typeCheckIdentifierAssignment(asNode *parser.AssignmentNode, assignee *parser.IdentifierNode) error {
  varName := assignee.Name
  varSig, ok := tc.VarTable.Lookup(varName)
  if !ok {
    return shared.NewError(assignee.Loc, "undefined variable '%s'", varName)
  }

  if !varSig.Mutable {
    return shared.NewError(asNode.Loc, "cannot assign to immutable variable '%s'", varName)
  }

  currType, err := ResolveFieldChain(assignee, varSig.Type)
  if err != nil {
    return err
  }

  exprType, err := tc.typeCheckExpression(asNode.Value, currType)
  if err != nil {
    return err
  }

  if exprType != currType {
    return shared.NewError(asNode.Loc,
      "cannot assign value of type '%s' to variable '%s' of type '%s'",
      exprType, varName, varSig.Type,
      )
  }
  return nil
}

func (tc *TypeChecker) typeCheckPointerAssignment(asNode *parser.AssignmentNode, assignee *parser.UnaryOpNode) error {
  switch ident := assignee.Operand.(type) {
  case *parser.IdentifierNode:
    ptrType, err := tc.typeCheckExpression(assignee.Operand, shared.PRIMITIVE_VOID)
    if err != nil {
      return err
    }
    if !ptrType.IsPointer() {
      return shared.NewError(assignee.Loc, "left side of assignment must be a pointer, found '%s'", ptrType)
    }
    ptr := ptrType.(shared.Pointer)
    if ptr.Const {
      return shared.NewError(assignee.Loc, "cannot perform pointer assignment if the pointee is immutable")
    }

    
    currType, err := ResolveFieldChain(ident, ptr.To)
    if err != nil {
      return err
    }

    exprType, err := tc.typeCheckExpression(asNode.Value, currType)
    if err != nil {
      return err
    }
    if exprType != currType {
      return shared.NewError(asNode.Loc,
        "cannot assign value of type '%s' to variable of type '%s'",
        exprType, currType,
      )
    }
  default:
    return shared.NewError(assignee.Operand.GetLoc(), "left side of assignment must be a pointer to a variable")
  }
  return nil
}

func (tc *TypeChecker) typeCheckForLoop(loopNode *parser.ForNode, sig *FunctionSig) error {
  tc.enterScope()

  if len(loopNode.ExprsOrStmts) == 1 {
    condMb := loopNode.ExprsOrStmts[0]
    if _, ok := condMb.(parser.ExpressionNode); !ok {
      return shared.NewError(condMb.GetLoc(), "for loop condition must be a boolean expression")
    }
    cond := condMb.(parser.ExpressionNode)
    exprType, err := tc.typeCheckExpression(cond, shared.PRIMITIVE_BOOL)
    if err != nil {
      return err
    }

    if exprType != shared.PRIMITIVE_BOOL {
      return shared.NewError(cond.GetLoc(), "for loop condition must be a boolean expression, found '%s'", exprType)
    }
  } else if len(loopNode.ExprsOrStmts) == 3 {
    switch init := loopNode.ExprsOrStmts[0].(type) {
    case *parser.DeclarationNode:
      varName, varSig, err := tc.typeCheckDeclaration(init)
      if err != nil {
        return err
      }

      tc.VarTable.Define(varName, varSig)
    case *parser.AssignmentNode:
      if err := tc.typeCheckAssignment(init); err != nil {
        return err
      }
    default:
      return shared.NewError(init.GetLoc(), "for loop initialiser must be a declaration or assignment statement")
    }

    condMb := loopNode.ExprsOrStmts[1]
    if _, ok := condMb.(parser.ExpressionNode); !ok {
      return shared.NewError(condMb.GetLoc(), "for loop condition must be a boolean expression")
    }
    cond := condMb.(parser.ExpressionNode)
    exprType, err := tc.typeCheckExpression(cond, shared.PRIMITIVE_BOOL)
    if err != nil {
      return err
    }

    if exprType != shared.PRIMITIVE_BOOL {
      return shared.NewError(cond.GetLoc(), "for loop condition must be a boolean expression, found '%s'", exprType)
    }

    switch reass := loopNode.ExprsOrStmts[2].(type) {
    case *parser.AssignmentNode:
      if err := tc.typeCheckAssignment(reass); err != nil {
        return err
      }
    default:
      return shared.NewError(reass.GetLoc(), "for loop 'after' step must be an assignment statement")
    }
  } else if len(loopNode.ExprsOrStmts) != 0 {
    return shared.NewError(loopNode.Loc, "for loop must have either: no conditions, one condition or an initialiser, a condition and an 'after' assignment")
  }

  if _, err := tc.typeCheckBlock(loopNode.Body, sig, true, false); err != nil {
    return err
  }

  tc.exitScope()
  return nil
}

func (tc *TypeChecker) typeCheckIfStatement(ifNode *parser.IfNode, sig *FunctionSig, isLoop bool) (bool, error) {
  ifCondType, err := tc.typeCheckExpression(ifNode.IfBranch.Condition, shared.PRIMITIVE_BOOL)
  if err != nil {
    return false, err
  }
  if ifCondType != shared.PRIMITIVE_BOOL {
    return false, shared.NewError(ifNode.IfBranch.Condition.GetLoc(), "if condition must be boolean, found '%s'", ifCondType)
  }

  ifReturns, err := tc.typeCheckBlock(ifNode.IfBranch.Node, sig, isLoop, false)
  if err != nil {
    return false, err
  }
  alwaysReturns := ifReturns && ifNode.ElseBranch != nil

  for _, elseIfBranch := range ifNode.ElseIfBranches {
    elseIfCondType, err := tc.typeCheckExpression(elseIfBranch.Condition, shared.PRIMITIVE_BOOL)
    if err != nil {
      return false, err
    }
    if elseIfCondType != shared.PRIMITIVE_BOOL {
      return false, shared.NewError(elseIfBranch.Condition.GetLoc(), "else-if condition must be boolean, found '%s'", elseIfCondType)
    }

    elseIfReturns, err := tc.typeCheckBlock(elseIfBranch.Node, sig, isLoop, false)
    if err != nil {
      return false, err
    }
    if !alwaysReturns {
      alwaysReturns = elseIfReturns
    }
  }

  if ifNode.ElseBranch != nil {
    elseReturns, err := tc.typeCheckBlock(ifNode.ElseBranch, sig, isLoop, false)
    if err != nil {
      return false, err
    }
    if !alwaysReturns {
      alwaysReturns = elseReturns
    }
  }

  return alwaysReturns, nil
}


func ResolveFieldChain(field *parser.IdentifierNode, tpe shared.Type) (shared.Type, error) {
  if field.Next == nil {
    return tpe, nil
  }

  var strct shared.Struct
  if tpe.IsStruct() {
    strct = tpe.(shared.Struct)
  } else if tpe.IsPointer() && tpe.(shared.Pointer).To.IsStruct() {
    strct = tpe.(shared.Pointer).To.(shared.Struct)
  }

  for _, f := range strct.Fields {
    if f.L == field.Next.Name {
      return ResolveFieldChain(field.Next, f.R)
    }
  }

  return nil, shared.NewError(field.Next.Loc, "type has no field named '%s'", field.Next.Name)
}

func (tc *TypeChecker) ExtractFunctionSig(functionNode *parser.FunctionDefNode) (*FunctionSig, error) {
  var retType shared.Type
  if (functionNode.RetType != nil) {
    retTypeStr := functionNode.RetType.Name

    r, ok := tc.TypeTable.Lookup(retTypeStr)
    if !ok {
      return nil, shared.NewError(functionNode.RetType.Loc, "function '%s' has undefined return type '%s'", functionNode.Name, retTypeStr)
    }
    retType = r
  } else {
    if functionNode.Name == "main" {
      retType = shared.PRIMITIVE_I32
    } else {
      retType = shared.PRIMITIVE_VOID
    }
  }

  argTypes := make([]shared.Pair[string, shared.Type], len(functionNode.Args))
  for i, arg := range functionNode.Args {
    tpe := arg.R.Name
    resolved, ok := tc.TypeTable.Lookup(tpe)
    if !ok {
      return nil, shared.NewError(arg.R.Loc, "parameter '%s' has undefined type '%s'", arg.L, tpe)
    }
    argTypes[i] = shared.Pair[string, shared.Type]{L: arg.L, R: resolved}
  }

  return &FunctionSig{
    ArgTypes: argTypes,
    RetType: retType,
    Name: functionNode.Name,
    HasVariadic: functionNode.HasVariadic,
  }, nil
}
