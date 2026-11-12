package sema

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"strings"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func (a *Analyser) fillRetainedArguments(template *symbols.Symbol, values []parser.ExpressionNode, arguments []types.Type, use parser.Node) bool {
	for index, parameter := range template.ParameterizedParameters {
		if parameter.ValueType == nil {
			continue
		}
		if index >= len(template.ValueParameterPositions) {
			a.errorf(use, "missing retained compile-time parameter metadata for %s", parameter.Name)
			return false
		}
		position := template.ValueParameterPositions[index]
		if position < 0 || position >= len(values) {
			a.errorf(use, "missing compile-time argument for %s", parameter.Name)
			return false
		}
		argument, ok := retainedArgumentValue(values[position], parameter.ValueType)
		if !ok {
			a.errorf(values[position], "argument for $%s must be known at compile time", parameter.Name)
			return false
		}
		arguments[index] = argument
	}
	return true
}

func retainedArgumentValue(expression parser.ExpressionNode, valueType types.Type) (types.Type, bool) {
	switch node := expression.(type) {
	case *parser.CompileTimeExpressionNode:
		return retainedArgumentValue(node.Operand, valueType)
	case *parser.BoolLiteralNode:
		if valueType.Equals(types.PrimitiveBool) {
			return types.ConstantType{ValueType: valueType, Value: node.Value}, true
		}
	case *parser.IntegerLiteralNode:
		if integer, ok := new(big.Int).SetString(node.Value, 0); ok && !valueType.Equals(types.PrimitiveBool) {
			return types.ConstantType{ValueType: valueType, Value: integer.String()}, true
		}
	case *parser.UnaryOpNode:
		if node.Op == parser.UnaryOpNegate {
			if positive, ok := retainedArgumentValue(node.Operand, valueType); ok {
				constant := positive.(types.ConstantType)
				if integer, ok := new(big.Int).SetString(constant.Value, 10); ok {
					return types.ConstantType{ValueType: valueType, Value: integer.Neg(integer).String()}, true
				}
			}
		}
	case *parser.IdentifierNode:
		if node.Symbol != nil {
			if parameter, ok := node.Symbol.ParameterizedOrigin.(types.TypeParameter); ok && parameter.ValueType != nil && parameter.ValueType.Equals(valueType) {
				return parameter, true
			}
			if node.Symbol.Comptime && node.Symbol.ComptimeInteger != "" {
				value := node.Symbol.ComptimeInteger
				if valueType.Equals(types.PrimitiveBool) {
					if value == "1" {
						value = "true"
					} else if value == "0" {
						value = "false"
					}
				}
				return types.ConstantType{ValueType: valueType, Value: value}, true
			}
		}
	}
	return nil, false
}

type parameterizedFunctionInfo struct {
	node            *parser.FunctionDefNode
	root            *parser.RootNode
	module          string
	specializations map[string]*parser.FunctionDefNode
}

type parameterizedValueInfo struct {
	node            *parser.DeclarationNode
	root            *parser.RootNode
	module          string
	trusted         bool
	specializations map[string]*parser.DeclarationNode
}

type parameterizedAliasSpecialization struct {
	state    aliasState
	resolved types.Type
	symbol   *symbols.Symbol
}

type parameterizedAliasInfo struct {
	node            *parser.TypeAliasNode
	root            *parser.RootNode
	module          string
	parameters      []types.TypeParameter
	specializations map[string]*parameterizedAliasSpecialization
}

func parameterizedOwner(module, name string) string {
	return module + ":" + name
}

func specializationKey(arguments []types.Type) string {
	parts := make([]string, len(arguments))
	for i, argument := range arguments {
		parts[i] = types.Identity(argument)
	}
	return strings.Join(parts, ",")
}

func SpecializationName(module, name string, arguments []types.Type) string {
	identity := parameterizedOwner(module, name) + "<" + specializationKey(arguments) + ">"
	return name + "$" + fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}

func specializationDisplayName(name string, arguments []types.Type) string {
	parts := make([]string, len(arguments))
	for i, argument := range arguments {
		parts[i] = argument.String()
	}
	return name + "<" + strings.Join(parts, ", ") + ">"
}

func (a *Analyser) makeParameterizedParameters(name string, nodes []parser.ParameterizedParameterNode) []types.TypeParameter {
	previous := a.typeParameterBindings
	bindings := make(map[string]types.Type, len(previous)+len(nodes))
	for name, binding := range previous {
		bindings[name] = binding
	}
	a.typeParameterBindings = bindings
	parameters := make([]types.TypeParameter, len(nodes))
	owner := parameterizedOwner(a.currentMod, name)
	for i, node := range nodes {
		parameter := types.TypeParameter{Owner: owner, Name: node.Name, Index: i}
		if node.ValueType != nil {
			parameter.ValueType = a.resolveTypeNode(node.ValueType)
		}
		parameters[i] = parameter
		if node.ValueType == nil {
			bindings[node.Name] = parameter
		}
	}
	for i, node := range nodes {
		if node.ValueType != nil {
			continue
		}
		if node.Constraint == nil {
			continue
		}
		constraint := a.resolveTypeNode(node.Constraint)
		if types.HasError(constraint) {
			parameters[i].Constraint = types.ErrorType{}
			bindings[node.Name] = parameters[i]
			continue
		}
		if _, ok := types.Underlying(constraint).(types.TraitType); !ok {
			a.errorf(node, "type parameter constraint must be a trait type, got %v", constraint)
			constraint = types.ErrorType{}
		}
		parameters[i].Constraint = constraint
		bindings[node.Name] = parameters[i]
	}
	a.typeParameterBindings = previous
	return parameters
}

