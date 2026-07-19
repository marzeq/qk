package sema

import (
	"sort"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func traitPointer(t types.Type) (types.TraitPointerType, bool) {
	p, ok := types.Underlying(t).(types.TraitPointerType)
	return p, ok
}

func concretePointer(t types.Type) (types.PointerType, bool) {
	p, ok := types.Underlying(t).(types.PointerType)
	return p, ok
}

func (a *Analyser) structuralConformance(from types.Type, target types.TraitPointerType, at parser.Node) ([]*symbols.Symbol, bool) {
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
		if method == nil || method.StaticMethod || (method.DefinitionModule != a.currentMod && !method.Public) {
			return nil, false
		}
		sig := method.Signature
		if len(sig.Parameters) != len(requirement.Parameters)+1 || sig.ReturnType == nil || !sig.ReturnType.Equals(requirement.ReturnType) {
			return nil, false
		}
		actualReceiver := types.TraitReceiverValue
		if receiver, ok := types.Underlying(sig.Parameters[0]).(types.PointerType); ok {
			actualReceiver = types.TraitReceiverPointer
			if receiver.Mutable {
				actualReceiver = types.TraitReceiverMutablePointer
			}
		}
		if actualReceiver != requirement.Receiver {
			return nil, false
		}
		if requirement.Receiver == types.TraitReceiverValue && !types.IsComplete(pointer.Base) {
			return nil, false
		}
		for j, param := range requirement.Parameters {
			if !sig.Parameters[j+1].Equals(param) {
				return nil, false
			}
		}
		selected[i] = method
	}
	return selected, true
}

func (v *Validator) traitConversion(node parser.ExpressionNode, expected types.Type) (*parser.CastNode, bool) {
	target, ok := traitPointer(expected)
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
			if available.Name != required.Name || available.Receiver != required.Receiver || !available.ReturnType.Equals(required.ReturnType) || len(available.Parameters) != len(required.Parameters) {
				continue
			}
			matches := true
			for i := range required.Parameters {
				if !available.Parameters[i].Equals(required.Parameters[i]) {
					matches = false
					break
				}
			}
			if matches {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
