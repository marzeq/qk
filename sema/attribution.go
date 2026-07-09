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

func (a *Attributor) AttributeModule(root *parser.RootNode) {
	a.attributeNode(root)
}

func (a *Attributor) Errors() []error {
	return a.errors
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

		if n.ExternFrom != "" && n.RetTypeNode == nil {
			a.errorf(n, "extern function must have a return type annotation")
		}

		if n.Symbol.Signature.ReturnType == nil {
			switch b := n.Body.(type) {
			case *parser.BlockNode:
				returnNodes := collectFunctionReturnNodes(b.Body)
				if len(returnNodes) > 0 {
					var current types.Type

					first := returnNodes[0]
					if first.ReturnValue == nil {
						current = types.PrimitiveVoid
					} else {
						current = first.ReturnValue.GetType()
					}

					for _, returnNode := range returnNodes[1:] {
						var t types.Type
						if returnNode.ReturnValue == nil {
							t = types.PrimitiveVoid
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
					n.Symbol.Signature.ReturnType = types.PrimitiveVoid
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

	case *parser.PointerAssignmentNode:
		a.attributeExpr(n.Assignee)
		a.attributeExpr(n.Value)

	case *parser.IndexAssignmentNode:
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

	case *parser.ModuleAccessNode:
		if n.Symbol == nil || n.Symbol.Type == nil {
			a.errorf(n, "undefined identifier: %s:%s", n.ModName, n.Ident)
			n.SetType(types.ErrorType{})
		} else {
			n.SetType(n.Symbol.Type)
		}

	case *parser.IntegerLiteralNode:
		n.SetType(types.UntypedInt{})

	case *parser.FloatLiteralNode:
		n.SetType(types.UntypedFloat{})

	case *parser.BoolLiteralNode:
		n.SetType(types.PrimitiveBool)

	case *parser.StringLiteralNode:
		n.SetType(types.SliceType{
			Base: types.PrimitiveChar,
			Size: len(n.Value),
		})

	case *parser.CharLiteralNode:
		n.SetType(types.PrimitiveChar)

	case *parser.NilLiteralNode:
		n.SetType(types.PointerType{
			Base: types.PrimitiveVoid,
		})

	case *parser.StructLiteralNode:
		for _, field := range n.Fields {
			a.attributeExpr(field.R)
		}

		if n.Symbol != nil {
			n.SetType(n.Symbol.TypeInfo)
		} else {
			fields := make([]shared.Pair[string, types.Type], 0, len(n.Fields))
			for _, field := range n.Fields {
				fields = append(fields, shared.Pair[string, types.Type]{
					L: field.L,
					R: field.R.GetType(),
				})
			}
			n.SetType(types.StructType{Fields: fields})
		}

	case *parser.SliceLiteralNode:
		typs := []types.Type{}
		for _, elem := range n.Elements {
			a.attributeExpr(elem)
			typs = append(typs, elem.GetType())
		}
		if len(typs) > 0 {
			currentType := typs[0]
			for _, t := range typs[1:] {
				got := types.PromoteNumeric(currentType, t)
				if got.Equals(types.ErrorType{}) {
					a.errorf(n, "inconsistent slice element types: %v and %v", got, t)
				}
				currentType = got
			}
			n.SetType(types.SliceType{
				Base: currentType,
				Size: len(n.Elements),
			})
		} else {
			n.SetType(types.SliceType{
				Base: types.PrimitiveVoid,
				Size: 0,
			})
		}

	case *parser.FunctionCallNode:
		if n.Symbol.Signature.ReturnType != nil {
			n.SetType(n.Symbol.Signature.ReturnType)
		} else {
			n.SetType(types.PrimitiveVoid)
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
		case parser.UnaryOpNegate:
			if types.IsSigned(n.Operand.GetType()) || types.IsFloat(n.Operand.GetType()) {
				n.SetType(n.Operand.GetType())
			}
		case parser.UnaryOpLogicalNot:
			if n.Operand.GetType().Equals(types.PrimitiveBool) {
				n.SetType(types.PrimitiveBool)
			} else {
				a.errorf(n, "logical not operator requires a boolean operand")
				n.SetType(types.ErrorType{})
			}
		case parser.UnaryOpReference:
			n.SetType(types.PointerType{
				Base: n.Operand.GetType(),
			})
		case parser.UnaryOpDereference:
			if ptr, ok := n.Operand.GetType().(types.PointerType); ok {
				n.SetType(ptr.Base)
			} else {
				a.errorf(n, "cannot dereference non-pointer type")
				n.SetType(types.ErrorType{})
			}
		case parser.UnaryOpSliceLen:
			switch n.Operand.GetType().(type) {
			case types.SliceType:
				n.SetType(types.PrimitiveUsz)
			default:
				a.errorf(n, "slice length operator requires a slice operand")
				n.SetType(types.ErrorType{})
			}
		default:
			panic("unhandled unary operator")
		}

	case *parser.IndexExprNode:
		a.attributeExpr(n.Subject)
		a.attributeExpr(n.Index)

		switch t := n.Subject.GetType().(type) {
		case types.SliceType:
			n.SetType(t.Base)
		case types.PointerType:
			n.SetType(t.Base)
		default:
			a.errorf(n, "cannot index type %v", n.Subject.GetType())
			n.SetType(types.ErrorType{})
		}

	case *parser.FieldAccessNode:
		a.attributeExpr(n.Subject)
		switch t := n.Subject.GetType().(type) {
		case types.StructType:
			found := false
			for _, field := range t.Fields {
				if field.L == n.Field.Name {
					n.SetType(field.R)
					found = true
					break
				}
			}
			if !found {
				a.errorf(n, "struct type %v does not have a field named %s", t, n.Field)
				n.SetType(types.ErrorType{})
			}
		default:
			a.errorf(n, "cannot access field of type %v", n.Subject.GetType())
			n.SetType(types.ErrorType{})
		}

	case *parser.BinaryOpNode:
		a.attributeExpr(n.Operand1)
		a.attributeExpr(n.Operand2)

		t1 := n.Operand1.GetType()
		t2 := n.Operand2.GetType()

		switch n.Op {
		case parser.BinaryOpAdd, parser.BinaryOpSubtract, parser.BinaryOpMultiply, parser.BinaryOpDivide, parser.BinaryOpModulo:
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
		case parser.BinaryOpEqual, parser.BinaryOpNotEqual, parser.BinaryOpLess, parser.BinaryOpLessEqual, parser.BinaryOpGreater, parser.BinaryOpGreaterEqual:
			if types.IsNumeric(t1) && types.IsNumeric(t2) {
				n.SetType(types.PrimitiveBool)
			} else if t1.Equals(t2) {
				n.SetType(types.PrimitiveBool)
			} else if types.IsPointer(t1) && types.IsPointer(t2) {
				n.SetType(types.PrimitiveBool)
			} else {
				a.errorf(n, "comparison operators require operands of the same type or both numeric types")
				n.SetType(types.ErrorType{})
			}
		case parser.BinaryOpLogicalAnd, parser.BinaryOpLogicalOr:
			if t1.Equals(types.PrimitiveBool) && t2.Equals(types.PrimitiveBool) {
				n.SetType(types.PrimitiveBool)
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
		n.SetType(types.PrimitiveUsz)

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
			if n.Keyword == tokeniser.KeywordReturn {
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
