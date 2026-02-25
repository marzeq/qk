package parser

import (
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/tokeniser"
	"github.com/marzeq/qk/types"
)

type (
	Node           interface{ GetLoc() shared.Location }
	ExpressionNode interface {
		Node
		_expression()
	}
)

type RootNode struct {
	Body []Node
	Loc  shared.Location
}

func (n RootNode) GetLoc() shared.Location { return n.Loc }

type IdentifierNode struct {
	Name   string
	Next   *IdentifierNode
	Loc    shared.Location
	Symbol *symbols.Symbol
	Type   types.Type
}

func (n IdentifierNode) GetLoc() shared.Location { return n.Loc }
func (n IdentifierNode) String() string {
	if n.Next != nil {
		return n.Name + "." + n.Next.String()
	}
	return n.Name
}
func (n IdentifierNode) _expression() {}

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
func (n ModuleAccessNode) _expression() {}

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

type ArrayTypeNode struct {
	ElementType TypeNode
	Size        int
	Loc         shared.Location
}

func (n ArrayTypeNode) GetLoc() shared.Location { return n.Loc }
func (n ArrayTypeNode) _type()                  {}

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
func (n BoolLiteralNode) _expression()            {}

type IntegerLiteralNode struct {
	Value string
	Loc   shared.Location
	Type  types.Type
}

func (n IntegerLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n IntegerLiteralNode) _expression()            {}

type FloatLiteralNode struct {
	Value string
	Loc   shared.Location
	Type  types.Type
}

func (n FloatLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n FloatLiteralNode) _expression()            {}

type StringLiteralNode struct {
	Value string
	Loc   shared.Location
	Type  types.Type
}

func (n StringLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n StringLiteralNode) _expression()            {}

type CharLiteralNode struct {
	Value byte
	Loc   shared.Location
	Type  types.Type
}

func (n CharLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n CharLiteralNode) _expression()            {}

type NilLiteralNode struct {
	Loc  shared.Location
	Type types.Type
}

func (n NilLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n NilLiteralNode) _expression()            {}

type StructLiteralNode struct {
	Name   *ModuleAccessNode
	Fields []shared.Pair[string, ExpressionNode] // field name, value
	Loc    shared.Location
	Symbol *symbols.Symbol
	Type   types.Type
}

func (n StructLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n StructLiteralNode) _expression()            {}

type ArrayLiteralNode struct {
	Elements []ExpressionNode
	Loc      shared.Location
	Type     types.Type
}

func (n ArrayLiteralNode) GetLoc() shared.Location { return n.Loc }
func (n ArrayLiteralNode) _expression()            {}

type FunctionCallNode struct {
	Name   *ModuleAccessNode
	Args   []ExpressionNode
	Loc    shared.Location
	Symbol *symbols.Symbol
	Type   types.Type
}

func (n FunctionCallNode) GetLoc() shared.Location { return n.Loc }
func (n FunctionCallNode) _expression()            {}

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
func (n IfExprNode) _expression()            {}

type GivenExprNode struct {
	Block     *BlockNode
	FinalExpr ExpressionNode
	Loc       shared.Location
	Type      types.Type
}

func (n GivenExprNode) GetLoc() shared.Location { return n.Loc }
func (n GivenExprNode) _expression()            {}

type UnaryOpType uint

const (
	UNARY_OP_LOGICAL_NOT UnaryOpType = iota
	UNARY_OP_NEGATE
	UNARY_OP_REFERENCE
	UNARY_OP_DEREFERENCE
	UNARY_OP_ARRAY_LEN
)

func (u UnaryOpType) String() string {
	switch u {
	case UNARY_OP_LOGICAL_NOT:
		return "not"
	case UNARY_OP_NEGATE:
		return "-"
	case UNARY_OP_REFERENCE:
		return "&"
	case UNARY_OP_DEREFERENCE:
		return "*"
	case UNARY_OP_ARRAY_LEN:
		return "[]"
	default:
		return "unknown"
	}
}

type UnaryOpNode struct {
	Op      UnaryOpType
	Operand ExpressionNode
	Loc     shared.Location
	Type    types.Type
}

func (n UnaryOpNode) GetLoc() shared.Location { return n.Loc }
func (n UnaryOpNode) _expression()            {}

type IndexExprNode struct {
	Subject ExpressionNode
	Index   ExpressionNode
	Loc     shared.Location
	Type    types.Type
}

func (n IndexExprNode) GetLoc() shared.Location { return n.Loc }
func (n IndexExprNode) _expression()            {}

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
	Op       BinaryOpType
	Operand1 ExpressionNode
	Operand2 ExpressionNode
	Loc      shared.Location
	Type     types.Type
}

func (n BinaryOpNode) GetLoc() shared.Location { return n.Loc }
func (n BinaryOpNode) _expression()            {}

type CastNode struct {
	ToType  TypeNode
	Operand ExpressionNode
	Loc     shared.Location
	Type    types.Type
}

func (n CastNode) GetLoc() shared.Location { return n.Loc }
func (n CastNode) _expression()            {}

type SizeOfNode struct {
	Operand TypeNode
	Loc     shared.Location
	Type    types.Type
}

func (n SizeOfNode) GetLoc() shared.Location { return n.Loc }
func (n SizeOfNode) _expression()            {}

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

type FunctionNodeType struct {
	Name    string
	Type    TypeNode
	Mutable bool
}
type FunctionDefNode struct {
	Name        string
	Args        []FunctionNodeType
	RetType     FunctionNodeType
	Body        Node
	ExternFrom  string
	HasVariadic bool
	Extern      bool
	Pub         bool
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
	Keyword     tokeniser.KeywordType
	ReturnValue ExpressionNode // only for "return"
	Loc         shared.Location
}

func (n ControlKeywordNode) GetLoc() shared.Location { return n.Loc }

type DeclarationNode struct {
	Name    string
	Mutable bool
	Pub     bool
	Type    TypeNode
	Value   ExpressionNode
	Loc     shared.Location
	Symbol  *symbols.Symbol
}

func (n DeclarationNode) GetLoc() shared.Location { return n.Loc }

type AssignmentNode struct {
	Assignee Node
	Value    ExpressionNode
	Loc      shared.Location
}

func (n AssignmentNode) GetLoc() shared.Location { return n.Loc }

type ArrayAssignmentNode struct {
	Assignee ExpressionNode
	Index    ExpressionNode
	Value    ExpressionNode
	Loc      shared.Location
}

func (n ArrayAssignmentNode) GetLoc() shared.Location { return n.Loc }

type BlockNode struct {
	Body []Node
	Loc  shared.Location
}

func (n BlockNode) GetLoc() shared.Location { return n.Loc }
