package sema

import (
	"fmt"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

type Validator struct {
	analyser        *Analyser
	currentFunction *symbols.Symbol
	errors          []error
}

func (a *Analyser) NewValidator() *Validator {
	return &Validator{analyser: a}
}

func (v *Validator) errorf(node parser.Node, format string, args ...any) {
	v.errors = append(v.errors, shared.NewError(node.GetLoc(), format, args...))
}

func (v *Validator) ValidateModule(root *parser.RootNode) []error {
	v.validateNode(root)
	return v.errors
}

func (v *Validator) validateNode(node parser.Node) {
	switch n := node.(type) {

	case *parser.RootNode:
		for _, stmt := range n.Body {
			v.validateNode(stmt)
		}

	case *parser.FunctionDefNode:
		prev := v.currentFunction
		v.currentFunction = n.Symbol

		if n.Body != nil {
			v.validateNode(n.Body)
		} else {
			if n.ExternFrom == "" {
				v.errorf(n, "function declaration missing body or extern")
				return
			}
		}

		v.currentFunction = prev

	case *parser.BlockNode:
		for _, stmt := range n.Body {
			switch s := stmt.(type) {
			case *parser.DeclarationNode:
				v.finaliseDeclaration(s)
			default:
				v.validateNode(s)
			}
		}

	case *parser.DeclarationNode:
		v.finaliseDeclaration(n)

	case *parser.AssignmentNode:
		v.validateAssignment(n)

	case *parser.ArrayAssignmentNode:
		v.validateArrayAssignment(n)

	case *parser.IfNode:
		v.validateIf(n)

	case *parser.ForNode:
		v.validateNode(n.Body)

	case *parser.ControlKeywordNode:
		if n.ReturnValue != nil {
			v.validateReturn(n)
		}

	case parser.ExpressionNode:
		v.validateExpr(n)

	case *parser.TypeAliasNode:
		// pass

	default:
		panic(fmt.Sprintf("unhandled node type %T", n))
	}
}

func (v *Validator) finaliseDeclaration(n *parser.DeclarationNode) {
	if n.Value == nil {
		return
	}

	v.validateExpr(n.Value)

	valueType := n.Value.GetType()

	if n.TypeNode != nil {
		declared := v.analyser.resolveTypeNode(n.TypeNode)

		if !valueType.CanCoerceTo(declared) {
			v.errorf(n, "cannot assign %v to %v", valueType, declared)
			return
		}

		if !valueType.Equals(declared) {
			n.Value = &parser.CastNode{
				Operand: n.Value,
				Type:    declared,
			}
		}

		n.Symbol.Type = declared
		return
	}

	if types.IsUntyped(valueType) {
		valueType = types.DefaultUntyped(valueType)
	}

	n.Symbol.Type = valueType
}

func (v *Validator) validateAssignment(n *parser.AssignmentNode) {
	if !v.validateLValue(n.Assignee) {
		return
	}

	v.validateExpr(n.Assignee)
	v.validateExpr(n.Value)

	lhsType := n.Assignee.GetType()
	rhsType := n.Value.GetType()

	if !rhsType.CanCoerceTo(lhsType) {
		v.errorf(n, "cannot assign %v to %v", rhsType, lhsType)
		return
	}

	if !rhsType.Equals(lhsType) {
		n.Value = &parser.CastNode{
			Operand: n.Value,
			Type:    lhsType,
		}
	}
}

func (v *Validator) validateLValue(expr parser.ExpressionNode) bool {
	switch e := expr.(type) {

	case *parser.IdentifierNode:
		if !e.Symbol.Mutable {
			v.errorf(e, "cannot assign to immutable symbol")
			return false
		}

	case *parser.UnaryOpNode:
		if e.Op == parser.UNARY_OP_DEREFERENCE {
			v.validateExpr(e.Operand)
			return true
		}
		v.errorf(expr, "invalid assignment target")
		return false

	default:

		v.errorf(expr, "invalid assignment target!")
		return false
	}

	return true
}

func (v *Validator) validateArrayAssignment(n *parser.ArrayAssignmentNode) {
	v.validateExpr(n.Assignee)
	v.validateExpr(n.Index)
	v.validateExpr(n.Value)

	containerType := n.Assignee.GetType()

	var base types.Type
	switch t := containerType.(type) {
	case types.ArrayType:
		base = t.Base
	case types.PointerType:
		base = t.Base
	default:
		v.errorf(n, "cannot index into non-array type")
		return
	}

	valueType := n.Value.GetType()

	if !valueType.CanCoerceTo(base) {
		v.errorf(n, "cannot assign %v to %v", valueType, base)
		return
	}

	if !valueType.Equals(base) {
		n.Value = &parser.CastNode{
			Operand: n.Value,
			Type:    base,
		}
	}
}

func (v *Validator) validateIf(n *parser.IfNode) {
	v.validateExpr(n.IfBranch.Condition)
	if !n.IfBranch.Condition.GetType().Equals(types.PRIMITIVE_BOOL) {
		v.errorf(n, "if condition must be bool")
	}

	v.validateNode(n.IfBranch.Node)

	for _, elif := range n.ElseIfBranches {
		v.validateExpr(elif.Condition)
		if !elif.Condition.GetType().Equals(types.PRIMITIVE_BOOL) {
			v.errorf(n, "elseif condition must be bool")
		}
		v.validateNode(elif.Node)
	}

	if n.ElseBranch != nil {
		v.validateNode(n.ElseBranch)
	}
}

func (v *Validator) validateReturn(n *parser.ControlKeywordNode) {
	if v.currentFunction == nil {
		v.errorf(n, "return outside of function")
		return
	}
	expected := v.currentFunction.Signature.ReturnType

	valueType := n.ReturnValue.GetType()

	if !valueType.CanCoerceTo(expected) {
		v.errorf(n, "invalid return type")
		return
	}

	if !valueType.Equals(expected) {
		n.ReturnValue = &parser.CastNode{
			Operand: n.ReturnValue,
			Type:    expected,
		}
	}
}

func (v *Validator) validateExpr(node parser.ExpressionNode) {
	if _, ok := node.GetType().(types.ErrorType); ok {
		return
	}

	switch n := node.(type) {

	case *parser.FunctionCallNode:
		if n.Symbol == nil || n.Symbol.Kind != symbols.SymbolKindFunction {
			v.errorf(n, "not callable")
			return
		}

		sig := n.Symbol.Signature

		if !sig.Variadic && len(n.Args) != len(sig.Parameters) {
			v.errorf(n, "wrong number of arguments")
			return
		}

		for i, arg := range n.Args {
			v.validateExpr(arg)

			if i >= len(sig.Parameters) {
				continue
			}

			paramType := sig.Parameters[i]
			argType := arg.GetType()

			if !argType.CanCoerceTo(paramType) {
				v.errorf(n, "cannot pass %v to %v", argType, paramType)
				continue
			}

			if !argType.Equals(paramType) {
				n.Args[i] = &parser.CastNode{
					Operand: arg,
					Type:    paramType,
				}
			}
		}

	case *parser.CastNode:
		v.validateExpr(n.Operand)

		if !n.Operand.GetType().CanCastTo(n.Type) {
			v.errorf(n, "invalid cast")
		}

	case *parser.UnaryOpNode:
		v.validateExpr(n.Operand)

		operandType := n.Operand.GetType()

		switch n.Op {

		case parser.UNARY_OP_LOGICAL_NOT:
			if !operandType.Equals(types.PRIMITIVE_BOOL) {
				v.errorf(n, "operator not requires bool")
			}

		case parser.UNARY_OP_NEGATE:
			if !types.IsNumeric(operandType) {
				v.errorf(n, "operator - requires numeric type")
			}

		case parser.UNARY_OP_REFERENCE:

		case parser.UNARY_OP_DEREFERENCE:
			if _, ok := operandType.(types.PointerType); !ok {
				v.errorf(n, "cannot dereference non-pointer type")
			}

		case parser.UNARY_OP_ARRAY_LEN:
			switch operandType.(type) {
			case types.ArrayType:
				// OK
			default:
				v.errorf(n, "[] operator requires array or pointer")
			}

		default:
			v.errorf(n, "unknown unary operator")
		}

	case *parser.BinaryOpNode:
		v.validateExpr(n.Operand1)
		v.validateExpr(n.Operand2)

		t1 := n.Operand1.GetType()
		t2 := n.Operand2.GetType()

		switch n.Op {

		case parser.BINARY_OP_ADD,
			parser.BINARY_OP_SUBTRACT,
			parser.BINARY_OP_MULTIPLY,
			parser.BINARY_OP_DIVIDE,
			parser.BINARY_OP_MODULO:

			if !types.IsNumeric(t1) || !types.IsNumeric(t2) {
				v.errorf(n, "arithmetic operators require numeric operands")
			}

		case parser.BINARY_OP_LOGICAL_AND,
			parser.BINARY_OP_LOGICAL_OR:

			if !t1.Equals(types.PRIMITIVE_BOOL) ||
				!t2.Equals(types.PRIMITIVE_BOOL) {
				v.errorf(n, "logical operators require bool operands")
			}

		case parser.BINARY_OP_EQUAL,
			parser.BINARY_OP_NOT_EQUAL:

			if !t1.CanCoerceTo(t2) && !t2.CanCoerceTo(t1) {
				v.errorf(n, "incompatible types for comparison")
			}

		case parser.BINARY_OP_LESS,
			parser.BINARY_OP_LESS_EQUAL,
			parser.BINARY_OP_GREATER,
			parser.BINARY_OP_GREATER_EQUAL:

			if !types.IsNumeric(t1) || !types.IsNumeric(t2) {
				v.errorf(n, "ordering operators require numeric operands")
			}

		default:
			v.errorf(n, "unknown binary operator")
		}

	case *parser.IndexExprNode:
		v.validateExpr(n.Subject)
		v.validateExpr(n.Index)

		indexType := n.Index.GetType()
		if !types.IsInteger(indexType) {
			v.errorf(n, "index must be integer")
		}

		switch n.Subject.GetType().(type) {
		case types.ArrayType, types.PointerType:
			// OK
		default:
			v.errorf(n, "cannot index into non-array type")
		}

	case *parser.IfExprNode:
		v.validateExpr(n.IfBranch.Condition)

		if !n.IfBranch.Condition.GetType().Equals(types.PRIMITIVE_BOOL) {
			v.errorf(n, "if expression condition must be bool")
		}

		v.validateExpr(n.IfBranch.Node)

		for _, elif := range n.ElseIfBranches {
			v.validateExpr(elif.Condition)

			if !elif.Condition.GetType().Equals(types.PRIMITIVE_BOOL) {
				v.errorf(n, "elseif condition must be bool")
			}

			v.validateExpr(elif.Node)
		}

		if n.ElseBranch != nil {
			v.validateExpr(n.ElseBranch)
		}

	case *parser.ArrayLiteralNode:
		base := n.GetType().(types.ArrayType).Base

		for i, el := range n.Elements {
			v.validateExpr(el)

			if !el.GetType().CanCoerceTo(base) {
				v.errorf(n, "array element type mismatch")
				continue
			}

			if !el.GetType().Equals(base) {
				n.Elements[i] = &parser.CastNode{
					Operand: el,
					Type:    base,
				}
			}
		}

	case *parser.IntegerLiteralNode,
		*parser.FloatLiteralNode,
		*parser.BoolLiteralNode,
		*parser.StringLiteralNode,
		*parser.CharLiteralNode,
		*parser.NilLiteralNode,
		*parser.IdentifierNode,
		*parser.SizeOfNode:
		// nothing to validate

	default:
		panic(fmt.Sprintf("unhandled expression type %T", n))
	}

	if types.IsUntyped(node.GetType()) {
		node.SetType(types.DefaultUntyped(node.GetType()))
	}
}
