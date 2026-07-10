package parser

import (
	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/tokeniser"
	"github.com/marzeq/qk/types"
)

type (
	Node           interface{ GetLoc() shared.Location }
	ExpressionNode interface {
		Node
		SetType(types.Type)
		GetType() types.Type
	}
)

type RootNode struct {
	Body []Node
	Loc  shared.Location
}

func (n RootNode) GetLoc() shared.Location { return n.Loc }

type IdentifierNode struct {
	Name   string
	Loc    shared.Location
	Symbol *symbols.Symbol
}

func (n IdentifierNode) GetLoc() shared.Location { return n.Loc }
func (n IdentifierNode) String() string          { return n.Name }
func (n *IdentifierNode) SetType(t types.Type)   {}
func (n *IdentifierNode) GetType() types.Type {
	if n.Symbol == nil {
		panic("identifier node has no symbol")
	}
	return n.Symbol.Type
}

type ModuleAccessNode struct {
	ModName string
	Ident   *IdentifierNode
	Loc     shared.Location
	Symbol  *symbols.Symbol
	Type    types.Type
}

func (n ModuleAccessNode) GetLoc() shared.Location { return n.Loc }
func (n ModuleAccessNode) String() string {
	if n.ModName == "" {
		return n.Ident.String()
	}
	return n.ModName + ":" + n.Ident.String()
}
func (n *ModuleAccessNode) SetType(t types.Type) { n.Type = t }
func (n *ModuleAccessNode) GetType() types.Type  { return n.Type }

type FieldAccessNode struct {
	Subject ExpressionNode
	Field   *IdentifierNode

	Loc  shared.Location
	Type types.Type
}

func (n FieldAccessNode) GetLoc() shared.Location { return n.Loc }
func (n *FieldAccessNode) SetType(t types.Type)   { n.Type = t }
func (n *FieldAccessNode) GetType() types.Type    { return n.Type }

type NamedTypeNode struct {
	ModName string
	Name    string

	Loc shared.Location
}

func (n NamedTypeNode) GetLoc() shared.Location { return n.Loc }
func (n NamedTypeNode) _type()                  {}

type StructField struct {
	Name string
	Type TypeNode
}
type StructTypeNode struct {
	Fields []StructField
	Loc    shared.Location
}

func (n StructTypeNode) GetLoc() shared.Location { return n.Loc }
func (n StructTypeNode) _type()                  {}

type SliceTypeNode struct {
	ElementType TypeNode
	Size        int
	Loc         shared.Location
}

func (n SliceTypeNode) GetLoc() shared.Location { return n.Loc }
func (n SliceTypeNode) _type()                  {}

type PointerTypeNode struct {
	BaseType TypeNode
	Loc      shared.Location
}

func (n PointerTypeNode) GetLoc() shared.Location { return n.Loc }
func (n PointerTypeNode) _type()                  {}

type TypeNode interface {
	Node

	_type() // marker method
}

type BoolLiteralNode struct {
	Value string
	Loc   shared.Location
	Type  types.Type
}

func (n BoolLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n *BoolLiteralNode) SetType(t types.Type)   { n.Type = t }
func (n *BoolLiteralNode) GetType() types.Type    { return n.Type }

type IntegerLiteralNode struct {
	Value string
	Loc   shared.Location
	Type  types.Type
}

func (n IntegerLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n *IntegerLiteralNode) SetType(t types.Type)   { n.Type = t }
func (n *IntegerLiteralNode) GetType() types.Type    { return n.Type }

type FloatLiteralNode struct {
	Value string
	Loc   shared.Location
	Type  types.Type
}

func (n FloatLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n *FloatLiteralNode) SetType(t types.Type)   { n.Type = t }
func (n *FloatLiteralNode) GetType() types.Type    { return n.Type }

type StringLiteralNode struct {
	Value string
	Loc   shared.Location
	Type  types.Type
}

func (n StringLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n *StringLiteralNode) SetType(t types.Type)   { n.Type = t }
func (n *StringLiteralNode) GetType() types.Type    { return n.Type }

type CharLiteralNode struct {
	Value byte
	Loc   shared.Location
	Type  types.Type
}

func (n CharLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n *CharLiteralNode) SetType(t types.Type)   { n.Type = t }
func (n *CharLiteralNode) GetType() types.Type    { return n.Type }

type NilLiteralNode struct {
	Loc  shared.Location
	Type types.Type
}

func (n NilLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n *NilLiteralNode) SetType(t types.Type)   { n.Type = t }
func (n *NilLiteralNode) GetType() types.Type    { return n.Type }

