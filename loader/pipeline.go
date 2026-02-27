package loader

import (
	"fmt"

	"github.com/marzeq/qk/sema"
)

func RunSemanticPipeline(mods map[string]*ModuleInfo, analyser *sema.Analyser, verbose, debug bool) []error {
	graph, err := BuildDependencyGraph(mods)
	if err != nil {
		return []error{err}
	}

	order, err := TopoSort(graph)
	if err != nil {
		return []error{err}
	}

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
