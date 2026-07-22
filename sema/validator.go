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
	for _, node := range root.Body {
		if module, ok := node.(*parser.ModuleNode); ok {
			v.analyser.currentMod = module.Name
			if mod := v.analyser.modules[module.Name]; mod != nil {
				v.analyser.current = mod.Scope
				v.analyser.currentTrustedStandardLibrary = mod.TrustedStandardLibrary
			}
			break
		}
	}
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
		v.validateAttributes(n, n.Attributes, "module", attributes.AttributeTypeLink)

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

		foreign, isForeign := n.Symbol.Attributes.Get(attributes.AttributeTypeForeign).(attributes.FunctionAttributeForeign)
		exported, isExported := n.Symbol.Attributes.Get(attributes.AttributeTypeExport).(attributes.FunctionAttributeExport)
		usesCABI := (isForeign && foreign.ABI == attributes.ForeignABIC) ||
			(isExported && exported.ABI == attributes.ForeignABIC)
		usesQKABI := !usesCABI
		if n.Symbol.Signature.Variadic && usesQKABI {
			v.errorf(n, "functions using the qk ABI cannot be variadic")
		}
		if n.Symbol.Signature.TypedVariadic && usesCABI {
			v.errorf(n, "typed variadic functions cannot use the c ABI")
		}

		seenDefault := false
		for i, param := range n.Symbol.Signature.Parameters {
			if usesCABI && types.HasTraitPointer(param) {
				v.errorf(n.Args[i].Type, "trait pointers cannot cross the c ABI")
			}
			if !types.IsComplete(param) {
				v.errorf(n.Args[i].Type, "function parameter cannot have incomplete type %v", param)
			}
			arg := n.Args[i]
			if n.Symbol.Signature.TypedVariadic && arg.Default != nil {
				v.errorf(arg.Default, "typed variadic functions cannot currently use default parameters")
			}
			if arg.Default == nil {
				if seenDefault {
					v.errorf(arg, "parameter without a default cannot follow a parameter with a default")
				}
				continue
			}
			seenDefault = true
			if usesCABI {
				v.errorf(arg.Default, "functions using the c ABI cannot have default parameters")
			}
			arg.Default = v.validateExprWithExpected(arg.Default, param)
		}
		if ret := n.Symbol.Signature.ReturnType; ret != nil && !types.IsComplete(ret) {
			v.errorf(n, "function return cannot have incomplete type %v", ret)
		} else if usesCABI && ret != nil && types.HasTraitPointer(ret) {
			v.errorf(n, "trait pointers cannot cross the c ABI")
		}
		if _, multiple := n.Symbol.Signature.ReturnType.(types.MultipleReturnType); multiple && usesCABI {
			v.errorf(n, "multiple return values are not supported by the c ABI; use abi \"qk\"")
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
		if n.Name == "_" {
			if n.Value != nil {
				v.validateExpr(n.Value)
			}
			break
		}
		v.validateAttributes(n, n.Attributes, "declaration", attributes.AttributeTypeForeign)
		v.finaliseDeclaration(n)
		if n.Symbol.Type != nil && !types.IsComplete(n.Symbol.Type) {
			v.errorf(n, "cannot declare a value of incomplete type %v", n.Symbol.Type)
		}
	case *parser.MultiDeclarationNode:
		v.validateMultiDeclaration(n)

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
		underlying := types.Underlying(n.Symbol.TypeInfo)
		if n.Transparent && types.IsOpaque(n.Symbol.TypeInfo) {
			v.errorf(n, "opaque type %q cannot be a transparent alias", n.Name)
		} else if _, trait := underlying.(types.TraitType); trait {
			if n.Transparent {
				v.errorf(n, "trait %q cannot be a transparent alias", n.Name)
			}
		} else if _, opaque := underlying.(types.OpaqueType); !opaque && !types.IsComplete(underlying) {
			v.errorf(n, "type %q contains an incomplete type by value", n.Name)
		}

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
	if _, multiple := valueType.(types.MultipleReturnType); multiple {
		v.errorf(n, "multiple return values must be unpacked into a matching target list")
		n.Symbol.Type = types.ErrorType{}
		return
	}

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
	if len(n.Assignees) > 0 {
		v.validateExpr(n.Value)
		result, ok := n.Value.GetType().(types.MultipleReturnType)
		if !ok {
			v.errorf(n, "multiple assignment requires a function returning multiple values")
			return
		}
		if len(result.Types) != len(n.Assignees) {
			v.errorf(n, "assignment has %d targets but call returns %d values", len(n.Assignees), len(result.Types))
			return
		}
		for i, target := range n.Assignees {
			if id, ok := target.(*parser.IdentifierNode); ok && id.Name == "_" {
				continue
			}
			if !v.validateLValue(target) {
				continue
			}
			if !result.Types[i].CanCoerceTo(target.GetType()) {
				v.errorf(target, "cannot assign %v to %v", result.Types[i], target.GetType())
			}
		}
		return
	}
	if id, ok := n.Assignee.(*parser.IdentifierNode); ok && id.Name == "_" {
		v.validateExpr(n.Value)
		return
	}
	if field, ok := n.Assignee.(*parser.FieldAccessNode); ok && field.IsFlagTest {
		if !v.validateLValue(field.Subject) {
			return
		}
		n.Value = v.validateExprWithExpected(n.Value, types.PrimitiveBool)
		return
	}
	if !v.validateLValue(n.Assignee) {
		return
	}

	v.validateExpr(n.Assignee)

	lhsType := n.Assignee.GetType()
	n.Value = v.validateExprWithExpected(n.Value, lhsType)
}

