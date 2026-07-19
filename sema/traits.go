package sema

import (
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
		if method == nil || method.StaticMethod || (module != a.currentMod && !method.Public) {
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
