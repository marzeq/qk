package main

import (
	"testing"

	"github.com/marzeq/qk/ir"
)

func TestPruneUnreachableIRAcrossModules(t *testing.T) {
	main := &ir.Module{Entry: "main"}
	entry := ir.NewFunction("main", ir.LinkageExternal, nil)
	entry.Visibility = ir.VisibilityHidden
	entry.NewBlock("entry").Instr = []ir.Instr{
		ir.Call{Name: "helper"},
		ir.AddressOfGlobal{Name: "callback"},
	}
	main.AddFunction(entry)
	main.AddFunction(ir.NewFunction("unused_main", ir.LinkageInternal, nil))
	main.AddGlobal(ir.Global{Name: "runtime_state"})

	dependency := &ir.Module{}
	helper := ir.NewFunction("helper", ir.LinkageExternal, nil)
	helper.Visibility = ir.VisibilityHidden
	dependency.AddFunction(helper)
	callback := ir.NewFunction("callback_target", ir.LinkageInternal, nil)
	dependency.AddFunction(callback)
	dependency.AddFunction(ir.NewFunction("unused_dependency", ir.LinkageInternal, nil))
	dependency.AddFunction(ir.NewFunction("exported", ir.LinkageExternal, nil))
	dependency.AddGlobal(ir.Global{Name: "callback", Value: ir.FunctionConstOperand("callback_target", nil)})
	dependency.AddGlobal(ir.Global{Name: "unused_global"})

	pruneUnreachableIR(map[string]*ir.Module{"main": main, "dependency": dependency}, "runtime_state")
	if len(main.Functions) != 1 || main.Functions[0].Name != "main" {
		t.Fatalf("main functions: %v", functionNames(main))
	}
	if len(dependency.Functions) != 3 || dependency.Functions[0].Name != "helper" || dependency.Functions[1].Name != "callback_target" || dependency.Functions[2].Name != "exported" {
		t.Fatalf("dependency functions: %v", functionNames(dependency))
	}
	if len(main.Globals) != 1 || main.Globals[0].Name != "runtime_state" {
		t.Fatalf("runtime globals: %v", main.Globals)
	}
	if len(dependency.Globals) != 1 || dependency.Globals[0].Name != "callback" {
		t.Fatalf("dependency globals: %v", dependency.Globals)
	}
}

func functionNames(module *ir.Module) []string {
	names := make([]string, len(module.Functions))
	for i, fn := range module.Functions {
		names[i] = fn.Name
	}
	return names
}
