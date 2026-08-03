package sema

import (
	"fmt"
	"strconv"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/tokeniser"
	"github.com/marzeq/qk/types"
)

type Attributor struct {
	analyser      *Analyser
	errors        []error
	templatesOnly bool
	functionState map[*symbols.Symbol]functionAttributionState
}

func (a *Analyser) NewAttributor() *Attributor {
	return &Attributor{
		analyser:      a,
		functionState: make(map[*symbols.Symbol]functionAttributionState),
	}
}

type functionDefinitionInfo struct {
	node   *parser.FunctionDefNode
	module string
}

type functionAttributionState uint8

const (
	functionUnattributed functionAttributionState = iota
	functionAttributing
	functionAttributed
)

func (a *Attributor) errorf(node parser.Node, format string, args ...any) {
	a.errors = append(a.errors, shared.NewError(node.GetLoc(), format, args...))
}

func (a *Attributor) AttributeModule(root *parser.RootNode) {
	a.templatesOnly = false
	a.selectModule(root)
	a.attributeNode(root)
}

func (a *Attributor) AttributeGenericTemplates(root *parser.RootNode) {
	a.templatesOnly = true
	a.selectModule(root)
	for _, node := range root.Body {
		function, ok := node.(*parser.FunctionDefNode)
		if !ok || !function.IsGeneric() {
			continue
		}
		bindings := make(map[string]types.Type, len(function.Symbol.GenericParameters))
		for _, parameter := range function.Symbol.GenericParameters {
			bindings[parameter.Name] = parameter
		}
		a.analyser.withDefinitionContext(
			a.analyser.currentMod,
			a.analyser.currentTrustedStandardLibrary,
			bindings,
			func() { a.attributeNode(function) },
		)
	}
	a.templatesOnly = false
}

func (a *Attributor) selectModule(root *parser.RootNode) {
	path := a.analyser.modulePaths[root]
	a.analyser.currentMod = path
	if mod := a.analyser.modules[path]; mod != nil {
		a.analyser.current = mod.Scope
		a.analyser.currentTrustedStandardLibrary = mod.TrustedStandardLibrary
	}
	a.analyser.currentImports = a.analyser.importsByModule[path]
	a.analyser.aliases = a.analyser.aliasesByModule[path]
}

func (a *Attributor) Errors() []error {
	return a.errors
}

func (a *Attributor) attributeNode(node parser.Node) {
	switch n := node.(type) {
	case *parser.RootNode:
		for _, stmt := range n.Body {
			if function, ok := stmt.(*parser.FunctionDefNode); ok && function.IsGeneric() {
				if !a.templatesOnly {
					continue
				}
			} else if a.templatesOnly {
				continue
			}
			a.attributeNode(stmt)
		}

	case *parser.ImportNode, *parser.ModuleNode:
		// pass

	case *parser.TypeAliasNode:
		// pass

	case *parser.FunctionDefNode:
		if n.GenericInstance {
			break
		}
		if a.functionState[n.Symbol] != functionUnattributed {
			break
		}
		a.functionState[n.Symbol] = functionAttributing
		defer func() { a.functionState[n.Symbol] = functionAttributed }()
		restoreSpecialization := a.enterSpecialization(n.Symbol)
		defer restoreSpecialization()
		for _, arg := range n.Args {
			if arg.Default != nil {
				a.attributeExpr(arg.Default)
			}
		}
		if n.Body != nil {
			if n.ExpressionBody {
				a.attributeExpr(n.Body.(parser.ExpressionNode))
			} else {
				a.attributeNode(n.Body)
			}
		}

		foreignAttr := n.Attributes.Get(attributes.AttributeTypeForeign)

		if foreignAttr != nil && n.RetTypeNode == nil {
			a.errorf(n, "foreign function must have a return type annotation")
		}

		if n.Symbol.Signature.ReturnType == nil {
			var candidates []returnTypeCandidate
			for _, ret := range collectFunctionReturnNodesFromNode(n.Body) {
				if ret.ReturnValue == nil {
					candidates = append(candidates, returnTypeCandidate{node: ret, ty: types.PrimitiveVoid})
				} else {
					candidates = append(candidates, returnTypeCandidate{node: ret, ty: ret.ReturnValue.GetType()})
				}
			}
			if n.ExpressionBody && parser.NodeFallsThrough(n.Body) {
				body := n.Body.(parser.ExpressionNode)
				candidates = append(candidates, returnTypeCandidate{node: n, ty: body.GetType()})
			}
			n.Symbol.Signature.ReturnType = a.mergeReturnTypes(candidates)

			if types.HasUntyped(n.Symbol.Signature.ReturnType) {
				a.errorf(n, "cannot infer function return type from untyped numeric value; add a return type annotation or cast")
				n.Symbol.Signature.ReturnType = types.ErrorType{}
			}
		}

	case *parser.BlockNode:
		a.attributeBlock(n)

	case *parser.DeclarationNode:
		if len(n.GenericParameters) != 0 {
			break
		}
		restoreSpecialization := a.enterSpecialization(n.Symbol)
		defer restoreSpecialization()
		if n.Value != nil {
			a.attributeExpr(n.Value)
		}
		if n.Name == "_" {
			break
		}

		if n.Value != nil && n.Symbol.Type == nil {
			n.Symbol.Type = n.Value.GetType()
		}
		if n.Value != nil && n.Symbol.GenericOrigin == nil {
			n.Symbol.GenericOrigin = genericExpressionOrigin(n.Value)
		}
		if n.Value != nil && n.Symbol.StaticTraitView == nil {
			n.Symbol.StaticTraitView = staticTraitView(n.Value)
		}
	case *parser.MultiDeclarationNode:
		if cast, ok := n.Value.(*parser.CastNode); ok {
			cast.Checked = true
		}
		a.attributeExpr(n.Value)
		if result, ok := n.Value.GetType().(types.MultipleReturnType); ok {
			for i, sym := range n.Symbols {
				if sym != nil && i < len(result.Types) {
					sym.Type = result.Types[i]
				}
			}
		}
		if len(n.Symbols) != 0 && n.Symbols[0] != nil {
			n.Symbols[0].StaticTraitView = staticTraitView(n.Value)
		}

	case *parser.AssignmentNode:
		if len(n.Assignees) > 0 {
			if cast, ok := n.Value.(*parser.CastNode); ok {
				cast.Checked = true
			}
		}
		if len(n.Assignees) > 0 {
			for _, target := range n.Assignees {
				if id, ok := target.(*parser.IdentifierNode); !ok || id.Name != "_" {
					a.attributeExpr(target)
				}
			}
		} else {
			if id, ok := n.Assignee.(*parser.IdentifierNode); !ok || id.Name != "_" {
				a.attributeExpr(n.Assignee)
			}
		}
		a.attributeExpr(n.Value)

	case *parser.IfNode:
		a.attributeIf(n)

	case *parser.MatchNode:
		a.attributeMatch(n)

	case *parser.ForNode:
		for _, node := range n.ExprsOrStmts {
			a.attributeNode(node)
		}
		a.attributeNode(n.Body)

	case *parser.RangeForNode:
		a.attributeExpr(n.Start)
		a.attributeExpr(n.End)
		if n.Symbol != nil {
			n.Symbol.Type = types.PrimitiveUsz
		}
		a.attributeNode(n.Body)

	case *parser.ForEachNode:
		a.attributeExpr(n.Iterable)
		if n.Symbol != nil {
			switch iterable := types.Underlying(n.Iterable.GetType()).(type) {
			case types.SliceType:
				n.Symbol.Type = iterable.Base
			case types.ArrayType:
				n.Symbol.Type = iterable.Base
			}
		}
		a.attributeNode(n.Body)

	case *parser.ControlKeywordNode:
		for _, value := range n.ReturnValues {
			a.attributeExpr(value)
		}

	case *parser.DeferNode:
		a.attributeNode(n.Action)

	case parser.ExpressionNode:
		a.attributeExpr(n)

	default:
		panic(fmt.Sprintf("unexpected node type: %T\n", node))
	}
}

