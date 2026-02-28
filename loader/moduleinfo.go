package loader

import (
	"fmt"

	"github.com/marzeq/qk/parser"
)

type PartialModuleInfo struct {
	Name    string
	Imports []string
	Root    *parser.RootNode
}

func CollectModuleInfo(root *parser.RootNode) (*PartialModuleInfo, error) {
	name := "main"
	imports := []string{}
	seenModule := false

	for i, node := range root.Body {
		switch n := node.(type) {

		case *parser.ModuleNode:
			if i != 0 {
				return nil, fmt.Errorf("module declaration must be first statement")
			}
			if seenModule {
				return nil, fmt.Errorf("multiple module declarations")
			}
			seenModule = true
			name = n.Name

		case *parser.ImportNode:
			imports = append(imports, n.Modules...)
		}
	}

	return &PartialModuleInfo{
		Name:    name,
		Imports: imports,
		Root:    root,
	}, nil
}

type ModuleInfo struct {
	Name    string
	Imports []string
	Roots   []*parser.RootNode
}

func BuildModules(partials []*PartialModuleInfo) (map[string]*ModuleInfo, error) {
	modules := map[string]*ModuleInfo{}

	for _, p := range partials {
		if existing, ok := modules[p.Name]; ok {
			existing.Roots = append(existing.Roots, p.Root)
			existing.Imports = mergeImports(existing.Imports, p.Imports)
		} else {
			modules[p.Name] = &ModuleInfo{
				Name:    p.Name,
				Imports: unique(p.Imports),
				Roots:   []*parser.RootNode{p.Root},
			}
		}
	}

	return modules, nil
}

func mergeImports(a, b []string) []string {
	return unique(append(a, b...))
}

func unique(in []string) []string {
	seen := map[string]struct{}{}
	var out []string

	for _, v := range in {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}

	return out
}
