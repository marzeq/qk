package sema

import (
	"sort"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func (a *Analyser) traitPointer(t types.Type) (types.TraitPointerType, bool) {
	p, ok := types.Underlying(t).(types.TraitPointerType)
	if ok {
		p.Trait = a.completeTrait(p.Trait)
	}
	return p, ok
}

func (a *Analyser) completeTrait(trait types.TraitType) types.TraitType {
	if trait.Any || len(trait.Methods) != 0 || trait.Name == "" {
		return completeDirectTraitReferences(trait)
	}
	if complete, ok := a.nominalTraits[trait.Module+":"+trait.Name]; ok {
		return completeDirectTraitReferences(complete)
	}
	if module := a.modules[trait.Module]; module != nil {
		if symbol, ok := module.Scope.Resolve(trait.Name); ok {
			if complete, ok := types.Underlying(symbol.TypeInfo).(types.TraitType); ok {
				return completeDirectTraitReferences(complete)
			}
		}
	}
	return trait
}

func completeDirectTraitReferences(trait types.TraitType) types.TraitType {
	if len(trait.Methods) == 0 {
		return trait
	}
	methods := append([]types.TraitMethod(nil), trait.Methods...)
	for index := range methods {
		if pointer, ok := types.Underlying(methods[index].ReturnType).(types.TraitPointerType); ok &&
			len(pointer.Trait.Methods) == 0 && pointer.Trait.Equals(trait) {
			pointer.Trait = trait
			methods[index].ReturnType = pointer
		}
	}
	trait.Methods = methods
	return trait
}

func concretePointer(t types.Type) (types.PointerType, bool) {
	p, ok := types.Underlying(t).(types.PointerType)
	return p, ok
}

func (a *Analyser) structuralConformance(from types.Type, target types.TraitPointerType, at parser.Node) ([]*symbols.Symbol, bool) {
	target.Trait = a.completeTrait(target.Trait)
	pointer, ok := concretePointer(from)
	if !ok || (!pointer.Mutable && target.Mutable) {
		return nil, false
	}
	if target.Trait.Any {
		return nil, true
	}
	module, owner, _, ok := methodOwnerIdentity(pointer.Base)
	if !ok {
		return nil, false
	}
	methodSet := a.methods[module+":"+owner]
	selected := make([]*symbols.Symbol, len(target.Trait.Methods))
	for i, requirement := range target.Trait.Methods {
		method := methodSet[requirement.Name]
		if method == nil {
			if requirement.HasDefault {
				method = a.defaultTraitMethod(target.Trait, i, pointer.Base, at)
			}
			if method == nil {
				return nil, false
			}
		} else if method.StaticMethod {
			return nil, false
		}
		sig := method.Signature
		if len(sig.Parameters) != len(requirement.Parameters)+1 {
			return nil, false
		}
		available := types.TraitMethod{
			Name: requirement.Name, GenericParameters: method.GenericParameters,
			Receiver: method.MethodReceiver, Parameters: sig.Parameters[1:], ReturnType: sig.ReturnType,
		}
		if !traitMethodShapeMatches(requirement, available, pointer.Base) {
			return nil, false
		}
		if requirement.Receiver == types.TraitReceiverValue && !types.IsComplete(pointer.Base) {
			return nil, false
		}
		selected[i] = method
	}
	return selected, true
}

func (v *Validator) traitConversion(node parser.ExpressionNode, expected types.Type) (*parser.CastNode, bool) {
	target, ok := v.analyser.traitPointer(expected)
	if !ok {
		return nil, false
	}
	methods, conforms := v.analyser.structuralConformance(node.GetType(), target, node)
	if !conforms {
		return nil, false
	}
	pointer, _ := concretePointer(node.GetType())
	return &parser.CastNode{Operand: node, Loc: node.GetLoc(), Type: expected, TraitConversion: true, ConcreteType: pointer.Base, TraitMethods: methods}, true
}

func (a *Analyser) runtimeTraitImplementers(trait types.TraitType, at parser.Node) []types.Type {
	if trait.Any || len(trait.Methods) == 0 {
		return nil
	}
	target := types.TraitPointerType{Trait: trait}
	result := []types.Type{}
	for _, concrete := range a.sortedConcreteTypes() {
		if _, ok := a.structuralConformance(types.PointerType{Base: concrete}, target, at); ok {
			result = append(result, concrete)
		}
	}
	return result
}

func (a *Analyser) runtimeTraitCastCandidates(trait types.TraitType, mutable bool, at parser.Node) []parser.TraitCastCandidate {
	target := types.TraitPointerType{Trait: trait, Mutable: mutable}
	result := []parser.TraitCastCandidate{}
	for _, concrete := range a.sortedConcreteTypes() {
		if methods, ok := a.structuralConformance(types.PointerType{Base: concrete, Mutable: mutable}, target, at); ok {
			result = append(result, parser.TraitCastCandidate{ConcreteType: concrete, Methods: methods})
		}
	}
	return result
}

func (a *Analyser) sortedConcreteTypes() []types.Type {
	keys := make([]string, 0, len(a.concreteTypes))
	for key := range a.concreteTypes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]types.Type, 0, len(keys))
	for _, key := range keys {
		result = append(result, a.concreteTypes[key])
	}
	return result
}

func traitImplementsTrait(source, target types.TraitType) bool {
	if target.Any || len(target.Methods) == 0 {
		return true
	}
	for _, required := range target.Methods {
		found := false
		for _, available := range source.Methods {
			if available.Name != required.Name {
				continue
			}
			if !traitMethodShapeMatches(required, available, nil) {
				return false
			}
			found = true
			break
		}
		if !found && !required.HasDefault {
			return false
		}
	}
	return true
}

func traitMethodShapeMatches(required, available types.TraitMethod, self types.Type) bool {
	if available.Name != required.Name || available.Receiver != required.Receiver ||
		len(available.Parameters) != len(required.Parameters) ||
		len(available.GenericParameters) != len(required.GenericParameters) {
		return false
	}

	substitutions := make(map[string]types.Type, len(required.GenericParameters))
	for i, parameter := range required.GenericParameters {
		substitutions[parameter.Key()] = available.GenericParameters[i]
	}
	for i, parameter := range required.GenericParameters {
		requiredConstraint := substituteTraitMethodType(parameter.Constraint, substitutions, self)
		availableConstraint := available.GenericParameters[i].Constraint
		if !optionalTypesEqual(requiredConstraint, availableConstraint) {
			return false
		}
	}
	for i, parameter := range required.Parameters {
		requiredParameter := substituteTraitMethodType(parameter, substitutions, self)
		if !available.Parameters[i].Equals(requiredParameter) {
			return false
		}
	}
	requiredReturn := substituteTraitMethodType(required.ReturnType, substitutions, self)
	return optionalTypesEqual(requiredReturn, available.ReturnType)
}

func substituteTraitMethodType(t types.Type, substitutions map[string]types.Type, self types.Type) types.Type {
	t = types.Substitute(t, substitutions)
	if self != nil {
		t = types.SubstituteSelf(t, self)
	}
	return t
}

func optionalTypesEqual(left, right types.Type) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equals(right)
}