func typeArgumentBindings(parameters []types.TypeParameter, arguments []types.Type) map[string]types.Type {
	bindings := make(map[string]types.Type, len(parameters))
	for i, parameter := range parameters {
		if parameter.ValueType == nil {
			bindings[parameter.Name] = arguments[i]
		}
	}
	return bindings
}

func typeSubstitutionBindings(parameters []types.TypeParameter, arguments []types.Type) map[string]types.Type {
	bindings := make(map[string]types.Type, len(parameters))
	for i, parameter := range parameters {
		bindings[parameter.Key()] = arguments[i]
	}
	return bindings
}

func hasTypeParameters(arguments []types.Type) bool {
	for _, argument := range arguments {
		if types.HasTypeParameter(argument) {
			return true
		}
	}
	return false
}

func substituteFunctionSignature(signature *symbols.FunctionSignature, substitutions map[string]types.Type) *symbols.FunctionSignature {
	if signature == nil {
		return nil
	}
	result := *signature
	result.Parameters = make([]types.Type, len(signature.Parameters))
	for i, parameter := range signature.Parameters {
		result.Parameters[i] = types.Substitute(parameter, substitutions)
	}
	result.ReturnType = types.Substitute(signature.ReturnType, substitutions)
	result.VariadicElement = types.Substitute(signature.VariadicElement, substitutions)
	return &result
}

func dependentParameterizedFunctionSymbol(template *symbols.Symbol, arguments []types.Type) *symbols.Symbol {
	result := *template
	result.Template = false
	result.TemplateSymbol = template
	result.TypeArguments = append([]types.Type(nil), arguments...)
	result.ParameterizedParameters = nil
	result.Signature = substituteFunctionSignature(
		template.Signature,
		typeSubstitutionBindings(template.ParameterizedParameters, arguments),
	)
	return &result
}

func staticTraitViewForParameterizedType(t types.Type) *types.StaticTraitView {
	access := types.TraitReceiverValue
	if pointer, ok := t.(types.PointerType); ok {
		t = pointer.Base
		access = types.TraitReceiverPointer
		if pointer.Mutable {
			access = types.TraitReceiverMutablePointer
		}
	}
	parameter, ok := t.(types.TypeParameter)
	if !ok || parameter.Constraint == nil {
		return nil
	}
	trait, ok := types.Underlying(parameter.Constraint).(types.TraitType)
	if !ok {
		return nil
	}
	return &types.StaticTraitView{Trait: trait, Access: access}
}

func parameterizedTypeParameterBase(t types.Type) (types.TypeParameter, bool) {
	if pointer, ok := t.(types.PointerType); ok {
		t = pointer.Base
	}
	parameter, ok := t.(types.TypeParameter)
	return parameter, ok
}

func (a *Analyser) resolveParameterizedArguments(nodes []parser.TypeNode) []types.Type {
	return a.resolveParameterizedArgumentsAt(nodes, false)
}

func (a *Analyser) resolveParameterizedArgumentsAt(nodes []parser.TypeNode, indirect bool) []types.Type {
	arguments := make([]types.Type, len(nodes))
	for i, node := range nodes {
		arguments[i] = a.resolveTypeNodeAt(node, indirect)
	}
	return arguments
}

func (a *Analyser) checkParameterizedArguments(node parser.Node, parameters []types.TypeParameter, arguments []types.Type) bool {
	return a.checkParameterizedArgumentsFrom(node, parameters, arguments, 0)
}

func (a *Analyser) checkParameterizedArgumentsFrom(node parser.Node, parameters []types.TypeParameter, arguments []types.Type, start int) bool {
	if len(parameters) != len(arguments) {
		a.errorf(node, "parameterized binding expects %d type arguments, got %d", len(parameters), len(arguments))
		return false
	}
	valid := true
	for i, parameter := range parameters {
		if types.HasError(arguments[i]) || types.HasError(parameter.Constraint) {
			valid = false
			continue
		}
		if i < start {
			continue
		}
		if parameter.ValueType != nil {
			switch argument := arguments[i].(type) {
			case types.ConstantType:
				if !argument.ValueType.Equals(parameter.ValueType) {
					a.errorf(node, "compile-time value for %s must have type %v", parameter.Name, parameter.ValueType)
					valid = false
				}
			case types.TypeParameter:
				if argument.ValueType == nil || !argument.ValueType.Equals(parameter.ValueType) {
					a.errorf(node, "compile-time value for %s must have type %v", parameter.Name, parameter.ValueType)
					valid = false
				}
			default:
				a.errorf(node, "compile-time value for %s is not a known scalar", parameter.Name)
				valid = false
			}
			continue
		}
		if parameter.Constraint == nil {
			continue
		}
		if argumentParameter, ok := arguments[i].(types.TypeParameter); ok {
			if argumentParameter.Constraint != nil && argumentParameter.Constraint.Equals(parameter.Constraint) {
				continue
			}
			a.errorf(node, "type parameter %s does not satisfy constraint %v for %s", argumentParameter.Name, parameter.Constraint, parameter.Name)
			valid = false
			continue
		}
		trait, ok := types.Underlying(parameter.Constraint).(types.TraitType)
		if !ok {
			valid = false
			continue
		}
		target := types.TraitPointerType{Trait: trait}
		probe := types.PointerType{Base: arguments[i]}
		if _, conforms := a.structuralConformance(probe, target, node); !conforms {
			a.errorf(node, "type argument %v does not satisfy constraint %v for %s", arguments[i], parameter.Constraint, parameter.Name)
			valid = false
		}
	}
	return valid
}

