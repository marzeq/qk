package sema

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

type Validator struct {
	analyser                      *Analyser
	currentFunction               *symbols.Symbol
	currentFunctionParameters     map[*symbols.Symbol]int
	currentTypedVariadicParameter *symbols.Symbol
	errors                        []error
	warnings                      []error
	validatingTemplate            bool
}

func (a *Analyser) NewValidator() *Validator {
	return &Validator{analyser: a}
}

func (v *Validator) errorf(node parser.Node, format string, args ...any) {
	v.errors = append(v.errors, shared.NewError(node.GetLoc(), format, args...))
}

func (v *Validator) warnLocf(kind shared.WarningKind, loc shared.Location, format string, args ...any) {
	v.warnings = append(v.warnings, shared.NewWarning(kind, loc, format, args...))
}

func (v *Validator) warnIfUnused(symbol *symbols.Symbol, loc shared.Location, warningKind shared.WarningKind, bindingKind string) {
	if v.analyser.currentTrustedStandardLibrary || symbol == nil || symbol.Name == "_" || symbol.Referenced {
		return
	}
	v.warnLocf(warningKind, loc, "unused %s %q", bindingKind, symbol.Name)
}

func (v *Validator) ValidateModule(root *parser.RootNode) {
	v.validatingTemplate = false
	v.selectModule(root)
	v.validateNode(root)
}

func (v *Validator) ValidateGenericTemplates(root *parser.RootNode) {
	v.validatingTemplate = true
	v.selectModule(root)
	for _, node := range root.Body {
		function, ok := node.(*parser.FunctionDefNode)
		if !ok || !function.IsGeneric() {
			continue
		}
		bindings := v.analyser.templateBindings(function.Symbol)
		v.analyser.withDefinitionContext(
			v.analyser.currentMod,
			v.analyser.currentTrustedStandardLibrary,
			bindings,
			func() { v.validateNode(function) },
		)
	}
	v.validatingTemplate = false
}

func (v *Validator) selectModule(root *parser.RootNode) {
	path := v.analyser.modulePaths[root]
	v.analyser.currentMod = path
	if mod := v.analyser.modules[path]; mod != nil {
		v.analyser.current = mod.Scope
		v.analyser.currentTrustedStandardLibrary = mod.TrustedStandardLibrary
	}
	v.analyser.currentImports = v.analyser.importsByModule[path]
	v.analyser.aliases = v.analyser.aliasesByModule[path]
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
		if n.GenericInstance || (n.IsGeneric() && !v.validatingTemplate) {
			return
		}
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
		prevFunctionParameters := v.currentFunctionParameters
		prevTypedVariadicParameter := v.currentTypedVariadicParameter
		v.currentFunction = n.Symbol
		v.currentFunctionParameters = make(map[*symbols.Symbol]int, len(n.Args))
		for i, argument := range n.Args {
			v.currentFunctionParameters[argument.Symbol] = i
		}
		v.currentTypedVariadicParameter = nil
		if n.Symbol.Signature.TypedVariadic {
			v.currentTypedVariadicParameter = n.Args[len(n.Args)-1].Symbol
		}

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
			if hasVoidValue(param) {
				v.errorf(n.Args[i].Type, "function parameter cannot have type void")
			}
			if usesCABI && types.HasTraitPointer(param) {
				v.errorf(n.Args[i].Type, "trait pointers cannot cross the c ABI")
			}
			if usesCABI && types.HasTaggedUnion(param) {
				v.errorf(n.Args[i].Type, "tagged unions cannot cross the c ABI; expose an explicitly tagged union through @reprof(...) instead")
			}
			if _, array := types.Underlying(param).(types.ArrayType); usesCABI && array {
				v.errorf(n.Args[i].Type, "arrays cannot be direct c ABI parameters; pass a pointer or wrap the array in a struct")
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
		} else if usesCABI && ret != nil && types.HasTaggedUnion(ret) {
			v.errorf(n, "tagged unions cannot cross the c ABI; expose an explicitly tagged union through @reprof(...) instead")
		} else if _, array := types.Underlying(ret).(types.ArrayType); usesCABI && ret != nil && array {
			v.errorf(n, "arrays cannot be returned directly through the c ABI; return a struct containing the array")
		}
		if ret := n.Symbol.Signature.ReturnType; ret != nil && !ret.Equals(types.PrimitiveVoid) {
			if _, multiple := ret.(types.MultipleReturnType); !multiple && hasVoidValue(ret) {
				v.errorf(n, "function return cannot have void as a value type")
			}
		}
		if multiple, ok := n.Symbol.Signature.ReturnType.(types.MultipleReturnType); ok {
			if usesCABI {
				v.errorf(n, "multiple return values are not supported by the c ABI; use abi \"qk\"")
			}
			for _, result := range multiple.Types {
				if hasVoidValue(result) {
					v.errorf(n, "multiple return values cannot contain void")
					break
				}
			}
		}

		if n.Body != nil {
			if n.ExpressionBody {
				body := n.Body.(parser.ExpressionNode)
				if parser.NodeFallsThrough(n.Body) {
					n.Body = v.validateExprWithExpected(body, n.Symbol.Signature.ReturnType)
				} else {
					v.validateExpr(body)
				}
			} else {
				v.validateNode(n.Body)
				returnType := n.Symbol.Signature.ReturnType
				_, invalidReturnType := returnType.(types.ErrorType)
				if returnType != nil &&
					!returnType.Equals(types.PrimitiveVoid) &&
					!invalidReturnType &&
					parser.NodeFallsThrough(n.Body) {
					v.errorf(n.Body, "non-void function may fall through without returning a value")
				}
			}
		}
		if n.Body != nil {
			for i, arg := range n.Args {
				if n.Symbol.Method && !n.Symbol.StaticMethod && i == 0 && arg.Name == "self" {
					continue
				}
				v.warnIfUnused(arg.Symbol, arg.GetLoc(), shared.WarningUnusedParameter, "function parameter")
			}
		}

		v.currentFunction = prev
		v.currentFunctionParameters = prevFunctionParameters
		v.currentTypedVariadicParameter = prevTypedVariadicParameter

	case *parser.BlockNode:
		for _, stmt := range n.Body {
			v.validateStatement(stmt)
		}

	case *parser.DeclarationNode:
		if len(n.GenericParameters) != 0 {
			return
		}
		if n.Name == "_" {
			if n.Value != nil {
				v.validateValueExpr(n.Value)
			}
			break
		}
		v.validateAttributes(n, n.Attributes, "declaration", attributes.AttributeTypeForeign)
		v.finaliseDeclaration(n)
		if n.Comptime && !isGenericComptimeExpression(n.Value) {
			v.errorf(n, "comptime generic initializer must be a constant integer expression")
		}
		if n.Symbol.Type != nil && !types.IsComplete(n.Symbol.Type) {
			v.errorf(n, "cannot declare a value of incomplete type %v", n.Symbol.Type)
		}
		if n.Attributes.Get(attributes.AttributeTypeForeign) != nil && types.HasTaggedUnion(n.Symbol.Type) {
			v.errorf(n, "tagged unions cannot cross a foreign boundary; expose an explicitly tagged union through @reprof(...) instead")
		}
		if v.currentFunction != nil {
			v.warnIfUnused(n.Symbol, n.NameLoc, shared.WarningUnusedVariable, "variable")
		}
	case *parser.MultiDeclarationNode:
		v.validateMultiDeclaration(n)
		for i, symbol := range n.Symbols {
			v.warnIfUnused(symbol, n.NameLocs[i], shared.WarningUnusedVariable, "variable")
		}

	case *parser.AssignmentNode:
		v.validateAssignment(n)

	case *parser.IfNode:
		v.validateIf(n)

	case *parser.MatchNode:
		v.validateMatch(n, nil)

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
		if len(n.GenericParameters) != 0 {
			return
		}
		underlying := types.Underlying(n.Symbol.TypeInfo)
		if n.Transparent && types.IsOpaque(n.Symbol.TypeInfo) {
			v.errorf(n, "opaque type %q cannot be a transparent alias", n.Name)
		} else if _, trait := underlying.(types.TraitType); trait {
			if n.Transparent {
				v.errorf(n, "trait %q cannot be a transparent alias", n.Name)
			}
		} else if _, opaque := underlying.(types.OpaqueType); !opaque && !types.IsComplete(underlying) {
			v.errorf(n, "type %q contains an incomplete type by value", n.Name)
		} else {
			switch underlying.(type) {
			case types.StructType, types.UnionType, types.SliceType, types.ArrayType:
				if hasVoidValue(underlying) {
					v.errorf(n, "type %q contains void by value", n.Name)
				}
			}
		}

	default:
		panic(fmt.Sprintf("unhandled node type %T", n))
	}
}

