package sema

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

type Validator struct {
	analyser        *Analyser
	currentFunction *symbols.Symbol
	errors          []error
	warnings        []error
}

func (a *Analyser) NewValidator() *Validator {
	return &Validator{analyser: a}
}

func (v *Validator) errorf(node parser.Node, format string, args ...any) {
	v.errors = append(v.errors, shared.NewError(node.GetLoc(), format, args...))
}

func (v *Validator) warnf(node parser.Node, format string, args ...any) {
	v.warnings = append(v.warnings, shared.NewWarning(node.GetLoc(), format, args...))
}

func (v *Validator) ValidateModule(root *parser.RootNode) {
	v.validateNode(root)
}

func (v *Validator) Errors() []error {
	return v.errors
}

func (v *Validator) Warnings() []error {
	return v.warnings
}

func (v *Validator) validateNode(node parser.Node) {
	switch n := node.(type) {

	case *parser.RootNode:
		for _, stmt := range n.Body {
			v.validateNode(stmt)
		}

	case *parser.ImportNode:

	case *parser.ModuleNode:
		v.validateAttributes(n, n.Attributes, "module", attributes.AttributeTypeLinks)

	case *parser.FunctionDefNode:
		v.validateAttributes(n, n.Attributes, "function",
			attributes.AttributeTypeInline,
			attributes.AttributeTypeNoInline,
			attributes.AttributeTypeNoReturn,
			attributes.AttributeTypeForeign,
			attributes.AttributeTypeExport,
		)
		if n.Attributes.Get(attributes.AttributeTypeForeign) != nil &&
			n.Attributes.Get(attributes.AttributeTypeExport) != nil {
			v.errorf(n, "function cannot be both foreign and exported")
		}

		prev := v.currentFunction
		v.currentFunction = n.Symbol

		if n.Symbol.Signature.Variadic && n.Symbol.Attributes.Get(attributes.AttributeTypeForeign) == nil {
			v.errorf(n, "non-foreign functions cannot be variadic")
		}

		if n.Body != nil {
			v.validateNode(n.Body)
		}

		v.currentFunction = prev

	case *parser.BlockNode:
		for _, stmt := range n.Body {
			v.validateNode(stmt)
		}

	case *parser.DeclarationNode:
		v.validateAttributes(n, n.Attributes, "declaration", attributes.AttributeTypeForeign)
		v.finaliseDeclaration(n)

	case *parser.AssignmentNode:
		v.validateAssignment(n)

	case *parser.IfNode:
		v.validateIf(n)

	case *parser.ForNode:
		v.validateFor(n)

	case *parser.RangeForNode:
		v.validateRangeFor(n)

	case *parser.ForEachNode:
		v.validateForEach(n)

	case *parser.ControlKeywordNode:
		if n.ReturnValue != nil {
			v.validateReturn(n)
		}

	case *parser.DeferNode:
		v.validateNode(n.Action)

	case parser.ExpressionNode:
		v.validateExpr(n)

	case *parser.TypeAliasNode:
		// pass

	default:
		panic(fmt.Sprintf("unhandled node type %T", n))
	}
}

