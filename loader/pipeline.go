package loader

import (
	"fmt"

	"github.com/marzeq/qk/sema"
)

func RunSemanticPipeline(mods map[string]*ModuleInfo) error {
	graph, err := BuildDependencyGraph(mods)
	if err != nil {
		return err
	}

	order, err := TopoSort(graph)
	if err != nil {
		return err
	}

	analyser := sema.NewAnalyser()

	for _, name := range order {
		info := mods[name]

		for _, root := range info.Roots {
			analyser.AnalyseModule(root, name)
		}
	}

	if len(analyser.Errors()) > 0 {
		for _, err := range analyser.Errors() {
			fmt.Println(err)
		}
		return fmt.Errorf("semantic analysis failed")
	}

	return nil
}