func (v *Validator) validateStatement(node parser.Node) {
	v.validateNode(node)

	switch n := node.(type) {
	case *parser.FunctionCallNode:
		return
	case *parser.InlineAsmNode:
		return
	case *parser.BlockNode:
		if !n.Expression {
			return
		}
	case *parser.IfNode:
		if !n.Expression {
			return
		}
	case *parser.MatchNode:
		if !n.Expression {
			return
		}
	case parser.ExpressionNode:
		// Any other expression in statement position computes a value that is
		// immediately discarded. Require that intent to be explicit.
	default:
		return
	}

	v.errorf(node, "expression result is unused; assign it to '_' to discard it")
}

func isGenericComptimeExpression(node parser.ExpressionNode) bool {
	switch node := node.(type) {
	case *parser.IntegerLiteralNode, *parser.BoolLiteralNode, *parser.CharLiteralNode,
		*parser.SizeOfNode:
		return true
	case *parser.CastNode:
		return isGenericComptimeExpression(node.Operand)
	case *parser.UnaryOpNode:
		switch node.Op {
		case parser.UnaryOpNegate, parser.UnaryOpBitwiseNot, parser.UnaryOpLogicalNot:
			return isGenericComptimeExpression(node.Operand)
		}
	case *parser.BinaryOpNode:
		switch node.Op {
		case parser.BinaryOpAdd, parser.BinaryOpSubtract, parser.BinaryOpMultiply,
			parser.BinaryOpDivide, parser.BinaryOpModulo, parser.BinaryOpBitwiseAnd,
			parser.BinaryOpBitwiseOr, parser.BinaryOpBitwiseXor, parser.BinaryOpShiftLeft,
			parser.BinaryOpShiftRight:
			return isGenericComptimeExpression(node.Operand1) && isGenericComptimeExpression(node.Operand2)
		}
	}
	return false
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
	if noInit, ok := n.Value.(*parser.NoInitializerNode); ok {
		if n.TypeNode == nil {
			v.errorf(n, "'---' declaration requires a type annotation")
			n.Symbol.Type = types.ErrorType{}
			return
		}
		declared := v.analyser.resolveTypeNode(n.TypeNode)
		noInit.SetType(declared)
		n.Symbol.Type = declared
		return
	}

	if n.TypeNode != nil {
		declared := v.analyser.resolveTypeNode(n.TypeNode)
		if hasVoidValue(declared) {
			v.errorf(n, "declaration cannot have type void")
			v.validateExpr(n.Value)
			n.Symbol.Type = types.ErrorType{}
			return
		}
		n.Value = v.validateExprWithExpected(n.Value, declared)

		n.Symbol.Type = declared
		return
	}

	if !v.validateValueExpr(n.Value) {
		n.Symbol.Type = types.ErrorType{}
		return
	}

	valueType := n.Value.GetType()
	if _, multiple := valueType.(types.MultipleReturnType); multiple {
		v.errorf(n, "multiple return values must be unpacked into a matching target list")
		n.Symbol.Type = types.ErrorType{}
		return
	}

	if types.HasUntyped(valueType) {
		if n.Symbol.InlineComptime {
			return
		}
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
		v.validateValueExpr(n.Value)
		result, ok := n.Value.GetType().(types.MultipleReturnType)
		if !ok {
			v.errorf(n, "multiple assignment requires a multiple-result expression")
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
		v.validateValueExpr(n.Value)
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
	v.validateValueExpr(n.Value)
	result, ok := n.Value.GetType().(types.MultipleReturnType)
	if !ok {
		v.errorf(n, "multiple declaration requires a multiple-result expression")
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
	return v.validateMutablePlace(expr, false)
}

func (v *Validator) validateMutablePlace(expr parser.ExpressionNode, reference bool) bool {
	switch e := expr.(type) {
	case *parser.IdentifierNode:
		if !e.GetSymbol().Mutable {
			if reference {
				v.errorf(e, "cannot take mutable reference of immutable variable")
			} else {
				v.errorf(e, "cannot assign to immutable symbol")
			}
			return false
		}
		v.validateExpr(e)

	case *parser.UnaryOpNode:
		if e.Op != parser.UnaryOpDereference {
			v.errorf(expr, "invalid assignment target")
			return false
		}
		return v.validateMutableAccessPath(e, true, reference)

	case *parser.FieldAccessNode:
		if e.TaggedUnionType != nil {
			if reference {
				v.errorf(e, "cannot take mutable reference of this expression")
			} else {
				v.errorf(e, "cannot assign to this expression")
			}
			return false
		}
		return v.validateMutableAccessPath(e, true, reference)

	case *parser.IndexExprNode:
		return v.validateMutableAccessPath(e, true, reference)

	default:
		if reference {
			v.errorf(expr, "cannot take reference of this expression")
		} else {
			v.errorf(expr, "cannot assign to this expression")
		}
		return false
	}

	return true
}

// validateMutableAccessPath validates every storage/view boundary used to
// reach a mutation. Pointer bindings are capabilities, so the binding itself
// need not be mutable, but every pointer that is dereferenced must be *mut.
// Mutable slices similarly carry element-write capability in their type.
func (v *Validator) validateMutableAccessPath(expr parser.ExpressionNode, requireMutableRoot bool, reference bool) bool {
	switch e := expr.(type) {
	case *parser.IdentifierNode:
		v.validateExpr(e)
		if !requireMutableRoot || e.Symbol == nil {
			return true
		}
		switch t := types.Underlying(e.GetType()).(type) {
		case types.PointerType:
			return true
		case types.SliceType:
			if t.Mutable {
				return true
			}
		}
		if !e.Symbol.Mutable {
			if reference {
				v.errorf(e, "cannot take mutable reference through immutable variable")
			} else {
				v.errorf(e, "cannot assign through immutable symbol")
			}
			return false
		}
		return true

	case *parser.FieldAccessNode:
		if e.TaggedUnionType != nil {
			if reference {
				v.errorf(e, "cannot take mutable reference of this expression")
			} else {
				v.errorf(e, "cannot assign to this expression")
			}
			return false
		}
		if e.ResolvedIdentifier != nil {
			return v.validateMutableAccessPath(e.ResolvedIdentifier, requireMutableRoot, reference)
		}
		if !v.validateMutableAccessPath(e.Subject, requireMutableRoot, reference) {
			return false
		}
		if pointer, ok := types.Underlying(e.Subject.GetType()).(types.PointerType); ok {
			return v.validateMutablePointerBoundary(e, pointer, reference)
		}
		return true

	case *parser.IndexExprNode:
		if !v.validateMutableAccessPath(e.Subject, requireMutableRoot, reference) {
			return false
		}
		switch subject := types.Underlying(e.Subject.GetType()).(type) {
		case types.SliceType:
			if !subject.Mutable {
				if reference {
					v.errorf(e, "cannot take mutable reference through immutable slice view")
				} else {
					v.errorf(e, "cannot assign through immutable slice view")
				}
				return false
			}
		case types.ArrayType:
			return true
		case types.PointerType:
			return v.validateMutablePointerBoundary(e, subject, reference)
		case types.ErrorType:
			return false
		default:
			if reference {
				v.errorf(e, "cannot take mutable reference through non-indexable value")
			} else {
				v.errorf(e, "cannot assign to index of non-slice type")
			}
			return false
		}
		return true

	case *parser.SliceExprNode:
		return v.validateMutableAccessPath(e.Subject, requireMutableRoot, reference)

	case *parser.UnaryOpNode:
		if e.Op != parser.UnaryOpDereference {
			break
		}
		pointer, ok := types.Underlying(e.Operand.GetType()).(types.PointerType)
		if !ok {
			return false
		}
		if !v.validateMutableAccessPath(e.Operand, requireMutableRoot, reference) {
			return false
		}
		return v.validateMutablePointerBoundary(e, pointer, reference)

	case *parser.FunctionCallNode:
		v.validateExpr(e)
		switch types.Underlying(e.GetType()).(type) {
		case types.PointerType, types.SliceType:
			return true
		}
		break

	default:
		// Casts and pointer arithmetic may also produce a capability. Their
		// validation prevents immutable-to-mutable upgrades.
		v.validateExpr(expr)
		switch types.Underlying(expr.GetType()).(type) {
		case types.PointerType, types.SliceType:
			return true
		}
	}

	if reference {
		v.errorf(expr, "cannot take mutable reference through this expression")
	} else {
		v.errorf(expr, "cannot assign through this expression")
	}
	return false
}

func (v *Validator) validateMutablePointerBoundary(node parser.Node, pointer types.PointerType, reference bool) bool {
	if pointer.Base.Equals(types.PrimitiveVoid) {
		if reference {
			v.errorf(node, "cannot take mutable reference through void pointer")
		} else {
			v.errorf(node, "cannot assign to dereferenced void pointer")
		}
		return false
	}
	if !pointer.Mutable {
		if reference {
			v.errorf(node, "cannot take mutable reference through immutable pointer")
		} else {
			v.errorf(node, "cannot assign through immutable pointer")
		}
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

func (v *Validator) validateExpressionBlock(n *parser.BlockNode, expected types.Type) {
	result, hasResult := parser.BlockResult(n)
	last := len(n.Body)
	if hasResult {
		last--
	}
	for _, child := range n.Body[:last] {
		v.validateStatement(child)
	}

	if !hasResult {
		if parser.NodeFallsThrough(n) {
			v.errorf(n, "block used as expression must end with an expression")
			n.SetType(types.ErrorType{})
		}
		return
	}

	if !parser.NodeFallsThrough(n) {
		v.validateStatement(result)
		return
	}

	if expected != nil {
		result = v.validateExprWithExpected(result, expected)
		n.Body[len(n.Body)-1] = result
		n.SetType(expected)
	} else {
		if !v.validateValueExpr(result) {
			n.SetType(types.ErrorType{})
			return
		}
		n.SetType(result.GetType())
	}
}

func (v *Validator) validateIfExpression(n *parser.IfNode, expected types.Type) {
	v.validateExpr(n.IfBranch.Condition)
	if !n.IfBranch.Condition.GetType().Equals(types.PrimitiveBool) {
		v.errorf(n, "if expression condition must be bool")
	}

	validateBranch := func(block *parser.BlockNode) {
		if expected != nil && parser.NodeFallsThrough(block) {
			v.validateExprWithExpected(block, expected)
		} else {
			v.validateExpr(block)
		}
	}
	validateBranch(n.IfBranch.Node)
	for _, branch := range n.ElseIfBranches {
		v.validateExpr(branch.Condition)
		if !branch.Condition.GetType().Equals(types.PrimitiveBool) {
			v.errorf(n, "elseif condition must be bool")
		}
		validateBranch(branch.Node)
	}
	if n.ElseBranch == nil {
		v.errorf(n, "if expression requires an else branch")
	} else {
		validateBranch(n.ElseBranch)
	}
	if expected != nil {
		n.SetType(expected)
		return
	}
	branches := []*parser.BlockNode{n.IfBranch.Node}
	for _, branch := range n.ElseIfBranches {
		branches = append(branches, branch.Node)
	}
	branches = append(branches, n.ElseBranch)
	for _, branch := range branches {
		if branch != nil {
			if _, erroneous := branch.GetType().(types.ErrorType); erroneous {
				n.SetType(types.ErrorType{})
				return
			}
		}
	}
}

func (v *Validator) validateMatch(n *parser.MatchNode, expected types.Type) {
	v.validateValueExpr(n.Subject)
	subjectType := n.Subject.GetType()
	covered := map[string]bool{}
	wildcard := false
	for i := range n.Arms {
		arm := &n.Arms[i]
		v.validateMatchPattern(arm.Pattern, subjectType)
		if wildcard || v.matchPatternFullyCovered(arm.Pattern, covered) {
			v.errorf(arm.Pattern, "match arm is unreachable because an earlier arm already covers its pattern")
		}
		if arm.Guard != nil {
			v.validateExpr(arm.Guard)
			if !arm.Guard.GetType().Equals(types.PrimitiveBool) {
				v.errorf(arm.Guard, "match arm guard must be bool")
			}
		} else {
			v.recordMatchCoverage(arm.Pattern, covered, &wildcard)
		}
		if n.Expression {
			if !parser.NodeFallsThrough(arm.Body) {
				v.validateExpr(arm.Body)
			} else if expected != nil {
				arm.Body = v.validateExprWithExpected(arm.Body, expected)
			} else {
				v.validateValueExpr(arm.Body)
			}
		} else {
			v.validateStatement(arm.Body)
		}
		for _, binding := range matchPatternBindings(arm.Pattern) {
			v.warnIfUnused(binding.Symbol, binding.Loc, shared.WarningUnusedVariable, "match binding")
		}
	}
	if n.Binding != nil {
		v.warnIfUnused(n.Binding, n.BindingLoc, shared.WarningUnusedVariable, "match subject binding")
	}
	if !v.matchIsExhaustive(subjectType, covered, wildcard) {
		v.errorf(n, "match is not exhaustive; add the missing cases or a '_' arm")
	}
	if !n.Expression {
		n.SetType(types.PrimitiveVoid)
	} else if expected != nil {
		n.SetType(expected)
	}
}

func (v *Validator) validateMatchPattern(pattern *parser.MatchPatternNode, subjectType types.Type) {
	if pattern == nil {
		return
	}
	switch pattern.Kind {
	case parser.MatchPatternWildcard, parser.MatchPatternVariant:
		return
	case parser.MatchPatternAlternative:
		for _, alternative := range pattern.Alternatives {
			if len(matchPatternBindings(alternative)) != 0 {
				v.errorf(alternative, "alternative match patterns cannot bind payload values")
			}
			v.validateMatchPattern(alternative, subjectType)
		}
	case parser.MatchPatternLiteral:
		switch pattern.Literal.(type) {
		case *parser.FloatLiteralNode:
			v.errorf(pattern, "floating-point match patterns are not supported; use an 'if' guard")
			return
		case *parser.StringLiteralNode, *parser.CStringLiteralNode:
			v.errorf(pattern, "string match patterns are not supported; use an 'if' guard")
			return
		}
		pattern.Literal = v.validateExprWithExpected(pattern.Literal, subjectType)
	case parser.MatchPatternRange:
		if !types.IsInteger(subjectType) {
			v.errorf(pattern, "range pattern requires an integer subject")
			return
		}
		pattern.Start = v.validateExprWithExpected(pattern.Start, subjectType)
		pattern.End = v.validateExprWithExpected(pattern.End, subjectType)
	}
}

func (v *Validator) recordMatchCoverage(pattern *parser.MatchPatternNode, covered map[string]bool, wildcard *bool) {
	if pattern == nil {
		return
	}
	switch pattern.Kind {
	case parser.MatchPatternWildcard:
		*wildcard = true
	case parser.MatchPatternVariant:
		covered[v.matchPatternCoverageKey(pattern)] = true
	case parser.MatchPatternLiteral:
		if key := v.matchPatternCoverageKey(pattern); key != "" {
			covered[key] = true
		}
	case parser.MatchPatternAlternative:
		for _, alternative := range pattern.Alternatives {
			v.recordMatchCoverage(alternative, covered, wildcard)
		}
	}
}

func (v *Validator) matchPatternFullyCovered(pattern *parser.MatchPatternNode, covered map[string]bool) bool {
	if pattern == nil {
		return false
	}
	switch pattern.Kind {
	case parser.MatchPatternVariant, parser.MatchPatternLiteral:
		key := v.matchPatternCoverageKey(pattern)
		return key != "" && covered[key]
	case parser.MatchPatternAlternative:
		if len(pattern.Alternatives) == 0 {
			return false
		}
		for _, alternative := range pattern.Alternatives {
			if !v.matchPatternFullyCovered(alternative, covered) {
				return false
			}
		}
		return true
	}
	return false
}

func (v *Validator) matchPatternCoverageKey(pattern *parser.MatchPatternNode) string {
	if pattern == nil {
		return ""
	}
	if pattern.Kind == parser.MatchPatternVariant {
		if pattern.TagValue != "" {
			return "variant-value:" + pattern.TagValue
		}
		return "variant:" + pattern.Variant
	}
	if pattern.Kind != parser.MatchPatternLiteral {
		return ""
	}
	switch literal := pattern.Literal.(type) {
	case *parser.BoolLiteralNode:
		return "bool:" + literal.Value
	case *parser.IntegerLiteralNode:
		return "integer:" + literal.Value
	case *parser.CharLiteralNode:
		return fmt.Sprintf("integer:%d", literal.Value)
	}
	return ""
}

func (v *Validator) matchIsExhaustive(subjectType types.Type, covered map[string]bool, wildcard bool) bool {
	if wildcard {
		return true
	}
	if info, tagged := types.TaggedUnion(subjectType); tagged {
		for _, variant := range info.Variants {
			if !covered["variant-value:"+variant.TagValue] {
				return false
			}
		}
		return true
	}
	if enumType, ok := types.Underlying(subjectType).(types.EnumType); ok {
		for _, value := range enumType.Values {
			if !covered["variant-value:"+value] {
				return false
			}
		}
		return true
	}
	if subjectType.Equals(types.PrimitiveBool) {
		return covered["bool:true"] && covered["bool:false"]
	}
	return false
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
		v.validateStatement(n.ExprsOrStmts[0])
		condition, ok := n.ExprsOrStmts[1].(parser.ExpressionNode)
		if !ok {
			v.errorf(n.ExprsOrStmts[1], "for loop condition must be an expression")
		} else {
			v.validateExpr(condition)
			if !condition.GetType().Equals(types.PrimitiveBool) {
				v.errorf(condition, "for loop condition must be bool")
			}
		}
		v.validateStatement(n.ExprsOrStmts[2])
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
	v.warnIfUnused(n.Symbol, n.NameLoc, shared.WarningUnusedVariable, "variable")

	iteratorType := types.PrimitiveUsz
	n.Start = v.validateExprWithExpected(n.Start, iteratorType)
	n.End = v.validateExprWithExpected(n.End, iteratorType)
	if n.Symbol != nil {
		n.Symbol.Type = iteratorType
	}
}

func (v *Validator) validateForEach(n *parser.ForEachNode) {
	v.validateExpr(n.Iterable)
	var element types.Type
	iterableType := types.Underlying(n.Iterable.GetType())
	switch iterable := iterableType.(type) {
	case types.SliceType:
		element = iterable.Base
	case types.ArrayType:
		element = iterable.Base
	default:
		v.errorf(n, "for loop iterable must be an array or slice")
		return
	}
	if n.ElementKind != parser.ForEachElementValue {
		slice, ok := iterableType.(types.SliceType)
		if !ok {
			v.errorf(n, "element pointer iteration requires a slice or string")
			return
		}
		if n.ElementKind == parser.ForEachElementMutablePointer && !slice.Mutable {
			v.errorf(n, "mutable element pointer iteration requires a mutable slice")
			return
		}
	}
	if types.HasUntyped(element) {
		v.errorf(n, "cannot infer for loop element type from untyped array or slice")
		return
	}
	if len(n.Destructure) != 0 {
		componentTypes, destructurable := forEachDestructureTypes(element)
		if !destructurable {
			v.errorf(n, "for loop destructuring requires an array or struct element")
			return
		}
		if len(n.Destructure) != len(componentTypes) {
			v.errorf(n, "destructuring pattern has %d bindings, but loop element has %d values",
				len(n.Destructure), len(componentTypes))
			return
		}
		for i := range n.Destructure {
			if n.Destructure[i].Symbol != nil {
				n.Destructure[i].Symbol.Type = componentTypes[i]
			}
		}
	} else if n.Symbol != nil {
		n.Symbol.Type = forEachElementType(n, element)
	}
	if n.IndexSymbol != nil {
		n.IndexSymbol.Type = types.PrimitiveUsz
	}
	v.validateNode(n.Body)
	if len(n.Destructure) != 0 {
		for i := range n.Destructure {
			binding := &n.Destructure[i]
			v.warnIfUnused(binding.Symbol, binding.Loc, shared.WarningUnusedVariable, "variable")
		}
	} else {
		v.warnIfUnused(n.Symbol, n.NameLoc, shared.WarningUnusedVariable, "variable")
	}
	if n.IndexName != "" {
		v.warnIfUnused(n.IndexSymbol, n.IndexNameLoc, shared.WarningUnusedVariable, "variable")
	}
}

func (v *Validator) validateReturn(n *parser.ControlKeywordNode) {
	if v.currentFunction == nil {
		v.errorf(n, "return outside of function")
		return
	}
	expected := v.currentFunction.Signature.ReturnType
	if multi, ok := expected.(types.MultipleReturnType); ok {
		if len(n.ReturnValues) == 1 {
			switch n.ReturnValue.(type) {
			case *parser.FunctionCallNode, *parser.InlineAsmNode:
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
	case *parser.InlineAsmNode:
		v.validateInlineAsm(n)

	case *parser.EnumLiteralNode:
		v.errorf(n, "cannot infer enum type for .%s", n.Variant)
		n.SetType(types.ErrorType{})

	case *parser.FunctionCallNode:
		if !v.completeGenericCall(n, nil) {
			return
		}
		if n.TaggedUnionType != nil {
			info, _ := types.TaggedUnion(n.TaggedUnionType)
			if n.TaggedUnionVariant < 0 || n.TaggedUnionVariant >= len(info.Variants) {
				v.errorf(n, "invalid tagged union constructor")
				return
			}
			variant := info.Variants[n.TaggedUnionVariant]
			if len(n.Args) != len(variant.Fields) {
				v.errorf(n, "tagged union variant %q expects %d arguments, but %d provided", variant.Name, len(variant.Fields), len(n.Args))
				return
			}
			for i := range n.Args {
				n.Args[i] = v.validateExprWithExpected(n.Args[i], variant.Fields[i].R)
			}
			n.SetType(n.TaggedUnionType)
			return
		}
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
				v.validateValueExpr(arg)
				if types.HasUntyped(arg.GetType()) {
					v.errorf(arg, "cannot infer type for variadic argument from untyped numeric value; add a cast")
				}
				continue
			}

			paramType := params[i]
			n.Args[i] = v.validateExprWithExpected(arg, paramType)
		}
		if n.Symbol != nil && n.Symbol.Signature != nil &&
			n.Symbol.Attributes.Get(attributes.AttributeTypeForeign) == nil {
			fixedCount := len(n.Symbol.Signature.Parameters)
			if n.Symbol.Signature.TypedVariadic {
				fixedCount--
			}
			crossModule := v.currentFunction != nil &&
				v.currentFunction.DefinitionModule != n.Symbol.DefinitionModule
			for i := 0; i < len(n.Args) && i < fixedCount; i++ {
				if constant, ok := specializationConstant(n.Args[i]); crossModule && ok {
					if n.Symbol.Signature.ConstantArguments == nil {
						n.Symbol.Signature.ConstantArguments = make(map[int]map[string]symbols.SpecializationConstant)
					}
					if n.Symbol.Signature.ConstantArguments[i] == nil {
						n.Symbol.Signature.ConstantArguments[i] = make(map[string]symbols.SpecializationConstant)
					}
					n.Symbol.Signature.ConstantArguments[i][constant.Key()] = constant
					continue
				}
				identifier, ok := n.Args[i].(*parser.IdentifierNode)
				if !ok || identifier.Symbol == nil || identifier.Symbol.Mutable || v.currentFunction == nil {
					continue
				}
				callerParameter, forwarded := v.currentFunctionParameters[identifier.Symbol]
				if !forwarded {
					continue
				}
				v.currentFunction.Signature.ConstantForwards = append(
					v.currentFunction.Signature.ConstantForwards,
					symbols.ConstantForward{
						CallerParameter: callerParameter,
						Callee:          n.Symbol.Signature,
						CalleeParameter: i,
					},
				)
			}
		}
		if typedVariadic {
			n.TypedVariadic = true
			n.TypedVariadicStart = len(params) - 1
			n.TypedVariadicSlice = params[len(params)-1]
			if n.Symbol != nil && n.Symbol.Attributes.Get(attributes.AttributeTypeForeign) == nil && !n.VariadicExpansion {
				if n.Symbol.Signature.TypedVariadicArities == nil {
					n.Symbol.Signature.TypedVariadicArities = make(map[int]bool)
				}
				n.Symbol.Signature.TypedVariadicArities[len(n.Args)-n.TypedVariadicStart] = true
			} else if n.Symbol != nil && n.Symbol.Attributes.Get(attributes.AttributeTypeForeign) == nil &&
				n.VariadicExpansion && v.currentTypedVariadicParameter != nil {
				if argument, ok := n.Args[n.TypedVariadicStart].(*parser.IdentifierNode); ok &&
					argument.Symbol == v.currentTypedVariadicParameter {
					v.currentFunction.Signature.TypedVariadicForwards = append(
						v.currentFunction.Signature.TypedVariadicForwards,
						n.Symbol.Signature,
					)
				}
			}
		} else if n.VariadicExpansion {
			v.errorf(n, "slice expansion requires a typed variadic function")
		}

	case *parser.CastNode:
		targetType := n.Type
		if n.Checked {
			targetType = n.CheckedType
		}
		if literal, ok := n.Operand.(*parser.SliceLiteralNode); ok {
			v.validateSliceLiteralWithExpected(literal, targetType)
			break
		}
		if identifier := comptimeIdentifier(n.Operand); identifier != nil && identifier.Symbol != nil &&
			identifier.Symbol.InlineComptime && types.IsUntyped(n.Operand.GetType()) {
			n.Operand = v.validateExprWithExpected(n.Operand, targetType)
		} else {
			v.validateExpr(n.Operand)
		}
		if n.StaticTraitView != nil {
			v.validateStaticTraitAssertion(n)
			break
		}
		if conversion, ok := v.traitConversion(n.Operand, targetType); ok {
			n.TraitConversion = true
			n.ConcreteType = conversion.ConcreteType
			n.TraitMethods = conversion.TraitMethods
			break
		}
		if target, ok := traitPointer(targetType); ok {
			got := n.Operand.GetType()
			parameter, symbolic := genericTypeParameterBase(got)
			constraint, constrained := types.TraitType{}, false
			if symbolic {
				constraint, constrained = types.Underlying(parameter.Constraint).(types.TraitType)
			}
			pointer, isPointer := types.Underlying(got).(types.PointerType)
			if constrained && isPointer && traitImplementsTrait(constraint, target.Trait) {
				if target.Mutable && !pointer.Mutable {
					v.errorf(n, "cannot cast immutable %v to mutable %v", got, targetType)
					break
				}
				n.ConcreteType = pointer.Base
				n.TraitConversion = true
				n.GenericAssertion = false
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
					if n.Checked {
						n.TraitUnwrap = false
						n.StaticAssertion = true
						n.AssertionMatches = false
					} else {
						v.errorf(n, "type %v does not conform to %v", targetPointer.Base, sourceTrait.Trait)
					}
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
				if n.Checked {
					n.TraitUnwrap = false
					n.StaticAssertion = true
					n.AssertionMatches = false
				} else {
					v.errorf(n, "type %v does not conform to %v", targetType, sourceTrait.Trait)
				}
			}
			break
		}
		if n.GenericAssertion {
			n.AssertionMatches = n.Operand.GetType().Equals(targetType)
			break
		}
		if sourceArray, ok := types.Underlying(n.Operand.GetType()).(types.ArrayType); ok {
			if targetSlice, ok := types.Underlying(targetType).(types.SliceType); ok && sourceArray.Base.Equals(targetSlice.Base) {
				reference := &parser.UnaryOpNode{Operand: n.Operand, Loc: n.Operand.GetLoc()}
				if !v.validateReferenceTarget(reference, n.Operand, targetSlice.Mutable) {
					break
				}
				return
			}
		}
		if !types.CanExplicitCast(n.Operand.GetType(), targetType) {
			if n.Checked {
				n.StaticAssertion = true
				n.AssertionMatches = false
				break
			}
			v.errorf(n, "cannot cast %v to %v", n.Operand.GetType(), targetType)
		}

	case *parser.ReprNode:
		v.validateExpr(n.Operand)

	case *parser.UnaryOpNode:
		v.validateExpr(n.Operand)

		operandType := n.Operand.GetType()

		switch n.Op {

		case parser.UnaryOpLogicalNot:
			if !operandType.Equals(types.PrimitiveBool) {
				v.errorf(n, "operator ! requires bool")
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
			switch types.Underlying(operandType).(type) {
			case types.SliceType, types.ArrayType:
				// OK
			default:
				v.errorf(n, "length operator requires an array or slice operand")
			}

		default:
			v.errorf(n, "unknown unary operator")
		}

	case *parser.BinaryOpNode:
		_, leftEnumShorthand := n.Operand1.(*parser.EnumLiteralNode)
		_, rightEnumShorthand := n.Operand2.(*parser.EnumLiteralNode)
		_, leftNil := n.Operand1.(*parser.NilLiteralNode)
		_, rightNil := n.Operand2.(*parser.NilLiteralNode)
		switch {
		case leftEnumShorthand && !rightEnumShorthand:
			v.validateExpr(n.Operand2)
			n.Operand1 = v.validateExprWithExpected(n.Operand1, n.Operand2.GetType())
		case rightEnumShorthand && !leftEnumShorthand:
			v.validateExpr(n.Operand1)
			n.Operand2 = v.validateExprWithExpected(n.Operand2, n.Operand1.GetType())
		case leftNil && !rightNil && (n.Op == parser.BinaryOpEqual || n.Op == parser.BinaryOpNotEqual):
			v.validateExpr(n.Operand2)
			n.Operand1 = v.validateExprWithExpected(n.Operand1, n.Operand2.GetType())
		case rightNil && !leftNil && (n.Op == parser.BinaryOpEqual || n.Op == parser.BinaryOpNotEqual):
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

			if types.IsNumeric(t1) && types.IsNumeric(t2) {
				common := types.PromoteNumeric(t1, t2)
				if _, isError := common.(types.ErrorType); isError {
					v.errorf(n, "incompatible types for comparison: %v and %v", t1, t2)
					break
				}
				if types.IsUntyped(common) {
					v.errorf(n, "cannot infer numeric type for comparison")
					break
				}
				n.Operand1 = v.validateExprWithExpected(n.Operand1, common)
				n.Operand2 = v.validateExprWithExpected(n.Operand2, common)
			} else if !t1.CanCoerceTo(t2) && !t2.CanCoerceTo(t1) {
				v.errorf(n, "incompatible types for comparison: %v and %v", t1, t2)
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
		case types.ArrayType:
			if idxLit, ok := n.Index.(*parser.IntegerLiteralNode); ok {
				idx, _ := strconv.Atoi(idxLit.Value)
				if idx < 0 || idx >= subjectType.Length {
					v.errorf(n, "index %d out of bounds for array of length %d", idx, subjectType.Length)
				}
			}

		case types.PointerType:
			if !types.IsComplete(subjectType.Base) {
				v.errorf(n, "cannot index pointer to incomplete type %v", subjectType.Base)
			}
		default:
			v.errorf(n, "cannot index into non-array-or-slice type")
		}

	case *parser.SliceExprNode:
		v.validateExpr(n.Subject)
		if n.Start != nil {
			v.validateExpr(n.Start)
			if !types.IsInteger(n.Start.GetType()) {
				v.errorf(n.Start, "slice start must be integer")
			} else {
				n.Start = v.validateExprWithExpected(n.Start, types.PrimitiveUsz)
			}
		}
		if n.End != nil {
			v.validateExpr(n.End)
			if !types.IsInteger(n.End.GetType()) {
				v.errorf(n.End, "slice end must be integer")
			} else {
				n.End = v.validateExprWithExpected(n.End, types.PrimitiveUsz)
			}
		}

		subjectType := types.Underlying(n.Subject.GetType())
		_, isSlice := subjectType.(types.SliceType)
		arrayType, isArray := subjectType.(types.ArrayType)
		pointerType, isPointer := subjectType.(types.PointerType)
		if !isSlice && !isArray && !isPointer {
			v.errorf(n, "cannot slice non-array, non-slice, or non-pointer type")
			break
		}
		if isPointer {
			if n.End == nil {
				v.errorf(n, "pointer slice requires an end bound")
			}
			if !types.IsComplete(pointerType.Base) {
				v.errorf(n, "cannot slice pointer to incomplete type %v", pointerType.Base)
			}
		}
		if isArray {
			result, _ := types.Underlying(n.GetType()).(types.SliceType)
			reference := &parser.UnaryOpNode{Operand: n.Subject, Loc: n.Subject.GetLoc()}
			v.validateReferenceTarget(reference, n.Subject, result.Mutable)
		}

		start, startKnown := staticIntegerValue(n.Start)
		if n.Start == nil {
			start = new(big.Int)
			startKnown = true
		}
		end, endKnown := staticIntegerValue(n.End)
		if n.End == nil && isArray {
			end = big.NewInt(int64(arrayType.Length))
			endKnown = true
		}

		if startKnown && start.Sign() < 0 {
			v.errorf(n.Start, "slice start cannot be negative")
		}
		if endKnown && end.Sign() < 0 {
			v.errorf(n.End, "slice end cannot be negative")
		}
		if startKnown && endKnown && start.Cmp(end) > 0 {
			v.errorf(n, "slice start %s exceeds end %s", start, end)
		}
		if isArray {
			size := big.NewInt(int64(arrayType.Length))
			if startKnown && start.Cmp(size) > 0 {
				v.errorf(n, "slice start %s out of bounds for array of length %d", start, arrayType.Length)
			}
			if endKnown && end.Cmp(size) > 0 {
				v.errorf(n, "slice end %s out of bounds for array of length %d", end, arrayType.Length)
			}
		}

	case *parser.FieldAccessNode:
		if n.TaggedUnionType != nil {
			return
		}
		if n.MethodSymbol != nil && n.MethodSymbol.Template {
			v.errorf(n, "generic method %q requires type arguments when used as a value", n.Field.Name)
			return
		}
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

	case *parser.BlockNode:
		v.validateExpressionBlock(n, nil)

	case *parser.IfNode:
		v.validateIfExpression(n, nil)

	case *parser.MatchNode:
		v.validateMatch(n, nil)

	case *parser.SliceLiteralNode:
		v.errorf(n, "cannot infer whether sequence literal is an array or slice; add a type annotation")
		if n.RepeatValue != nil {
			if _, noInit := n.RepeatValue.(*parser.NoInitializerNode); noInit {
				v.errorf(n, "an uninitialized repeated literal requires an expected array type")
			} else {
				v.validateValueExpr(n.RepeatValue)
			}
			n.RepeatAmount = v.validateExprWithExpected(n.RepeatAmount, types.PrimitiveUsz)
			n.SetType(types.ErrorType{})
			break
		}
		var common types.Type = nil

		if len(n.Elements) == 0 {
			v.errorf(n, "cannot infer element type of empty slice literal")
			n.SetType(types.ErrorType{})
			break
		}

		for _, el := range n.Elements {
			v.validateValueExpr(el)

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

		n.Type = types.ErrorType{}

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
		if n.NoInitRemaining {
			v.errorf(n, "'---' in a struct literal requires a named or expected struct type")
			n.SetType(types.ErrorType{})
			break
		}

		fields := make([]shared.Pair[string, types.Type], 0, len(n.Fields))
		for i, field := range n.Fields {
			v.validateValueExpr(field.R)
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
		*parser.EmbedNode,
		*parser.CStringLiteralNode,
		*parser.CharLiteralNode,
		*parser.NilLiteralNode:
		// nothing to validate

	case *parser.IdentifierNode:
		// Untyped compile-time integers remain untyped until a surrounding
		// ordinary expression supplies a concrete type.

	case *parser.NoInitializerNode:
		v.errorf(n, "'---' is only valid as a declaration initializer, a struct field initializer, or the final struct initializer entry")

	case *parser.SizeOfNode:
		if types.Underlying(n.OperandType).Equals(types.PrimitiveVoid) {
			v.errorf(n, "@sizeof requires an object type, got void")
		} else if !types.IsComplete(n.OperandType) {
			v.errorf(n, "@sizeof requires a complete type, got %v", n.OperandType)
		}

	case *parser.SizeOfExprNode:
		v.validateExpr(n.Operand)
		if types.Underlying(n.OperandType).Equals(types.PrimitiveVoid) {
			v.errorf(n, "@sizeof requires an object expression, got void")
		} else if !types.IsComplete(n.OperandType) {
			v.errorf(n, "@sizeof requires a complete type, got %v", n.OperandType)
		}

	case *parser.AlignOfNode:
		if !types.IsComplete(n.OperandType) {
			v.errorf(n, "@alignof requires a complete type, got %v", n.OperandType)
			break
		}
		switch operand := types.Underlying(n.OperandType).(type) {
		case types.FunctionType:
			v.errorf(n, "@alignof requires an object type, got %v", n.OperandType)
		case types.PrimitiveType:
			if operand == types.PrimitiveVoid {
				v.errorf(n, "@alignof requires an object type, got void")
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

func specializationConstant(expr parser.ExpressionNode) (symbols.SpecializationConstant, bool) {
	switch node := expr.(type) {
	case *parser.StringLiteralNode:
		if len(node.Value) > 256 {
			return symbols.SpecializationConstant{}, false
		}
		return symbols.SpecializationConstant{Kind: "str", Value: node.Value, Type: node.GetType()}, true
	case *parser.CStringLiteralNode:
		if len(node.Value) > 256 {
			return symbols.SpecializationConstant{}, false
		}
		return symbols.SpecializationConstant{Kind: "cstr", Value: node.Value, Type: node.GetType()}, true
	case *parser.BoolLiteralNode:
		return symbols.SpecializationConstant{Kind: "bool", Value: node.Value, Type: node.GetType()}, true
	case *parser.IntegerLiteralNode:
		return symbols.SpecializationConstant{Kind: "int", Value: node.Value, Type: node.GetType()}, true
	case *parser.FloatLiteralNode:
		return symbols.SpecializationConstant{Kind: "float", Value: node.Value, Type: node.GetType()}, true
	case *parser.CharLiteralNode:
		return symbols.SpecializationConstant{Kind: "char", Value: strconv.Itoa(int(node.Value)), Type: node.GetType()}, true
	default:
		return symbols.SpecializationConstant{}, false
	}
}

func (v *Validator) validateInlineAsm(n *parser.InlineAsmNode) {
	seenClobbers := make(map[string]struct{}, len(n.Clobbers))
	for i := range n.Outputs {
		output := &n.Outputs[i]
		if !validInlineAsmType(output.Type) {
			v.errorf(n, "@asm output %d has unsupported type %v; use an integer, float, pointer, enum, or flags type", i+1, output.Type)
		}
		if output.Constraint == "" || output.Constraint[0] != '=' {
			v.errorf(n, "@asm output %d constraint must begin with '='", i+1)
		} else if strings.ContainsAny(output.Constraint, ",*") {
			v.errorf(n, "@asm output %d constraint cannot contain ',' or use an indirect '*' operand", i+1)
		}
	}
	for i := range n.Inputs {
		input := &n.Inputs[i]
		v.validateExpr(input.Value)
		inputType := input.Value.GetType()
		if !validInlineAsmType(inputType) {
			v.errorf(input.Value, "@asm input %d has unsupported type %v; use an integer, float, pointer, enum, or flags value", i+1, inputType)
		}
		if types.HasUntyped(inputType) {
			v.errorf(input.Value, "@asm input %d has an inferred numeric type; add an explicit cast", i+1)
		}
		if input.Constraint == "" {
			v.errorf(n, "@asm input %d constraint cannot be empty", i+1)
		} else if input.Constraint[0] == '=' || input.Constraint[0] == '~' || strings.Contains(input.Constraint, ",") {
			v.errorf(n, "invalid @asm input %d constraint %q", i+1, input.Constraint)
		}
	}
	for _, clobber := range n.Clobbers {
		if clobber == "" || strings.ContainsAny(clobber, "{},") {
			v.errorf(n, "invalid @asm clobber name %q", clobber)
			continue
		}
		if _, duplicate := seenClobbers[clobber]; duplicate {
			v.errorf(n, "duplicate @asm clobber %q", clobber)
		}
		seenClobbers[clobber] = struct{}{}
	}
}

func validInlineAsmType(t types.Type) bool {
	if t == nil || types.HasUntyped(t) || types.HasTypeParameter(t) {
		return false
	}
	underlying := types.Underlying(t)
	switch underlying.(type) {
	case types.PrimitiveType, types.PointerType, types.EnumType, types.FlagsType:
		return !underlying.Equals(types.PrimitiveVoid)
	default:
		return false
	}
}

func (v *Validator) validateStaticTraitAssertion(node *parser.CastNode) {
	view := node.StaticTraitView
	source := node.Operand.GetType()
	pointer, _ := types.Underlying(source).(types.PointerType)
	_, _, sourceIsPointer, _ := methodOwnerIdentity(source)

	var concrete types.Type
	var probe types.PointerType
	if view.Access == types.TraitReceiverValue {
		if sourceIsPointer {
			concrete = pointer.Base
			probe = pointer
		} else {
			concrete = source
			probe = types.PointerType{Base: source}
		}
	} else if sourceIsPointer {
		if view.Access == types.TraitReceiverMutablePointer && !pointer.Mutable {
			v.errorf(node, "cannot reinterpret immutable %v as %v", source, view)
			return
		}
		concrete = pointer.Base
		probe = pointer
	} else {
		mutable := view.Access == types.TraitReceiverMutablePointer
		op := parser.UnaryOpReference
		if mutable {
			op = parser.UnaryOpMutableReference
		}
		reference := &parser.UnaryOpNode{Op: op, Operand: node.Operand, Loc: node.Operand.GetLoc(), Type: types.PointerType{Base: source, Mutable: mutable}}
		if !v.validateReferenceTarget(reference, node.Operand, mutable) {
			return
		}
		node.Operand = reference
		concrete = source
		probe = types.PointerType{Base: source, Mutable: mutable}
	}

	node.ConcreteType = concrete
	target := types.TraitPointerType{
		Trait:   view.Trait,
		Mutable: view.Access == types.TraitReceiverMutablePointer,
	}
	methods, conforms := v.analyser.structuralConformance(probe, target, node)
	node.AssertionMatches = conforms
	node.TraitMethods = methods
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
	if identifier := comptimeIdentifier(target); identifier != nil && identifier.Symbol != nil && identifier.Symbol.InlineComptime {
		v.errorf(node, "cannot take reference of untyped compile-time value %q", identifier.Name)
		return false
	}

	if mutable {
		return v.validateMutablePlace(target, true)
	}
	switch target := target.(type) {
	case *parser.IdentifierNode:
		if target.Symbol == nil || target.Symbol.Kind != symbols.SymbolKindVariable {
			v.errorf(node, "cannot take reference of this expression")
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
		return true

	case *parser.FieldAccessNode:
		if target.TaggedUnionType != nil {
			v.errorf(node, "cannot take reference of this expression")
			return false
		}
		if target.ResolvedIdentifier != nil {
			return v.validateReferenceTarget(node, target.ResolvedIdentifier, mutable)
		}
		return v.validateReferenceTarget(node, target.Subject, mutable)

	case *parser.IndexExprNode:
		switch subjectType := types.Underlying(target.Subject.GetType()).(type) {
		case types.SliceType:
			return v.validateReferenceTarget(node, target.Subject, mutable)
		case types.ArrayType:
			return v.validateReferenceTarget(node, target.Subject, mutable)
		case types.PointerType:
			if subjectType.Base.Equals(types.PrimitiveVoid) {
				v.errorf(node, "cannot take reference of index into void pointer")
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

func (v *Validator) validateValueExpr(node parser.ExpressionNode) bool {
	v.validateExpr(node)
	return v.requireExpressionValue(node)
}

func hasVoidValue(t types.Type) bool {
	switch t := types.Underlying(t).(type) {
	case types.PrimitiveType:
		return t == types.PrimitiveVoid
	case types.SliceType:
		return hasVoidValue(t.Base)
	case types.ArrayType:
		return hasVoidValue(t.Base)
	case types.StructType:
		for _, field := range t.Fields {
			if hasVoidValue(field.R) {
				return true
			}
		}
	case types.UnionType:
		for _, field := range t.Fields {
			if hasVoidValue(field.R) {
				return true
			}
		}
	}
	return false
}

func (v *Validator) requireExpressionValue(node parser.ExpressionNode) bool {
	if node == nil || node.GetType() == nil {
		return false
	}
	if hasVoidValue(node.GetType()) {
		v.errorf(node, "void expression cannot be used as a value")
		return false
	}
	return true
}

func (v *Validator) validateExprWithExpected(node parser.ExpressionNode, expected types.Type) (result parser.ExpressionNode) {
	defer func() {
		if result != nil {
			v.requireExpressionValue(result)
		}
	}()

	if identifier := comptimeIdentifier(node); identifier != nil && identifier.Symbol != nil &&
		identifier.Symbol.InlineComptime && types.IsUntyped(node.GetType()) && expected != nil &&
		!types.IsUntyped(expected) && node.GetType().CanCoerceTo(expected) {
		if _, erroneous := expected.(types.ErrorType); !erroneous {
			setComptimeReferenceType(node, expected)
			return node
		}
	}

	if call, ok := node.(*parser.FunctionCallNode); ok {
		if !v.completeGenericCall(call, expected) {
			return call
		}
		if literal, shorthand := call.Callee.(*parser.EnumLiteralNode); shorthand {
			if info, tagged := types.TaggedUnion(expected); tagged {
				variant, index, exists := info.Variant(literal.Variant)
				if !exists {
					v.errorf(literal, "%s has no variant %q", expected, literal.Variant)
					call.TaggedUnionType = types.ErrorType{}
					return call
				}
				literal.Value = variant.TagValue
				literal.SetType(info.Tag)
				call.TaggedUnionType = expected
				call.TaggedUnionVariant = index
				v.validateExpr(call)
				return call
			}
		}
	}

	switch n := node.(type) {
	case *parser.StringLiteralNode:
		cstr := v.analyser.universe.Symbols["cstr"].TypeInfo
		if expected != nil && expected.Equals(cstr) {
			return &parser.CStringLiteralNode{
				Value: n.Value,
				Loc:   n.Loc,
				Type:  cstr,
			}
		}
	case *parser.NilLiteralNode:
		if types.IsPointer(expected) || isTraitPointerType(expected) {
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
			if info, tagged := types.TaggedUnion(expected); tagged {
				variant, index, variantExists := info.Variant(n.Variant)
				if !variantExists {
					v.errorf(n, "%s has no variant %q", expected, n.Variant)
					n.SetType(types.ErrorType{})
					return n
				}
				if len(variant.Fields) != 0 {
					v.errorf(n, "tagged union variant %q requires %d payload arguments", variant.Name, len(variant.Fields))
					n.SetType(types.ErrorType{})
					return n
				}
				n.Value = variant.TagValue
				n.SetType(info.Tag)
				constructor := &parser.FunctionCallNode{
					Callee: n, Loc: n.Loc, TaggedUnionType: expected, TaggedUnionVariant: index,
				}
				v.validateExpr(constructor)
				return constructor
			}
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
	case *parser.BlockNode:
		v.validateExpressionBlock(n, expected)
		return n
	case *parser.IfNode:
		v.validateIfExpression(n, expected)
		return n
	case *parser.MatchNode:
		v.validateMatch(n, expected)
		return n
	}

	if n, ok := node.(*parser.IdentifierNode); ok &&
		types.IsUntyped(n.GetType()) && !types.IsUntyped(expected) && n.GetType().CanCoerceTo(expected) {
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
	if sourceArray, ok := types.Underlying(got).(types.ArrayType); ok {
		if targetSlice, ok := types.Underlying(expected).(types.SliceType); ok && sourceArray.Base.Equals(targetSlice.Base) {
			reference := &parser.UnaryOpNode{Operand: node, Loc: node.GetLoc()}
			if v.validateReferenceTarget(reference, node, targetSlice.Mutable) {
				return &parser.CastNode{Operand: node, Loc: node.GetLoc(), Type: expected}
			}
			return node
		}
	}
	if cast, ok := v.traitConversion(node, expected); ok {
		return cast
	}
	if target, ok := traitPointer(expected); ok {
		if types.HasUntyped(got) {
			v.errorf(node, "cannot infer concrete type for trait conversion; add a type annotation or cast")
			return node
		}
		if parameter, symbolic := genericTypeParameterBase(got); symbolic {
			constraint, constrained := types.Underlying(parameter.Constraint).(types.TraitType)
			if constrained && traitImplementsTrait(constraint, target.Trait) {
				if pointer, isPointer := types.Underlying(got).(types.PointerType); isPointer {
					if target.Mutable && !pointer.Mutable {
						v.errorf(node, "cannot use immutable %v as mutable %v", got, expected)
						return node
					}
					return &parser.CastNode{
						Operand: node, Loc: node.GetLoc(), Type: expected,
						TraitConversion: true, ConcreteType: pointer.Base,
					}
				}
				pointer := types.PointerType{Base: got, Mutable: target.Mutable}
				op := parser.UnaryOpReference
				if target.Mutable {
					op = parser.UnaryOpMutableReference
				}
				reference := &parser.UnaryOpNode{Op: op, Operand: node, Loc: node.GetLoc(), Type: pointer}
				if !target.Mutable || v.validateReferenceTarget(reference, node, true) {
					return &parser.CastNode{
						Operand: reference, Loc: node.GetLoc(), Type: expected,
						TraitConversion: true, ConcreteType: got,
					}
				}
				return node
			}
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

func (v *Validator) completeGenericCall(call *parser.FunctionCallNode, expected types.Type) bool {
	if call.Symbol == nil || call.Symbol.TemplateSymbol == nil {
		return true
	}
	template := call.Symbol.TemplateSymbol
	unresolved := false
	for i, argument := range call.Symbol.TypeArguments {
		if i < len(template.GenericParameters) && argument.Equals(template.GenericParameters[i]) {
			unresolved = true
			break
		}
	}
	if !unresolved {
		return true
	}

	var arguments []types.Type
	var err error
	if expected == nil {
		arguments, err = inferGenericArguments(
			template.GenericParameters, template.Signature.Parameters, expressionTypes(call.Args),
			template.Signature.TypedVariadic, call.VariadicExpansion,
		)
	} else {
		arguments, err = inferGenericArgumentsWithResult(
			template.GenericParameters, template.Signature.Parameters, expressionTypes(call.Args),
			template.Signature.ReturnType, expected,
			template.Signature.TypedVariadic, call.VariadicExpansion,
		)
	}
	if err != nil {
		v.errorf(call, "%v", err)
		call.SetType(types.ErrorType{})
		return false
	}
	for i, argument := range arguments {
		if i < len(template.GenericParameters) && argument.Equals(template.GenericParameters[i]) {
			v.errorf(call, "cannot infer type argument %s", template.GenericParameters[i].Name)
			call.SetType(types.ErrorType{})
			return false
		}
	}

	beforeErrors := len(v.analyser.errors)
	if hasTypeParameters(arguments) {
		if v.analyser.checkGenericArguments(call, template.GenericParameters, arguments) {
			call.Symbol = dependentGenericFunctionSymbol(template, arguments)
		}
	} else if specialization := v.analyser.specializeGenericFunction(template, arguments, call); specialization != nil {
		call.Symbol = specialization.Symbol
	}
	if len(v.analyser.errors) > beforeErrors {
		v.errors = append(v.errors, v.analyser.errors[beforeErrors:]...)
		v.analyser.errors = v.analyser.errors[:beforeErrors]
		call.SetType(types.ErrorType{})
		return false
	}
	if call.Symbol == nil {
		call.SetType(types.ErrorType{})
		return false
	}
	if call.Name != nil {
		call.Name.Symbol = call.Symbol
	}
	if call.Symbol.Signature.ReturnType != nil {
		call.SetType(call.Symbol.Signature.ReturnType)
	} else {
		call.SetType(types.PrimitiveVoid)
	}
	return true
}

func comptimeIdentifier(node parser.ExpressionNode) *parser.IdentifierNode {
	switch n := node.(type) {
	case *parser.IdentifierNode:
		return n
	case *parser.FieldAccessNode:
		return n.ResolvedIdentifier
	default:
		return nil
	}
}

func setComptimeReferenceType(node parser.ExpressionNode, t types.Type) {
	node.SetType(t)
	if field, ok := node.(*parser.FieldAccessNode); ok {
		field.ResolvedIdentifier.SetType(t)
	}
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
	underlying := types.Underlying(expected)
	var elementType types.Type
	expectedLength := -1
	switch target := underlying.(type) {
	case types.SliceType:
		elementType = target.Base
	case types.ArrayType:
		elementType = target.Base
		expectedLength = target.Length
	default:
		v.validateExpr(n)
		return
	}
	if n.RepeatValue != nil {
		size := -1
		if amount, ok := staticIntegerValue(n.RepeatAmount); ok && amount.IsInt64() {
			size = int(amount.Int64())
		}
		if noInit, ok := n.RepeatValue.(*parser.NoInitializerNode); ok {
			if expectedLength < 0 {
				v.errorf(n, "uninitialized repeated literal requires an array type")
			}
			noInit.SetType(elementType)
		} else {
			n.RepeatValue = v.validateExprWithExpected(n.RepeatValue, elementType)
		}
		n.RepeatAmount = v.validateExprWithExpected(n.RepeatAmount, types.PrimitiveUsz)
		if expectedLength >= 0 {
			if size < 0 {
				v.errorf(n, "array repetition count must be a compile-time integer")
			} else if size != expectedLength {
				v.errorf(n, "cannot assign repeated literal of length %d to %v", size, expected)
			}
		}
		n.SetType(expected)
		return
	}

	if expectedLength >= 0 && len(n.Elements) != expectedLength {
		v.errorf(n, "sequence literal has length %d, expected %v", len(n.Elements), expected)
	}

	for i, element := range n.Elements {
		n.Elements[i] = v.validateExprWithExpected(element, elementType)
	}

	n.SetType(expected)
}

func staticIntegerValue(expr parser.ExpressionNode) (*big.Int, bool) {
	switch n := expr.(type) {
	case *parser.IntegerLiteralNode:
		base := 10
		if strings.HasPrefix(n.Value, "0x") || strings.HasPrefix(n.Value, "0X") ||
			strings.HasPrefix(n.Value, "0b") || strings.HasPrefix(n.Value, "0B") ||
			strings.HasPrefix(n.Value, "0o") || strings.HasPrefix(n.Value, "0O") {
			base = 0
		}
		value, ok := new(big.Int).SetString(n.Value, base)
		return value, ok
	case *parser.UnaryOpNode:
		if n.Op != parser.UnaryOpNegate {
			return nil, false
		}
		value, ok := staticIntegerValue(n.Operand)
		if !ok {
			return nil, false
		}
		return new(big.Int).Neg(value), true
	case *parser.CastNode:
		return staticIntegerValue(n.Operand)
	case *parser.BinaryOpNode:
		left, leftOK := staticIntegerValue(n.Operand1)
		right, rightOK := staticIntegerValue(n.Operand2)
		if !leftOK || !rightOK {
			return nil, false
		}
		result := new(big.Int)
		switch n.Op {
		case parser.BinaryOpAdd:
			return result.Add(left, right), true
		case parser.BinaryOpSubtract:
			return result.Sub(left, right), true
		case parser.BinaryOpMultiply:
			return result.Mul(left, right), true
		case parser.BinaryOpDivide:
			if right.Sign() == 0 {
				return nil, false
			}
			return result.Quo(left, right), true
		case parser.BinaryOpModulo:
			if right.Sign() == 0 {
				return nil, false
			}
			return result.Rem(left, right), true
		case parser.BinaryOpBitwiseAnd:
			return result.And(left, right), true
		case parser.BinaryOpBitwiseOr:
			return result.Or(left, right), true
		case parser.BinaryOpBitwiseXor:
			return result.Xor(left, right), true
		case parser.BinaryOpShiftLeft, parser.BinaryOpShiftRight:
			if !right.IsUint64() {
				return nil, false
			}
			if n.Op == parser.BinaryOpShiftLeft {
				return result.Lsh(left, uint(right.Uint64())), true
			}
			return result.Rsh(left, uint(right.Uint64())), true
		}
		return nil, false
	default:
		return nil, false
	}
}

func (v *Validator) validateStructLiteralWithExpected(n *parser.StructLiteralNode, expected types.Type) {
	if flagType, ok := types.Underlying(expected).(types.FlagsType); ok {
		if n.NoInitRemaining {
			v.errorf(n, "flags literals cannot use '---'")
			n.SetType(types.ErrorType{})
			return
		}
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
		if n.NoInitRemaining && len(n.Fields) == 0 {
			n.SetType(expected)
			return
		}
		if len(n.Fields) != 1 {
			v.errorf(n, "union literal must initialize exactly one field")
			n.SetType(types.ErrorType{})
			return
		}
		field := n.Fields[0]
		for _, unionField := range unionType.Fields {
			if unionField.L == field.L {
				if noInit, ok := field.R.(*parser.NoInitializerNode); ok {
					noInit.SetType(unionField.R)
				} else {
					n.Fields[0].R = v.validateExprWithExpected(field.R, unionField.R)
				}
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
	if structType.TaggedUnion != nil {
		v.errorf(n, "tagged union values must be constructed with %v.Variant(...) syntax", expected)
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

		if noInit, ok := field.R.(*parser.NoInitializerNode); ok {
			noInit.SetType(expectedFieldType)
		} else {
			n.Fields[i].R = v.validateExprWithExpected(field.R, expectedFieldType)
		}
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
	if len(missingFields) > 0 && !n.NoInitRemaining {
		v.errorf(n, "missing fields in struct literal: %v", strings.Join(missingFields, ", "))
	}

	n.SetType(expected)
}
