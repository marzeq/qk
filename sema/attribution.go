package sema

import (
	"fmt"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
	"github.com/marzeq/qk/types"
)

type Attributor struct {
	analyser *Analyser
	errors   []error
}

func (a *Analyser) NewAttributor() *Attributor {
	return &Attributor{analyser: a}
}

func (a *Attributor) errorf(node parser.Node, format string, args ...any) {
	a.errors = append(a.errors, shared.NewError(node.GetLoc(), format, args...))
}

func (a *Attributor) AttributeModule(root *parser.RootNode) []error {
	a.attributeNode(root)

	if len(a.errors) > 0 {
		return a.errors
	}
	return nil
}

func (a *Attributor) attributeNode(node parser.Node) {
	switch n := node.(type) {
	case *parser.RootNode:
		for _, stmt := range n.Body {
			a.attributeNode(stmt)
		}

	case *parser.ImportNode, *parser.ModuleNode:
		// pass

	case *parser.TypeAliasNode:
		// pass

	case *parser.FunctionDefNode:
		if n.Body != nil {
			a.attributeNode(n.Body)
		}

		if n.Symbol.Signature.ReturnType == nil {
			switch b := n.Body.(type) {
			case *parser.BlockNode:
				returnNodes := collectFunctionReturnNodes(b.Body)
				if len(returnNodes) > 0 {
					var current types.Type

					first := returnNodes[0]
					if first.ReturnValue == nil {
						current = types.PRIMITIVE_VOID
					} else {
						current = first.ReturnValue.GetType()
					}

					for _, returnNode := range returnNodes[1:] {
						var t types.Type
						if returnNode.ReturnValue == nil {
							t = types.PRIMITIVE_VOID
						} else {
							t = returnNode.ReturnValue.GetType()
						}

						if types.IsNumeric(current) && types.IsNumeric(t) {
							got := types.PromoteNumeric(current, t)
							if current.Equals(types.ErrorType{}) {
								a.errorf(returnNode, "inconsistent return types: %v and %v", got, t)
							}
							current = got
							continue
						}

						if !current.Equals(t) {
							a.errorf(returnNode, "inconsistent return types: %v and %v", current, t)
							current = types.ErrorType{}
							break
						}
					}

					n.Symbol.Signature.ReturnType = current
				} else {
					n.Symbol.Signature.ReturnType = types.PRIMITIVE_VOID
				}

			case parser.ExpressionNode:
				n.Symbol.Signature.ReturnType = b.GetType()

			case nil:

			default:
				panic(fmt.Sprintf("unexpected function body type: %T\n", n.Body))
			}
		}

	case *parser.BlockNode:
		for _, stmt := range n.Body {
			a.attributeNode(stmt)
		}

	case *parser.DeclarationNode:
		if n.Value != nil {
			a.attributeExpr(n.Value)
		}

		if n.Value != nil && n.Symbol.Type == nil {
			n.Symbol.Type = n.Value.GetType()
		}

	case *parser.AssignmentNode:
		a.attributeExpr(n.Value)

	case *parser.ArrayAssignmentNode:
		a.attributeExpr(n.Assignee)
		a.attributeExpr(n.Value)
		a.attributeExpr(n.Index)

	case *parser.IfNode:
		a.attributeExpr(n.IfBranch.Condition)
		a.attributeNode(n.IfBranch.Node)
		for _, elif := range n.ElseIfBranches {
			a.attributeExpr(elif.Condition)
			a.attributeNode(elif.Node)
		}
		if n.ElseBranch != nil {
			a.attributeNode(n.ElseBranch)
		}

	case *parser.ForNode:
		for _, node := range n.ExprsOrStmts {
			a.attributeNode(node)
		}
		a.attributeNode(n.Body)

	case *parser.ControlKeywordNode:
		if n.ReturnValue != nil {
			a.attributeExpr(n.ReturnValue)
		}

	case parser.ExpressionNode:
		a.attributeExpr(n)

	default:
		panic(fmt.Sprintf("unexpected node type: %T\n", node))
	}
}

