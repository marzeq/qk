package loader

import (
	"fmt"
	"path/filepath"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
)

type PartialModuleInfo struct {
	Name                   string
	Imports                []string
	Root                   *parser.RootNode
	Links                  []attributes.Link
	TrustedStandardLibrary bool
}

func CollectModuleInfo(root *parser.RootNode, trustedStandardLibrary bool) (*PartialModuleInfo, error) {
	name := ""
	imports := []string{}
	seenModule := false
	links := []attributes.Link{}

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
			for _, attr := range n.Attributes {
				linkAttr, ok := attr.(attributes.ModuleAttributeLink)
				if !ok {
					continue
				}
				for _, link := range linkAttr.Links {
					if (link.Kind == attributes.LinkPath || link.Kind == attributes.LinkSearchPath) && !filepath.IsAbs(link.Value) {
						link.Value = filepath.Join(filepath.Dir(n.Loc.FilePath), link.Value)
					}
					if link.Kind == attributes.LinkPath || link.Kind == attributes.LinkSearchPath {
						link.Value = filepath.Clean(link.Value)
					}
					links = append(links, link)
				}
			}

		case *parser.ImportNode:
			imports = append(imports, n.Modules...)
		}
	}

	if !seenModule || name == "" {
		return nil, fmt.Errorf("module declaration is missing or empty")
	}
	if name == "std" && !trustedStandardLibrary {
		return nil, fmt.Errorf("module name %q is reserved for compiler-trusted standard-library sources", name)
	}
	if trustedStandardLibrary && name != "std" {
		return nil, fmt.Errorf("trusted standard-library source declares module %q instead of %q", name, "std")
	}

	return &PartialModuleInfo{
		Name:                   name,
		Imports:                imports,
		Root:                   root,
		Links:                  links,
		TrustedStandardLibrary: trustedStandardLibrary,
	}, nil
}

type ModuleInfo struct {
	Name                   string
	Imports                []string
	Roots                  []*parser.RootNode
	Links                  []attributes.Link
	TrustedStandardLibrary bool
}

func BuildModules(partials []*PartialModuleInfo) (map[string]*ModuleInfo, error) {
	modules := map[string]*ModuleInfo{}

	for _, p := range partials {
		if existing, ok := modules[p.Name]; ok {
			if existing.TrustedStandardLibrary != p.TrustedStandardLibrary {
				return nil, fmt.Errorf("cannot mix trusted and untrusted sources in module %q", p.Name)
			}
			existing.Roots = append(existing.Roots, p.Root)
			existing.Imports = mergeImports(existing.Imports, p.Imports)
			existing.Links = append(existing.Links, p.Links...)
		} else {
			modules[p.Name] = &ModuleInfo{
				Name:                   p.Name,
				Imports:                unique(p.Imports),
				Roots:                  []*parser.RootNode{p.Root},
				Links:                  append([]attributes.Link(nil), p.Links...),
				TrustedStandardLibrary: p.TrustedStandardLibrary,
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