func (v *Validator) validateAttributes(node parser.Node, attrs attributes.Attributes, entity string, allowed ...attributes.AttributeType) {
	for _, attr := range attrs {
		valid := false
		for _, attrType := range allowed {
			if attr.GetType() == attrType {
				valid = true
				break
			}
		}
		if !valid {
			v.errorf(node, "@%s attribute does not apply to %ss", attr.GetType(), entity)
		}
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

	if types.HasUntyped(valueType) {
		if _, ok := valueType.(types.UnresolvedEnum); ok {
			v.errorf(n, "cannot infer enum type for declaration; add a type annotation")
		} else {
			v.errorf(n, "cannot infer declaration type from untyped numeric value; add a type annotation or cast")
		}
		n.Symbol.Type = types.ErrorType{}
		return
	}

	n.Symbol.Type = valueType
}

func (v *Validator) validateAssignment(n *parser.AssignmentNode) {
	if !v.validateLValue(n.Assignee) {
		return
	}

	v.validateExpr(n.Assignee)

	lhsType := n.Assignee.GetType()
	n.Value = v.validateExprWithExpected(n.Value, lhsType)
}

func (v *Validator) validateLValue(expr parser.ExpressionNode) bool {
	switch e := expr.(type) {

	case *parser.IdentifierNode:
		if !e.GetSymbol().Mutable {
			v.errorf(e, "cannot assign to immutable symbol")
			return false
		}
		v.validateExpr(e)

	case *parser.UnaryOpNode:
		switch e.Op {
		case parser.UnaryOpDereference:
			switch ptrType := types.Underlying(e.Operand.GetType()).(type) {
			case types.PointerType:
				if ptrType.Base.Equals(types.PrimitiveVoid) {
					v.errorf(e, "cannot assign to dereferenced void pointer")
					return false
				}
				if !ptrType.Mutable {
					v.errorf(e, "cannot assign to dereferenced immutable pointer")
					return false
				}
			case types.ErrorType:
				// do nothing, error already reported
			default:
				panic(fmt.Sprintf("unreachable: dereference of non-pointer type %T", e.Operand.GetType()))
			}
			v.validateExpr(e.Operand)
		default:
			v.errorf(expr, "invalid assignment target")
			return false
		}

	case *parser.FieldAccessNode:
		if !v.validateLValue(e.Subject) {
			return false
		}

	case *parser.IndexExprNode:
		// A slice's mutability comes from the place that holds the slice,
		// whereas a pointer carries the mutability of the pointed-to data.
		switch subjectType := types.Underlying(e.Subject.GetType()).(type) {
		case types.SliceType:
			if !v.validateLValue(e.Subject) {
				return false
			}

		case types.PointerType:
			if subjectType.Base.Equals(types.PrimitiveVoid) {
				v.errorf(e, "cannot assign to dereferenced void pointer")
				return false
			}
			if !subjectType.Mutable {
				v.errorf(e, "cannot assign to dereferenced immutable pointer")
				return false
			}

		case types.ErrorType:
			// Do not add an lvalue error after attribution has already reported one.
		default:
			v.errorf(e, "cannot assign to index of non-slice type")
			return false
		}

	default:
		v.errorf(expr, "cannot assign to this expression")
		return false
	}

	return true
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

func (v *Validator) validateFor(n *parser.ForNode) {
	switch len(n.ExprsOrStmts) {
	case 0:
	case 1:
		condition, ok := n.ExprsOrStmts[0].(parser.ExpressionNode)
		if !ok {
			v.errorf(n.ExprsOrStmts[0], "for loop condition must be an expression")
		} else {
			v.validateExpr(condition)
			if !condition.GetType().Equals(types.PrimitiveBool) {
				v.errorf(condition, "for loop condition must be bool")
			}
		}
	case 3:
		v.validateNode(n.ExprsOrStmts[0])
		condition, ok := n.ExprsOrStmts[1].(parser.ExpressionNode)
		if !ok {
			v.errorf(n.ExprsOrStmts[1], "for loop condition must be an expression")
		} else {
			v.validateExpr(condition)
			if !condition.GetType().Equals(types.PrimitiveBool) {
				v.errorf(condition, "for loop condition must be bool")
			}
		}
		v.validateNode(n.ExprsOrStmts[2])
	default:
		v.errorf(n, "for loop must have a condition or initializer, condition, and post expression")
		return
	}

	v.validateNode(n.Body)
}

func (v *Validator) validateRangeFor(n *parser.RangeForNode) {
	v.validateExpr(n.Start)
	v.validateExpr(n.End)

	boundType := types.PromoteNumeric(n.Start.GetType(), n.End.GetType())
	if !types.IsInteger(boundType) {
		v.errorf(n, "range bounds must be integer types")
		return
	}
	if n.Symbol != nil {
		n.Symbol.Type = types.PrimitiveUsz
	}

	v.validateNode(n.Body)

	iteratorType := types.PrimitiveUsz
	n.Start = v.validateExprWithExpected(n.Start, iteratorType)
	n.End = v.validateExprWithExpected(n.End, iteratorType)
	if n.Symbol != nil {
		n.Symbol.Type = iteratorType
	}
}

func (v *Validator) validateForEach(n *parser.ForEachNode) {
	v.validateExpr(n.Iterable)
	slice, ok := types.Underlying(n.Iterable.GetType()).(types.SliceType)
	if !ok {
		v.errorf(n, "for loop iterable must be a slice")
		return
	}
	if types.HasUntyped(slice.Base) {
		v.errorf(n, "cannot infer for loop element type from untyped slice")
		return
	}
	if n.Symbol != nil {
		n.Symbol.Type = slice.Base
	}
	v.validateNode(n.Body)
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
	case *parser.EnumLiteralNode:
		v.errorf(n, "cannot infer enum type for .%s", n.Variant)
		n.SetType(types.ErrorType{})

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
				if types.HasUntyped(arg.GetType()) {
					v.errorf(arg, "cannot infer type for variadic argument from untyped numeric value; add a cast")
				}
				continue
			}

			paramType := sig.Parameters[i]
			n.Args[i] = v.validateExprWithExpected(arg, paramType)
		}

	case *parser.CastNode:
		v.validateExpr(n.Operand)

		if types.IsUntyped(n.Operand.GetType()) {
			n.Operand = v.createCast(n.Operand, n.Type)
		}

		if !types.CanExplicitCast(n.Operand.GetType(), n.Type) {
			v.errorf(n, "cannot cast %v to %v", n.Operand.GetType(), n.Type)
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
			if !types.IsSigned(operandType) && !types.IsFloat(operandType) {
				v.errorf(n, "operator - requires a signed integer or float")
			}

		case parser.UnaryOpBitwiseNot:
			if !types.IsInteger(operandType) {
				v.errorf(n, "operator ~ requires an integer")
			}

		case parser.UnaryOpReference:
			fallthrough
		case parser.UnaryOpMutableReference:

			switch op := n.Operand.(type) {
			case *parser.IdentifierNode:
				if n.Op == parser.UnaryOpMutableReference && op.Symbol != nil && !op.Symbol.Mutable {
					v.errorf(n, "taking mutable reference of immutable variable")
				}
			case *parser.FieldAccessNode:
				panic(fmt.Sprintf("todo: taking reference of field access expression %T", n.Operand))
			case *parser.IndexExprNode:
				panic(fmt.Sprintf("todo: taking reference of index expression %T", n.Operand))
			default:
				v.errorf(n, "cannot take reference of this expression")
			}

		case parser.UnaryOpDereference:
			if ptrType, ok := types.Underlying(operandType).(types.PointerType); ok {
				if ptrType.Base.Equals(types.PrimitiveVoid) {
					v.errorf(n, "cannot dereference void pointer")
				}
			} else {
				v.errorf(n, "cannot dereference non-pointer type")
			}

		case parser.UnaryOpSliceLen:
			switch operandType.(type) {
			case types.SliceType:
				// OK
			default:
				v.errorf(n, "slice length operator requires a slice operand")
			}

		default:
			v.errorf(n, "unknown unary operator")
		}

	case *parser.BinaryOpNode:
		_, leftEnumShorthand := n.Operand1.(*parser.EnumLiteralNode)
		_, rightEnumShorthand := n.Operand2.(*parser.EnumLiteralNode)
		switch {
		case leftEnumShorthand && !rightEnumShorthand:
			v.validateExpr(n.Operand2)
			n.Operand1 = v.validateExprWithExpected(n.Operand1, n.Operand2.GetType())
		case rightEnumShorthand && !leftEnumShorthand:
			v.validateExpr(n.Operand1)
			n.Operand2 = v.validateExprWithExpected(n.Operand2, n.Operand1.GetType())
		default:
			v.validateExpr(n.Operand1)
			v.validateExpr(n.Operand2)
		}

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
				break
			}

			common := types.PromoteNumeric(t1, t2)
			if _, isError := common.(types.ErrorType); isError {
				v.errorf(n, "incompatible types for arithmetic: %v and %v", t1, t2)
				break
			}
			if !types.IsUntyped(common) {
				n.Operand1 = v.validateExprWithExpected(n.Operand1, common)
				n.Operand2 = v.validateExprWithExpected(n.Operand2, common)
				n.SetType(common)
			}

		case parser.BinaryOpLogicalAnd,
			parser.BinaryOpLogicalOr:

			if !t1.Equals(types.PrimitiveBool) ||
				!t2.Equals(types.PrimitiveBool) {
				v.errorf(n, "logical operators require bool operands")
			}

		case parser.BinaryOpBitwiseAnd,
			parser.BinaryOpBitwiseXor,
			parser.BinaryOpBitwiseOr,
			parser.BinaryOpShiftLeft,
			parser.BinaryOpShiftRight:

			if !types.IsInteger(t1) || !types.IsInteger(t2) {
				v.errorf(n, "bitwise operators require integer operands")
				break
			}
			common := types.PromoteNumeric(t1, t2)
			if _, isError := common.(types.ErrorType); isError {
				v.errorf(n, "incompatible integer types for bitwise operation: %v and %v", t1, t2)
				break
			}
			if !types.IsUntyped(common) {
				n.Operand1 = v.validateExprWithExpected(n.Operand1, common)
				n.Operand2 = v.validateExprWithExpected(n.Operand2, common)
				n.SetType(common)
			}

		case parser.BinaryOpEqual,
			parser.BinaryOpNotEqual:

			if !t1.CanCoerceTo(t2) && !t2.CanCoerceTo(t1) {
				v.errorf(n, "incompatible types for comparison: %v and %v", t1, t2)
				break
			}
			if types.IsNumeric(t1) && types.IsNumeric(t2) {
				common := types.PromoteNumeric(t1, t2)
				if types.IsUntyped(common) {
					v.errorf(n, "cannot infer numeric type for comparison")
					break
				}
				n.Operand1 = v.validateExprWithExpected(n.Operand1, common)
				n.Operand2 = v.validateExprWithExpected(n.Operand2, common)
			}

		case parser.BinaryOpLess,
			parser.BinaryOpLessEqual,
			parser.BinaryOpGreater,
			parser.BinaryOpGreaterEqual:

			if !types.IsNumeric(t1) || !types.IsNumeric(t2) {
				v.errorf(n, "ordering operators require numeric operands")
				break
			}
			common := types.PromoteNumeric(t1, t2)
			if types.IsUntyped(common) {
				v.errorf(n, "cannot infer numeric type for comparison")
				break
			}
			n.Operand1 = v.validateExprWithExpected(n.Operand1, common)
			n.Operand2 = v.validateExprWithExpected(n.Operand2, common)

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

		switch types.Underlying(n.Subject.GetType()).(type) {
		case types.SliceType:
			if t, ok := types.Underlying(n.Subject.GetType()).(types.SliceType); ok {
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
		if n.IsEnumValue {
			return
		}
		v.validateExpr(n.Subject)
		subjectType := types.Underlying(n.Subject.GetType())
		if unionType, ok := subjectType.(types.UnionType); ok {
			for _, field := range unionType.Fields {
				if field.L == n.Field.Name {
					n.SetType(field.R)
					return
				}
			}
			v.errorf(n, "union type has no field %q", n.Field)
			return
		}
		structType, ok := subjectType.(types.StructType)
		if !ok {
			v.errorf(n, "cannot access field of non-struct type")
			return
		}
		fieldIndex := -1
		var fieldType types.Type
		for i, field := range structType.Fields {
			if field.L == n.Field.Name {
				fieldIndex = i
				fieldType = field.R
				break
			}
			if field.L == "" {
				if embedded, ok := field.R.(types.UnionType); ok {
					for _, unionField := range embedded.Fields {
						if unionField.L == n.Field.Name {
							fieldIndex = i
							fieldType = unionField.R
							break
						}
					}
				}
			}
			if fieldIndex != -1 {
				break
			}
		}
		if fieldIndex == -1 {
			v.errorf(n, "struct type has no field %q", n.Field)
			return
		}
		n.SetType(fieldType)

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
		if n.RepeatValue != nil {
			size := -1
			if amount, ok := n.RepeatAmount.(*parser.IntegerLiteralNode); ok {
				if parsed, err := strconv.Atoi(amount.Value); err == nil {
					size = parsed
				}
			}
			v.validateExpr(n.RepeatValue)
			n.RepeatAmount = v.validateExprWithExpected(n.RepeatAmount, types.PrimitiveUsz)
			n.SetType(types.SliceType{Base: n.RepeatValue.GetType(), Size: size})
			break
		}
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
		*parser.SizeOfNode,
		*parser.SizeOfExprNode:
		// nothing to validate

	default:
		panic(fmt.Sprintf("unhandled expression type %T", n))
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
	case *parser.EnumLiteralNode:
		enumType, ok := types.Underlying(expected).(types.EnumType)
		if !ok {
			v.errorf(n, "enum shorthand .%s requires an expected enum type", n.Variant)
			n.SetType(types.ErrorType{})
			return n
		}
		value, exists := enumType.VariantValue(n.Variant)
		if !exists {
			v.errorf(n, "enum %s has no variant %q", enumType, n.Variant)
			n.SetType(types.ErrorType{})
			return n
		}
		n.Value = value
		n.SetType(expected)
		return n
	case *parser.IntegerLiteralNode:
		if types.IsNumeric(expected) {
			n.SetType(expected)
			return n
		}
		n.SetType(types.UntypedInt{})
	case *parser.FloatLiteralNode:
		if types.IsFloat(expected) {
			n.SetType(expected)
			return n
		}
		n.SetType(types.UntypedFloat{})
	case *parser.UnaryOpNode:
		if (types.IsNumeric(expected) && n.Op == parser.UnaryOpNegate) ||
			(types.IsInteger(expected) && n.Op == parser.UnaryOpBitwiseNot) {
			n.Operand = v.validateExprWithExpected(n.Operand, expected)
			n.SetType(expected)
			return n
		}
	case *parser.BinaryOpNode:
		if types.IsNumeric(expected) && (isArithmeticOperator(n.Op) || isBitwiseOperator(n.Op)) {
			n.Operand1 = v.validateExprWithExpected(n.Operand1, expected)
			n.Operand2 = v.validateExprWithExpected(n.Operand2, expected)
			n.SetType(expected)
			return n
		}
	case *parser.IfExprNode:
		v.validateExpr(n.IfBranch.Condition)
		n.IfBranch.Node = v.validateExprWithExpected(n.IfBranch.Node, expected)
		for i, branch := range n.ElseIfBranches {
			v.validateExpr(branch.Condition)
			n.ElseIfBranches[i].Node = v.validateExprWithExpected(branch.Node, expected)
		}
		if n.ElseBranch != nil {
			n.ElseBranch = v.validateExprWithExpected(n.ElseBranch, expected)
		}
		n.SetType(expected)
		return n
	case *parser.GivenExprNode:
		v.validateNode(n.Block)
		n.FinalExpr = v.validateExprWithExpected(n.FinalExpr, expected)
		n.SetType(expected)
		return n
	}

	if n, ok := node.(*parser.IdentifierNode); ok &&
		types.IsUntyped(n.GetType()) && !types.IsUntyped(expected) {
		n.Symbol.Type = expected
		n.SetType(expected)
		return n
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
		v.errorf(node, "cannot use %v as %v", got, expected)
		return node
	}

	if !got.Equals(expected) {
		return v.createCast(node, expected)
	}

	return node
}

