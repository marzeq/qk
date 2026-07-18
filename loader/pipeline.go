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
	out := make(map[string]*ir.Module, len(order))

	for _, name := range order {
		info := mods[name]
		dependencyInitializers := make([]string, 0, len(info.Imports))
		for _, dependency := range info.Imports {
			if dependencyIR := out[dependency]; dependencyIR != nil && dependencyIR.Initializer != "" {
				dependencyInitializers = append(dependencyInitializers, dependencyIR.Initializer)
			}
		}
		gen := &irgen.Generator{
			ModuleName: name, MainModule: mainModule,
			DependencyInitializers: dependencyInitializers,
		}
		modIR := gen.GenerateRoots(info.Roots)
		deduplicateIRDeclarations(modIR)
		out[name] = modIR
	}

	if verbose && debug {
		fmt.Println("completed ir generation phase")
	}

	return out, nil
}

func deduplicateIRDeclarations(module *ir.Module) {
	definedFunctions := make(map[string]bool, len(module.Functions))
	for _, fn := range module.Functions {
		definedFunctions[fn.Name] = true
	}
	seenFunctions := make(map[string]bool)
	externs := module.Externs[:0]
	for _, extern := range module.Externs {
		name := extern.Name
		if extern.From != "" {
			name = extern.From
		}
		if definedFunctions[name] || seenFunctions[name] {
			continue
		}
		seenFunctions[name] = true
		externs = append(externs, extern)
	}
	module.Externs = externs

	definedGlobals := make(map[string]bool, len(module.Globals))
	for _, global := range module.Globals {
		definedGlobals[global.Name] = true
	}
	seenGlobals := make(map[string]bool)
	externGlobals := module.ExternGlobals[:0]
	for _, global := range module.ExternGlobals {
		if definedGlobals[global.Name] || seenGlobals[global.Name] {
			continue
		}
		seenGlobals[global.Name] = true
		externGlobals = append(externGlobals, global)
	}
	module.ExternGlobals = externGlobals
}
