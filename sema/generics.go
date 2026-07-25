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
	trusted         bool
	specializations map[string]*parser.FunctionDefNode
	attributed      map[string]bool
	attributing     map[string]bool
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
	bindings := make(map[string]types.Type, len(nodes))
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
	clone := parser.CloneSyntax(info.node).(*parser.FunctionDefNode)
	clone.GenericParameters = nil
	clone.Name = specializationName(info.module, template.Name, arguments)
	if clone.MethodOwner != "" {
		clone.MethodOwner = ""
	}
	info.specializations[key] = clone
	info.root.Body = append(info.root.Body, clone)
	bindings := typeArgumentBindings(template.GenericParameters, arguments)
	a.withDefinitionContext(info.module, info.trusted, bindings, func() {
		a.collectPlainFunctionSignature(clone)
		if clone.Symbol != nil {
			clone.Symbol.TemplateSymbol = template
			clone.Symbol.TypeArguments = append([]types.Type(nil), arguments...)
			clone.Symbol.Method = template.Method
			clone.Symbol.StaticMethod = template.StaticMethod
		}
		a.visitFunction(clone)
	})
	return clone
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
	info := a.analyser.genericFunctions[template]
	specialization := a.analyser.specializeGenericFunction(template, arguments, use)
	if info == nil || specialization == nil {
		return nil
	}
	key := specializationKey(arguments)
	if info.attributed[key] || info.attributing[key] {
		return specialization
	}
	info.attributing[key] = true
	bindings := typeArgumentBindings(template.GenericParameters, arguments)
	a.analyser.withDefinitionContext(info.module, info.trusted, bindings, func() {
		a.attributeNode(specialization)
	})
	delete(info.attributing, key)
	info.attributed[key] = true
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
