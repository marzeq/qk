package loader

import (
	"fmt"

	"github.com/marzeq/qk/codegen/irgen"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/sema"
)

func ComputeModuleOrder(mods map[string]*ModuleInfo) ([]string, []error) {
	graph, err := BuildDependencyGraph(mods)
	if err != nil {
		return nil, []error{err}
	}

	order, err := TopoSort(graph)
	if err != nil {
		return nil, []error{err}
	}

	return order, nil
}

func RunSemanticPipeline(mods map[string]*ModuleInfo, analyser *sema.Analyser, order []string, verbose, debug bool) []error {

	for _, name := range order {
		info := mods[name]

		for _, root := range info.Roots {
			analyser.AnalyseModule(root, name)
		}
	}

	if len(analyser.Errors()) > 0 {
		return analyser.Errors()
	}

	if verbose {
		fmt.Println("completed analysis phase")
	}

	attributor := analyser.NewAttributor()

	for _, name := range order {
		info := mods[name]

		for _, root := range info.Roots {
			attributor.AttributeModule(root)
		}
	}

	if len(attributor.Errors()) > 0 {
		return attributor.Errors()
	}

	if verbose {
		fmt.Println("completed attribution phase")
	}

	validator := analyser.NewValidator()

	for _, name := range order {
		info := mods[name]
		for _, root := range info.Roots {
			validator.ValidateModule(root)
		}
	}

	if len(validator.Errors()) > 0 {
		return validator.Errors()
	}

	if verbose {
		fmt.Println("completed validation phase")
	}

	if debug {
		for _, name := range order {
			info := mods[name]
			for _, root := range info.Roots {
				analyser.DebugCheck(root)
			}
		}

		if verbose {
			fmt.Println("completed debug checks")
		}
	}

	return nil
}

func GenerateIRModules(mods map[string]*ModuleInfo, order []string, verbose bool) (map[string]*ir.Module, []error) {
	irMods := map[string]*ir.Module{}

	for _, name := range order {
		info := mods[name]
		out := &ir.Module{}

		for _, root := range info.Roots {
			gen := &irgen.Generator{}
			modIR := gen.Generate(root)
			out.Functions = append(out.Functions, modIR.Functions...)
		}

		irMods[name] = out
	}

	if verbose {
		fmt.Println("completed ir generation phase")
	}

	return irMods, nil
}