func (a *Analyser) withDefinitionContext(module string, trusted bool, bindings map[string]types.Type, action func()) {
	previousScope, previousModule := a.current, a.currentMod
	previousTrusted, previousImports := a.currentTrustedStandardLibrary, a.currentImports
	previousImportAliases := a.currentImportAliases
	previousAliases, previousBindings := a.aliases, a.typeParameterBindings
	previousTraitContext := a.resolvingTraitMethodTypes
	a.currentMod = module
	a.current = a.modules[module].Scope
	a.currentTrustedStandardLibrary = trusted
	a.currentImports = a.importsByModule[module]
	a.currentImportAliases = a.importAliasesByModule[module]
	a.aliases = a.aliasesByModule[module]
	a.typeParameterBindings = bindings
	a.resolvingTraitMethodTypes = false
	action()
	a.current, a.currentMod = previousScope, previousModule
	a.currentTrustedStandardLibrary, a.currentImports = previousTrusted, previousImports
	a.currentImportAliases = previousImportAliases
	a.aliases, a.typeParameterBindings = previousAliases, previousBindings
	a.resolvingTraitMethodTypes = previousTraitContext
}

func (a *Analyser) specializeParameterizedAlias(info *parameterizedAliasInfo, arguments []types.Type, use parser.Node, indirect bool) *symbols.Symbol {
	if info == nil {
		a.errorf(use, "parameterized type metadata is unavailable")
		return nil
	}
	arguments = append([]types.Type(nil), arguments...)
	for index, parameter := range info.parameters {
		if index >= len(arguments) || parameter.ValueType == nil {
			continue
		}
		if constant, ok := arguments[index].(types.ConstantType); ok {
			if _, untyped := constant.ValueType.(types.UntypedInt); untyped {
				constant.ValueType = parameter.ValueType
				arguments[index] = constant
			}
		}
	}
	if !a.checkParameterizedArguments(use, info.parameters, arguments) {
		return nil
	}
	key := specializationKey(arguments)
	displayName := specializationDisplayName(info.node.Name, arguments)
	if existing := info.specializations[key]; existing != nil {
		if existing.state == aliasResolving {
			if indirect {
				if _, ok := info.node.Type.(*parser.TraitTypeNode); ok && !info.node.Transparent {
					return &symbols.Symbol{
						Name: displayName, Kind: symbols.SymbolKindType,
						TypeInfo: types.TraitType{Module: info.module, Name: displayName},
					}
				}
				return &symbols.Symbol{
					Name: displayName,
					Kind: symbols.SymbolKindType,
					TypeInfo: &types.AliasRef{
						Module: info.module, Name: displayName,
						Target: &existing.resolved,
					},
				}
			}
			a.errorf(use, "circular parameterized type definition detected")
			return nil
		}
		return existing.symbol
	}
	specialization := &parameterizedAliasSpecialization{state: aliasResolving}
	info.specializations[key] = specialization
	bindings := typeArgumentBindings(info.parameters, arguments)
	var selectionError error
	a.withDefinitionContext(info.module, a.modules[info.module].TrustedStandardLibrary, bindings, func() {
		selectedType := info.node.Type
		if info.node.DeferredStage {
			if a.deferredTypeSelector == nil {
				selectionError = fmt.Errorf("specialization-dependent type selection is unavailable")
				return
			}
			clone := parser.CloneSyntax(info.node).(*parser.TypeAliasNode)
			clone.Symbol = info.node.Symbol
			stageBindings := make(map[string]types.Type, len(info.parameters))
			for name, binding := range bindings {
				stageBindings[name] = binding
			}
			for i, parameter := range info.parameters {
				if parameter.ValueType != nil {
					stageBindings[parameter.Name] = arguments[i]
				}
			}
			selectedType, selectionError = a.deferredTypeSelector(clone, info.root, stageBindings)
			if selectionError != nil {
				return
			}
		}
		resolved := a.resolveTypeNodeAt(selectedType, indirect)
		if trait, ok := resolved.(types.TraitType); ok && !info.node.Transparent {
			trait.Module, trait.Name = info.module, displayName
			resolved = trait
			a.nominalTraits[trait.Module+":"+trait.Name] = trait
			a.registerTraitDefaults(info.node, trait, info.root)
		} else if !info.node.Transparent {
			resolved = types.DefinedType{
				Module: info.module, Name: displayName, Underlying: resolved,
				ParameterizedName: info.node.Name, TypeArguments: append([]types.Type(nil), arguments...),
			}
			a.concreteTypes[info.module+":"+displayName] = resolved
		}
		specialization.resolved = resolved
		underlying := types.Underlying(resolved)
		if types.HasError(resolved) {
			// Type resolution already reported the primary error.
		} else if info.node.Transparent && types.IsOpaque(resolved) {
			a.errorf(info.node, "opaque parameterized type %q cannot be a transparent alias", info.node.Name)
		} else if _, trait := underlying.(types.TraitType); trait {
			if info.node.Transparent {
				a.errorf(info.node, "parameterized trait %q cannot be a transparent alias", info.node.Name)
			}
		} else if _, opaque := underlying.(types.OpaqueType); !opaque && !types.IsComplete(underlying) {
			a.errorf(info.node, "parameterized type %q contains an incomplete type by value", info.node.Name)
		}
	})
	if selectionError != nil {
		a.errorf(use, "%v", selectionError)
		delete(info.specializations, key)
		return nil
	}
	specialization.state = aliasResolved
	specialization.symbol = &symbols.Symbol{
		Name: displayName, Kind: symbols.SymbolKindType, TypeInfo: specialization.resolved,
		Public: info.node.Pub, DefinitionModule: info.module, TemplateSymbol: info.node.Symbol,
		TypeArguments: append([]types.Type(nil), arguments...),
	}
	return specialization.symbol
}

