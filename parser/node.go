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
	Name               string
	Module             string
	ResolvedModuleName string
	TypeArguments      []TypeNode
	ResolvedTypeArgs   []types.Type
	Loc                shared.Location
	Symbol             *symbols.Symbol
	Type               types.Type
}

func (n IdentifierNode) GetLoc() shared.Location { return n.Loc }
func (n IdentifierNode) String() string {
	if n.Module == "" {
		return n.Name
	}
	return n.Module + "." + n.Name
}
func (n *IdentifierNode) SetType(t types.Type)       { n.Type = t }
func (n *IdentifierNode) GetType() types.Type        { return n.Type }
func (n *IdentifierNode) GetSymbol() *symbols.Symbol { return n.Symbol }

type FieldAccessNode struct {
	Subject ExpressionNode
	Field   *IdentifierNode

	Loc          shared.Location
	Type         types.Type
	EnumValue    string
	IsEnumValue  bool
	IsFlagValue  bool
	IsFlagTest   bool
	FlagValue    string
	FlagType     types.Type
	MethodSymbol *symbols.Symbol
	MethodModule string
	// ResolvedIdentifier is set when this dotted access names a declaration in
	// an imported module rather than a value field.
	ResolvedIdentifier *IdentifierNode
	// ModulePath is the canonical path of an intermediate module namespace.
	ModulePath string
}

func (n FieldAccessNode) GetLoc() shared.Location { return n.Loc }
func (n *FieldAccessNode) SetType(t types.Type)   { n.Type = t }
func (n *FieldAccessNode) GetType() types.Type    { return n.Type }

type EnumLiteralNode struct {
	Variant string
	Value   string
	Loc     shared.Location
	Type    types.Type
}

func (n EnumLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n *EnumLiteralNode) SetType(t types.Type)   { n.Type = t }
func (n *EnumLiteralNode) GetType() types.Type    { return n.Type }

