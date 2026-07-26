package sema

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

type genericFunctionInfo struct {
	node            *parser.FunctionDefNode
	root            *parser.RootNode
	module          string
	specializations map[string]*parser.FunctionDefNode
}

type genericValueInfo struct {
	node            *parser.DeclarationNode
	root            *parser.RootNode
	module          string
	trusted         bool
	specializations map[string]*parser.DeclarationNode
}

type genericAliasSpecialization struct {
	state    aliasState
	resolved types.Type
	symbol   *symbols.Symbol
}

type genericAliasInfo struct {
	node            *parser.TypeAliasNode
	module          string
	parameters      []types.TypeParameter
	specializations map[string]*genericAliasSpecialization
}

func genericOwner(module, name string) string {
	return module + ":" + name
}

func specializationKey(arguments []types.Type) string {
	parts := make([]string, len(arguments))
	for i, argument := range arguments {
		parts[i] = types.Identity(argument)
	}
	return strings.Join(parts, ",")
}

func specializationName(module, name string, arguments []types.Type) string {
	identity := genericOwner(module, name) + "<" + specializationKey(arguments) + ">"
	return name + "$" + fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}

func specializationDisplayName(name string, arguments []types.Type) string {
	parts := make([]string, len(arguments))
	for i, argument := range arguments {
		parts[i] = argument.String()
	}
	return name + "<" + strings.Join(parts, ", ") + ">"
}