func (a *Analyser) specializeParameterizedValue(template *symbols.Symbol, arguments []types.Type, use parser.Node) *symbols.Symbol {
	info := a.parameterizedValues[template]
	if info == nil || !a.checkParameterizedArguments(use, template.ParameterizedParameters, arguments) {
		return nil
	}
	key := specializationKey(arguments)
	if existing := info.specializations[key]; existing != nil {
		return existing.Symbol
	}
	clone := parser.CloneSyntax(info.node).(*parser.DeclarationNode)
	clone.ParameterizedParameters = nil
	clone.Name = SpecializationName(info.module, template.Name, arguments)
	info.specializations[key] = clone
	info.root.Body = append(info.root.Body, clone)
	bindings := typeArgumentBindings(template.ParameterizedParameters, arguments)
	a.withDefinitionContext(info.module, info.trusted, bindings, func() {
		a.collectGlobalVariable(clone)
		if clone.Symbol != nil {
			clone.Symbol.TemplateSymbol = template
			clone.Symbol.TypeArguments = append([]types.Type(nil), arguments...)
		}
		if clone.Value != nil {
			a.visitExpression(clone.Value)
		}
	})
	return clone.Symbol
}

func (a *Analyser) specializeParameterizedFunction(template *symbols.Symbol, arguments []types.Type, use parser.Node) *parser.FunctionDefNode {
	if template.TraitDefaultTemplate != nil {
		arguments = append([]types.Type{template.TraitDefaultSelf}, arguments...)
		template = template.TraitDefaultTemplate
	}
	info := a.parameterizedFunctions[template]
	checkFrom := 0
	if template.TraitDefault {
		checkFrom = 1
	}
	if info == nil || !a.checkParameterizedArgumentsFrom(use, template.ParameterizedParameters, arguments, checkFrom) {
		return nil
	}
	key := specializationKey(arguments)
	if existing := info.specializations[key]; existing != nil {
		return existing
	}
	substitutions := typeSubstitutionBindings(template.ParameterizedParameters, arguments)
	name := SpecializationName(info.module, template.Name, arguments)
	instanceSymbol := a.substituteSymbol(template, substitutions, use)
	instanceSymbol.Name = name
	instanceSymbol.Template = false
	instanceSymbol.TemplateSymbol = template
	instanceSymbol.TypeArguments = append([]types.Type(nil), arguments...)
	instanceSymbol.ParameterizedParameters = nil

	instance := &parser.FunctionDefNode{Symbol: instanceSymbol}
	info.specializations[key] = instance
	if info.node.DeferredStage {
		if a.deferredStageSelector == nil {
			a.errorf(use, "specialization-dependent compile-time selection is unavailable")
			return nil
		}
		cloned := parser.CloneSyntax(info.node).(*parser.FunctionDefNode)
		cloned.Symbol = instanceSymbol
		cloned.Name = name
		cloned.ParameterizedParameters = nil
		cloned.MethodOwnerParameterizedParameters = nil
		cloned.MethodOwnerType = nil
		cloned.DeferredStage = false
		cloned.MethodOwner = ""
		bindings := typeArgumentBindings(template.ParameterizedParameters, arguments)
		stageBindings := make(map[string]types.Type, len(bindings)+len(arguments))
		for name, binding := range bindings {
			stageBindings[name] = binding
		}
		for i, parameter := range template.ParameterizedParameters {
			if parameter.ValueType != nil {
				stageBindings[parameter.Name] = arguments[i]
			}
		}
		var selectionError error
		trusted := a.modules[info.module].TrustedStandardLibrary
		a.withDefinitionContext(info.module, trusted, bindings, func() {
			selectionError = a.deferredStageSelector(cloned, info.root, stageBindings)
			if selectionError != nil {
				return
			}
			a.visitFunction(cloned)
			attributor := a.NewAttributor()
			attributor.attributeNode(cloned)
			a.errors = append(a.errors, attributor.Errors()...)
			a.InferFunctionContexts(&parser.RootNode{Body: []parser.Node{cloned}})
			validator := a.NewValidator()
			validator.validateNode(cloned)
			a.errors = append(a.errors, validator.Errors()...)
		})
		if selectionError != nil {
			a.errors = append(a.errors, selectionError)
			delete(info.specializations, key)
			return nil
		}
		cloned.ParameterizedInstance = true
		*instance = *cloned
		info.root.Body = append(info.root.Body, instance)
		return instance
	}

	symbolsByOriginal := map[*symbols.Symbol]*symbols.Symbol{template: instanceSymbol}
	mapSymbol := func(original *symbols.Symbol) *symbols.Symbol {
		return a.instantiateFunctionSymbol(original, substitutions, symbolsByOriginal, use)
	}
	cloned := parser.CloneSemantic(info.node, func(t types.Type) types.Type {
		return a.instantiateType(t, substitutions, use)
	}, mapSymbol, func(node parser.Node) {
		a.instantiateSemanticNode(node)
		if lambda, ok := node.(*parser.LambdaNode); ok && lambda.Function != nil && lambda.Function.Symbol != nil {
			lambda.Function.Symbol.Name += "__" + instanceSymbol.Name
			lambda.Function.Name = lambda.Function.Symbol.Name
		}
	}).(*parser.FunctionDefNode)
	cloned.ParameterizedParameters = nil
	cloned.MethodOwnerParameterizedParameters = nil
	cloned.MethodOwnerType = nil
	cloned.ParameterizedInstance = true
	cloned.Name = name
	cloned.Symbol = instanceSymbol
	if cloned.MethodOwner != "" {
		cloned.MethodOwner = ""
	}
	*instance = *cloned
	info.root.Body = append(info.root.Body, instance)
	return instance
}

