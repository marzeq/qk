package parser

import (
	"github.com/marzeq/quokka/shared"
)

type NodeType uint

const (
	NODE_TYPE_ROOT NodeType = iota

	// EXPRESSIONS

	NODE_TYPE_IDENTIFIER
	NODE_TYPE_BOOL_LITERAL
	NODE_TYPE_NUMBER_LITERAL
	NODE_TYPE_STRING_LITERAL
	NODE_TYPE_CHAR_LITERAL

	NODE_TYPE_FUNCTION_CALL

	NODE_TYPE_IF_EXPR

	NODE_TYPE_UNARY_OP
	NODE_TYPE_BINARY_OP

	NODE_TYPE_CAST

	// STATEMENTS

	NODE_TYPE_IMPORT
	NODE_TYPE_FUNCTION_DEF
	NODE_TYPE_IF
	NODE_TYPE_FOR
	NODE_TYPE_CONTROL_KEYWORD

	NODE_TYPE_DECLARATION
	NODE_TYPE_ASSIGNMENT

	NODE_TYPE_BLOCK
)

func (nt NodeType) IsExpression() bool {
	return nt == NODE_TYPE_IDENTIFIER ||
		nt == NODE_TYPE_BOOL_LITERAL ||
		nt == NODE_TYPE_NUMBER_LITERAL ||
		nt == NODE_TYPE_STRING_LITERAL ||
		nt == NODE_TYPE_CHAR_LITERAL ||
		nt == NODE_TYPE_FUNCTION_CALL ||
		nt == NODE_TYPE_IF_EXPR ||
		nt == NODE_TYPE_UNARY_OP ||
		nt == NODE_TYPE_BINARY_OP ||
		nt == NODE_TYPE_CAST
}

func (nt NodeType) IsStatement() bool {
	return !nt.IsExpression()
}

func (nt NodeType) String() string {
	switch nt {
	case NODE_TYPE_ROOT:
		return "ROOT"
	case NODE_TYPE_IDENTIFIER:
		return "IDENTIFIER"
	case NODE_TYPE_CONTROL_KEYWORD:
		return "CONTROL_KEYWORD"
	case NODE_TYPE_NUMBER_LITERAL:
		return "NUMBER_LITERAL"
	case NODE_TYPE_STRING_LITERAL:
		return "STRING_LITERAL"
	case NODE_TYPE_CHAR_LITERAL:
		return "CHAR_LITERAL"
	case NODE_TYPE_BOOL_LITERAL:
		return "BOOL_LITERAL"
	case NODE_TYPE_FUNCTION_CALL:
		return "FUNCTION_CALL"
	case NODE_TYPE_UNARY_OP:
		return "UNARY_OP"
	case NODE_TYPE_BINARY_OP:
		return "BINARY_OP"
	case NODE_TYPE_CAST:
		return "CAST"
	case NODE_TYPE_IF:
		return "IF"
	case NODE_TYPE_IF_EXPR:
		return "IF_EXPR"
	case NODE_TYPE_DECLARATION:
		return "DECLARATION"
	case NODE_TYPE_ASSIGNMENT:
		return "ASSIGNMENT"
	case NODE_TYPE_BLOCK:
		return "BLOCK"
	case NODE_TYPE_FUNCTION_DEF:
		return "FUNCTION_DEF"
	case NODE_TYPE_IMPORT:
		return "IMPORT"
	case NODE_TYPE_FOR:
		return "FOR"
	default:
		return "UNKNOWN"
	}
}

type Node struct {
	Type     NodeType
	ExprType shared.Type // set and used in the typechecker, do not use in parser
	Value    any
	Left     *Node
	Right    *Node
	Children []*Node
	Loc      shared.Location
}

type IfNodeValue struct {
	IfBranch       *IfBranch
	ElseIfBranches []*IfBranch
	ElseBranch     *IfBranch
}

type IfBranch struct {
	Condition *Node
	Node      *Node
}

type ForLoopValue struct {
	ExprsOrStmts []*Node
	Body         *Node
}

type FunctionValue struct {
	Name    *Node
	Args    []shared.Pair[*Node, *Node]
	RetType *Node
}

type DeclarationValue struct {
	Mutable bool
	Type    *Node
}