func (a *Attributor) attributeExpr(node parser.ExpressionNode) {
	switch n := node.(type) {
	case *parser.IdentifierNode:
		if n.Symbol == nil || n.Symbol.Type == nil {
			a.errorf(n, "undefined identifier: %s", n.Name)
			n.SetType(types.ErrorType{})
		} else {
			n.SetType(n.Symbol.Type)
		}

	case *parser.IntegerLiteralNode:
		n.SetType(types.UntypedInt{})

	case *parser.FloatLiteralNode:
		n.SetType(types.UntypedFloat{})

	case *parser.BoolLiteralNode:
		n.SetType(types.PRIMITIVE_BOOL)

	case *parser.StringLiteralNode:
		n.SetType(types.PointerType{
			Base: types.PRIMITIVE_CHAR,
		})

	case *parser.CharLiteralNode:
		n.SetType(types.PRIMITIVE_CHAR)

	case *parser.NilLiteralNode:
		n.SetType(types.PointerType{
			Base: types.PRIMITIVE_VOID,
		})

	case *parser.StructLiteralNode:
		if n.Symbol != nil {
			n.SetType(n.Symbol.TypeInfo)
		} else {
			a.errorf(n, "undefined struct type")
			n.SetType(types.ErrorType{})
		}
		for _, field := range n.Fields {
			a.attributeExpr(field.R)
		}

	case *parser.ArrayLiteralNode:
		typs := []types.Type{}
		for _, elem := range n.Elements {
			a.attributeExpr(elem)
			typs = append(typs, elem.GetType())
		}
		if len(typs) > 0 {
			currentType := typs[0]
			for _, t := range typs[1:] {
				got := types.PromoteNumeric(currentType, t)
				if currentType.Equals(types.ErrorType{}) {
					a.errorf(n, "inconsistent array element types: %v and %v", got, t)
				}
				currentType = got
			}
			n.SetType(types.ArrayType{
				Base: currentType,
				Size: len(n.Elements),
			})
		} else {
			n.SetType(types.ArrayType{
				Base: types.PRIMITIVE_VOID,
				Size: 0,
			})
		}

	case *parser.FunctionCallNode:
		if n.Symbol.Signature.ReturnType != nil {
			n.SetType(n.Symbol.Signature.ReturnType)
		} else {
			n.SetType(types.PRIMITIVE_VOID)
		}

		for _, arg := range n.Args {
			a.attributeExpr(arg)
		}

	case *parser.IfExprNode:
		a.attributeExpr(n.IfBranch.Condition)

		var current types.Type = types.ErrorType{}

		if n.IfBranch.Node != nil {
			a.attributeExpr(n.IfBranch.Node)
			current = n.IfBranch.Node.GetType()
		}

		for _, elif := range n.ElseIfBranches {
			a.attributeExpr(elif.Condition)
			a.attributeExpr(elif.Node)

			t := elif.Node.GetType()

			if types.IsNumeric(current) && types.IsNumeric(t) {
				current = types.PromoteNumeric(current, t)
				continue
			}

			if !current.Equals(t) {
				current = types.ErrorType{}
				break
			}
		}

		if n.ElseBranch != nil {
			a.attributeExpr(n.ElseBranch)
			t := n.ElseBranch.GetType()

			if types.IsNumeric(current) && types.IsNumeric(t) {
				current = types.PromoteNumeric(current, t)
			} else if !current.Equals(t) {
				current = types.ErrorType{}
			}
		}

		if current.Equals(types.ErrorType{}) {
			a.errorf(n, "inconsistent types in if expression branches")
		}
		n.SetType(current)

	case *parser.GivenExprNode:
		a.attributeNode(n.Block)
		a.attributeExpr(n.FinalExpr)
		n.SetType(n.FinalExpr.GetType())

	case *parser.UnaryOpNode:
		a.attributeExpr(n.Operand)
		switch n.Op {
		case parser.UNARY_OP_NEGATE:
			if types.IsSigned(n.Operand.GetType()) || types.IsFloat(n.Operand.GetType()) {
				n.SetType(n.Operand.GetType())
			}
		case parser.UNARY_OP_LOGICAL_NOT:
			if n.Operand.GetType().Equals(types.PRIMITIVE_BOOL) {
				n.SetType(types.PRIMITIVE_BOOL)
			} else {
				a.errorf(n, "logical not operator requires a boolean operand")
				n.SetType(types.ErrorType{})
			}
		case parser.UNARY_OP_REFERENCE:
			n.SetType(types.PointerType{
				Base: n.Operand.GetType(),
			})
		case parser.UNARY_OP_DEREFERENCE:
			if ptr, ok := n.Operand.GetType().(types.PointerType); ok {
				n.SetType(ptr.Base)
			} else {
				a.errorf(n, "cannot dereference non-pointer type")
				n.SetType(types.ErrorType{})
			}
		case parser.UNARY_OP_ARRAY_LEN:
			switch n.Operand.GetType().(type) {
			case types.ArrayType:
				n.SetType(types.PRIMITIVE_USZ)
			default:
				a.errorf(n, "array length operator requires an array operand")
				n.SetType(types.ErrorType{})
			}
		default:
			panic("unhandled unary operator")
		}

	case *parser.IndexExprNode:
		a.attributeExpr(n.Subject)
		a.attributeExpr(n.Index)

		switch t := n.Subject.GetType().(type) {
		case types.ArrayType:
			n.SetType(t.Base)
		case types.PointerType:
			n.SetType(t.Base)
		default:
			a.errorf(n, "cannot index type %v", n.Subject.GetType())
			n.SetType(types.ErrorType{})
		}

	case *parser.BinaryOpNode:
		a.attributeExpr(n.Operand1)
		a.attributeExpr(n.Operand2)

		t1 := n.Operand1.GetType()
		t2 := n.Operand2.GetType()

		switch n.Op {
		case parser.BINARY_OP_ADD, parser.BINARY_OP_SUBTRACT, parser.BINARY_OP_MULTIPLY, parser.BINARY_OP_DIVIDE, parser.BINARY_OP_MODULO:
			if types.IsNumeric(t1) && types.IsNumeric(t2) {
				got := types.PromoteNumeric(t1, t2)
				if got.Equals(types.ErrorType{}) {
					a.errorf(n, "incompatible types for binary operator: %v and %v", t1, t2)
					n.SetType(types.ErrorType{})
				} else {
					n.SetType(got)
				}
			} else {
				a.errorf(n, "arithmetic operators require numeric operands")
				n.SetType(types.ErrorType{})
			}
		case parser.BINARY_OP_EQUAL, parser.BINARY_OP_NOT_EQUAL, parser.BINARY_OP_LESS, parser.BINARY_OP_LESS_EQUAL, parser.BINARY_OP_GREATER, parser.BINARY_OP_GREATER_EQUAL:
			if types.IsNumeric(t1) && types.IsNumeric(t2) {
				n.SetType(types.PRIMITIVE_BOOL)
			} else if t1.Equals(t2) {
				n.SetType(types.PRIMITIVE_BOOL)
			} else if types.IsPointer(t1) && types.IsPointer(t2) {
				n.SetType(types.PRIMITIVE_BOOL)
			} else {
				a.errorf(n, "comparison operators require operands of the same type or both numeric types")
				n.SetType(types.ErrorType{})
			}
		case parser.BINARY_OP_LOGICAL_AND, parser.BINARY_OP_LOGICAL_OR:
			if t1.Equals(types.PRIMITIVE_BOOL) && t2.Equals(types.PRIMITIVE_BOOL) {
				n.SetType(types.PRIMITIVE_BOOL)
			} else {
				a.errorf(n, "logical operators require boolean operands")
				n.SetType(types.ErrorType{})
			}
		default:
			panic("unhandled binary operator")
		}

	case *parser.CastNode:
		a.attributeExpr(n.Operand)
		target := a.analyser.resolveTypeNode(n.ToType)
		n.SetType(target)

	case *parser.SizeOfNode:
		n.SetType(types.PRIMITIVE_USZ)

	default:
		panic(fmt.Sprintf("unexpected expression type: %T\n", node))
	}

	if node.GetType() == nil {
		panic("expression without type")
	}
}

func collectFunctionReturnNodes(body []parser.Node) []*parser.ControlKeywordNode {
	var returnNodes []*parser.ControlKeywordNode

	for _, stmt := range body {
		switch n := stmt.(type) {
		case *parser.ControlKeywordNode:
			if n.Keyword == tokeniser.KEYWORD_RETURN {
				returnNodes = append(returnNodes, n)
			}
		case *parser.IfNode:
			returnNodes = append(returnNodes, collectFunctionReturnNodes(n.IfBranch.Node.Body)...)
			for _, elif := range n.ElseIfBranches {
				returnNodes = append(returnNodes, collectFunctionReturnNodes(elif.Node.Body)...)
			}
			if n.ElseBranch != nil {
				returnNodes = append(returnNodes, collectFunctionReturnNodes(n.ElseBranch.Body)...)
			}
		case *parser.ForNode:
			returnNodes = append(returnNodes, collectFunctionReturnNodes(n.Body.Body)...)
		case *parser.BlockNode:
			returnNodes = append(returnNodes, collectFunctionReturnNodes(n.Body)...)
		}
	}

	return returnNodes
}