type StructLiteralNode struct {
	Name   *ModuleAccessNode
	Fields []shared.Pair[string, ExpressionNode] // field name, value
	Loc    shared.Location
	Symbol *symbols.Symbol
	Type   types.Type
}

func (n StructLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n *StructLiteralNode) SetType(t types.Type)   { n.Type = t }
func (n *StructLiteralNode) GetType() types.Type    { return n.Type }

type SliceLiteralNode struct {
	Elements []ExpressionNode
	Loc      shared.Location
	Type     types.Type
}

func (n SliceLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n *SliceLiteralNode) SetType(t types.Type)   { n.Type = t }
func (n *SliceLiteralNode) GetType() types.Type    { return n.Type }

type FunctionCallNode struct {
	Name   *ModuleAccessNode
	Args   []ExpressionNode
	Loc    shared.Location
	Symbol *symbols.Symbol
}

func (n FunctionCallNode) GetLoc() shared.Location { return n.Loc }
func (n *FunctionCallNode) SetType(t types.Type)   {}
func (n *FunctionCallNode) GetType() types.Type {
	if n.Symbol == nil || n.Symbol.Kind != symbols.SymbolKindFunction ||
		n.Symbol.Signature == nil {
		panic("invalid function call node")
	}
	if n.Symbol.Signature.ReturnType == nil {
		return types.PrimitiveVoid
	}
	return n.Symbol.Signature.ReturnType
}

type IfExprBranch struct {
	Condition ExpressionNode
	Node      ExpressionNode
}

type IfExprNode struct {
	IfBranch       IfExprBranch
	ElseIfBranches []IfExprBranch
	ElseBranch     ExpressionNode
	Loc            shared.Location
	Type           types.Type
}

func (n IfExprNode) GetLoc() shared.Location { return n.Loc }
func (n *IfExprNode) SetType(t types.Type)   { n.Type = t }
func (n *IfExprNode) GetType() types.Type    { return n.Type }

type GivenExprNode struct {
	Block     *BlockNode
	FinalExpr ExpressionNode
	Loc       shared.Location
	Type      types.Type
}

func (n GivenExprNode) GetLoc() shared.Location { return n.Loc }
func (n *GivenExprNode) SetType(t types.Type)   { n.Type = t }
func (n *GivenExprNode) GetType() types.Type    { return n.Type }

type UnaryOpKind uint

const (
	UnaryOpLogicalNot UnaryOpKind = iota
	UnaryOpNegate
	UnaryOpReference
	UnaryOpDereference
	UnaryOpSliceLen
)

func (u UnaryOpKind) String() string {
	switch u {
	case UnaryOpLogicalNot:
		return "not"
	case UnaryOpNegate:
		return "-"
	case UnaryOpReference:
		return "&"
	case UnaryOpDereference:
		return "*"
	case UnaryOpSliceLen:
		return "[]"
	default:
		return "unknown"
	}
}

type UnaryOpNode struct {
	Op      UnaryOpKind
	Operand ExpressionNode
	Loc     shared.Location
	Type    types.Type
}

func (n UnaryOpNode) GetLoc() shared.Location { return n.Loc }
func (n *UnaryOpNode) SetType(t types.Type)   { n.Type = t }
func (n *UnaryOpNode) GetType() types.Type    { return n.Type }

type IndexExprNode struct {
	Subject ExpressionNode
	Index   ExpressionNode
	Loc     shared.Location
	Type    types.Type
}

func (n IndexExprNode) GetLoc() shared.Location { return n.Loc }
func (n *IndexExprNode) SetType(t types.Type)   { n.Type = t }
func (n *IndexExprNode) GetType() types.Type    { return n.Type }

type BinaryOpKind uint

const (
	BinaryOpLogicalOr BinaryOpKind = iota
	BinaryOpLogicalAnd
	BinaryOpEqual
	BinaryOpNotEqual
	BinaryOpLess
	BinaryOpLessEqual
	BinaryOpGreater
	BinaryOpGreaterEqual
	BinaryOpAdd
	BinaryOpSubtract
	BinaryOpMultiply
	BinaryOpDivide
	BinaryOpModulo
)

type BinaryOpNode struct {
	Op       BinaryOpKind
	Operand1 ExpressionNode
	Operand2 ExpressionNode
	Loc      shared.Location
	Type     types.Type
}

func (n BinaryOpNode) GetLoc() shared.Location { return n.Loc }
func (n *BinaryOpNode) SetType(t types.Type)   { n.Type = t }
func (n *BinaryOpNode) GetType() types.Type    { return n.Type }