type NamedTypeNode struct {
	ModName       string
	Name          string
	TypeArguments []TypeNode

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

type EnumTypeNode struct {
	Name     string
	Module   string
	Variants []string
	Values   []string
	Loc      shared.Location
}

func (n EnumTypeNode) GetLoc() shared.Location { return n.Loc }
func (n EnumTypeNode) _type()                  {}

type FlagsTypeNode struct {
	Name       string
	Module     string
	Underlying TypeNode
	Variants   []string
	Values     []string
	Loc        shared.Location
}

func (n FlagsTypeNode) GetLoc() shared.Location { return n.Loc }
func (n FlagsTypeNode) _type()                  {}

type UnionTypeNode struct {
	Name   string
	Module string
	Fields []StructField
	Loc    shared.Location
}

func (n UnionTypeNode) GetLoc() shared.Location { return n.Loc }
func (n UnionTypeNode) _type()                  {}

type OpaqueTypeNode struct {
	Loc shared.Location
}

func (n OpaqueTypeNode) GetLoc() shared.Location { return n.Loc }
func (n OpaqueTypeNode) _type()                  {}

type TraitMethodNode struct {
	Name       string
	Receiver   MethodReceiverKind
	Args       []*FunctionNodeArg
	ReturnType TypeNode
	Loc        shared.Location
}

type TraitTypeNode struct {
	Methods []TraitMethodNode
	Loc     shared.Location
}

func (n TraitTypeNode) GetLoc() shared.Location { return n.Loc }
func (n TraitTypeNode) _type()                  {}

type SliceTypeNode struct {
	ElementType TypeNode
	Size        int
	Loc         shared.Location
}

func (n SliceTypeNode) GetLoc() shared.Location { return n.Loc }
func (n SliceTypeNode) _type()                  {}

type PointerTypeNode struct {
	BaseType TypeNode
	Mutable  bool
	Loc      shared.Location
}

func (n PointerTypeNode) GetLoc() shared.Location { return n.Loc }
func (n PointerTypeNode) _type()                  {}

type DynTypeNode struct {
	TraitType TypeNode
	Mutable   bool
	Loc       shared.Location
}

func (n DynTypeNode) GetLoc() shared.Location { return n.Loc }
func (n DynTypeNode) _type()                  {}

type FunctionTypeNode struct {
	Parameters    []TypeNode
	ReturnType    TypeNode
	TypedVariadic bool
	Loc           shared.Location
}

type MultipleReturnTypeNode struct {
	Types []TypeNode
	Loc   shared.Location
}

func (n MultipleReturnTypeNode) GetLoc() shared.Location { return n.Loc }
func (n MultipleReturnTypeNode) _type()                  {}

func (n FunctionTypeNode) GetLoc() shared.Location { return n.Loc }
func (n FunctionTypeNode) _type()                  {}

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

type CStringLiteralNode struct {
	Value string
	Loc   shared.Location
	Type  types.Type
}

func (n CStringLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n *CStringLiteralNode) SetType(t types.Type)   { n.Type = t }
func (n *CStringLiteralNode) GetType() types.Type    { return n.Type }

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

type NoInitializerNode struct {
	Loc  shared.Location
	Type types.Type
}

func (n NoInitializerNode) GetLoc() shared.Location { return n.Loc }
func (n *NoInitializerNode) SetType(t types.Type)   { n.Type = t }
func (n *NoInitializerNode) GetType() types.Type    { return n.Type }

type StructLiteralNode struct {
	Name            *IdentifierNode
	Fields          []shared.Pair[string, ExpressionNode] // field name, value
	FlagMembers     []string
	NoInitRemaining bool
	Loc             shared.Location
	Symbol          *symbols.Symbol
	Type            types.Type
}

func (n StructLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n *StructLiteralNode) SetType(t types.Type)   { n.Type = t }
func (n *StructLiteralNode) GetType() types.Type    { return n.Type }

type SliceLiteralNode struct {
	Elements     []ExpressionNode
	RepeatValue  ExpressionNode
	RepeatAmount ExpressionNode
	Loc          shared.Location
	Type         types.Type
}

func (n SliceLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n *SliceLiteralNode) SetType(t types.Type)   { n.Type = t }
func (n *SliceLiteralNode) GetType() types.Type    { return n.Type }

type FunctionCallNode struct {
	Callee ExpressionNode
	// Name and Symbol are populated for direct calls, preserving linkage and
	// foreign-ABI information that is not part of a callable value's type.
	Name   *IdentifierNode
	Args   []ExpressionNode
	Loc    shared.Location
	Symbol *symbols.Symbol
	Method bool

	TraitCall bool
	TraitSlot int

	TypedVariadic      bool
	TypedVariadicStart int
	TypedVariadicSlice types.Type
	VariadicExpansion  bool
}

func (n FunctionCallNode) GetLoc() shared.Location { return n.Loc }
func (n *FunctionCallNode) SetType(t types.Type)   {}
func (n *FunctionCallNode) GetType() types.Type {
	if n.Symbol != nil && n.Symbol.Kind == symbols.SymbolKindFunction && n.Symbol.Signature != nil {
		if n.Symbol.Signature.ReturnType == nil {
			return types.PrimitiveVoid
		}
		return n.Symbol.Signature.ReturnType
	}
	if n.Callee != nil {
		if ptr, ok := types.Underlying(n.Callee.GetType()).(types.PointerType); ok {
			if fn, ok := types.Underlying(ptr.Base).(types.FunctionType); ok {
				return fn.ReturnType
			}
		}
	}
	return types.ErrorType{}
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
	UnaryOpMutableReference
	UnaryOpDereference
	UnaryOpSliceLen
	UnaryOpBitwiseNot
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
	case UnaryOpBitwiseNot:
		return "~"
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

type SliceExprNode struct {
	Subject ExpressionNode
	Start   ExpressionNode
	End     ExpressionNode
	Loc     shared.Location
	Type    types.Type
}

func (n SliceExprNode) GetLoc() shared.Location { return n.Loc }
func (n *SliceExprNode) SetType(t types.Type)   { n.Type = t }
func (n *SliceExprNode) GetType() types.Type    { return n.Type }

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
	BinaryOpBitwiseAnd
	BinaryOpBitwiseXor
	BinaryOpBitwiseOr
	BinaryOpShiftLeft
	BinaryOpShiftRight
)

func (b BinaryOpKind) String() string {
	switch b {
	case BinaryOpLogicalOr:
		return "or"
	case BinaryOpLogicalAnd:
		return "and"
	case BinaryOpEqual:
		return "=="
	case BinaryOpNotEqual:
		return "!="
	case BinaryOpLess:
		return "<"
	case BinaryOpLessEqual:
		return "<="
	case BinaryOpGreater:
		return ">"
	case BinaryOpGreaterEqual:
		return ">="
	case BinaryOpAdd:
		return "+"
	case BinaryOpSubtract:
		return "-"
	case BinaryOpMultiply:
		return "*"
	case BinaryOpDivide:
		return "/"
	case BinaryOpModulo:
		return "%"
	case BinaryOpBitwiseAnd:
		return "&"
	case BinaryOpBitwiseXor:
		return "^"
	case BinaryOpBitwiseOr:
		return "|"
	case BinaryOpShiftLeft:
		return "<<"
	case BinaryOpShiftRight:
		return ">>"
	default:
		return "unknown"
	}
}

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

	TraitConversion  bool
	TraitRecast      bool
	TraitUnwrap      bool
	ConcreteType     types.Type
	TraitMethods     []*symbols.Symbol
	TraitCandidates  []TraitCastCandidate
	Checked          bool
	CheckedType      types.Type
	GenericAssertion bool
	AssertionMatches bool
	StaticTraitView  *types.StaticTraitView
}

