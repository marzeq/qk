package loader

import (
	"fmt"

	"github.com/marzeq/qk/codegen/irgen"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/sema"
)

func ComputeModuleOrder(mods map[string]*ModuleInfo, primaryModule string) ([]string, []error) {
	order, err := ModuleDependencyOrder(mods, primaryModule)
	if err != nil {
		return nil, []error{err}
	}

	return order, nil
}

func RunSemanticPipeline(mods map[string]*ModuleInfo, analyser *sema.Analyser, order []string, verbose, debug bool) (errors []error, warnings []error) {
	for _, name := range order {
		info := mods[name]

		for _, root := range info.Roots {
			analyser.AnalyseModule(root, name)
		}
	}

	if len(analyser.Errors()) > 0 {
		return analyser.Errors(), nil
	}

	if verbose && debug {
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
		return attributor.Errors(), nil
	}

	if verbose && debug {
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
		return validator.Errors(), nil
	}

	warnings = append(warnings, validator.Warnings()...)

	if verbose && debug {
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

	return nil, warnings
}

func GenerateIRModules(mods map[string]*ModuleInfo, mainModule string, order []string, verbose bool, debug bool) (map[string]*ir.Module, []error) {
	irMods := map[string]*ir.Module{}

	for _, name := range order {
		info := mods[name]
		out := &ir.Module{}

		for _, root := range info.Roots {
			gen := &irgen.Generator{ModuleName: name, MainModule: mainModule}
			modIR := gen.Generate(root)
			out.Functions = append(out.Functions, modIR.Functions...)
			out.Globals = append(out.Globals, modIR.Globals...)
			out.ExternGlobals = append(out.ExternGlobals, modIR.ExternGlobals...)
			if len(modIR.Externs) > 0 {
				out.Externs = append(out.Externs, modIR.Externs...)
			}
		}

		irMods[name] = out
	}

	if verbose && debug {
		fmt.Println("completed ir generation phase")
	}

	return irMods, nil
}