func (a *Analyser) specializeParameterizedFunctionSymbol(template *symbols.Symbol, arguments []types.Type, use parser.Node) *symbols.Symbol {
	if a.parameterizedFunctions[template] == nil {
		checkFrom := 0
		if template.TraitDefault {
			checkFrom = 1
		}
		if !a.checkParameterizedArgumentsFrom(use, template.ParameterizedParameters, arguments, checkFrom) {
			return nil
		}
		return dependentParameterizedFunctionSymbol(template, arguments)
	}
	instance := a.specializeParameterizedFunction(template, arguments, use)
	if instance == nil {
		return nil
	}
	return instance.Symbol
}

func (a *Analyser) instantiateSemanticNode(node parser.Node) {
	cast, ok := node.(*parser.CastNode)
	if !ok {
		return
	}
	targetType := cast.Type
	if cast.Checked {
		targetType = cast.CheckedType
	}
	if types.HasError(cast.Operand.GetType()) || types.HasError(targetType) {
		cast.SetType(types.ErrorType{})
		return
	}
	if cast.TraitConversion {
		if target, ok := a.traitPointer(targetType); ok {
			methods, conforms := a.structuralConformance(cast.Operand.GetType(), target, cast)
			if !conforms {
				a.errorf(cast, "type %v does not conform to %v", cast.ConcreteType, target.Trait)
			} else {
				pointer, _ := concretePointer(cast.Operand.GetType())
				cast.ConcreteType = pointer.Base
				cast.TraitMethods = methods
			}
		}
	}
	if cast.ParameterizedAssertion {
		target := targetType
		source := cast.Operand.GetType()
		if specializedExplicitCast(cast.Operand, source, target) {
			// A cast involving symbolic types may have been conservatively
			// attributed as an assertion. Once both sides are concrete, use the
			// same cast semantics as the equivalent non-parameterized expression.
			cast.ParameterizedAssertion = false
			cast.AssertionMatches = false
		} else {
			cast.AssertionMatches = source.Equals(target)
		}
	}
	if cast.StaticTraitView == nil {
		return
	}
	source := cast.Operand.GetType()
	pointer, _ := types.Underlying(source).(types.PointerType)
	_, _, sourceIsPointer, _ := methodOwnerIdentity(source)
	var concrete types.Type
	var probe types.PointerType
	if cast.StaticTraitView.Access == types.TraitReceiverValue {
		if sourceIsPointer {
			concrete = pointer.Base
			probe = pointer
		} else {
			concrete = source
			probe = types.PointerType{Base: source}
		}
	} else {
		concrete = pointer.Base
		probe = pointer
	}
	target := types.TraitPointerType{
		Trait:   cast.StaticTraitView.Trait,
		Mutable: cast.StaticTraitView.Access == types.TraitReceiverMutablePointer,
	}
	methods, conforms := a.structuralConformance(probe, target, cast)
	cast.ConcreteType = concrete
	cast.AssertionMatches = conforms
	cast.TraitMethods = methods
}

func specializedExplicitCast(operand parser.ExpressionNode, source, target types.Type) bool {
	if !types.CanExplicitCast(source, target) {
		return false
	}

	// Array-to-slice casts borrow their operand. Type compatibility alone is
	// insufficient: the equivalent concrete cast must also have a suitable
	// place from which to borrow.
	if _, ok := types.Underlying(source).(types.ArrayType); ok {
		if slice, ok := types.Underlying(target).(types.SliceType); ok {
			if slice.Mutable {
				return mutableArrayPlace(operand)
			}
			switch node := operand.(type) {
			case *parser.IdentifierNode:
				return node.Symbol != nil && node.Symbol.Kind == symbols.SymbolKindVariable
			case *parser.UnaryOpNode:
				if node.Op != parser.UnaryOpDereference {
					return false
				}
				pointer, ok := types.Underlying(node.Operand.GetType()).(types.PointerType)
				return ok && !pointer.Base.Equals(types.PrimitiveVoid)
			default:
				return false
			}
		}
	}

	return true
}

func (a *Analyser) substituteSymbol(
	original *symbols.Symbol,
	substitutions map[string]types.Type,
	use parser.Node,
) *symbols.Symbol {
	result := *original
	result.Type = a.instantiateType(original.Type, substitutions, use)
	result.ParameterizedOrigin = a.instantiateType(original.ParameterizedOrigin, substitutions, use)
	result.Signature = a.instantiateFunctionSignature(original.Signature, substitutions, use)
	result.TypeInfo = a.instantiateType(original.TypeInfo, substitutions, use)
	result.MethodOwnerType = a.instantiateType(original.MethodOwnerType, substitutions, use)
	result.TypeArguments = make([]types.Type, len(original.TypeArguments))
	for i, argument := range original.TypeArguments {
		result.TypeArguments[i] = a.instantiateType(argument, substitutions, use)
	}
	if original.StaticTraitView != nil {
		view := *original.StaticTraitView
		if trait, ok := a.instantiateType(view.Trait, substitutions, use).(types.TraitType); ok {
			view.Trait = trait
		}
		result.StaticTraitView = &view
	}
	if original.TraitRequirement {
		if trait, ok := a.instantiateType(original.RequirementTrait, substitutions, use).(types.TraitType); ok {
			result.RequirementTrait = trait
		}
	}
	return &result
}

