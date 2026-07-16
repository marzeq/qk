package loader

import "fmt"

func ModuleDependencyOrder(mods map[string]*ModuleInfo, primaryModule string) ([]string, error) {
	if _, ok := mods[primaryModule]; !ok {
		return nil, fmt.Errorf("primary module %q not found", primaryModule)
	}

	visited := map[string]VisitedStatusKind{}
	order := []string{}
	var visit func(string) error
	visit = func(name string) error {
		switch visited[name] {
		case VisitedStatusVisiting:
			return fmt.Errorf("circular import detected at %q", name)
		case VisitedStatusDone:
			return nil
		}

		info, ok := mods[name]
		if !ok {
			return fmt.Errorf("module imports unknown module %q", name)
		}
		visited[name] = VisitedStatusVisiting
		for _, dependency := range info.Imports {
			if _, ok := mods[dependency]; !ok {
				return fmt.Errorf("module %q imports unknown module %q", name, dependency)
			}
			if err := visit(dependency); err != nil {
				return err
			}
		}
		visited[name] = VisitedStatusDone
		order = append(order, name)
		return nil
	}

	if err := visit(primaryModule); err != nil {
		return nil, err
	}
	return order, nil
}

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