type TraitCastCandidate struct {
	ConcreteType types.Type
	Methods      []*symbols.Symbol
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

type AlignOfNode struct {
	Operand     TypeNode
	Expression  ExpressionNode
	OperandType types.Type
	Loc         shared.Location
	Type        types.Type
}

func (n AlignOfNode) GetLoc() shared.Location { return n.Loc }
func (n *AlignOfNode) SetType(t types.Type)   { n.Type = t }
func (n *AlignOfNode) GetType() types.Type    { return n.Type }

type OffsetOfNode struct {
	Operand     TypeNode
	OperandType types.Type
	Field       string
	Loc         shared.Location
	Type        types.Type
}

func (n OffsetOfNode) GetLoc() shared.Location { return n.Loc }
func (n *OffsetOfNode) SetType(t types.Type)   { n.Type = t }
func (n *OffsetOfNode) GetType() types.Type    { return n.Type }

type ImportNode struct {
	Modules []string
	Aliases []string
	Loc     shared.Location
}

func (n ImportNode) GetLoc() shared.Location { return n.Loc }

type ModuleNode struct {
	Name       string
	Attributes attributes.Attributes
	Loc        shared.Location
}

func (n ModuleNode) GetLoc() shared.Location { return n.Loc }

type FunctionNodeArg struct {
	Name    string
	Type    TypeNode
	Default ExpressionNode
	Mutable bool
	Symbol  *symbols.Symbol
}

func (a FunctionNodeArg) GetLoc() shared.Location { return a.Type.GetLoc() }

type GenericParameterNode struct {
	Name       string
	Constraint TypeNode
	Loc        shared.Location
}

func (n GenericParameterNode) GetLoc() shared.Location { return n.Loc }

type FunctionDefNode struct {
	Name              string
	MethodOwner       string
	Receiver          MethodReceiverKind
	GenericParameters []GenericParameterNode
	Args              []*FunctionNodeArg
	RetTypeNode       TypeNode
	Body              Node
	HasVariadic       bool
	TypedVariadic     bool
	Pub               bool
	Attributes        attributes.Attributes
	Loc               shared.Location
	Symbol            *symbols.Symbol
	GenericInstance   bool
}

type MethodReceiverKind uint8

const (
	MethodReceiverNone MethodReceiverKind = iota
	MethodReceiverValue
	MethodReceiverPointer
	MethodReceiverMutablePointer
)

func (n FunctionDefNode) GetLoc() shared.Location { return n.Loc }

type TypeAliasNode struct {
	Name              string
	GenericParameters []GenericParameterNode
	Type              TypeNode
	Transparent       bool
	Pub               bool
	Loc               shared.Location
	Symbol            *symbols.Symbol
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

type RangeForNode struct {
	Name      string
	Start     ExpressionNode
	End       ExpressionNode
	Inclusive bool
	Body      *BlockNode
	Loc       shared.Location
	Symbol    *symbols.Symbol
}

func (n RangeForNode) GetLoc() shared.Location { return n.Loc }

type ForEachNode struct {
	Name     string
	Iterable ExpressionNode
	Body     *BlockNode
	Loc      shared.Location
	Symbol   *symbols.Symbol
}

func (n ForEachNode) GetLoc() shared.Location { return n.Loc }

type ControlKeywordNode struct {
	Keyword      tokeniser.KeywordKind
	ReturnValue  ExpressionNode // only for "return"
	ReturnValues []ExpressionNode
	Loc          shared.Location
}

func (n ControlKeywordNode) GetLoc() shared.Location { return n.Loc }

type DeferNode struct {
	Action Node
	Loc    shared.Location
}

func (n DeferNode) GetLoc() shared.Location { return n.Loc }

type DeclarationNode struct {
	Name              string
	GenericParameters []GenericParameterNode
	Mutable           bool
	Pub               bool
	TypeNode          TypeNode
	Value             ExpressionNode
	Comptime          bool
	Attributes        attributes.Attributes
	Loc               shared.Location
	Symbol            *symbols.Symbol
}

type MultiDeclarationNode struct {
	Names   []string
	Value   ExpressionNode
	Loc     shared.Location
	Symbols []*symbols.Symbol
}

func (n MultiDeclarationNode) GetLoc() shared.Location { return n.Loc }

func (n DeclarationNode) GetLoc() shared.Location { return n.Loc }

type AssignmentNode struct {
	Assignee  ExpressionNode
	Assignees []ExpressionNode
	Value     ExpressionNode
	Compound  bool
	Loc       shared.Location
}

func (n AssignmentNode) GetLoc() shared.Location { return n.Loc }

type BlockNode struct {
	Body []Node
	Loc  shared.Location
}

func (n BlockNode) GetLoc() shared.Location { return n.Loc }