func (a *Analyser) instantiateFunctionSignature(
	signature *symbols.FunctionSignature,
	substitutions map[string]types.Type,
	use parser.Node,
) *symbols.FunctionSignature {
	if signature == nil {
		return nil
	}
	result := *signature
	result.Parameters = make([]types.Type, len(signature.Parameters))
	for i, parameter := range signature.Parameters {
		result.Parameters[i] = a.instantiateType(parameter, substitutions, use)
	}
	result.ReturnType = a.instantiateType(signature.ReturnType, substitutions, use)
	result.VariadicElement = a.instantiateType(signature.VariadicElement, substitutions, use)
	return &result
}

func (a *Analyser) instantiateType(t types.Type, substitutions map[string]types.Type, use parser.Node) types.Type {
	t = types.Substitute(t, substitutions)
	if t == nil {
		return nil
	}
	switch current := t.(type) {
	case types.DefinedType:
		for i, argument := range current.TypeArguments {
			current.TypeArguments[i] = a.instantiateType(argument, substitutions, use)
		}
		if current.ParameterizedName != "" && !hasTypeParameters(current.TypeArguments) {
			module := a.modules[current.Module]
			if module != nil {
				if template, ok := module.Scope.Resolve(current.ParameterizedName); ok {
					if info := a.parameterizedAliases[template]; info != nil {
						if specialized := a.specializeParameterizedAlias(info, current.TypeArguments, use, false); specialized != nil {
							return specialized.TypeInfo
						}
					}
				}
			}
		}
		current.Underlying = a.instantiateType(current.Underlying, substitutions, use)
		return current
	case types.PointerType:
		current.Base = a.instantiateType(current.Base, substitutions, use)
		return current
	case types.SliceType:
		current.Base = a.instantiateType(current.Base, substitutions, use)
		return current
	case types.ArrayType:
		current.Base = a.instantiateType(current.Base, substitutions, use)
		return current
	case types.StructType:
		for i := range current.Fields {
			current.Fields[i].R = a.instantiateType(current.Fields[i].R, substitutions, use)
		}
		return current
	case types.UnionType:
		for i := range current.Fields {
			current.Fields[i].R = a.instantiateType(current.Fields[i].R, substitutions, use)
		}
		return current
	case types.FunctionType:
		for i, parameter := range current.Parameters {
			current.Parameters[i] = a.instantiateType(parameter, substitutions, use)
		}
		current.ReturnType = a.instantiateType(current.ReturnType, substitutions, use)
		current.VariadicElement = a.instantiateType(current.VariadicElement, substitutions, use)
		return current
	case types.MultipleReturnType:
		for i, item := range current.Types {
			current.Types[i] = a.instantiateType(item, substitutions, use)
		}
		return current
	case types.TraitPointerType:
		if trait, ok := a.instantiateType(current.Trait, substitutions, use).(types.TraitType); ok {
			current.Trait = trait
		}
		return current
	case types.TraitType:
		for i := range current.Methods {
			for j := range current.Methods[i].ParameterizedParameters {
				current.Methods[i].ParameterizedParameters[j].Constraint = a.instantiateType(
					current.Methods[i].ParameterizedParameters[j].Constraint,
					substitutions,
					use,
				)
			}
			for j, parameter := range current.Methods[i].Parameters {
				current.Methods[i].Parameters[j] = a.instantiateType(parameter, substitutions, use)
			}
			current.Methods[i].ReturnType = a.instantiateType(current.Methods[i].ReturnType, substitutions, use)
		}
		return current
	default:
		return current
	}
}

