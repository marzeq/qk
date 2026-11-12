package main

import (
	"fmt"

	"github.com/marzeq/qk/ir"
)

// splitEmissionModule divides large executable modules into independent LLVM
// units. Every unit sees declarations for definitions owned by its siblings;
// the original semantic IR remains intact for diagnostics and IR dumps.
func splitEmissionModule(module *ir.Module) []*ir.Module {
	var runtimeFunctions []*ir.Function
	for _, fn := range module.Functions {
		if !fn.CompileTimeOnly && len(fn.Blocks) != 0 {
			runtimeFunctions = append(runtimeFunctions, fn)
		}
	}
	if len(runtimeFunctions) < 48 {
		return []*ir.Module{module}
	}

	const parts = 3
	groups := make([][]*ir.Function, parts)
	weights := make([]int, parts)
	for _, fn := range runtimeFunctions {
		weight := 1
		for _, block := range fn.Blocks {
			weight += len(block.Instr)
		}
		index := 0
		for i := 1; i < parts; i++ {
			if weights[i] < weights[index] {
				index = i
			}
		}
		groups[index] = append(groups[index], fn)
		weights[index] += weight
	}

	result := make([]*ir.Module, parts)
	for index, group := range groups {
		unit := &ir.Module{
			Externs:       append([]ir.ExternDecl(nil), module.Externs...),
			ExternGlobals: append([]ir.ExternGlobal(nil), module.ExternGlobals...),
		}
		if index == 0 {
			unit.Globals = append([]ir.Global(nil), module.Globals...)
			unit.Initializer = module.Initializer
			unit.Entry = module.Entry
		} else {
			for _, global := range module.Globals {
				unit.ExternGlobals = append(unit.ExternGlobals, ir.ExternGlobal{
					Name: global.Name, Type: global.Type, Mutable: global.Mutable,
					Visibility: ir.VisibilityHidden,
				})
			}
		}
		owned := make(map[string]bool, len(group))
		for _, fn := range group {
			copy := *fn
			if copy.Linkage == ir.LinkageInternal {
				copy.Linkage = ir.LinkageExternal
				copy.Visibility = ir.VisibilityHidden
			}
			unit.Functions = append(unit.Functions, &copy)
			owned[fn.Name] = true
		}
		declared := make(map[string]bool, len(unit.Externs))
		for _, declaration := range unit.Externs {
			declared[declaration.Name] = true
		}
		for _, fn := range runtimeFunctions {
			if owned[fn.Name] || declared[fn.Name] {
				continue
			}
			visibility := fn.Visibility
			if fn.Linkage == ir.LinkageInternal {
				visibility = ir.VisibilityHidden
			}
			unit.Externs = append(unit.Externs, ir.ExternDecl{
				Name: fn.Name, Signature: fn.Signature, Visibility: visibility,
			})
		}
		result[index] = unit
	}
	for index := range result[0].Globals {
		if result[0].Globals[index].Linkage == ir.LinkageInternal {
			result[0].Globals[index].Linkage = ir.LinkageExternal
			result[0].Globals[index].Visibility = ir.VisibilityHidden
		}
	}
	return result
}

func emissionUnitName(module string, index int) string {
	if index == 0 {
		return module
	}
	return fmt.Sprintf("%s#%d", module, index)
}