func (a *Analyser) makeGenericParameters(name string, nodes []parser.GenericParameterNode) []types.TypeParameter {
	previous := a.typeParameterBindings
	bindings := make(map[string]types.Type, len(previous)+len(nodes))
	for name, binding := range previous {
		bindings[name] = binding
	}
	a.typeParameterBindings = bindings
	parameters := make([]types.TypeParameter, len(nodes))
	owner := genericOwner(a.currentMod, name)
	for i, node := range nodes {
		parameter := types.TypeParameter{Owner: owner, Name: node.Name, Index: i}
		parameters[i] = parameter
		bindings[node.Name] = parameter
	}
	for i, node := range nodes {
		if node.Constraint == nil {
			continue
		}
		constraint := a.resolveTypeNode(node.Constraint)
		if _, ok := types.Underlying(constraint).(types.TraitType); !ok {
			a.errorf(node, "generic constraint must be a trait type, got %v", constraint)
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
		bindings[parameter.Name] = arguments[i]
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

func dependentGenericFunctionSymbol(template *symbols.Symbol, arguments []types.Type) *symbols.Symbol {
	result := *template
	result.Template = false
	result.TemplateSymbol = template
	result.TypeArguments = append([]types.Type(nil), arguments...)
	result.GenericParameters = nil
	result.Signature = substituteFunctionSignature(
		template.Signature,
		typeSubstitutionBindings(template.GenericParameters, arguments),
	)
	return &result
}

func staticTraitViewForGenericType(t types.Type) *types.StaticTraitView {
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

func genericTypeParameterBase(t types.Type) (types.TypeParameter, bool) {
	if pointer, ok := t.(types.PointerType); ok {
		t = pointer.Base
	}
	parameter, ok := t.(types.TypeParameter)
	return parameter, ok
}

func (a *Analyser) resolveGenericArguments(nodes []parser.TypeNode) []types.Type {
	arguments := make([]types.Type, len(nodes))
	for i, node := range nodes {
		arguments[i] = a.resolveTypeNode(node)
	}
	return arguments
}

func (a *Analyser) checkGenericArguments(node parser.Node, parameters []types.TypeParameter, arguments []types.Type) bool {
	if len(parameters) != len(arguments) {
		a.errorf(node, "generic binding expects %d type arguments, got %d", len(parameters), len(arguments))
		return false
	}
	valid := true
	for i, parameter := range parameters {
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
	previousAliases, previousBindings := a.aliases, a.typeParameterBindings
	previousTraitContext := a.resolvingTraitMethodTypes
	a.currentMod = module
	a.current = a.modules[module].Scope
	a.currentTrustedStandardLibrary = trusted
	a.currentImports = a.importsByModule[module]
	a.aliases = a.aliasesByModule[module]
	a.typeParameterBindings = bindings
	a.resolvingTraitMethodTypes = false
	action()
	a.current, a.currentMod = previousScope, previousModule
	a.currentTrustedStandardLibrary, a.currentImports = previousTrusted, previousImports
	a.aliases, a.typeParameterBindings = previousAliases, previousBindings
	a.resolvingTraitMethodTypes = previousTraitContext
}

func (a *Analyser) specializeGenericAlias(info *genericAliasInfo, arguments []types.Type, use parser.Node, indirect bool) *symbols.Symbol {
	if info == nil {
		a.errorf(use, "generic type metadata is unavailable")
		return nil
	}
	if !a.checkGenericArguments(use, info.parameters, arguments) {
		return nil
	}
	key := specializationKey(arguments)
	if existing := info.specializations[key]; existing != nil {
		if existing.state == aliasResolving {
			if indirect {
				return &symbols.Symbol{
					Name: specializationDisplayName(info.node.Name, arguments),
					Kind: symbols.SymbolKindType,
					TypeInfo: &types.AliasRef{
						Module: info.module, Name: specializationDisplayName(info.node.Name, arguments),
						Target: &existing.resolved,
					},
				}
			}
			a.errorf(use, "circular generic type definition detected")
			return nil
		}
		return existing.symbol
	}
	displayName := specializationDisplayName(info.node.Name, arguments)
	specialization := &genericAliasSpecialization{state: aliasResolving}
	info.specializations[key] = specialization
	bindings := typeArgumentBindings(info.parameters, arguments)
	a.withDefinitionContext(info.module, a.modules[info.module].TrustedStandardLibrary, bindings, func() {
		resolved := a.resolveTypeNodeAt(info.node.Type, false)
		if trait, ok := resolved.(types.TraitType); ok && !info.node.Transparent {
			trait.Module, trait.Name = info.module, displayName
			resolved = trait
		} else if !info.node.Transparent {
			resolved = types.DefinedType{
				Module: info.module, Name: displayName, Underlying: resolved,
				GenericName: info.node.Name, TypeArguments: append([]types.Type(nil), arguments...),
			}
			a.concreteTypes[info.module+":"+displayName] = resolved
		}
		specialization.resolved = resolved
		underlying := types.Underlying(resolved)
		if info.node.Transparent && types.IsOpaque(resolved) {
			a.errorf(info.node, "opaque generic type %q cannot be a transparent alias", info.node.Name)
		} else if _, trait := underlying.(types.TraitType); trait {
			if info.node.Transparent {
				a.errorf(info.node, "generic trait %q cannot be a transparent alias", info.node.Name)
			}
		} else if _, opaque := underlying.(types.OpaqueType); !opaque && !types.IsComplete(underlying) {
			a.errorf(info.node, "generic type %q contains an incomplete type by value", info.node.Name)
		}
	})
	specialization.state = aliasResolved
	specialization.symbol = &symbols.Symbol{
		Name: displayName, Kind: symbols.SymbolKindType, TypeInfo: specialization.resolved,
		Public: info.node.Pub, DefinitionModule: info.module, TemplateSymbol: info.node.Symbol,
		TypeArguments: append([]types.Type(nil), arguments...),
	}
	return specialization.symbol
}

func (a *Analyser) specializeGenericValue(template *symbols.Symbol, arguments []types.Type, use parser.Node) *symbols.Symbol {
	info := a.genericValues[template]
	if info == nil || !a.checkGenericArguments(use, template.GenericParameters, arguments) {
		return nil
	}
	key := specializationKey(arguments)
	if existing := info.specializations[key]; existing != nil {
		return existing.Symbol
	}
	clone := parser.CloneSyntax(info.node).(*parser.DeclarationNode)
	clone.GenericParameters = nil
	clone.Name = specializationName(info.module, template.Name, arguments)
	info.specializations[key] = clone
	info.root.Body = append(info.root.Body, clone)
	bindings := typeArgumentBindings(template.GenericParameters, arguments)
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

func (a *Analyser) specializeGenericFunction(template *symbols.Symbol, arguments []types.Type, use parser.Node) *parser.FunctionDefNode {
	info := a.genericFunctions[template]
	if info == nil || !a.checkGenericArguments(use, template.GenericParameters, arguments) {
		return nil
	}
	key := specializationKey(arguments)
	if existing := info.specializations[key]; existing != nil {
		return existing
	}
	substitutions := typeSubstitutionBindings(template.GenericParameters, arguments)
	name := specializationName(info.module, template.Name, arguments)
	instanceSymbol := a.substituteSymbol(template, substitutions, use)
	instanceSymbol.Name = name
	instanceSymbol.Template = false
	instanceSymbol.TemplateSymbol = template
	instanceSymbol.TypeArguments = append([]types.Type(nil), arguments...)
	instanceSymbol.GenericParameters = nil

	instance := &parser.FunctionDefNode{Symbol: instanceSymbol}
	info.specializations[key] = instance

	symbolsByOriginal := map[*symbols.Symbol]*symbols.Symbol{template: instanceSymbol}
	mapSymbol := func(original *symbols.Symbol) *symbols.Symbol {
		return a.instantiateFunctionSymbol(original, substitutions, symbolsByOriginal, use)
	}
	cloned := parser.CloneSemantic(info.node, func(t types.Type) types.Type {
		return a.instantiateType(t, substitutions, use)
	}, mapSymbol, func(node parser.Node) {
		a.instantiateSemanticNode(node)
	}).(*parser.FunctionDefNode)
	cloned.GenericParameters = nil
	cloned.MethodOwnerGenericParameters = nil
	cloned.GenericInstance = true
	cloned.Name = name
	cloned.Symbol = instanceSymbol
	if cloned.MethodOwner != "" {
		cloned.MethodOwner = ""
	}
	*instance = *cloned
	info.root.Body = append(info.root.Body, instance)
	return instance
}

func (a *Analyser) instantiateSemanticNode(node parser.Node) {
	cast, ok := node.(*parser.CastNode)
	if !ok {
		return
	}
	if cast.TraitConversion {
		targetType := cast.Type
		if cast.Checked {
			targetType = cast.CheckedType
		}
		if target, ok := traitPointer(targetType); ok {
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
	if cast.GenericAssertion {
		target := cast.Type
		if cast.Checked {
			target = cast.CheckedType
		}
		cast.AssertionMatches = cast.Operand.GetType().Equals(target)
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

func (a *Analyser) substituteSymbol(
	original *symbols.Symbol,
	substitutions map[string]types.Type,
	use parser.Node,
) *symbols.Symbol {
	result := *original
	result.Type = a.instantiateType(original.Type, substitutions, use)
	result.GenericOrigin = a.instantiateType(original.GenericOrigin, substitutions, use)
	result.Signature = a.instantiateFunctionSignature(original.Signature, substitutions, use)
	result.TypeInfo = a.instantiateType(original.TypeInfo, substitutions, use)
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
		if current.GenericName != "" && !hasTypeParameters(current.TypeArguments) {
			module := a.modules[current.Module]
			if module != nil {
				if template, ok := module.Scope.Resolve(current.GenericName); ok {
					if info := a.genericAliases[template]; info != nil {
						if specialized := a.specializeGenericAlias(info, current.TypeArguments, use, false); specialized != nil {
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
			for j := range current.Methods[i].GenericParameters {
				current.Methods[i].GenericParameters[j].Constraint = a.instantiateType(
					current.Methods[i].GenericParameters[j].Constraint,
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
			if len(substituted.TypeArguments) != len(method.GenericParameters) || hasTypeParameters(substituted.TypeArguments) {
				a.errorf(use, "cannot resolve generic trait method %q type arguments", method.Name)
				cloned[original] = substituted
				return substituted
			}
			specialization := a.specializeGenericFunction(method, substituted.TypeArguments, use)
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
				specialization := a.specializeGenericFunction(
					original.TemplateSymbol,
					substituted.TypeArguments,
					use,
				)
				if specialization != nil && specialization.Symbol != nil {
					cloned[original] = specialization.Symbol
					return specialization.Symbol
				}
			case symbols.SymbolKindVariable:
				if specialization := a.specializeGenericValue(
					original.TemplateSymbol,
					substituted.TypeArguments,
					use,
				); specialization != nil {
					cloned[original] = specialization
					return specialization
				}
			case symbols.SymbolKindType:
				if specialization := a.specializeGenericAlias(
					a.genericAliases[original.TemplateSymbol],
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

func inferGenericArguments(
	parameters []types.TypeParameter,
	patterns, actuals []types.Type,
	typedVariadic, variadicExpansion bool,
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
		if err := inferGenericType(pattern, actual, inferred); err != nil {
			return nil, err
		}
	}
	arguments := make([]types.Type, len(parameters))
	for i, parameter := range parameters {
		argument, ok := inferred[parameter.Key()]
		if !ok {
			return nil, fmt.Errorf("cannot infer type argument %s", parameter.Name)
		}
		if types.HasUntyped(argument) {
			return nil, fmt.Errorf("untyped numeric value cannot infer type argument %s", parameter.Name)
		}
		arguments[i] = argument
	}
	return arguments, nil
}

func inferGenericType(pattern, actual types.Type, inferred map[string]types.Type) error {
	if parameter, ok := pattern.(types.TypeParameter); ok {
		if types.HasUntyped(actual) {
			return fmt.Errorf("untyped numeric value cannot infer type argument %s", parameter.Name)
		}
		if previous, exists := inferred[parameter.Key()]; exists && !previous.Equals(actual) {
			return fmt.Errorf("conflicting inferred types for %s: %v and %v", parameter.Name, previous, actual)
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
		return inferGenericType(pattern.Base, actual.Base, inferred)
	case types.SliceType:
		actual, ok := actual.(types.SliceType)
		if !ok {
			return nil
		}
		return inferGenericType(pattern.Base, actual.Base, inferred)
	case types.DefinedType:
		actual, ok := actual.(types.DefinedType)
		if !ok || pattern.Module != actual.Module || pattern.GenericName == "" || pattern.GenericName != actual.GenericName ||
			len(pattern.TypeArguments) != len(actual.TypeArguments) {
			return nil
		}
		for i := range pattern.TypeArguments {
			if err := inferGenericType(pattern.TypeArguments[i], actual.TypeArguments[i], inferred); err != nil {
				return err
			}
		}
	case types.FunctionType:
		actual, ok := actual.(types.FunctionType)
		if !ok || len(pattern.Parameters) != len(actual.Parameters) {
			return nil
		}
		for i := range pattern.Parameters {
			if err := inferGenericType(pattern.Parameters[i], actual.Parameters[i], inferred); err != nil {
				return err
			}
		}
		return inferGenericType(pattern.ReturnType, actual.ReturnType, inferred)
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

func (a *Attributor) attributeGenericSpecialization(template *symbols.Symbol, arguments []types.Type, use parser.Node) *parser.FunctionDefNode {
	specialization := a.analyser.specializeGenericFunction(template, arguments, use)
	return specialization
}

func (a *Attributor) enterSpecialization(symbol *symbols.Symbol) func() {
	previous := a.analyser.typeParameterBindings
	if symbol != nil && symbol.TemplateSymbol != nil {
		a.analyser.typeParameterBindings = typeArgumentBindings(symbol.TemplateSymbol.GenericParameters, symbol.TypeArguments)
	}
	return func() {
		a.analyser.typeParameterBindings = previous
	}
}