func (a *Analyser) instantiateFunctionSymbol(
	original *symbols.Symbol,
	substitutions map[string]types.Type,
	cloned map[*symbols.Symbol]*symbols.Symbol,
	use parser.Node,
) *symbols.Symbol {
	if existing := cloned[original]; existing != nil {
		return existing
	}
	substituted := a.substituteSymbol(original, substitutions, use)
	if original.TraitRequirement {
		receiver := substituted.Signature.Parameters[0]
		if types.HasError(receiver) || types.HasError(substituted.RequirementTrait) {
			cloned[original] = substituted
			return substituted
		}
		var probe types.PointerType
		if original.RequirementAccess == types.TraitReceiverValue {
			probe = types.PointerType{Base: receiver}
		} else {
			var ok bool
			probe, ok = types.Underlying(receiver).(types.PointerType)
			if !ok {
				a.errorf(use, "cannot instantiate trait receiver %v", receiver)
				cloned[original] = substituted
				return substituted
			}
		}
		target := types.TraitPointerType{
			Trait:   substituted.RequirementTrait,
			Mutable: original.RequirementAccess == types.TraitReceiverMutablePointer,
		}
		methods, conforms := a.structuralConformance(probe, target, use)
		if !conforms || original.RequirementSlot >= len(methods) {
			a.errorf(use, "type %v does not implement %v", probe.Base, target.Trait)
			cloned[original] = substituted
			return substituted
		}
		method := methods[original.RequirementSlot]
		if method.Template {
			if len(substituted.TypeArguments) != len(method.ParameterizedParameters) || hasTypeParameters(substituted.TypeArguments) {
				a.errorf(use, "cannot resolve parameterized trait method %q type arguments", method.Name)
				cloned[original] = substituted
				return substituted
			}
			specialization := a.specializeParameterizedFunction(method, substituted.TypeArguments, use)
			if specialization == nil || specialization.Symbol == nil {
				cloned[original] = substituted
				return substituted
			}
			method = specialization.Symbol
		}
		cloned[original] = method
		return method
	}
	if original.TemplateSymbol != nil {
		if !hasTypeParameters(substituted.TypeArguments) {
			switch original.TemplateSymbol.Kind {
			case symbols.SymbolKindFunction:
				specialization := a.specializeParameterizedFunction(
					original.TemplateSymbol,
					substituted.TypeArguments,
					use,
				)
				if specialization != nil && specialization.Symbol != nil {
					cloned[original] = specialization.Symbol
					return specialization.Symbol
				}
			case symbols.SymbolKindVariable:
				if specialization := a.specializeParameterizedValue(
					original.TemplateSymbol,
					substituted.TypeArguments,
					use,
				); specialization != nil {
					cloned[original] = specialization
					return specialization
				}
			case symbols.SymbolKindType:
				if specialization := a.specializeParameterizedAlias(
					a.parameterizedAliases[original.TemplateSymbol],
					substituted.TypeArguments,
					use,
					false,
				); specialization != nil {
					cloned[original] = specialization
					return specialization
				}
			}
		}
		cloned[original] = substituted
		return substituted
	}
	if !a.isGlobalSymbol(original) {
		cloned[original] = substituted
		return substituted
	}
	return original
}

func (a *Analyser) isGlobalSymbol(symbol *symbols.Symbol) bool {
	for _, candidate := range a.universe.Symbols {
		if candidate == symbol {
			return true
		}
	}
	for _, module := range a.modules {
		for _, candidate := range module.Scope.Symbols {
			if candidate == symbol {
				return true
			}
		}
	}
	return false
}

func inferParameterizedArguments(
	parameters []types.TypeParameter,
	patterns, actuals []types.Type,
	typedVariadic, variadicExpansion bool,
) ([]types.Type, error) {
	return inferParameterizedArgumentsForCall(parameters, patterns, actuals, nil, nil, typedVariadic, variadicExpansion, false)
}

func inferParameterizedArgumentsPartial(
	parameters []types.TypeParameter,
	patterns, actuals []types.Type,
	typedVariadic, variadicExpansion bool,
) ([]types.Type, error) {
	return inferParameterizedArgumentsForCall(parameters, patterns, actuals, nil, nil, typedVariadic, variadicExpansion, true)
}

// preserveResolvedTypeArguments keeps explicit or argument-derived choices
// fixed while inference fills parameter slots that are still symbolic.
func preserveResolvedTypeArguments(parameters []types.TypeParameter, fixed, inferred []types.Type) []types.Type {
	for i := range inferred {
		if i < len(fixed) && i < len(parameters) && fixed[i] != nil && !fixed[i].Equals(parameters[i]) {
			inferred[i] = fixed[i]
		}
	}
	return inferred
}

func inferParameterizedArgumentsForCall(
	parameters []types.TypeParameter,
	patterns, actuals []types.Type,
	resultPattern, expectedResult types.Type,
	typedVariadic, variadicExpansion, allowIncomplete bool,
) ([]types.Type, error) {
	inferred := make(map[string]types.Type)
	for i, actual := range actuals {
		if len(patterns) == 0 {
			break
		}
		patternIndex := i
		if patternIndex >= len(patterns) {
			if !typedVariadic {
				break
			}
			patternIndex = len(patterns) - 1
		}
		pattern := patterns[patternIndex]
		if typedVariadic && patternIndex == len(patterns)-1 && !variadicExpansion {
			if slice, ok := types.Underlying(pattern).(types.SliceType); ok {
				pattern = slice.Base
			}
		}
		if err := inferParameterizedType(pattern, actual, inferred); err != nil {
			return nil, err
		}
	}
	if resultPattern != nil && expectedResult != nil {
		inferMissingParameterizedType(resultPattern, expectedResult, inferred)
	}
	arguments := make([]types.Type, len(parameters))
	for i, parameter := range parameters {
		if parameter.ValueType != nil {
			arguments[i] = parameter
			continue
		}
		argument, ok := inferred[parameter.Key()]
		if !ok {
			if allowIncomplete {
				arguments[i] = parameter
				continue
			}
			return nil, fmt.Errorf("cannot infer type argument %s", parameter.Name)
		}
		if types.HasUntyped(argument) {
			if allowIncomplete {
				arguments[i] = parameter
				continue
			}
			return nil, fmt.Errorf("untyped numeric value cannot infer type argument %s", parameter.Name)
		}
		arguments[i] = argument
	}
	return arguments, nil
}

