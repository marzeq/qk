package loader

import "fmt"

func ModuleDependencyOrder(mods map[string]*ModuleInfo, primaryModule string) ([]string, error) {
	if _, ok := mods[primaryModule]; !ok {
		return nil, fmt.Errorf("primary package %q not found", primaryModule)
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
			return fmt.Errorf("package imports unknown package %q", name)
		}
		visited[name] = VisitedStatusVisiting
		for _, dependency := range info.Imports {
			if _, ok := mods[dependency]; !ok {
				return fmt.Errorf("package %q imports unknown package %q", name, dependency)
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

type VisitedStatusKind int

const (
	VisitedStatusVisiting VisitedStatusKind = iota + 1
	VisitedStatusDone
)