type CastNode struct {
	ToType  TypeNode
	Operand ExpressionNode
	Loc     shared.Location
	Type    types.Type
}

func (n CastNode) GetLoc() shared.Location { return n.Loc }
func (n *CastNode) SetType(t types.Type)   { n.Type = t }
func (n *CastNode) GetType() types.Type    { return n.Type }

type SizeOfNode struct {
	Operand     TypeNode
	OperandType types.Type
	Loc         shared.Location
	Type        types.Type
}

func (n SizeOfNode) GetLoc() shared.Location { return n.Loc }
func (n *SizeOfNode) SetType(t types.Type)   { n.Type = t }
func (n *SizeOfNode) GetType() types.Type    { return n.Type }

type SizeOfExprNode struct {
	Operand     ExpressionNode
	OperandType types.Type
	Loc         shared.Location
	Type        types.Type
}

func (n SizeOfExprNode) GetLoc() shared.Location { return n.Loc }
func (n *SizeOfExprNode) SetType(t types.Type)   { n.Type = t }
func (n *SizeOfExprNode) GetType() types.Type    { return n.Type }

type ImportNode struct {
	Modules []string
	Loc     shared.Location
}

func (n ImportNode) GetLoc() shared.Location { return n.Loc }

type ModuleNode struct {
	Name string
	Loc  shared.Location
}

func (n ModuleNode) GetLoc() shared.Location { return n.Loc }

type FunctionNodeArg struct {
	Name    string
	Type    TypeNode
	Mutable bool
	Symbol  *symbols.Symbol
}

func (a FunctionNodeArg) GetLoc() shared.Location { return a.Type.GetLoc() }

type FunctionDefNode struct {
	Name        string
	Args        []*FunctionNodeArg
	RetTypeNode TypeNode
	Body        Node
	HasVariadic bool
	Extern      bool
	Pub         bool
	Attributes  attributes.Attributes
	Loc         shared.Location
	Symbol      *symbols.Symbol
}

func (n FunctionDefNode) GetLoc() shared.Location { return n.Loc }

type TypeAliasNode struct {
	Name   string
	Type   TypeNode
	Pub    bool
	Loc    shared.Location
	Symbol *symbols.Symbol
}

func (n TypeAliasNode) GetLoc() shared.Location { return n.Loc }

type IfBranch struct {
	Condition ExpressionNode
	Node      *BlockNode
}
type IfNode struct {
	IfBranch       IfBranch
	ElseIfBranches []IfBranch
	ElseBranch     *BlockNode
	Loc            shared.Location
}

func (n IfNode) GetLoc() shared.Location { return n.Loc }

type ForNode struct {
	ExprsOrStmts []Node
	Body         *BlockNode
	Loc          shared.Location
}

func (n ForNode) GetLoc() shared.Location { return n.Loc }

type ControlKeywordNode struct {
	Keyword     tokeniser.KeywordKind
	ReturnValue ExpressionNode // only for "return"
	Loc         shared.Location
}

func (n ControlKeywordNode) GetLoc() shared.Location { return n.Loc }

type DeclarationNode struct {
	Name     string
	Mutable  bool
	Pub      bool
	TypeNode TypeNode
	Value    ExpressionNode
	Loc      shared.Location
	Symbol   *symbols.Symbol
}

func (n DeclarationNode) GetLoc() shared.Location { return n.Loc }

type AssignmentNode struct {
	Assignee ExpressionNode
	Value    ExpressionNode
	Loc      shared.Location
}

func (n AssignmentNode) GetLoc() shared.Location { return n.Loc }

type PointerAssignmentNode struct {
	Assignee ExpressionNode
	Value    ExpressionNode
	Loc      shared.Location
}

func (n PointerAssignmentNode) GetLoc() shared.Location { return n.Loc }

type MemberAssignmentNode struct {
	Assignee ExpressionNode
	Field    *IdentifierNode
	Value    ExpressionNode
	Loc      shared.Location
}

func (n MemberAssignmentNode) GetLoc() shared.Location { return n.Loc }

type IndexAssignmentNode struct {
	Assignee ExpressionNode
	Index    ExpressionNode
	Value    ExpressionNode
	Loc      shared.Location
}

func (n IndexAssignmentNode) GetLoc() shared.Location { return n.Loc }

type BlockNode struct {
	Body []Node
	Loc  shared.Location
}

func (n BlockNode) GetLoc() shared.Location { return n.Loc }
