package parser

import "github.com/marzeq/quokka/shared"

type Node interface { GetLoc() shared.Location }
type ExpressionNode interface {
  Node
  GetType() shared.Type
  SetType(t shared.Type)
}

type RootNode struct {
  Body []Node
  Loc shared.Location
}
func (n RootNode) GetLoc() shared.Location { return n.Loc }

type IdentifierNode struct {
  Name string
  Next *IdentifierNode
  Loc shared.Location
  ExprType shared.Type
}
func (n IdentifierNode) GetLoc() shared.Location { return n.Loc }
func (n IdentifierNode) GetType() shared.Type { return n.ExprType }
func (n IdentifierNode) SetType(t shared.Type) { n.ExprType = t }

type BoolLiteralNode struct {
  Value string
  Loc shared.Location
  ExprType shared.Type
}
func (n BoolLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n BoolLiteralNode) GetType() shared.Type { return n.ExprType }
func (n BoolLiteralNode) SetType(t shared.Type) { n.ExprType = t }

type NumberLiteralNode struct {
  Value string
  Loc shared.Location
  ExprType shared.Type
}
func (n NumberLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n NumberLiteralNode) GetType() shared.Type { return n.ExprType }
func (n NumberLiteralNode) SetType(t shared.Type) { n.ExprType = t }

type StringLiteralNode struct {
  Value string
  Loc shared.Location
  ExprType shared.Type
}
func (n StringLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n StringLiteralNode) GetType() shared.Type { return n.ExprType }
func (n StringLiteralNode) SetType(t shared.Type) { n.ExprType = t }

type CharLiteralNode struct {
  Value byte
  Loc shared.Location
  ExprType shared.Type
}
func (n CharLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n CharLiteralNode) GetType() shared.Type { return n.ExprType }
func (n CharLiteralNode) SetType(t shared.Type) { n.ExprType = t }

type StructLiteralNode struct {
  Name *IdentifierNode
  Fields []shared.Pair[string, Node] // field name, value
  Loc shared.Location
  ExprType shared.Type
}
func (n StructLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n StructLiteralNode) GetType() shared.Type { return n.ExprType }
func (n StructLiteralNode) SetType(t shared.Type) { n.ExprType = t }

type FunctionCallNode struct {
  Name *IdentifierNode
  Args []ExpressionNode
  Loc shared.Location
  ExprType shared.Type
}
func (n FunctionCallNode) GetLoc() shared.Location { return n.Loc }
func (n FunctionCallNode) GetType() shared.Type { return n.ExprType }
func (n FunctionCallNode) SetType(t shared.Type) { n.ExprType = t }

type IfExprBranch struct {
  Condition ExpressionNode
  Node ExpressionNode
}

type IfExprNode struct {
  IfBranch IfExprBranch
  ElseIfBranches []IfExprBranch
  ElseBranch ExpressionNode
  Loc shared.Location
  ExprType shared.Type
}
func (n IfExprNode) GetLoc() shared.Location { return n.Loc }
func (n IfExprNode) GetType() shared.Type { return n.ExprType }
func (n IfExprNode) SetType(t shared.Type) { n.ExprType = t }

type UnaryOpType uint

const (
  UNARY_OP_LOGICAL_NOT UnaryOpType = iota
  UNARY_OP_NEGATE
  UNARY_OP_REFERENCE
  UNARY_OP_DEREFERENCE
)

func (u UnaryOpType) String() string {
  switch u {
  case UNARY_OP_LOGICAL_NOT: return "not"
  case UNARY_OP_NEGATE: return "-"
  case UNARY_OP_REFERENCE: return "&"
  case UNARY_OP_DEREFERENCE: return "*"
  default: return "unknown"
  }
}

type UnaryOpNode struct {
  Op UnaryOpType
  Operand ExpressionNode
  Loc shared.Location
  ExprType shared.Type
}
func (n UnaryOpNode) GetLoc() shared.Location { return n.Loc }
func (n UnaryOpNode) GetType() shared.Type { return n.ExprType }
func (n UnaryOpNode) SetType(t shared.Type) { n.ExprType = t }

type BinaryOpType uint