func inferMissingParameterizedType(pattern, actual types.Type, inferred map[string]types.Type) {
	if parameter, ok := pattern.(types.TypeParameter); ok {
		if previous, exists := inferred[parameter.Key()]; !exists || types.HasUntyped(previous) {
			if !types.HasUntyped(actual) {
				inferred[parameter.Key()] = actual
			}
		}
		return
	}
	switch pattern := pattern.(type) {
	case types.PointerType:
		if actual, ok := actual.(types.PointerType); ok {
			inferMissingParameterizedType(pattern.Base, actual.Base, inferred)
		}
	case types.SliceType:
		if actual, ok := actual.(types.SliceType); ok {
			inferMissingParameterizedType(pattern.Base, actual.Base, inferred)
		}
	case types.ArrayType:
		if actual, ok := actual.(types.ArrayType); ok && pattern.Length == actual.Length {
			inferMissingParameterizedType(pattern.Base, actual.Base, inferred)
		}
	case types.DefinedType:
		actual, ok := actual.(types.DefinedType)
		if !ok || pattern.Module != actual.Module || pattern.ParameterizedName == "" || pattern.ParameterizedName != actual.ParameterizedName ||
			len(pattern.TypeArguments) != len(actual.TypeArguments) {
			return
		}
		for i := range pattern.TypeArguments {
			inferMissingParameterizedType(pattern.TypeArguments[i], actual.TypeArguments[i], inferred)
		}
	case types.FunctionType:
		actual, ok := actual.(types.FunctionType)
		if !ok || len(pattern.Parameters) != len(actual.Parameters) {
			return
		}
		for i := range pattern.Parameters {
			inferMissingParameterizedType(pattern.Parameters[i], actual.Parameters[i], inferred)
		}
		inferMissingParameterizedType(pattern.ReturnType, actual.ReturnType, inferred)
	case types.MultipleReturnType:
		actual, ok := actual.(types.MultipleReturnType)
		if !ok || len(pattern.Types) != len(actual.Types) {
			return
		}
		for i := range pattern.Types {
			inferMissingParameterizedType(pattern.Types[i], actual.Types[i], inferred)
		}
	}
}

func inferParameterizedType(pattern, actual types.Type, inferred map[string]types.Type) error {
	if parameter, ok := pattern.(types.TypeParameter); ok {
		if _, ambiguous := actual.(types.SequenceType); ambiguous {
			return nil
		}
		if types.HasUntyped(actual) {
			if previous, exists := inferred[parameter.Key()]; !exists || types.HasUntyped(previous) {
				inferred[parameter.Key()] = actual
			}
			// A typed argument may replace or already have replaced this
			// placeholder; validation then performs the numeric coercion.
			return nil
		}
		if previous, exists := inferred[parameter.Key()]; exists {
			if types.HasUntyped(previous) {
				inferred[parameter.Key()] = actual
				return nil
			}
			if !previous.Equals(actual) {
				return fmt.Errorf("conflicting inferred types for %s: %v and %v", parameter.Name, previous, actual)
			}
		}
		inferred[parameter.Key()] = actual
		return nil
	}
	switch pattern := pattern.(type) {
	case types.PointerType:
		actual, ok := actual.(types.PointerType)
		if !ok {
			return nil
		}
		return inferParameterizedType(pattern.Base, actual.Base, inferred)
	case types.SliceType:
		switch actual := actual.(type) {
		case types.SliceType:
			return inferParameterizedType(pattern.Base, actual.Base, inferred)
		case types.SequenceType:
			return inferParameterizedType(pattern.Base, actual.Base, inferred)
		}
		return nil
	case types.ArrayType:
		switch actual := actual.(type) {
		case types.ArrayType:
			if pattern.Length == actual.Length {
				return inferParameterizedType(pattern.Base, actual.Base, inferred)
			}
		case types.SequenceType:
			if pattern.Length == actual.Length {
				return inferParameterizedType(pattern.Base, actual.Base, inferred)
			}
		}
		return nil
	case types.DefinedType:
		actual, ok := actual.(types.DefinedType)
		if !ok || pattern.Module != actual.Module || pattern.ParameterizedName == "" || pattern.ParameterizedName != actual.ParameterizedName ||
			len(pattern.TypeArguments) != len(actual.TypeArguments) {
			return nil
		}
		for i := range pattern.TypeArguments {
			if err := inferParameterizedType(pattern.TypeArguments[i], actual.TypeArguments[i], inferred); err != nil {
				return err
			}
		}
	case types.FunctionType:
		actual, ok := actual.(types.FunctionType)
		if !ok || len(pattern.Parameters) != len(actual.Parameters) {
			return nil
		}
		for i := range pattern.Parameters {
			if err := inferParameterizedType(pattern.Parameters[i], actual.Parameters[i], inferred); err != nil {
				return err
			}
		}
		return inferParameterizedType(pattern.ReturnType, actual.ReturnType, inferred)
	}
	return nil
}

func expressionTypes(expressions []parser.ExpressionNode) []types.Type {
	result := make([]types.Type, len(expressions))
	for i, expression := range expressions {
		result[i] = expression.GetType()
	}
	return result
}

func (a *Attributor) attributeParameterizedSpecialization(template *symbols.Symbol, arguments []types.Type, use parser.Node) *parser.FunctionDefNode {
	specialization := a.analyser.specializeParameterizedFunction(template, arguments, use)
	return specialization
}

func (a *Attributor) enterSpecialization(symbol *symbols.Symbol) func() {
	previous := a.analyser.typeParameterBindings
	if symbol != nil && symbol.TemplateSymbol != nil {
		a.analyser.typeParameterBindings = typeArgumentBindings(symbol.TemplateSymbol.ParameterizedParameters, symbol.TypeArguments)
	}
	return func() {
		a.analyser.typeParameterBindings = previous
	}
}
