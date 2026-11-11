package sema

import (
	"fmt"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/types"
)

// TypeSatisfies answers the same question as a concrete type argument's
// structural trait constraint. The staging evaluator uses it for core.types.
func (a *Analyser) TypeSatisfies(concrete, requirement types.Type) (bool, error) {
	trait, ok := types.Underlying(requirement).(types.TraitType)
	if !ok {
		return false, fmt.Errorf("core.types.satisfies requires a trait as its second argument")
	}
	if !types.IsComplete(concrete) {
		return false, fmt.Errorf("core.types.satisfies requires a completed concrete type")
	}
	probe := types.PointerType{Base: concrete}
	_, conforms := a.structuralConformance(probe, types.TraitPointerType{Trait: trait}, &parser.ResolvedTypeNode{})
	return conforms, nil
}