const (
  BINARY_OP_LOGICAL_OR BinaryOpType = iota
  BINARY_OP_LOGICAL_AND
  BINARY_OP_EQUAL
  BINARY_OP_NOT_EQUAL
  BINARY_OP_LESS
  BINARY_OP_LESS_EQUAL
  BINARY_OP_GREATER
  BINARY_OP_GREATER_EQUAL
  BINARY_OP_ADD
  BINARY_OP_SUBTRACT
  BINARY_OP_MULTIPLY
  BINARY_OP_DIVIDE
  BINARY_OP_MODULO
)

type BinaryOpNode struct {
  Op BinaryOpType
  Operand1 ExpressionNode
  Operand2 ExpressionNode
  Loc shared.Location
  ExprType shared.Type
}
func (n BinaryOpNode) GetLoc() shared.Location { return n.Loc }
func (n BinaryOpNode) GetType() shared.Type { return n.ExprType }
func (n BinaryOpNode) SetType(t shared.Type) { n.ExprType = t }

type CastNode struct {
  ToType *IdentifierNode
  Operand ExpressionNode
  Loc shared.Location
  ExprType shared.Type
}
func (n CastNode) GetLoc() shared.Location { return n.Loc }
func (n CastNode) GetType() shared.Type { return n.ExprType }
func (n CastNode) SetType(t shared.Type) { n.ExprType = t }

type ImportNode struct {
  Module string
  Loc shared.Location
}
func (n ImportNode) GetLoc() shared.Location { return n.Loc }

type FunctionDefNode struct {
  Name string
  Args []shared.Pair[string, *IdentifierNode] // arg name, arg type
  RetType *IdentifierNode
  Body Node
  HasVariadic bool
  Loc shared.Location
}
func (n FunctionDefNode) GetLoc() shared.Location { return n.Loc }

type StructDefNode struct {
  Name string
  Fields []shared.Pair[string, *IdentifierNode] // field name, field type
  Loc shared.Location
}
func (n StructDefNode) GetLoc() shared.Location { return n.Loc }

type IfBranch struct {
  Condition ExpressionNode
  Node *BlockNode
}
type IfNode struct {
  IfBranch IfBranch
  ElseIfBranches []IfBranch
  ElseBranch *BlockNode
  Loc shared.Location
}
func (n IfNode) GetLoc() shared.Location { return n.Loc }

type ForNode struct {
  ExprsOrStmts []Node
  Body *BlockNode
  Loc shared.Location
}
func (n ForNode) GetLoc() shared.Location { return n.Loc }

type KeywordType uint

const (
  KEYWORD_TYPE_BREAK KeywordType = iota
  KEYWORD_TYPE_CONTINUE
  KEYWORD_TYPE_RETURN
)

func (k KeywordType) String() string {
  switch k {
  case KEYWORD_TYPE_BREAK: return "break"
  case KEYWORD_TYPE_CONTINUE: return "continue"
  case KEYWORD_TYPE_RETURN: return "return"
  default: return "unknown"
  }
}

func KeywordTypeFromString(s string) (KeywordType, bool) {
  switch s {
  case "break": return KEYWORD_TYPE_BREAK, true
  case "continue": return KEYWORD_TYPE_CONTINUE, true
  case "return": return KEYWORD_TYPE_RETURN, true
  default: return 0, false
  }
}

type ControlKeywordNode struct {
  Keyword KeywordType
  ReturnValue ExpressionNode // only for "return"
  Loc shared.Location
}
func (n ControlKeywordNode) GetLoc() shared.Location { return n.Loc }

type DeclarationNode struct {
  Name string
  Mutable bool
  Type *IdentifierNode
  Value ExpressionNode
  Loc shared.Location
}
func (n DeclarationNode) GetLoc() shared.Location { return n.Loc }

type AssignmentNode struct {
  Assignee Node
  Value ExpressionNode
  Loc shared.Location
}
func (n AssignmentNode) GetLoc() shared.Location { return n.Loc }

type BlockNode struct {
  Body []Node
  Loc shared.Location
}
func (n BlockNode) GetLoc() shared.Location { return n.Loc }
