package loader

import (
	"reflect"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/types"
)

type traitFlowKey struct {
	module  string
	name    string
	any     bool
	mutable bool
}

type traitRecast struct {
	node   *parser.CastNode
	source types.TraitPointerType
	target types.TraitPointerType
}

// narrowRuntimeTraitCastCandidates limits a dynamic trait-to-trait cast to
// concrete types that can actually reach its source trait in this compilation.
// Without this whole-program pass, every structural conformer is referenced by
// every recast and therefore kept alive by the linker even when no value of
// that concrete type is ever erased into the source trait.
func narrowRuntimeTraitCastCandidates(mods map[string]*ModuleInfo, order []string) {
	possible := make(map[traitFlowKey][]types.Type)
	var recasts []traitRecast

	for _, name := range order {
		walkParserNodes(mods[name].Root, func(node parser.Node) {
			cast, ok := node.(*parser.CastNode)
			if !ok {
				return
			}
			targetType := cast.Type
			if cast.Checked {
				targetType = cast.CheckedType
			}
			target, targetIsTrait := types.Underlying(targetType).(types.TraitPointerType)
			if cast.TraitConversion && targetIsTrait && cast.ConcreteType != nil {
				addPossibleTraitType(possible, target, cast.ConcreteType)
			}
			if !cast.TraitRecast || !targetIsTrait || cast.Operand == nil {
				return
			}
			source, sourceIsTrait := types.Underlying(cast.Operand.GetType()).(types.TraitPointerType)
			if sourceIsTrait {
				recasts = append(recasts, traitRecast{node: cast, source: source, target: target})
			}
		})
	}

	changed := true
	for changed {
		changed = false
		for _, recast := range recasts {
			sources := possible[flowKey(recast.source)]
			for _, candidate := range recast.node.TraitCandidates {
				if containsType(sources, candidate.ConcreteType) && addPossibleTraitType(possible, recast.target, candidate.ConcreteType) {
					changed = true
				}
			}
		}
	}

	for _, recast := range recasts {
		sources := possible[flowKey(recast.source)]
		kept := recast.node.TraitCandidates[:0]
		for _, candidate := range recast.node.TraitCandidates {
			if containsType(sources, candidate.ConcreteType) {
				kept = append(kept, candidate)
			}
		}
		recast.node.TraitCandidates = kept
	}
}

// demandedDynamicTraitSlots records the method slots that can be reached by
// dynamic dispatch in this compilation. Vtables retain their complete layout,
// but IR generation can leave unused entries null so those entries do not keep
// otherwise dead method implementations reachable.
func demandedDynamicTraitSlots(mods map[string]*ModuleInfo, order []string) map[string]map[int]bool {
	used := make(map[string]map[int]bool)
	externallyOpen := false
	for _, name := range order {
		walkParserNodes(mods[name].Root, func(node parser.Node) {
			switch node := node.(type) {
			case *parser.FunctionCallNode:
				if !node.TraitCall || len(node.Args) == 0 {
					return
				}
				pointer, ok := types.Underlying(node.Args[0].GetType()).(types.TraitPointerType)
				if !ok {
					return
				}
				key := pointer.Trait.String()
				if used[key] == nil {
					used[key] = make(map[int]bool)
				}
				used[key][node.TraitSlot] = true
			case *parser.FunctionDefNode:
				if node.Symbol == nil || node.Symbol.Signature == nil ||
					(node.Symbol.Attributes.Get(attributes.AttributeTypeExport) == nil &&
						node.Symbol.Attributes.Get(attributes.AttributeTypeForeign) == nil) {
					return
				}
				for _, parameter := range node.Symbol.Signature.Parameters {
					externallyOpen = externallyOpen || types.HasTraitPointer(parameter)
				}
				externallyOpen = externallyOpen || types.HasTraitPointer(node.Symbol.Signature.ReturnType)
			}
		})
	}
	if externallyOpen {
		// A foreign or exported QK ABI can expose a generated trait value to code
		// outside this compilation, whose dynamic calls are not visible here.
		return nil
	}
	return used
}

func flowKey(pointer types.TraitPointerType) traitFlowKey {
	return traitFlowKey{
		module:  pointer.Trait.Module,
		name:    pointer.Trait.Name,
		any:     pointer.Trait.Any,
		mutable: pointer.Mutable,
	}
}

func addPossibleTraitType(possible map[traitFlowKey][]types.Type, target types.TraitPointerType, concrete types.Type) bool {
	changed := addPossibleType(possible, flowKey(target), concrete)
	if target.Mutable {
		target.Mutable = false
		changed = addPossibleType(possible, flowKey(target), concrete) || changed
	}
	return changed
}

func addPossibleType(possible map[traitFlowKey][]types.Type, key traitFlowKey, concrete types.Type) bool {
	if containsType(possible[key], concrete) {
		return false
	}
	possible[key] = append(possible[key], concrete)
	return true
}

func containsType(haystack []types.Type, needle types.Type) bool {
	for _, candidate := range haystack {
		if candidate.Equals(needle) {
			return true
		}
	}
	return false
}

var parserPackagePath = reflect.TypeOf(parser.RootNode{}).PkgPath()

// walkParserNodes follows only parser-owned aggregate fields. Symbol and type
// pointers deliberately remain leaves, avoiding the semantic graph's cycles.
func walkParserNodes(root parser.Node, visit func(parser.Node)) {
	var walk func(reflect.Value)
	walk = func(value reflect.Value) {
		if !value.IsValid() {
			return
		}
		if value.Kind() == reflect.Interface {
			if value.IsNil() {
				return
			}
			walk(value.Elem())
			return
		}
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return
			}
			if node, ok := value.Interface().(parser.Node); ok {
				visit(node)
				walk(value.Elem())
			}
			return
		}
		switch value.Kind() {
		case reflect.Struct:
			if value.Type().PkgPath() != parserPackagePath {
				return
			}
			for i := 0; i < value.NumField(); i++ {
				walk(value.Field(i))
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < value.Len(); i++ {
				walk(value.Index(i))
			}
		}
	}

	walk(reflect.ValueOf(root))
}