func isBitwiseOperator(op parser.BinaryOpKind) bool {
	switch op {
	case parser.BinaryOpBitwiseAnd, parser.BinaryOpBitwiseXor, parser.BinaryOpBitwiseOr,
		parser.BinaryOpShiftLeft, parser.BinaryOpShiftRight:
		return true
	default:
		return false
	}
}

func isArithmeticOperator(op parser.BinaryOpKind) bool {
	switch op {
	case parser.BinaryOpAdd,
		parser.BinaryOpSubtract,
		parser.BinaryOpMultiply,
		parser.BinaryOpDivide,
		parser.BinaryOpModulo:
		return true
	default:
		return false
	}
}

func (v *Validator) validateSliceLiteralWithExpected(n *parser.SliceLiteralNode, expected types.Type) {
	sliceType, ok := types.Underlying(expected).(types.SliceType)
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
	if n.RepeatValue != nil {
		size := -1
		if amount, ok := n.RepeatAmount.(*parser.IntegerLiteralNode); ok {
			if parsed, err := strconv.Atoi(amount.Value); err == nil {
				size = parsed
			}
		}
		n.RepeatValue = v.validateExprWithExpected(n.RepeatValue, sliceType.Base)
		n.RepeatAmount = v.validateExprWithExpected(n.RepeatAmount, types.PrimitiveUsz)
		if sliceType.Size != -1 && size != -1 && size != sliceType.Size {
			v.errorf(n, "cannot assign repeated slice of size %d to [%v, %d]", size, sliceType.Base, sliceType.Size)
		}
		n.SetType(expected)
		return
	}

	if sliceType.Size != -1 && len(n.Elements) != sliceType.Size {
		v.errorf(n, "cannot assign [%v, %d] to [%v, %d]", sliceType.Base, len(n.Elements), sliceType.Base, sliceType.Size)
	}

	for i, element := range n.Elements {
		n.Elements[i] = v.validateExprWithExpected(element, sliceType.Base)
	}

	n.SetType(expected)
}