func (v *Validator) validateMultiDeclaration(n *parser.MultiDeclarationNode) {
	v.validateExpr(n.Value)
	result, ok := n.Value.GetType().(types.MultipleReturnType)
	if !ok {
		v.errorf(n, "multiple declaration requires a function returning multiple values")
		return
	}
	if len(result.Types) != len(n.Names) {
		v.errorf(n, "declaration has %d names but call returns %d values", len(n.Names), len(result.Types))
		return
	}
	for i, sym := range n.Symbols {
		if sym != nil {
			sym.Type = result.Types[i]
		}
	}
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
		if e.ResolvedIdentifier != nil {
			return v.validateLValue(e.ResolvedIdentifier)
		}
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
	if multi, ok := expected.(types.MultipleReturnType); ok {
		if len(n.ReturnValues) == 1 {
			if _, forwarded := n.ReturnValue.(*parser.FunctionCallNode); forwarded {
				n.ReturnValue = v.validateExprWithExpected(n.ReturnValue, expected)
				return
			}
		}
		if len(n.ReturnValues) != len(multi.Types) {
			v.errorf(n, "return has %d values but function returns %d", len(n.ReturnValues), len(multi.Types))
			return
		}
		for i := range n.ReturnValues {
			n.ReturnValues[i] = v.validateExprWithExpected(n.ReturnValues[i], multi.Types[i])
		}
		return
	}
	if len(n.ReturnValues) > 1 {
		v.errorf(n, "too many return values")
		return
	}
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
		var params []types.Type
		variadic := false
		typedVariadic := false
		var variadicElement types.Type
		if n.Symbol != nil && n.Symbol.Kind == symbols.SymbolKindFunction {
			params = n.Symbol.Signature.Parameters
			variadic = n.Symbol.Signature.Variadic
			typedVariadic = n.Symbol.Signature.TypedVariadic
			variadicElement = n.Symbol.Signature.VariadicElement
		} else {
			v.validateExpr(n.Callee)
			ptr, ok := types.Underlying(n.Callee.GetType()).(types.PointerType)
			if !ok {
				v.errorf(n, "expression of type %s is not callable", n.Callee.GetType())
				return
			}
			fn, ok := types.Underlying(ptr.Base).(types.FunctionType)
			if !ok {
				v.errorf(n, "expression of type %s is not callable", n.Callee.GetType())
				return
			}
			params = fn.Parameters
			typedVariadic = fn.TypedVariadic
			variadicElement = fn.VariadicElement
		}

		got, expected := len(n.Args), len(params)
		if typedVariadic {
			expected--
			variadic = true
		}
		required := expected
		if n.Symbol != nil && n.Symbol.Kind == symbols.SymbolKindFunction {
			required = n.Symbol.Signature.RequiredParameters
		}
		if n.Method {
			got--
			expected--
			required--
		}
		if got < required || (!variadic && got > expected) {
			callable := "callable expression"
			if n.Symbol != nil {
				kind := "function"
				if n.Method {
					kind = "method"
				}
				callable = fmt.Sprintf("%s %q", kind, n.Symbol.Name)
			} else if n.Callee != nil {
				callable = fmt.Sprintf("callable of type %v", n.Callee.GetType())
			}
			if variadic {
				v.errorf(n, "%s expects at least %d %s, but %d %s provided",
					callable, required, argumentWord(required), got, wasWere(got))
			} else if required != expected {
				v.errorf(n, "%s expects between %d and %d arguments, but %d %s provided",
					callable, required, expected, got, wasWere(got))
			} else {
				v.errorf(n, "%s expects %d %s, but %d %s provided",
					callable, expected, argumentWord(expected), got, wasWere(got))
			}
			return
		}

		for i, arg := range n.Args {
			if typedVariadic && i >= len(params)-1 {
				if n.VariadicExpansion {
					if i != len(params)-1 || len(n.Args) != len(params) {
						v.errorf(n, "slice expansion must supply the complete typed variadic tail")
						continue
					}
					n.Args[i] = v.validateExprWithExpected(arg, params[len(params)-1])
				} else {
					n.Args[i] = v.validateExprWithExpected(arg, variadicElement)
				}
				continue
			}
			if i >= len(params) {
				v.validateExpr(arg)
				if types.HasUntyped(arg.GetType()) {
					v.errorf(arg, "cannot infer type for variadic argument from untyped numeric value; add a cast")
				}
				continue
			}

			paramType := params[i]
			n.Args[i] = v.validateExprWithExpected(arg, paramType)
		}
		if typedVariadic {
			n.TypedVariadic = true
			n.TypedVariadicStart = len(params) - 1
			n.TypedVariadicSlice = params[len(params)-1]
		} else if n.VariadicExpansion {
			v.errorf(n, "slice expansion requires a typed variadic function")
		}

	case *parser.CastNode:
		targetType := n.Type
		if n.Checked {
			targetType = n.CheckedType
		}
		if literal, ok := n.Operand.(*parser.SliceLiteralNode); ok && len(literal.Elements) == 0 && literal.RepeatValue == nil {
			v.validateSliceLiteralWithExpected(literal, targetType)
			break
		}
		v.validateExpr(n.Operand)
		if conversion, ok := v.traitConversion(n.Operand, targetType); ok {
			n.TraitConversion = true
			n.ConcreteType = conversion.ConcreteType
			n.TraitMethods = conversion.TraitMethods
			break
		}
		if target, ok := traitPointer(targetType); ok {
			got := n.Operand.GetType()
			if types.HasUntyped(got) {
				v.errorf(n, "cannot infer concrete type for trait conversion; add a type annotation or cast")
				break
			}
			pointer := types.PointerType{Base: got, Mutable: target.Mutable}
			methods, conforms := v.analyser.structuralConformance(pointer, target, n)
			if conforms {
				op := parser.UnaryOpReference
				if target.Mutable {
					op = parser.UnaryOpMutableReference
				}
				reference := &parser.UnaryOpNode{Op: op, Operand: n.Operand, Loc: n.Operand.GetLoc(), Type: pointer}
				if !v.validateReferenceTarget(reference, n.Operand, target.Mutable) {
					break
				}
				n.Operand = reference
				n.TraitConversion = true
				n.ConcreteType = got
				n.TraitMethods = methods
				break
			}
		}

		if types.IsUntyped(n.Operand.GetType()) {
			n.Operand = v.createCast(n.Operand, targetType)
		}

		if sourceTrait, ok := traitPointer(n.Operand.GetType()); ok {
			if targetTrait, targetIsTrait := traitPointer(targetType); targetIsTrait {
				if !sourceTrait.Mutable && targetTrait.Mutable {
					v.errorf(n, "cannot cast immutable %v to mutable %v", n.Operand.GetType(), targetType)
					break
				}
				n.TraitRecast = true
				n.TraitCandidates = v.analyser.runtimeTraitCastCandidates(targetTrait.Trait, targetTrait.Mutable, n)
				break
			}
			targetPointer, pointerTarget := types.Underlying(targetType).(types.PointerType)
			if pointerTarget {
				if !sourceTrait.Mutable && targetPointer.Mutable {
					v.errorf(n, "cannot unwrap immutable %v as %v", n.Operand.GetType(), targetType)
				}
				n.TraitUnwrap = true
				n.ConcreteType = targetPointer.Base
				probe := types.PointerType{Base: targetPointer.Base, Mutable: sourceTrait.Mutable}
				if _, conforms := v.analyser.structuralConformance(probe, sourceTrait, n); !conforms {
					v.errorf(n, "type %v does not conform to %v", targetPointer.Base, sourceTrait.Trait)
				}
				break
			}
			if !types.IsComplete(targetType) {
				v.errorf(n, "cannot unwrap incomplete type %v by value", targetType)
				break
			}
			n.TraitUnwrap = true
			n.ConcreteType = targetType
			probe := types.PointerType{Base: targetType, Mutable: sourceTrait.Mutable}
			if _, conforms := v.analyser.structuralConformance(probe, sourceTrait, n); !conforms {
				v.errorf(n, "type %v does not conform to %v", targetType, sourceTrait.Trait)
			}
			break
		}
		if from, ok := types.Underlying(n.Operand.GetType()).(types.SliceType); ok {
			if to, ok := types.Underlying(targetType).(types.SliceType); ok && from.Base.Equals(to.Base) {
				break
			}
		}
		if !types.CanExplicitCast(n.Operand.GetType(), targetType) {
			v.errorf(n, "cannot cast %v to %v", n.Operand.GetType(), targetType)
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
			if !types.IsInteger(operandType) && !isFlagsType(operandType) {
				v.errorf(n, "operator ~ requires an integer")
			}

		case parser.UnaryOpReference:
			v.validateReferenceTarget(n, n.Operand, false)
		case parser.UnaryOpMutableReference:
			v.validateReferenceTarget(n, n.Operand, true)

		case parser.UnaryOpDereference:
			if ptrType, ok := types.Underlying(operandType).(types.PointerType); ok {
				if ptrType.Base.Equals(types.PrimitiveVoid) {
					v.errorf(n, "cannot dereference void pointer")
				} else if !types.IsComplete(ptrType.Base) {
					v.errorf(n, "cannot dereference pointer to incomplete type %v", ptrType.Base)
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
			leftPtr, leftIsPtr := types.Underlying(t1).(types.PointerType)
			rightPtr, rightIsPtr := types.Underlying(t2).(types.PointerType)
			if n.Op == parser.BinaryOpAdd && leftIsPtr && types.IsInteger(t2) {
				v.validatePointerArithmeticBase(n, leftPtr.Base)
				n.Operand2 = v.validateExprWithExpected(n.Operand2, types.PrimitiveIsz)
				break
			}
			if n.Op == parser.BinaryOpAdd && types.IsInteger(t1) && rightIsPtr {
				v.validatePointerArithmeticBase(n, rightPtr.Base)
				n.Operand1 = v.validateExprWithExpected(n.Operand1, types.PrimitiveIsz)
				break
			}
			if n.Op == parser.BinaryOpSubtract && leftIsPtr && types.IsInteger(t2) {
				v.validatePointerArithmeticBase(n, leftPtr.Base)
				n.Operand2 = v.validateExprWithExpected(n.Operand2, types.PrimitiveIsz)
				break
			}
			if n.Op == parser.BinaryOpSubtract && leftIsPtr && rightIsPtr {
				if !leftPtr.Base.Equals(rightPtr.Base) {
					v.errorf(n, "cannot subtract pointers to different types %v and %v", leftPtr.Base, rightPtr.Base)
				} else {
					v.validatePointerArithmeticBase(n, leftPtr.Base)
				}
				break
			}

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

			if isFlagsType(t1) {
				if n.Op == parser.BinaryOpShiftLeft || n.Op == parser.BinaryOpShiftRight {
					if !types.IsInteger(t2) {
						v.errorf(n, "flags shift count must be an integer")
					}
				} else if !t1.Equals(t2) {
					v.errorf(n, "flags bitwise operands must have the same type")
				}
				n.SetType(t1)
				break
			}
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

		switch subjectType := types.Underlying(n.Subject.GetType()).(type) {
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
			if !types.IsComplete(subjectType.Base) {
				v.errorf(n, "cannot index pointer to incomplete type %v", subjectType.Base)
			}
		default:
			v.errorf(n, "cannot index into non-slice type")
		}

	case *parser.FieldAccessNode:
		if n.IsEnumValue || n.IsFlagValue || n.IsFlagTest || n.MethodSymbol != nil || n.ResolvedIdentifier != nil || n.ModulePath != "" {
			return
		}
		v.validateExpr(n.Subject)
		subjectType := types.Underlying(n.Subject.GetType())
		if ptr, ok := subjectType.(types.PointerType); ok {
			subjectType = types.Underlying(ptr.Base)
		}
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
			v.errorf(n, "cannot infer element type of empty slice literal")
			n.SetType(types.ErrorType{})
			break
		}

		for _, el := range n.Elements {
			v.validateExpr(el)

			if common == nil {
				common = el.GetType()
			} else {
				common = types.CommonType(common, el.GetType())
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
		*parser.CStringLiteralNode,
		*parser.CharLiteralNode,
		*parser.NilLiteralNode,
		*parser.IdentifierNode:
		// nothing to validate

	case *parser.SizeOfNode:
		if !types.IsComplete(n.OperandType) {
			v.errorf(n, "sizeof requires a complete type, got %v", n.OperandType)
		}

	case *parser.SizeOfExprNode:
		v.validateExpr(n.Operand)
		if !types.IsComplete(n.OperandType) {
			v.errorf(n, "sizeof requires a complete type, got %v", n.OperandType)
		}

	case *parser.AlignOfNode:
		if !types.IsComplete(n.OperandType) {
			v.errorf(n, "alignof requires a complete type, got %v", n.OperandType)
			break
		}
		switch operand := types.Underlying(n.OperandType).(type) {
		case types.FunctionType:
			v.errorf(n, "alignof requires an object type, got %v", n.OperandType)
		case types.PrimitiveType:
			if operand == types.PrimitiveVoid {
				v.errorf(n, "alignof requires an object type, got void")
			}
		}

	case *parser.OffsetOfNode:
		operand := types.Underlying(n.OperandType)
		switch operand.(type) {
		case types.StructType, types.UnionType:
		default:
			v.errorf(n, "offsetof requires a struct or union type")
			return
		}
		if !hasOffsetField(operand, n.Field) {
			v.errorf(n, "%v has no field %q", n.OperandType, n.Field)
		}

	default:
		panic(fmt.Sprintf("unhandled expression type %T", n))
	}

}

func (v *Validator) validatePointerArithmeticBase(node parser.Node, base types.Type) {
	underlying := types.Underlying(base)
	if underlying.Equals(types.PrimitiveVoid) {
		v.errorf(node, "cannot perform arithmetic on void pointer")
	} else if !types.IsComplete(base) {
		v.errorf(node, "cannot perform arithmetic on pointer to incomplete type %v", base)
	} else if _, function := underlying.(types.FunctionType); function {
		v.errorf(node, "cannot perform arithmetic on function pointer")
	}
}

func argumentWord(count int) string {
	if count == 1 {
		return "argument"
	}
	return "arguments"
}

func wasWere(count int) string {
	if count == 1 {
		return "was"
	}
	return "were"
}

func hasOffsetField(operand types.Type, name string) bool {
	var fields []shared.Pair[string, types.Type]
	switch operand := types.Underlying(operand).(type) {
	case types.StructType:
		fields = operand.Fields
	case types.UnionType:
		fields = operand.Fields
	default:
		return false
	}
	for _, field := range fields {
		if field.L == name || field.L == "" && hasOffsetField(field.R, name) {
			return true
		}
	}
	return false
}

func (v *Validator) validateReferenceTarget(node *parser.UnaryOpNode, target parser.ExpressionNode, mutable bool) bool {
	switch target := target.(type) {
	case *parser.IdentifierNode:
		if target.Symbol == nil || target.Symbol.Kind != symbols.SymbolKindVariable {
			v.errorf(node, "cannot take reference of this expression")
			return false
		}
		if mutable && !target.Symbol.Mutable {
			v.errorf(node, "cannot take mutable reference of immutable variable")
			return false
		}
		return true

	case *parser.UnaryOpNode:
		if target.Op != parser.UnaryOpDereference {
			v.errorf(node, "cannot take reference of this expression")
			return false
		}
		pointer, ok := types.Underlying(target.Operand.GetType()).(types.PointerType)
		if !ok || pointer.Base.Equals(types.PrimitiveVoid) {
			v.errorf(node, "cannot take reference of dereferenced void pointer")
			return false
		}
		if mutable && !pointer.Mutable {
			v.errorf(node, "cannot take mutable reference of dereferenced immutable pointer")
			return false
		}
		return true

	case *parser.FieldAccessNode:
		if target.ResolvedIdentifier != nil {
			return v.validateReferenceTarget(node, target.ResolvedIdentifier, mutable)
		}
		return v.validateReferenceTarget(node, target.Subject, mutable)

	case *parser.IndexExprNode:
		switch subjectType := types.Underlying(target.Subject.GetType()).(type) {
		case types.SliceType:
			return v.validateReferenceTarget(node, target.Subject, mutable)
		case types.PointerType:
			if subjectType.Base.Equals(types.PrimitiveVoid) {
				v.errorf(node, "cannot take reference of index into void pointer")
				return false
			}
			if mutable && !subjectType.Mutable {
				v.errorf(node, "cannot take mutable reference of index into immutable pointer")
				return false
			}
			return true
		default:
			v.errorf(node, "cannot take reference of this expression")
			return false
		}

	default:
		v.errorf(node, "cannot take reference of this expression")
		return false
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
	case *parser.NilLiteralNode:
		if types.IsPointer(expected) {
			n.SetType(expected)
			return n
		}
	case *parser.EnumLiteralNode:
		var value string
		var exists bool
		switch t := types.Underlying(expected).(type) {
		case types.EnumType:
			value, exists = t.VariantValue(n.Variant)
		case types.FlagsType:
			value, exists = t.VariantValue(n.Variant)
		default:
			v.errorf(n, "member shorthand .%s requires an expected enum or flags type", n.Variant)
			n.SetType(types.ErrorType{})
			return n
		}
		if !exists {
			v.errorf(n, "%s has no member %q", expected, n.Variant)
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
			((types.IsInteger(expected) || isFlagsType(expected)) && n.Op == parser.UnaryOpBitwiseNot) {
			n.Operand = v.validateExprWithExpected(n.Operand, expected)
			n.SetType(expected)
			return n
		}
	case *parser.BinaryOpNode:
		if isFlagsType(expected) && isBitwiseOperator(n.Op) {
			n.Operand1 = v.validateExprWithExpected(n.Operand1, expected)
			if n.Op == parser.BinaryOpShiftLeft || n.Op == parser.BinaryOpShiftRight {
				v.validateExpr(n.Operand2)
			} else {
				n.Operand2 = v.validateExprWithExpected(n.Operand2, expected)
			}
			n.SetType(expected)
			return n
		}
		leftIsPointer := types.IsPointer(n.Operand1.GetType())
		rightIsPointer := types.IsPointer(n.Operand2.GetType())
		if types.IsNumeric(expected) && !leftIsPointer && !rightIsPointer &&
			(isArithmeticOperator(n.Op) || isBitwiseOperator(n.Op)) {
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
	if cast, ok := v.traitConversion(node, expected); ok {
		return cast
	}
	if target, ok := traitPointer(expected); ok {
		if types.HasUntyped(got) {
			v.errorf(node, "cannot infer concrete type for trait conversion; add a type annotation or cast")
			return node
		}
		pointer := types.PointerType{Base: got, Mutable: target.Mutable}
		methods, conforms := v.analyser.structuralConformance(pointer, target, node)
		if conforms {
			op := parser.UnaryOpReference
			if target.Mutable {
				op = parser.UnaryOpMutableReference
			}
			reference := &parser.UnaryOpNode{Op: op, Operand: node, Loc: node.GetLoc(), Type: pointer}
			if !target.Mutable || v.validateReferenceTarget(reference, node, true) {
				return &parser.CastNode{Operand: reference, Loc: node.GetLoc(), Type: expected, TraitConversion: true, ConcreteType: got, TraitMethods: methods}
			}
			return node
		}
	}
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

func isFlagsType(t types.Type) bool { _, ok := types.Underlying(t).(types.FlagsType); return ok }

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
	if flagType, ok := types.Underlying(expected).(types.FlagsType); ok {
		if len(n.Fields) != 0 {
			v.errorf(n, "flags literals use '.member' entries")
			n.SetType(types.ErrorType{})
			return
		}
		seen := map[string]struct{}{}
		for _, member := range n.FlagMembers {
			if _, duplicate := seen[member]; duplicate {
				v.errorf(n, "duplicate flag member %q", member)
				continue
			}
			seen[member] = struct{}{}
			if _, exists := flagType.VariantValue(member); !exists {
				v.errorf(n, "unknown flag member %q", member)
			}
		}
		n.SetType(expected)
		return
	}
	if len(n.FlagMembers) != 0 {
		v.errorf(n, "only flags literals may use '.member' entries")
		n.SetType(types.ErrorType{})
		return
	}
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
