package loader

import (
	"github.com/marzeq/qk/sema"
)

func RunSemanticPipeline(mods map[string]*ModuleInfo, analyser *sema.Analyser) []error {
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

	return nil
}