func (a *Attributor) attributeExpr(node parser.ExpressionNode) {
	switch n := node.(type) {
	case *parser.IdentifierNode:
		if n.Symbol != nil && n.Symbol.TemplateSymbol != nil && !a.templatesOnly && !hasTypeParameters(n.Symbol.TypeArguments) {
			if specialization := a.attributeGenericSpecialization(n.Symbol.TemplateSymbol, n.Symbol.TypeArguments, n); specialization != nil {
				n.Symbol = specialization.Symbol
			}
		}
		if n.Symbol != nil {
			a.attributeFunctionDefinition(n.Symbol)
		}
		if n.Symbol != nil && n.Symbol.Template {
			if len(n.ResolvedTypeArgs) == 0 {
				a.errorf(n, "generic binding %q requires type arguments", n.Symbol.Name)
				n.SetType(types.ErrorType{})
			} else if a.templatesOnly || hasTypeParameters(n.ResolvedTypeArgs) {
				if a.analyser.checkGenericArguments(n, n.Symbol.GenericParameters, n.ResolvedTypeArgs) {
					n.Symbol = dependentGenericFunctionSymbol(n.Symbol, n.ResolvedTypeArgs)
				} else {
					n.SetType(types.ErrorType{})
				}
			} else if specialization := a.attributeGenericSpecialization(n.Symbol, n.ResolvedTypeArgs, n); specialization != nil {
				n.Symbol = specialization.Symbol
			}
		}
		if n.Symbol != nil && n.Symbol.Template {
			n.SetType(types.ErrorType{})
		} else if n.Symbol != nil && n.Symbol.Kind == symbols.SymbolKindFunction && n.Symbol.Signature != nil {
			a.attributeFunctionDefinition(n.Symbol)
			ret := n.Symbol.Signature.ReturnType
			if ret == nil {
				ret = types.PrimitiveVoid
			}
			n.SetType(types.PointerType{Base: types.FunctionType{Parameters: n.Symbol.Signature.Parameters, ReturnType: ret, TypedVariadic: n.Symbol.Signature.TypedVariadic, VariadicElement: n.Symbol.Signature.VariadicElement}})
		} else if n.Symbol == nil || n.Symbol.Type == nil {
			a.errorf(n, "undefined identifier: %s", n.String())
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
		n.SetType(a.analyser.universe.Symbols["str"].TypeInfo)

	case *parser.CStringLiteralNode:
		n.SetType(a.analyser.universe.Symbols["cstr"].TypeInfo)

	case *parser.CharLiteralNode:
		n.SetType(types.PrimitiveU8)

	case *parser.NilLiteralNode:
		n.SetType(types.PointerType{
			Base:    types.PrimitiveVoid,
			Mutable: false,
		})

	case *parser.NoInitializerNode:
		n.SetType(types.NoInitializerType{})

	case *parser.EnumLiteralNode:
		// Leading-dot enum literals are resolved later from an expected type.
		n.SetType(types.UnresolvedEnum{})

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
		if n.RepeatValue != nil {
			a.attributeExpr(n.RepeatValue)
			a.attributeExpr(n.RepeatAmount)
			size := -1
			if amount, ok := n.RepeatAmount.(*parser.IntegerLiteralNode); ok {
				if parsed, err := strconv.Atoi(amount.Value); err == nil {
					size = parsed
				}
			}
			n.SetType(types.SequenceType{Base: n.RepeatValue.GetType(), Length: size})
			break
		}
		typs := []types.Type{}
		for _, elem := range n.Elements {
			a.attributeExpr(elem)
			typs = append(typs, elem.GetType())
		}
		if len(typs) > 0 {
			currentType := typs[0]
			for _, t := range typs[1:] {
				got := types.CommonType(currentType, t)
				if got.Equals(types.ErrorType{}) {
					a.errorf(n, "inconsistent slice element types: expected %v, got %v", currentType, t)
				}
				currentType = got
			}
			n.SetType(types.SequenceType{Base: currentType, Length: len(n.Elements)})
		} else {
			n.SetType(types.SequenceType{Base: types.PrimitiveVoid, Length: 0})
		}

	case *parser.FunctionCallNode:
		if n.TaggedUnionType != nil {
			for _, arg := range n.Args {
				a.attributeExpr(arg)
			}
			if n.TaggedUnionTemplate != nil {
				info, tagged := types.TaggedUnion(n.TaggedUnionType)
				if !tagged || n.TaggedUnionVariant < 0 || n.TaggedUnionVariant >= len(info.Variants) {
					a.errorf(n, "invalid tagged union constructor")
					n.TaggedUnionType = types.ErrorType{}
					break
				}
				variant := info.Variants[n.TaggedUnionVariant]
				if len(n.Args) != len(variant.Fields) {
					break
				}
				patterns := make([]types.Type, len(variant.Fields))
				for i := range variant.Fields {
					patterns[i] = variant.Fields[i].R
				}
				generic := a.analyser.genericAliases[n.TaggedUnionTemplate]
				arguments, err := inferGenericArguments(
					generic.parameters, patterns, expressionTypes(n.Args), false, false,
				)
				if err != nil {
					a.errorf(n, "%v", err)
					n.TaggedUnionType = types.ErrorType{}
					break
				}
				specialization := a.analyser.specializeGenericAlias(generic, arguments, n, false)
				if specialization == nil {
					n.TaggedUnionType = types.ErrorType{}
					break
				}
				n.TaggedUnionType = specialization.TypeInfo
				n.TaggedUnionTemplate = nil
			}
			n.SetType(n.TaggedUnionType)
			break
		}
		methodHandled := false
		if n.Symbol == nil {
			methodHandled = a.attributeMethodCall(n)
		}
		if n.Symbol == nil && !methodHandled {
			a.attributeExpr(n.Callee)
			if member, ok := n.Callee.(*parser.FieldAccessNode); ok && member.MethodSymbol != nil && member.MethodSymbol.StaticMethod {
				n.Symbol = member.MethodSymbol
				n.Name = &parser.IdentifierNode{
					Name:   member.MethodSymbol.Name,
					Symbol: member.MethodSymbol,
					Loc:    member.Loc,
				}
				if member.MethodModule != a.analyser.currentMod {
					n.Name.Module = member.MethodModule
					n.Name.ResolvedModuleName = member.MethodModule
				}
			}
		}
		if n.Symbol != nil {
			a.attributeFunctionDefinition(n.Symbol)
		}
		if n.Symbol != nil && n.Symbol.Signature.ReturnType != nil {
			n.SetType(n.Symbol.Signature.ReturnType)
		} else if n.Symbol != nil {
			n.SetType(types.PrimitiveVoid)
		}
		for _, arg := range n.Args {
			a.attributeExpr(arg)
		}

		if n.Symbol != nil && n.Symbol.Template {
			template := n.Symbol
			arguments := []types.Type(nil)
			var err error
			if n.Name != nil && len(n.Name.ResolvedTypeArgs) != 0 {
				arguments = n.Name.ResolvedTypeArgs
			} else {
				arguments, err = inferGenericArgumentsPartial(
					template.GenericParameters, template.Signature.Parameters, expressionTypes(n.Args),
					template.Signature.TypedVariadic, n.VariadicExpansion,
				)
			}
			if err != nil {
				a.errorf(n, "%v", err)
				n.SetType(types.ErrorType{})
			} else if a.templatesOnly || hasTypeParameters(arguments) {
				if a.analyser.checkGenericArguments(n, template.GenericParameters, arguments) {
					n.Symbol = dependentGenericFunctionSymbol(template, arguments)
					if n.Name != nil {
						n.Name.Symbol = n.Symbol
					}
				} else {
					n.SetType(types.ErrorType{})
				}
			} else if specialization := a.attributeGenericSpecialization(template, arguments, n); specialization != nil {
				n.Symbol = specialization.Symbol
				if n.Name != nil {
					n.Name.Symbol = specialization.Symbol
				}
			}
		} else if n.Symbol != nil && n.Symbol.TemplateSymbol != nil && !a.templatesOnly && !hasTypeParameters(n.Symbol.TypeArguments) {
			template := n.Symbol.TemplateSymbol
			if specialization := a.attributeGenericSpecialization(template, n.Symbol.TypeArguments, n); specialization != nil {
				n.Symbol = specialization.Symbol
				if n.Name != nil {
					n.Name.Symbol = specialization.Symbol
				}
			}
		}
		if n.Symbol != nil {
			a.attributeFunctionDefinition(n.Symbol)
			if n.Symbol.Signature.ReturnType != nil {
				n.SetType(n.Symbol.Signature.ReturnType)
			} else {
				n.SetType(types.PrimitiveVoid)
			}
		}

	case *parser.BlockNode:
		a.attributeBlock(n)

	case *parser.IfNode:
		a.attributeIf(n)

	case *parser.MatchNode:
		a.attributeMatch(n)

	case *parser.UnaryOpNode:
		a.attributeExpr(n.Operand)
		switch n.Op {
		case parser.UnaryOpNegate:
			if types.IsSigned(n.Operand.GetType()) || types.IsFloat(n.Operand.GetType()) {
				n.SetType(n.Operand.GetType())
			} else {
				a.errorf(n, "cannot apply negation operator to non-numeric type: %v", n.Operand.GetType())
				n.SetType(types.ErrorType{})
			}
		case parser.UnaryOpLogicalNot:
			if n.Operand.GetType().Equals(types.PrimitiveBool) {
				n.SetType(types.PrimitiveBool)
			} else {
				a.errorf(n, "cannot apply logical not operator to non-boolean type: %v", n.Operand.GetType())
				n.SetType(types.ErrorType{})
			}
		case parser.UnaryOpReference:
			n.SetType(types.PointerType{
				Base:    n.Operand.GetType(),
				Mutable: false,
			})
		case parser.UnaryOpMutableReference:
			n.SetType(types.PointerType{
				Base:    n.Operand.GetType(),
				Mutable: true,
			})
		case parser.UnaryOpDereference:
			if ptr, ok := types.Underlying(n.Operand.GetType()).(types.PointerType); ok {
				n.SetType(ptr.Base)
			} else {
				a.errorf(n, "cannot dereference non-pointer type: %v", n.Operand.GetType())
				n.SetType(types.ErrorType{})
			}
		case parser.UnaryOpSliceLen:
			switch types.Underlying(n.Operand.GetType()).(type) {
			case types.SliceType, types.ArrayType:
				n.SetType(types.PrimitiveUsz)
			default:
				a.errorf(n, "cannot get length of non-array-or-slice type: %v", n.Operand.GetType())
				n.SetType(types.ErrorType{})
			}
		case parser.UnaryOpBitwiseNot:
			if types.IsInteger(n.Operand.GetType()) {
				n.SetType(n.Operand.GetType())
			} else if _, ok := types.Underlying(n.Operand.GetType()).(types.FlagsType); ok {
				n.SetType(n.Operand.GetType())
			} else {
				a.errorf(n, "cannot apply bitwise not operator to non-integer type: %v", n.Operand.GetType())
				n.SetType(types.ErrorType{})
			}
		default:
			panic("unhandled unary operator")
		}

	case *parser.IndexExprNode:
		a.attributeExpr(n.Subject)
		a.attributeExpr(n.Index)

		switch t := types.Underlying(n.Subject.GetType()).(type) {
		case types.SliceType:
			n.SetType(t.Base)
		case types.ArrayType:
			n.SetType(t.Base)
		case types.PointerType:
			n.SetType(t.Base)
		default:
			a.errorf(n, "cannot index type %v", n.Subject.GetType())
			n.SetType(types.ErrorType{})
		}

		switch n.Index.GetType().(type) {
		case types.UntypedInt:
			n.Index = &parser.CastNode{
				Operand: n.Index,
				Type:    types.PrimitiveUsz,
			}
		case types.UntypedFloat:
			a.errorf(n, "cannot use untyped float as index; cast to integer type")
			n.SetType(types.ErrorType{})
		}

	case *parser.SliceExprNode:
		a.attributeExpr(n.Subject)
		if n.Start != nil {
			a.attributeExpr(n.Start)
		}
		if n.End != nil {
			a.attributeExpr(n.End)
		}

		switch t := types.Underlying(n.Subject.GetType()).(type) {
		case types.SliceType:
			result := types.Type(types.SliceType{Base: t.Base, Mutable: t.Mutable})
			if defined, ok := n.Subject.GetType().(types.DefinedType); ok && defined.Module == "" && defined.Name == "str" {
				result = defined
			}
			n.SetType(result)
		case types.PointerType:
			n.SetType(types.SliceType{Base: t.Base, Mutable: t.Mutable})
		case types.ArrayType:
			n.SetType(types.SliceType{Base: t.Base, Mutable: mutableArrayPlace(n.Subject)})
		default:
			a.errorf(n, "cannot slice type %v", n.Subject.GetType())
			n.SetType(types.ErrorType{})
		}

		normalizeBound := func(bound *parser.ExpressionNode) {
			if *bound == nil {
				return
			}
			switch (*bound).GetType().(type) {
			case types.UntypedInt:
				*bound = &parser.CastNode{
					Operand: *bound,
					Type:    types.PrimitiveUsz,
				}
			case types.UntypedFloat:
				a.errorf(n, "cannot use untyped float as slice bound; cast to integer type")
				n.SetType(types.ErrorType{})
			}
		}
		normalizeBound(&n.Start)
		normalizeBound(&n.End)

	case *parser.FieldAccessNode:
		if n.ResolvedIdentifier != nil {
			ident := n.ResolvedIdentifier
			if ident.Symbol.TemplateSymbol != nil && !a.templatesOnly && !hasTypeParameters(ident.Symbol.TypeArguments) {
				if specialization := a.attributeGenericSpecialization(ident.Symbol.TemplateSymbol, ident.Symbol.TypeArguments, ident); specialization != nil {
					ident.Symbol = specialization.Symbol
				}
			}
			a.attributeFunctionDefinition(ident.Symbol)
			if ident.Symbol.Template {
				if len(ident.ResolvedTypeArgs) == 0 {
					a.errorf(n, "generic binding %q requires type arguments", ident.Symbol.Name)
					n.SetType(types.ErrorType{})
					break
				}
				if a.templatesOnly || hasTypeParameters(ident.ResolvedTypeArgs) {
					if !a.analyser.checkGenericArguments(ident, ident.Symbol.GenericParameters, ident.ResolvedTypeArgs) {
						n.SetType(types.ErrorType{})
						break
					}
					ident.Symbol = dependentGenericFunctionSymbol(ident.Symbol, ident.ResolvedTypeArgs)
				} else if specialization := a.attributeGenericSpecialization(ident.Symbol, ident.ResolvedTypeArgs, ident); specialization != nil {
					ident.Symbol = specialization.Symbol
				}
			}
			switch ident.Symbol.Kind {
			case symbols.SymbolKindVariable:
				ident.SetType(ident.Symbol.Type)
			case symbols.SymbolKindFunction:
				a.attributeFunctionDefinition(ident.Symbol)
				ret := ident.Symbol.Signature.ReturnType
				if ret == nil {
					ret = types.PrimitiveVoid
				}
				ident.SetType(types.PointerType{Base: types.FunctionType{Parameters: ident.Symbol.Signature.Parameters, ReturnType: ret, TypedVariadic: ident.Symbol.Signature.TypedVariadic, VariadicElement: ident.Symbol.Signature.VariadicElement}})
			case symbols.SymbolKindType:
				ident.SetType(ident.Symbol.TypeInfo)
			}
			n.SetType(ident.GetType())
			break
		}
		if n.ModulePath != "" {
			break
		}
		if a.attributeMethodValue(n) {
			break
		}
		if ident := resolvedTypeIdentifier(n.Subject); ident != nil {
			if info, tagged := types.TaggedUnion(ident.Symbol.TypeInfo); tagged {
				variant, index, exists := info.Variant(n.Field.Name)
				if !exists {
					a.errorf(n, "tagged union %v has no variant %q", ident.Symbol.TypeInfo, n.Field.Name)
					n.SetType(types.ErrorType{})
				} else if len(variant.Fields) != 0 {
					a.errorf(n, "tagged union variant %q requires %d payload arguments", variant.Name, len(variant.Fields))
					n.SetType(types.ErrorType{})
				} else {
					n.TaggedUnionType = ident.Symbol.TypeInfo
					n.TaggedUnionVariant = index
					n.SetType(ident.Symbol.TypeInfo)
				}
				break
			}
			if enumType, ok := types.Underlying(ident.Symbol.TypeInfo).(types.EnumType); ok {
				ident.SetType(enumType)
				value, exists := enumType.VariantValue(n.Field.Name)
				if !exists {
					a.errorf(n, "enum %s has no variant %q", enumType, n.Field.Name)
					n.SetType(types.ErrorType{})
				} else {
					n.IsEnumValue = true
					n.EnumValue = value
					n.SetType(ident.Symbol.TypeInfo)
				}
				break
			}
			if flagType, ok := types.Underlying(ident.Symbol.TypeInfo).(types.FlagsType); ok {
				ident.SetType(flagType)
				value, exists := flagType.VariantValue(n.Field.Name)
				if !exists {
					a.errorf(n, "flags %s has no member %q", ident.Symbol.TypeInfo, n.Field.Name)
					n.SetType(types.ErrorType{})
				} else {
					n.IsFlagValue, n.FlagValue, n.FlagType = true, value, ident.Symbol.TypeInfo
					n.SetType(ident.Symbol.TypeInfo)
				}
				break
			}
		}
		if ident, ok := n.Subject.(*parser.IdentifierNode); ok &&
			ident.Symbol != nil && ident.Symbol.Kind == symbols.SymbolKindModule {
			a.errorf(n, "cannot resolve module-qualified name %s.%s", ident.Name, n.Field.Name)
			n.SetType(types.ErrorType{})
			break
		}

		a.attributeExpr(n.Subject)
		subjectType := types.Underlying(n.Subject.GetType())
		if ptr, ok := subjectType.(types.PointerType); ok {
			subjectType = types.Underlying(ptr.Base)
		}
		if info, tagged := types.TaggedUnion(subjectType); tagged && info.Auto && n.Field.Name == "tag" {
			a.errorf(n, "the tag of an @auto tagged union is compiler-private")
			n.SetType(types.ErrorType{})
			break
		}
		switch t := subjectType.(type) {
		case types.FlagsType:
			value, exists := t.VariantValue(n.Field.Name)
			if !exists {
				a.errorf(n, "flags %v has no member %q", n.Subject.GetType(), n.Field.Name)
				n.SetType(types.ErrorType{})
			} else if value == "0" {
				a.errorf(n, "zero-valued flag %q cannot be used as a boolean field", n.Field.Name)
				n.SetType(types.ErrorType{})
			} else {
				n.IsFlagTest, n.FlagValue, n.FlagType = true, value, n.Subject.GetType()
				n.SetType(types.PrimitiveBool)
			}
		case types.StructType:
			found := false
			for _, field := range t.Fields {
				if field.L == n.Field.Name {
					n.SetType(field.R)
					found = true
					break
				}
				if field.L == "" {
					if embedded, ok := field.R.(types.UnionType); ok {
						for _, unionField := range embedded.Fields {
							if unionField.L == n.Field.Name {
								n.SetType(unionField.R)
								found = true
								break
							}
						}
					}
				}
				if found {
					break
				}
			}
			if !found {
				a.errorf(n, "type %v does not have a field named %q", fieldOwnerDisplayType(n.Subject.GetType()), n.Field.Name)
				n.SetType(types.ErrorType{})
			}
		case types.UnionType:
			found := false
			for _, field := range t.Fields {
				if field.L == n.Field.Name {
					n.SetType(field.R)
					found = true
					break
				}
			}
			if !found {
				a.errorf(n, "type %v does not have a field named %q", fieldOwnerDisplayType(n.Subject.GetType()), n.Field.Name)
				n.SetType(types.ErrorType{})
			}
		default:
			a.errorf(n, "cannot access field of type %v", n.Subject.GetType())
			n.SetType(types.ErrorType{})
		}

	case *parser.BinaryOpNode:
		a.attributeExpr(n.Operand1)
		a.attributeExpr(n.Operand2)
		if literal, ok := n.Operand1.(*parser.EnumLiteralNode); ok && isUnresolvedEnum(literal.GetType()) {
			if isEnumOrFlags(n.Operand2.GetType()) {
				a.resolveEnumLiteral(literal, n.Operand2.GetType())
			}
		}
		if literal, ok := n.Operand2.(*parser.EnumLiteralNode); ok && isUnresolvedEnum(literal.GetType()) {
			if isEnumOrFlags(n.Operand1.GetType()) {
				a.resolveEnumLiteral(literal, n.Operand1.GetType())
			}
		}
		for _, operand := range []parser.ExpressionNode{n.Operand1, n.Operand2} {
			if literal, ok := operand.(*parser.EnumLiteralNode); ok && isUnresolvedEnum(literal.GetType()) {
				a.errorf(literal, "cannot infer enum type for .%s", literal.Variant)
				literal.SetType(types.ErrorType{})
			}
		}

		t1 := n.Operand1.GetType()
		t2 := n.Operand2.GetType()

		switch n.Op {
		case parser.BinaryOpAdd, parser.BinaryOpSubtract, parser.BinaryOpMultiply, parser.BinaryOpDivide, parser.BinaryOpModulo:
			p1, pointer1 := types.Underlying(t1).(types.PointerType)
			p2, pointer2 := types.Underlying(t2).(types.PointerType)
			if n.Op == parser.BinaryOpAdd && pointer1 && types.IsInteger(t2) {
				n.SetType(t1)
			} else if n.Op == parser.BinaryOpAdd && types.IsInteger(t1) && pointer2 {
				n.SetType(t2)
			} else if n.Op == parser.BinaryOpSubtract && pointer1 && types.IsInteger(t2) {
				n.SetType(t1)
			} else if n.Op == parser.BinaryOpSubtract && pointer1 && pointer2 && p1.Base.Equals(p2.Base) {
				n.SetType(types.PrimitiveIsz)
			} else if types.IsNumeric(t1) && types.IsNumeric(t2) {
				got := types.PromoteNumeric(t1, t2)
				if got.Equals(types.ErrorType{}) {
					a.errorf(n, "incompatible types for binary operator: %v and %v", t1, t2)
					n.SetType(types.ErrorType{})
				} else {
					n.SetType(got)
				}
			} else {
				a.errorf(n, "cannot apply arithmetic operator to non-numeric types: %v and %v", t1, t2)
				n.SetType(types.ErrorType{})
			}
		case parser.BinaryOpEqual, parser.BinaryOpNotEqual, parser.BinaryOpLess, parser.BinaryOpLessEqual, parser.BinaryOpGreater, parser.BinaryOpGreaterEqual:
			if types.IsNumeric(t1) && types.IsNumeric(t2) {
				n.SetType(types.PrimitiveBool)
			} else if (n.Op == parser.BinaryOpEqual || n.Op == parser.BinaryOpNotEqual) &&
				((isNilLiteral(n.Operand1) && isTraitPointerType(t2)) ||
					(isTraitPointerType(t1) && isNilLiteral(n.Operand2))) {
				n.SetType(types.PrimitiveBool)
			} else if t1.Equals(t2) {
				n.SetType(types.PrimitiveBool)
			} else if types.IsPointer(t1) && types.IsPointer(t2) {
				n.SetType(types.PrimitiveBool)
			} else {
				a.errorf(n, "cannot compare values of types %v and %v", t1, t2)
				n.SetType(types.ErrorType{})
			}
		case parser.BinaryOpLogicalAnd, parser.BinaryOpLogicalOr:
			if t1.Equals(types.PrimitiveBool) && t2.Equals(types.PrimitiveBool) {
				n.SetType(types.PrimitiveBool)
			} else {
				a.errorf(n, "cannot apply logical operator to non-boolean types: %v and %v", t1, t2)
				n.SetType(types.ErrorType{})
			}
		case parser.BinaryOpBitwiseAnd, parser.BinaryOpBitwiseXor, parser.BinaryOpBitwiseOr,
			parser.BinaryOpShiftLeft, parser.BinaryOpShiftRight:
			if _, leftFlags := types.Underlying(t1).(types.FlagsType); leftFlags {
				if (n.Op == parser.BinaryOpShiftLeft || n.Op == parser.BinaryOpShiftRight) && types.IsInteger(t2) {
					n.SetType(t1)
				} else if t1.Equals(t2) {
					n.SetType(t1)
				} else {
					a.errorf(n, "flags bitwise operands must have the same type")
					n.SetType(types.ErrorType{})
				}
				break
			}
			if types.IsInteger(t1) && types.IsInteger(t2) {
				got := types.PromoteNumeric(t1, t2)
				if got.Equals(types.ErrorType{}) {
					a.errorf(n, "incompatible integer types for bitwise operator: %v and %v", t1, t2)
					n.SetType(types.ErrorType{})
				} else {
					n.SetType(got)
				}
			} else {
				a.errorf(n, "cannot apply bitwise operator to non-integer types: %v and %v", t1, t2)
				n.SetType(types.ErrorType{})
			}
		default:
			panic("unhandled binary operator")
		}

	case *parser.CastNode:
		a.attributeExpr(n.Operand)
		n.GenericAssertion = genericExpressionOrigin(n.Operand) != nil
		var target types.Type
		view := n.StaticTraitView
		if n.ToType != nil {
			target, view = a.analyser.resolveCastTarget(n.ToType)
		} else {
			target = n.Type
			if n.Checked && n.CheckedType != nil {
				target = n.CheckedType
			}
			if target == nil {
				a.errorf(n, "cast is missing target type")
				target = types.ErrorType{}
			}
		}
		if view != nil {
			n.StaticTraitView = view
			target = n.Operand.GetType()
			if view.Access != types.TraitReceiverValue {
				_, _, targetIsPointer, _ := methodOwnerIdentity(target)
				if pointer, ok := types.Underlying(target).(types.PointerType); ok && targetIsPointer {
					target = types.PointerType{Base: pointer.Base, Mutable: view.Access == types.TraitReceiverMutablePointer}
				} else {
					target = types.PointerType{Base: target, Mutable: view.Access == types.TraitReceiverMutablePointer}
				}
			}
		}
		n.CheckedType = target
		if n.Checked {
			n.SetType(types.MultipleReturnType{Types: []types.Type{target, types.PrimitiveBool}})
		} else {
			n.SetType(target)
		}

	case *parser.ReprNode:
		a.attributeExpr(n.Operand)
		operand := n.Operand.GetType()
		if pointer, ok := types.Underlying(operand).(types.PointerType); ok {
			if pointer.Mutable {
				a.errorf(n, "@repr does not permit mutable tagged union pointers")
				n.SetType(types.ErrorType{})
				break
			}
			repr, tagged := types.TaggedUnionRepr(pointer.Base)
			if !tagged {
				a.errorf(n, "@repr requires an explicitly tagged union value or immutable pointer, got %v", operand)
				n.SetType(types.ErrorType{})
				break
			}
			n.SetType(types.PointerType{Base: repr})
			break
		}
		repr, tagged := types.TaggedUnionRepr(operand)
		if !tagged {
			a.errorf(n, "@repr requires an explicitly tagged union value or immutable pointer, got %v", operand)
			n.SetType(types.ErrorType{})
			break
		}
		n.SetType(repr)

	case *parser.SizeOfNode:
		n.SetType(types.PrimitiveUsz)
		n.OperandType = a.analyser.resolveTypeNode(n.Operand)

	case *parser.SizeOfExprNode:
		a.attributeExpr(n.Operand)
		n.SetType(types.PrimitiveUsz)
		n.OperandType = n.Operand.GetType()

	case *parser.AlignOfNode:
		n.SetType(types.PrimitiveUsz)
		if n.Expression != nil {
			a.attributeExpr(n.Expression)
			n.OperandType = n.Expression.GetType()
		} else {
			n.OperandType = a.analyser.resolveTypeNode(n.Operand)
		}

	case *parser.OffsetOfNode:
		n.SetType(types.PrimitiveUsz)
		n.OperandType = a.analyser.resolveTypeNode(n.Operand)

	default:
		panic(fmt.Sprintf("unexpected expression type: %T\n", node))
	}

	if node.GetType() == nil {
		panic("expression without type")
	}
}

func mutableArrayPlace(expr parser.ExpressionNode) bool {
	switch n := expr.(type) {
	case *parser.IdentifierNode:
		return n.Symbol != nil && n.Symbol.Mutable
	case *parser.FieldAccessNode:
		if n.ResolvedIdentifier != nil {
			return mutableArrayPlace(n.ResolvedIdentifier)
		}
		return mutableArrayPlace(n.Subject)
	case *parser.IndexExprNode:
		return mutableArrayPlace(n.Subject)
	case *parser.UnaryOpNode:
		if n.Op == parser.UnaryOpDereference {
			pointer, ok := types.Underlying(n.Operand.GetType()).(types.PointerType)
			return ok && pointer.Mutable
		}
	}
	return false
}

func isNilLiteral(node parser.ExpressionNode) bool {
	_, ok := node.(*parser.NilLiteralNode)
	return ok
}

func isTraitPointerType(t types.Type) bool {
	_, ok := types.Underlying(t).(types.TraitPointerType)
	return ok
}

func genericExpressionOrigin(node parser.ExpressionNode) types.Type {
	switch n := node.(type) {
	case *parser.IdentifierNode:
		if n.Symbol != nil {
			return n.Symbol.GenericOrigin
		}
	case *parser.UnaryOpNode:
		origin := genericExpressionOrigin(n.Operand)
		if origin == nil {
			return nil
		}
		switch n.Op {
		case parser.UnaryOpReference:
			return types.PointerType{Base: origin}
		case parser.UnaryOpMutableReference:
			return types.PointerType{Base: origin, Mutable: true}
		case parser.UnaryOpDereference:
			if pointer, ok := types.Underlying(origin).(types.PointerType); ok {
				return pointer.Base
			}
		}
	case *parser.FunctionCallNode:
		if n.Symbol != nil && n.Symbol.TemplateSymbol != nil {
			origin := n.Symbol.TemplateSymbol.Signature.ReturnType
			if types.HasTypeParameter(origin) {
				return origin
			}
		}
	}
	return nil
}

func staticTraitView(node parser.ExpressionNode) *types.StaticTraitView {
	switch n := node.(type) {
	case *parser.CastNode:
		return n.StaticTraitView
	case *parser.IdentifierNode:
		if n.Symbol != nil {
			return n.Symbol.StaticTraitView
		}
	case *parser.UnaryOpNode:
		view := staticTraitView(n.Operand)
		if view == nil {
			return nil
		}
		result := *view
		switch n.Op {
		case parser.UnaryOpReference:
			result.Access = types.TraitReceiverPointer
		case parser.UnaryOpMutableReference:
			result.Access = types.TraitReceiverMutablePointer
		case parser.UnaryOpDereference:
			result.Access = types.TraitReceiverValue
		default:
			return nil
		}
		return &result
	}
	return nil
}

func fieldOwnerDisplayType(t types.Type) types.Type {
	// Preserve a nominal pointer type's name in diagnostics. Only peel an
	// actual pointer expression to describe the type whose fields were queried.
	if ptr, ok := t.(types.PointerType); ok {
		return ptr.Base
	}
	return t
}

func (a *Attributor) attributeMethodValue(n *parser.FieldAccessNode) bool {
	ident := resolvedTypeIdentifier(n.Subject)
	if ident == nil {
		return false
	}
	module, owner := "", ""
	var ownerArguments []types.Type
	if ident.Symbol.Template && ident.Symbol.Kind == symbols.SymbolKindType {
		module, owner = ident.Symbol.DefinitionModule, ident.Symbol.Name
	} else {
		var ok bool
		module, owner, _, ok = methodOwnerIdentity(ident.Symbol.TypeInfo)
		if !ok {
			return false
		}
		if ident.Symbol.Kind == symbols.SymbolKindType && ident.Symbol.TemplateSymbol != nil {
			ownerArguments = ident.Symbol.TypeArguments
		}
	}
	method := a.analyser.methods[module+":"+owner][n.Field.Name]
	if method == nil {
		return false
	}
	if method.DefinitionModule != a.analyser.currentMod && !method.Public {
		a.errorf(n, "method %q is not public", n.Field.Name)
		n.SetType(types.ErrorType{})
		return true
	}
	a.attributeFunctionDefinition(method)
	if method.Template && (len(ownerArguments) != 0 || len(n.Field.TypeArguments) != 0) {
		arguments := append([]types.Type(nil), ownerArguments...)
		arguments = append(arguments, a.analyser.resolveGenericArguments(n.Field.TypeArguments)...)
		if a.templatesOnly || hasTypeParameters(arguments) {
			if !a.analyser.checkGenericArguments(n, method.GenericParameters, arguments) {
				n.SetType(types.ErrorType{})
				return true
			}
			method = dependentGenericFunctionSymbol(method, arguments)
		} else {
			specialization := a.analyser.specializeGenericFunction(method, arguments, n)
			if specialization == nil {
				n.SetType(types.ErrorType{})
				return true
			}
			method = specialization.Symbol
		}
	}
	ret := method.Signature.ReturnType
	if ret == nil {
		ret = types.PrimitiveVoid
	}
	n.MethodSymbol = method
	n.MethodModule = method.DefinitionModule
	n.SetType(types.PointerType{Base: types.FunctionType{Parameters: method.Signature.Parameters, ReturnType: ret, TypedVariadic: method.Signature.TypedVariadic, VariadicElement: method.Signature.VariadicElement}})
	return true
}

func (a *Attributor) attributeFunctionDefinition(symbol *symbols.Symbol) {
	if symbol == nil || symbol.Signature == nil || symbol.Signature.ReturnType != nil || a.functionState[symbol] != functionUnattributed {
		return
	}
	definition := a.analyser.functionDefinitions[symbol]
	if definition == nil {
		return
	}
	mod := a.analyser.modules[definition.module]
	if mod == nil {
		return
	}
	bindings := map[string]types.Type(nil)
	if definition.node.IsGeneric() {
		bindings = make(map[string]types.Type, len(symbol.GenericParameters))
		for _, parameter := range symbol.GenericParameters {
			bindings[parameter.Name] = parameter
		}
	}
	previousTemplatesOnly := a.templatesOnly
	a.templatesOnly = definition.node.IsGeneric()
	defer func() { a.templatesOnly = previousTemplatesOnly }()
	a.analyser.withDefinitionContext(definition.module, mod.TrustedStandardLibrary, bindings, func() {
		a.attributeNode(definition.node)
	})
}

func resolvedTypeIdentifier(expr parser.ExpressionNode) *parser.IdentifierNode {
	if ident, ok := expr.(*parser.IdentifierNode); ok && ident.Symbol != nil && ident.Symbol.Kind == symbols.SymbolKindType {
		return ident
	}
	if field, ok := expr.(*parser.FieldAccessNode); ok && field.ResolvedIdentifier != nil && field.ResolvedIdentifier.Symbol.Kind == symbols.SymbolKindType {
		return field.ResolvedIdentifier
	}
	return nil
}

func (a *Attributor) attributeMethodCall(n *parser.FunctionCallNode) bool {
	member, ok := n.Callee.(*parser.FieldAccessNode)
	if !ok {
		return false
	}
	if ident, ok := member.Subject.(*parser.IdentifierNode); ok && ident.Symbol != nil && ident.Symbol.Kind == symbols.SymbolKindType {
		return false
	}
	a.attributeExpr(member.Subject)
	if view := staticTraitView(member.Subject); view != nil {
		return a.attributeStaticTraitMethodCall(n, member, view)
	}
	if traitPtr, ok := traitPointer(member.Subject.GetType()); ok {
		for slot, requirement := range traitPtr.Trait.Methods {
			if requirement.Name != member.Field.Name {
				continue
			}
			if requirement.Receiver == types.TraitReceiverMutablePointer && !traitPtr.Mutable {
				a.errorf(n, "method %q requires mutable trait access", requirement.Name)
				n.SetType(types.ErrorType{})
				return true
			}
			if len(requirement.GenericParameters) != 0 {
				a.errorf(n, "generic trait method %q cannot be called dynamically", requirement.Name)
				n.SetType(types.ErrorType{})
				return true
			}
			params := append([]types.Type{member.Subject.GetType()}, requirement.Parameters...)
			n.Symbol = symbols.NewFunction(requirement.Name, &symbols.FunctionSignature{Parameters: params, RequiredParameters: len(params), ReturnType: requirement.ReturnType})
			n.Args = append([]parser.ExpressionNode{member.Subject}, n.Args...)
			n.Method = true
			n.TraitCall = true
			n.TraitSlot = slot
			return true
		}
		return false
	}
	module, owner, receiverIsPointer, ok := methodOwnerIdentity(member.Subject.GetType())
	var method *symbols.Symbol
	if ok {
		method = a.analyser.methods[module+":"+owner][member.Field.Name]
	}
	adjustment := structuralReceiverDirect
	if method != nil && method.MethodOwnerType != nil {
		_, patternAdjustment, matches := structuralReceiverMatch(method, member.Subject.GetType())
		if !matches {
			method = nil
		} else {
			adjustment = patternAdjustment
		}
	}
	if method == nil {
		var ambiguous bool
		method, adjustment, ambiguous = a.analyser.findStructuralMethod(member.Field.Name, member.Subject.GetType())
		if ambiguous {
			a.errorf(n, "method %q is ambiguous for receiver type %v", member.Field.Name, member.Subject.GetType())
			n.SetType(types.ErrorType{})
			return true
		}
		if method == nil {
			return false
		}
	}
	if method.StaticMethod {
		return false
	}
	if method.DefinitionModule != a.analyser.currentMod && !method.Public {
		a.errorf(n, "method %q is not public", member.Field.Name)
		n.SetType(types.ErrorType{})
		return true
	}
	if method.Template && len(member.Field.TypeArguments) != 0 {
		arguments := a.analyser.resolveGenericArguments(member.Field.TypeArguments)
		if a.templatesOnly || hasTypeParameters(arguments) {
			if !a.analyser.checkGenericArguments(n, method.GenericParameters, arguments) {
				n.SetType(types.ErrorType{})
				return true
			}
			method = dependentGenericFunctionSymbol(method, arguments)
		} else {
			specialization := a.analyser.specializeGenericFunction(method, arguments, n)
			if specialization == nil {
				n.SetType(types.ErrorType{})
				return true
			}
			method = specialization.Symbol
		}
	}
	receiver := member.Subject
	if method.MethodOwnerType != nil {
		switch adjustment {
		case structuralReceiverReference:
			receiver = &parser.UnaryOpNode{Op: parser.UnaryOpReference, Operand: receiver, Loc: receiver.GetLoc()}
		case structuralReceiverMutableReference:
			receiver = &parser.UnaryOpNode{Op: parser.UnaryOpMutableReference, Operand: receiver, Loc: receiver.GetLoc()}
		case structuralReceiverDereference:
			receiver = &parser.UnaryOpNode{Op: parser.UnaryOpDereference, Operand: receiver, Loc: receiver.GetLoc()}
		}
	} else {
		expected := method.Signature.Parameters[0]
		expectsPointer := method.MethodReceiver != types.TraitReceiverValue
		if expectsPointer && !receiverIsPointer {
			op := parser.UnaryOpReference
			if ptr := types.Underlying(expected).(types.PointerType); ptr.Mutable {
				op = parser.UnaryOpMutableReference
			}
			receiver = &parser.UnaryOpNode{Op: op, Operand: receiver, Loc: receiver.GetLoc()}
		} else if !expectsPointer && receiverIsPointer {
			receiver = &parser.UnaryOpNode{Op: parser.UnaryOpDereference, Operand: receiver, Loc: receiver.GetLoc()}
		}
	}
	n.Args = append([]parser.ExpressionNode{receiver}, n.Args...)
	n.Symbol = method
	n.Method = true
	if method.DefinitionModule != a.analyser.currentMod {
		n.Name = &parser.IdentifierNode{Name: method.Name, Module: method.DefinitionModule, ResolvedModuleName: method.DefinitionModule, Loc: member.Loc, Symbol: method}
	}
	return true
}

type structuralReceiverAdjustment uint8

const (
	structuralReceiverDirect structuralReceiverAdjustment = iota
	structuralReceiverReference
	structuralReceiverMutableReference
	structuralReceiverDereference
)

func (a *Analyser) findStructuralMethod(name string, actual types.Type) (*symbols.Symbol, structuralReceiverAdjustment, bool) {
	var selected *symbols.Symbol
	selectedAdjustment := structuralReceiverDirect
	selectedRank := 100
	ambiguous := false
	for _, candidate := range a.structuralMethods[name] {
		if candidate.StaticMethod || len(candidate.Signature.Parameters) == 0 {
			continue
		}
		rank, adjustment, matches := structuralReceiverMatch(candidate, actual)
		if !matches || rank > selectedRank {
			continue
		}
		if rank == selectedRank {
			ambiguous = true
			continue
		}
		selected = candidate
		selectedAdjustment = adjustment
		selectedRank = rank
		ambiguous = false
	}
	return selected, selectedAdjustment, ambiguous
}

func structuralReceiverMatch(method *symbols.Symbol, actual types.Type) (int, structuralReceiverAdjustment, bool) {
	expected := method.Signature.Parameters[0]
	if typePatternMatches(expected, actual, false) {
		return 0, structuralReceiverDirect, true
	}
	if typePatternMatches(expected, actual, true) {
		return 1, structuralReceiverDirect, true
	}
	owner := method.MethodOwnerType
	if method.MethodReceiver != types.TraitReceiverValue {
		if typePatternMatches(owner, actual, false) {
			if method.MethodReceiver == types.TraitReceiverMutablePointer {
				return 2, structuralReceiverMutableReference, true
			}
			return 2, structuralReceiverReference, true
		}
		if typePatternMatches(owner, actual, true) {
			if method.MethodReceiver == types.TraitReceiverMutablePointer {
				return 3, structuralReceiverMutableReference, true
			}
			return 3, structuralReceiverReference, true
		}
	}
	if method.MethodReceiver == types.TraitReceiverValue {
		if pointer, ok := actual.(types.PointerType); ok && typePatternMatches(owner, pointer.Base, false) {
			return 2, structuralReceiverDereference, true
		}
	}
	return 0, structuralReceiverDirect, false
}

func typePatternMatches(pattern, actual types.Type, allowCapabilityCoercion bool) bool {
	if _, ok := pattern.(types.TypeParameter); ok {
		return true
	}
	switch pattern := pattern.(type) {
	case types.DefinedType:
		actual, ok := actual.(types.DefinedType)
		if !ok || pattern.Module != actual.Module || len(pattern.TypeArguments) != len(actual.TypeArguments) {
			return false
		}
		if pattern.GenericName != "" || actual.GenericName != "" {
			if pattern.GenericName == "" || pattern.GenericName != actual.GenericName {
				return false
			}
		} else if pattern.Name != actual.Name {
			return false
		}
		for i := range pattern.TypeArguments {
			if !typePatternMatches(pattern.TypeArguments[i], actual.TypeArguments[i], allowCapabilityCoercion) {
				return false
			}
		}
		return true
	case types.PointerType:
		actual, ok := actual.(types.PointerType)
		if !ok || (pattern.Mutable != actual.Mutable && !(allowCapabilityCoercion && !pattern.Mutable && actual.Mutable)) {
			return false
		}
		return typePatternMatches(pattern.Base, actual.Base, allowCapabilityCoercion)
	case types.SliceType:
		actual, ok := actual.(types.SliceType)
		if !ok || (pattern.Mutable != actual.Mutable && !(allowCapabilityCoercion && !pattern.Mutable && actual.Mutable)) {
			return false
		}
		return typePatternMatches(pattern.Base, actual.Base, allowCapabilityCoercion)
	case types.ArrayType:
		actual, ok := actual.(types.ArrayType)
		return ok && pattern.Length == actual.Length && typePatternMatches(pattern.Base, actual.Base, allowCapabilityCoercion)
	case types.FunctionType:
		actual, ok := actual.(types.FunctionType)
		if !ok || len(pattern.Parameters) != len(actual.Parameters) || pattern.TypedVariadic != actual.TypedVariadic {
			return false
		}
		for i := range pattern.Parameters {
			if !typePatternMatches(pattern.Parameters[i], actual.Parameters[i], allowCapabilityCoercion) {
				return false
			}
		}
		return typePatternMatches(pattern.ReturnType, actual.ReturnType, allowCapabilityCoercion)
	default:
		return pattern.Equals(actual)
	}
}

func (a *Attributor) attributeStaticTraitMethodCall(
	call *parser.FunctionCallNode,
	member *parser.FieldAccessNode,
	view *types.StaticTraitView,
) bool {
	var requirement *types.TraitMethod
	requirementSlot := -1
	for i := range view.Trait.Methods {
		if view.Trait.Methods[i].Name == member.Field.Name {
			requirement = &view.Trait.Methods[i]
			requirementSlot = i
			break
		}
	}
	if requirement == nil {
		a.errorf(call, "trait %v has no method %q", view.Trait, member.Field.Name)
		call.SetType(types.ErrorType{})
		return true
	}
	if requirement.Receiver == types.TraitReceiverPointer && view.Access == types.TraitReceiverValue {
		a.errorf(call, "method %q requires *%v access", requirement.Name, view.Trait)
		call.SetType(types.ErrorType{})
		return true
	}
	if requirement.Receiver == types.TraitReceiverMutablePointer && view.Access != types.TraitReceiverMutablePointer {
		a.errorf(call, "method %q requires *mut %v access", requirement.Name, view.Trait)
		call.SetType(types.ErrorType{})
		return true
	}

	subjectType := member.Subject.GetType()
	if parameter, symbolic := genericTypeParameterBase(subjectType); symbolic {
		parameters := make([]types.Type, len(requirement.Parameters)+1)
		parameters[0] = subjectType
		for i, required := range requirement.Parameters {
			parameters[i+1] = types.SubstituteSelf(required, parameter)
		}
		method := symbols.NewFunction(requirement.Name, &symbols.FunctionSignature{
			Parameters:         parameters,
			RequiredParameters: len(parameters),
			ReturnType:         types.SubstituteSelf(requirement.ReturnType, parameter),
		})
		method.Method = true
		method.MethodReceiver = requirement.Receiver
		method.GenericParameters = append([]types.TypeParameter(nil), requirement.GenericParameters...)
		method.Template = len(method.GenericParameters) != 0
		method.TraitRequirement = true
		method.RequirementTrait = view.Trait
		method.RequirementSlot = requirementSlot
		method.RequirementAccess = view.Access
		call.Args = append([]parser.ExpressionNode{member.Subject}, call.Args...)
		call.Symbol = method
		call.Method = true
		if method.Template && len(member.Field.TypeArguments) != 0 {
			arguments := a.analyser.resolveGenericArguments(member.Field.TypeArguments)
			if !a.analyser.checkGenericArguments(call, method.GenericParameters, arguments) {
				call.SetType(types.ErrorType{})
				return true
			}
			call.Symbol = dependentGenericFunctionSymbol(method, arguments)
		}
		return true
	}

	_, _, subjectIsPointer, _ := methodOwnerIdentity(subjectType)
	receiverIsPointer := view.Access != types.TraitReceiverValue || subjectIsPointer
	var probe types.PointerType
	if receiverIsPointer {
		probe, _ = types.Underlying(subjectType).(types.PointerType)
	} else {
		probe = types.PointerType{Base: subjectType, Mutable: view.Access == types.TraitReceiverMutablePointer}
	}
	target := types.TraitPointerType{
		Trait:   view.Trait,
		Mutable: view.Access == types.TraitReceiverMutablePointer,
	}
	methods, conforms := a.analyser.structuralConformance(probe, target, call)
	if !conforms {
		a.errorf(call, "type %v does not implement %v", subjectType, view.Trait)
		call.SetType(types.ErrorType{})
		return true
	}
	method := methods[requirementSlot]
	if method.Template && len(member.Field.TypeArguments) != 0 {
		arguments := a.analyser.resolveGenericArguments(member.Field.TypeArguments)
		if a.templatesOnly || hasTypeParameters(arguments) {
			if !a.analyser.checkGenericArguments(call, method.GenericParameters, arguments) {
				call.SetType(types.ErrorType{})
				return true
			}
			method = dependentGenericFunctionSymbol(method, arguments)
		} else {
			specialization := a.analyser.specializeGenericFunction(method, arguments, call)
			if specialization == nil {
				call.SetType(types.ErrorType{})
				return true
			}
			method = specialization.Symbol
		}
	}

	receiver := member.Subject
	expected := method.Signature.Parameters[0]
	expectsPointer := method.MethodReceiver != types.TraitReceiverValue
	if expectsPointer && !receiverIsPointer {
		op := parser.UnaryOpReference
		if ptr := types.Underlying(expected).(types.PointerType); ptr.Mutable {
			op = parser.UnaryOpMutableReference
		}
		receiver = &parser.UnaryOpNode{Op: op, Operand: receiver, Loc: receiver.GetLoc()}
	} else if !expectsPointer && receiverIsPointer {
		receiver = &parser.UnaryOpNode{Op: parser.UnaryOpDereference, Operand: receiver, Loc: receiver.GetLoc()}
	}
	call.Args = append([]parser.ExpressionNode{receiver}, call.Args...)
	call.Symbol = method
	call.Method = true
	if method.DefinitionModule != a.analyser.currentMod {
		call.Name = &parser.IdentifierNode{
			Name: method.Name, Module: method.DefinitionModule, ResolvedModuleName: method.DefinitionModule,
			Loc: member.Loc, Symbol: method,
		}
	}
	return true
}

func methodOwnerIdentity(t types.Type) (module, name string, pointer bool, ok bool) {
	if defined, isDefined := t.(types.DefinedType); isDefined && defined.Module == "" && defined.Name == "str" {
		return "builtin", "str", false, true
	}
	if ptr, isPointer := types.Underlying(t).(types.PointerType); isPointer && !ptr.Mutable && ptr.Base.Equals(types.PrimitiveU8) {
		return "builtin", "cstr", false, true
	}
	if ptr, isPointer := types.Underlying(t).(types.PointerType); isPointer {
		t = ptr.Base
		pointer = true
	}
	if defined, isDefined := t.(types.DefinedType); isDefined && defined.Module == "" && defined.Name == "str" {
		return "builtin", "str", pointer, true
	}
	switch t := t.(type) {
	case types.DefinedType:
		if t.GenericName != "" {
			return t.Module, t.GenericName, pointer, true
		}
		return t.Module, t.Name, pointer, true
	case *types.AliasRef:
		return t.Module, t.Name, pointer, true
	case types.PrimitiveType:
		return "builtin", t.String(), pointer, true
	default:
		return "", "", pointer, false
	}
}

func isUnresolvedEnum(t types.Type) bool {
	_, ok := t.(types.UnresolvedEnum)
	return ok
}

func isEnumOrFlags(t types.Type) bool {
	switch types.Underlying(t).(type) {
	case types.EnumType, types.FlagsType:
		return true
	}
	return false
}

func (a *Attributor) resolveEnumLiteral(n *parser.EnumLiteralNode, expected types.Type) {
	var value string
	var ok bool
	switch t := types.Underlying(expected).(type) {
	case types.EnumType:
		value, ok = t.VariantValue(n.Variant)
	case types.FlagsType:
		value, ok = t.VariantValue(n.Variant)
	}
	if !ok {
		a.errorf(n, "%s has no member %q", expected, n.Variant)
		n.SetType(types.ErrorType{})
		return
	}
	n.Value = value
	n.SetType(expected)
}

type returnTypeCandidate struct {
	node parser.Node
	ty   types.Type
}

func (a *Attributor) mergeReturnTypes(candidates []returnTypeCandidate) types.Type {
	if len(candidates) == 0 {
		return types.PrimitiveVoid
	}
	current := candidates[0].ty
	for _, candidate := range candidates[1:] {
		if types.IsNumeric(current) && types.IsNumeric(candidate.ty) {
			current = types.PromoteNumeric(current, candidate.ty)
			if current.Equals(types.ErrorType{}) {
				a.errorf(candidate.node, "inconsistent return types")
				return current
			}
			continue
		}
		if !current.Equals(candidate.ty) {
			a.errorf(candidate.node, "inconsistent return types: expected %v, got %v", current, candidate.ty)
			return types.ErrorType{}
		}
	}
	return current
}

func (a *Attributor) attributeBlock(n *parser.BlockNode) {
	for _, child := range n.Body {
		a.attributeNode(child)
	}
	if !n.Expression {
		n.SetType(types.PrimitiveVoid)
		return
	}
	if result, ok := parser.BlockResult(n); ok {
		n.SetType(result.GetType())
		return
	}
	if !parser.NodeFallsThrough(n) {
		n.SetType(types.PrimitiveVoid)
		return
	}
	n.SetType(types.PrimitiveVoid)
}

func (a *Attributor) attributeIf(n *parser.IfNode) {
	a.attributeExpr(n.IfBranch.Condition)
	a.attributeNode(n.IfBranch.Node)
	for _, branch := range n.ElseIfBranches {
		a.attributeExpr(branch.Condition)
		a.attributeNode(branch.Node)
	}
	if n.ElseBranch != nil {
		a.attributeNode(n.ElseBranch)
	}
	if !n.Expression {
		n.SetType(types.PrimitiveVoid)
		return
	}

	var candidates []returnTypeCandidate
	addBranch := func(block *parser.BlockNode) {
		if block != nil && parser.NodeFallsThrough(block) {
			candidates = append(candidates, returnTypeCandidate{node: block, ty: block.GetType()})
		}
	}
	addBranch(n.IfBranch.Node)
	for _, branch := range n.ElseIfBranches {
		addBranch(branch.Node)
	}
	addBranch(n.ElseBranch)
	if len(candidates) == 0 {
		n.SetType(types.PrimitiveVoid)
		return
	}
	n.SetType(a.mergeReturnTypes(candidates))
}

func (a *Attributor) attributeMatch(n *parser.MatchNode) {
	a.attributeExpr(n.Subject)
	if n.Binding != nil {
		n.Binding.Type = n.Subject.GetType()
	}
	for i := range n.Arms {
		arm := &n.Arms[i]
		a.attributeMatchPattern(arm.Pattern, n.Subject.GetType())
		if arm.Guard != nil {
			a.attributeExpr(arm.Guard)
		}
		a.attributeExpr(arm.Body)
	}
	if !n.Expression {
		n.SetType(types.PrimitiveVoid)
		return
	}
	var candidates []returnTypeCandidate
	for i := range n.Arms {
		body := n.Arms[i].Body
		if parser.NodeFallsThrough(body) {
			candidates = append(candidates, returnTypeCandidate{node: body, ty: body.GetType()})
		}
	}
	if len(candidates) == 0 {
		n.SetType(types.PrimitiveVoid)
		return
	}
	n.SetType(a.mergeReturnTypes(candidates))
}

func (a *Attributor) attributeMatchPattern(pattern *parser.MatchPatternNode, subjectType types.Type) {
	if pattern == nil {
		return
	}
	switch pattern.Kind {
	case parser.MatchPatternLiteral:
		a.attributeExpr(pattern.Literal)
	case parser.MatchPatternRange:
		a.attributeExpr(pattern.Start)
		a.attributeExpr(pattern.End)
	case parser.MatchPatternAlternative:
		for _, alternative := range pattern.Alternatives {
			a.attributeMatchPattern(alternative, subjectType)
		}
	case parser.MatchPatternVariant:
		if info, tagged := types.TaggedUnion(subjectType); tagged {
			variant, _, ok := info.Variant(pattern.Variant)
			if !ok {
				a.errorf(pattern, "tagged union %v has no variant %q", subjectType, pattern.Variant)
				return
			}
			pattern.TagValue = variant.TagValue
			pattern.PayloadType = types.StructType{Fields: variant.Fields}
			if len(variant.Fields) == 0 && pattern.Payload {
				a.errorf(pattern, "empty variant .%s pattern does not take parentheses", variant.Name)
			}
			if len(pattern.Bindings) != len(variant.Fields) {
				a.errorf(pattern, "variant %q pattern expects %d payload bindings, but %d provided", variant.Name, len(variant.Fields), len(pattern.Bindings))
				return
			}
			seenFields := map[string]bool{}
			for i := range pattern.Bindings {
				binding := &pattern.Bindings[i]
				fieldIndex := i
				if binding.Field != "" {
					fieldIndex = -1
					for j, field := range variant.Fields {
						if field.L == binding.Field {
							fieldIndex = j
							break
						}
					}
					if fieldIndex < 0 {
						a.errorf(pattern, "variant %q has no payload field %q", variant.Name, binding.Field)
						continue
					}
					if seenFields[binding.Field] {
						a.errorf(pattern, "payload field %q appears more than once in pattern", binding.Field)
					}
					seenFields[binding.Field] = true
				}
				binding.Field = variant.Fields[fieldIndex].L
				if binding.Symbol != nil {
					binding.Symbol.Type = variant.Fields[fieldIndex].R
				}
			}
			return
		}
		enumType, ok := types.Underlying(subjectType).(types.EnumType)
		if !ok {
			a.errorf(pattern, "variant pattern .%s requires an enum or tagged union subject", pattern.Variant)
			return
		}
		value, ok := enumType.VariantValue(pattern.Variant)
		if !ok {
			a.errorf(pattern, "enum %v has no member %q", subjectType, pattern.Variant)
			return
		}
		if pattern.Payload {
			a.errorf(pattern, "enum member .%s has no payload", pattern.Variant)
		}
		pattern.TagValue = value
	}
}

func collectFunctionReturnNodes(body []parser.Node) []*parser.ControlKeywordNode {
	var returnNodes []*parser.ControlKeywordNode

	for _, node := range body {
		returnNodes = append(returnNodes, collectFunctionReturnNodesFromNode(node)...)
	}

	return returnNodes
}

func collectFunctionReturnNodesFromNode(node parser.Node) []*parser.ControlKeywordNode {
	if node == nil {
		return nil
	}
	switch n := node.(type) {
	case *parser.ControlKeywordNode:
		if n.Keyword == tokeniser.KeywordReturn {
			return []*parser.ControlKeywordNode{n}
		}
	case *parser.BlockNode:
		return collectFunctionReturnNodes(n.Body)
	case *parser.IfNode:
		result := collectFunctionReturnNodes(n.IfBranch.Node.Body)
		for _, branch := range n.ElseIfBranches {
			result = append(result, collectFunctionReturnNodes(branch.Node.Body)...)
		}
		if n.ElseBranch != nil {
			result = append(result, collectFunctionReturnNodes(n.ElseBranch.Body)...)
		}
		return result
	case *parser.MatchNode:
		var result []*parser.ControlKeywordNode
		for _, arm := range n.Arms {
			result = append(result, collectFunctionReturnNodesFromExpr(arm.Body)...)
		}
		return result
	case *parser.ForNode:
		return collectFunctionReturnNodes(n.Body.Body)
	case *parser.RangeForNode:
		return collectFunctionReturnNodes(n.Body.Body)
	case *parser.ForEachNode:
		return collectFunctionReturnNodes(n.Body.Body)
	case *parser.DeclarationNode:
		return collectFunctionReturnNodesFromExpr(n.Value)
	case *parser.MultiDeclarationNode:
		return collectFunctionReturnNodesFromExpr(n.Value)
	case *parser.AssignmentNode:
		return collectFunctionReturnNodesFromExpr(n.Value)
	case parser.ExpressionNode:
		return collectFunctionReturnNodesFromExpr(n)
	}
	return nil
}

func collectFunctionReturnNodesFromExpr(expr parser.ExpressionNode) []*parser.ControlKeywordNode {
	if expr == nil {
		return nil
	}
	collect := func(expressions ...parser.ExpressionNode) []*parser.ControlKeywordNode {
		var result []*parser.ControlKeywordNode
		for _, expression := range expressions {
			result = append(result, collectFunctionReturnNodesFromExpr(expression)...)
		}
		return result
	}
	switch n := expr.(type) {
	case *parser.BlockNode, *parser.IfNode, *parser.MatchNode:
		return collectFunctionReturnNodesFromNode(n)
	case *parser.BinaryOpNode:
		return collect(n.Operand1, n.Operand2)
	case *parser.UnaryOpNode:
		return collect(n.Operand)
	case *parser.FunctionCallNode:
		return collect(append([]parser.ExpressionNode{n.Callee}, n.Args...)...)
	case *parser.IndexExprNode:
		return collect(n.Subject, n.Index)
	case *parser.SliceExprNode:
		return collect(n.Subject, n.Start, n.End)
	case *parser.FieldAccessNode:
		return collect(n.Subject)
	case *parser.CastNode:
		return collect(n.Operand)
	case *parser.SizeOfExprNode:
		return collect(n.Operand)
	case *parser.AlignOfNode:
		return collect(n.Expression)
	case *parser.StructLiteralNode:
		var expressions []parser.ExpressionNode
		for _, field := range n.Fields {
			expressions = append(expressions, field.R)
		}
		return collect(expressions...)
	case *parser.SliceLiteralNode:
		return collect(append(append([]parser.ExpressionNode(nil), n.Elements...), n.RepeatValue, n.RepeatAmount)...)
	}
	return nil
}
