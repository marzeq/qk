package loader

import "fmt"

func BuildDependencyGraph(mods map[string]*ModuleInfo) (map[string][]string, error) {
	graph := map[string][]string{}

	for name, info := range mods {
		graph[name] = info.Imports

		for _, dep := range info.Imports {
			if _, ok := mods[dep]; !ok {
				return nil, fmt.Errorf("module %q imports unknown module %q", name, dep)
			}
		}
	}

	return graph, nil
}

type VisitedStatusKind int

const (
	VisitedStatusUnseen VisitedStatusKind = iota
	VisitedStatusVisiting
	VisitedStatusDone
)

func TopoSort(graph map[string][]string) ([]string, error) {
	visited := map[string]VisitedStatusKind{}
	order := []string{}

	var visit func(string) error
	visit = func(n string) error {
		switch visited[n] {
		case VisitedStatusVisiting:
			return fmt.Errorf("circular import detected at %q", n)
		case VisitedStatusDone:
			return nil
		}

		visited[n] = VisitedStatusVisiting

		for _, dep := range graph[n] {
			if err := visit(dep); err != nil {
				return err
			}
		}

		visited[n] = VisitedStatusDone
		order = append(order, n)
		return nil
	}

	for node := range graph {
		if visited[node] == VisitedStatusUnseen {
			if err := visit(node); err != nil {
				return nil, err
			}
		}
	}

	return order, nil
}