func (v *Validator) validateStructLiteralWithExpected(n *parser.StructLiteralNode, expected types.Type) {
	if unionType, ok := types.Underlying(expected).(types.UnionType); ok {
		if len(n.Fields) != 1 {
			v.errorf(n, "union literal must initialize exactly one field")
			n.SetType(types.ErrorType{})
			return
		}
		field := n.Fields[0]
		for _, unionField := range unionType.Fields {
			if unionField.L == field.L {
				n.Fields[0].R = v.validateExprWithExpected(field.R, unionField.R)
				n.SetType(expected)
				return
			}
		}
		v.errorf(n, "unknown field %q in union literal", field.L)
		n.SetType(types.ErrorType{})
		return
	}
	structType, ok := types.Underlying(expected).(types.StructType)
	if !ok {
		v.errorf(n, "cannot use struct literal for non-struct type %v", expected)
		n.SetType(types.ErrorType{})
		return
	}

	fieldTypes := make(map[string]types.Type, len(structType.Fields))
	for _, field := range structType.Fields {
		if field.L == "" {
			if embedded, ok := field.R.(types.UnionType); ok {
				for _, unionField := range embedded.Fields {
					fieldTypes[unionField.L] = unionField.R
				}
			}
			continue
		}
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
		if field.L == "" {
			if embedded, ok := field.R.(types.UnionType); ok {
				initialized := 0
				for _, unionField := range embedded.Fields {
					if _, ok := seen[unionField.L]; ok {
						initialized++
					}
				}
				if initialized == 0 {
					missingFields = append(missingFields, "<embedded union>")
				} else if initialized > 1 {
					v.errorf(n, "embedded union must initialize exactly one field")
				}
			}
			continue
		}
		if _, ok := seen[field.L]; !ok {
			missingFields = append(missingFields, field.L)
		}
	}
	if len(missingFields) > 0 {
		v.errorf(n, "missing fields in struct literal: %v", strings.Join(missingFields, ", "))
	}

	n.SetType(expected)
}
