package sema

import (
	"fmt"
	"strconv"
	"strings"

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

func (v *Validator) ValidateModule(root *parser.RootNode) {
	v.validateNode(root)
}

func (v *Validator) Errors() []error {
	return v.errors
}

func (v *Validator) validateNode(node parser.Node) {
	switch n := node.(type) {

	case *parser.RootNode:
		for _, stmt := range n.Body {
			v.validateNode(stmt)
		}

	case *parser.ImportNode, *parser.ModuleNode:

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

	case *parser.IndexAssignmentNode:
		v.validateIndexAssignment(n)

	case *parser.IfNode:
		v.validateIf(n)

	case *parser.ForNode:
		if n.Init != nil {
			v.validateNode(n.Init)
		}
		if n.Condition != nil {
			v.validateExpr(n.Condition)
			if !n.Condition.GetType().Equals(types.PrimitiveBool) {
				v.errorf(n.Condition, "for condition must be bool")
			}
		}
		if n.Post != nil {
			v.validateNode(n.Post)
		}
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

	if n.TypeNode != nil {
		declared := v.analyser.resolveTypeNode(n.TypeNode)
		n.Value = v.validateExprWithExpected(n.Value, declared)

		n.Symbol.Type = declared
		return
	}

	v.validateExpr(n.Value)

	valueType := n.Value.GetType()

	if types.IsUntyped(valueType) {
		valueType = types.DefaultUntyped(valueType)
	}

	n.Symbol.Type = valueType
}

func (v *Validator) validateAssignment(n *parser.AssignmentNode) {
	if !v.validateLValue(n.Subject) {
		return
	}

	v.validateExpr(n.Subject)

	lhsType := n.Subject.GetType()
	n.Value = v.validateExprWithExpected(n.Value, lhsType)
}

func (v *Validator) validateLValue(expr parser.ExpressionNode) bool {
	switch e := expr.(type) {

	case *parser.IdentifierNode:
		if !e.Symbol.Mutable {
			v.errorf(e, "cannot assign to immutable symbol")
			return false
		}

	case *parser.UnaryOpNode:
		if e.Op == parser.UnaryOpDereference {
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

func (v *Validator) validateIndexAssignment(n *parser.IndexAssignmentNode) {
	v.validateExpr(n.Subject)
	v.validateExpr(n.Index)

	containerType := n.Subject.GetType()

	indexType := n.Index.GetType()
	if !types.IsInteger(indexType) {
		v.errorf(n, "index must be integer")
		return
	}

	var base types.Type
	switch t := containerType.(type) {
	case types.SliceType:
		if t.Size != -1 {
			if idxLit, ok := n.Index.(*parser.IntegerLiteralNode); ok {
				idx, _ := strconv.Atoi(idxLit.Value)
				if idx < 0 || idx >= t.Size {
					v.errorf(n, "index %d out of bounds for slice of size %d", idx, t.Size)
				}
			}
		}
		base = t.Base
	case types.PointerType:
		base = t.Base
	default:
		v.errorf(n, "cannot index into non-slice type")
		return
	}

	n.Value = v.validateExprWithExpected(n.Value, base)
}

func (v *Validator) validateIf(n *parser.IfNode) {
	v.validateExpr(n.IfBranch.Condition)
	if !n.IfBranch.Condition.GetType().Equals(types.PrimitiveBool) {
		v.errorf(n, "if condition must be bool")
	}

	v.validateNode(n.IfBranch.Node)

	for _, elif := range n.ElseIfBranches {
		v.validateExpr(elif.Condition)
		if !elif.Condition.GetType().Equals(types.PrimitiveBool) {
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
	n.ReturnValue = v.validateExprWithExpected(n.ReturnValue, expected)
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
			if i >= len(sig.Parameters) {
				v.validateExpr(arg)
				continue
			}

			paramType := sig.Parameters[i]
			n.Args[i] = v.validateExprWithExpected(arg, paramType)
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

		case parser.UnaryOpLogicalNot:
			if !operandType.Equals(types.PrimitiveBool) {
				v.errorf(n, "operator not requires bool")
			}

		case parser.UnaryOpNegate:
			if !types.IsNumeric(operandType) {
				v.errorf(n, "operator - requires numeric type")
			}

		case parser.UnaryOpReference:

		case parser.UnaryOpDereference:
			if _, ok := operandType.(types.PointerType); !ok {
				v.errorf(n, "cannot dereference non-pointer type")
			}

		case parser.UnaryOpSliceLen:
			switch operandType.(type) {
			case types.SliceType:
				// OK
			default:
				v.errorf(n, "[] operator requires slice or pointer")
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

		case parser.BinaryOpAdd,
			parser.BinaryOpSubtract,
			parser.BinaryOpMultiply,
			parser.BinaryOpDivide,
			parser.BinaryOpModulo:

			if !types.IsNumeric(t1) || !types.IsNumeric(t2) {
				v.errorf(n, "arithmetic operators require numeric operands")
			}

		case parser.BinaryOpLogicalAnd,
			parser.BinaryOpLogicalOr:

			if !t1.Equals(types.PrimitiveBool) ||
				!t2.Equals(types.PrimitiveBool) {
				v.errorf(n, "logical operators require bool operands")
			}

		case parser.BinaryOpEqual,
			parser.BinaryOpNotEqual:

			if !t1.CanCoerceTo(t2) && !t2.CanCoerceTo(t1) {
				v.errorf(n, "incompatible types for comparison: %v and %v", t1, t2)
			}

		case parser.BinaryOpLess,
			parser.BinaryOpLessEqual,
			parser.BinaryOpGreater,
			parser.BinaryOpGreaterEqual:

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
		case types.SliceType:
			if t, ok := n.Subject.GetType().(types.SliceType); ok {
				if t.Size != -1 {
					if idxLit, ok := n.Index.(*parser.IntegerLiteralNode); ok {
						idx, _ := strconv.Atoi(idxLit.Value)
						if idx < 0 || idx >= t.Size {
							v.errorf(n, "index %d out of bounds for slice of size %d", idx, t.Size)
						}
					}
				}
			} else {
				panic("unreachable")
			}

		case types.PointerType:
			// OK
		default:
			v.errorf(n, "cannot index into non-slice type")
		}

	case *parser.FieldAccessNode:
		v.validateExpr(n.Subject)
		subjectType := n.Subject.GetType()
		structType, ok := subjectType.(types.StructType)
		if !ok {
			v.errorf(n, "cannot access field of non-struct type")
			return
		}
		fieldIndex := -1
		for i, field := range structType.Fields {
			if field.L == n.Field.Name {
				fieldIndex = i
				break
			}
		}
		if fieldIndex == -1 {
			v.errorf(n, "struct type has no field %q", n.Field)
			return
		}
		n.SetType(structType.Fields[fieldIndex].R)

	case *parser.IfExprNode:
		v.validateExpr(n.IfBranch.Condition)

		if !n.IfBranch.Condition.GetType().Equals(types.PrimitiveBool) {
			v.errorf(n, "if expression condition must be bool")
		}

		v.validateExpr(n.IfBranch.Node)

		for _, elif := range n.ElseIfBranches {
			v.validateExpr(elif.Condition)

			if !elif.Condition.GetType().Equals(types.PrimitiveBool) {
				v.errorf(n, "elseif condition must be bool")
			}

			v.validateExpr(elif.Node)
		}

		if n.ElseBranch != nil {
			v.validateExpr(n.ElseBranch)
		}

	case *parser.GivenExprNode:
		v.validateNode(n.Block)
		v.validateExpr(n.FinalExpr)

	case *parser.SliceLiteralNode:
		var common types.Type = nil

		if len(n.Elements) == 0 {
			n.Type = types.SliceType{
				Base: types.PrimitiveVoid,
				Size: 0,
			}
			break
		}

		for _, el := range n.Elements {
			v.validateExpr(el)

			if common == nil {
				common = el.GetType()
			} else {
				common = types.PromoteNumeric(common, el.GetType())
				if _, isErr := common.(types.ErrorType); isErr {
					v.errorf(n, "slice element type mismatch")
					return
				}
			}
		}

		if types.IsUntyped(common) {
			common = types.DefaultUntyped(common)
		}

		n.Type = types.SliceType{
			Base: common,
			Size: len(n.Elements),
		}

		for i, el := range n.Elements {
			if !el.GetType().Equals(common) {
				n.Elements[i] = v.createCast(el, common)
			}
		}

	case *parser.StructLiteralNode:
		if n.Symbol != nil {
			v.validateStructLiteralWithExpected(n, n.Symbol.TypeInfo)
			break
		}

		fields := make([]shared.Pair[string, types.Type], 0, len(n.Fields))
		for i, field := range n.Fields {
			v.validateExpr(field.R)
			n.Fields[i].R = field.R
			fields = append(fields, shared.Pair[string, types.Type]{
				L: field.L,
				R: field.R.GetType(),
			})
		}

		n.SetType(types.StructType{Fields: fields})

	case *parser.IntegerLiteralNode,
		*parser.FloatLiteralNode,
		*parser.BoolLiteralNode,
		*parser.StringLiteralNode,
		*parser.CharLiteralNode,
		*parser.NilLiteralNode,
		*parser.IdentifierNode,
		*parser.ModuleAccessNode,
		*parser.SizeOfNode:
		// nothing to validate

	default:
		panic(fmt.Sprintf("unhandled expression type %T", n))
	}

	if types.IsUntyped(node.GetType()) {
		node.SetType(types.DefaultUntyped(node.GetType()))
	}
}

func (v *Validator) createCast(node parser.ExpressionNode, target types.Type) parser.ExpressionNode {
	if node.GetType().Equals(target) {
		return node
	}

	switch n := node.(type) {
	case *parser.IntegerLiteralNode:
		n.SetType(target)
		return n
	case *parser.FloatLiteralNode:
		n.SetType(target)
		return n
	}

	return &parser.CastNode{
		Operand: node,
		Type:    target,
	}
}

func (v *Validator) validateExprWithExpected(node parser.ExpressionNode, expected types.Type) parser.ExpressionNode {
	switch n := node.(type) {
	case *parser.IntegerLiteralNode:
		n.SetType(types.UntypedInt{})
	case *parser.FloatLiteralNode:
		n.SetType(types.UntypedFloat{})
	}

	if n, ok := node.(*parser.StructLiteralNode); ok {
		v.validateStructLiteralWithExpected(n, expected)
		return n
	}

	if n, ok := node.(*parser.SliceLiteralNode); ok {
		v.validateSliceLiteralWithExpected(n, expected)
		return n
	}

	v.validateExpr(node)

	got := node.GetType()
	if !got.CanCoerceTo(expected) {
		v.errorf(node, "cannot assign %v to %v", got, expected)
		return node
	}

	if !got.Equals(expected) {
		return v.createCast(node, expected)
	}

	return node
}

func (v *Validator) validateSliceLiteralWithExpected(n *parser.SliceLiteralNode, expected types.Type) {
	sliceType, ok := expected.(types.SliceType)
	if !ok {
		v.validateExpr(n)
		got := n.GetType()
		if !got.CanCoerceTo(expected) {
			v.errorf(n, "cannot assign %v to %v", got, expected)
			return
		}
		if !got.Equals(expected) {
			n.SetType(expected)
		}
		return
	}

	if sliceType.Size != -1 && len(n.Elements) != sliceType.Size {
		v.errorf(n, "cannot assign [%v, %d] to [%v, %d]", sliceType.Base, len(n.Elements), sliceType.Base, sliceType.Size)
	}

	for i, element := range n.Elements {
		n.Elements[i] = v.validateExprWithExpected(element, sliceType.Base)
	}

	n.SetType(types.SliceType{
		Base: sliceType.Base,
		Size: len(n.Elements),
	})
}

func (v *Validator) validateStructLiteralWithExpected(n *parser.StructLiteralNode, expected types.Type) {
	structType, ok := expected.(types.StructType)
	if !ok {
		v.errorf(n, "cannot use struct literal for non-struct type %v", expected)
		n.SetType(types.ErrorType{})
		return
	}

	fieldTypes := make(map[string]types.Type, len(structType.Fields))
	for _, field := range structType.Fields {
		fieldTypes[field.L] = field.R
	}

	seen := make(map[string]struct{}, len(n.Fields))
	for i, field := range n.Fields {
		expectedFieldType, exists := fieldTypes[field.L]
		if !exists {
			v.errorf(n, "unknown field %q in struct literal", field.L)
			continue
		}

		if _, dup := seen[field.L]; dup {
			v.errorf(n, "duplicate field %q in struct literal", field.L)
			continue
		}
		seen[field.L] = struct{}{}

		n.Fields[i].R = v.validateExprWithExpected(field.R, expectedFieldType)
	}

	missingFields := []string{}
	for _, field := range structType.Fields {
		if _, ok := seen[field.L]; !ok {
			missingFields = append(missingFields, field.L)
		}
	}
	if len(missingFields) > 0 {
		v.errorf(n, "missing fields in struct literal: %v", strings.Join(missingFields, ", "))
	}

	n.SetType(structType)
}
