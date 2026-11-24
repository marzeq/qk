package loader

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
)

type PartialModuleInfo struct {
	Path                   string
	Name                   string
	Imports                []string
	Root                   *parser.RootNode
	Links                  []attributes.Link
	CompileTimeLinks       []attributes.Link
	TrustedStandardLibrary bool
}

func CollectModuleInfo(root *parser.RootNode, trustedStandardLibrary bool) (*PartialModuleInfo, error) {
	name := ""
	imports := []string{}
	seenModule := false
	links := []attributes.Link{}
	compileTimeLinks := []attributes.Link{}

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
				normalizeLinks := func(input []attributes.Link) []attributes.Link {
					output := make([]attributes.Link, 0, len(input))
					for _, link := range input {
						if (link.Kind == attributes.LinkPath || link.Kind == attributes.LinkSearchPath) && !filepath.IsAbs(link.Value) {
							link.Value = filepath.Join(filepath.Dir(n.Loc.FilePath), link.Value)
						}
						if link.Kind == attributes.LinkPath || link.Kind == attributes.LinkSearchPath {
							link.Value = filepath.Clean(link.Value)
						}
						output = append(output, link)
					}
					return output
				}
				links = append(links, normalizeLinks(linkAttr.Links)...)
				compileTimeLinks = append(compileTimeLinks, normalizeLinks(linkAttr.CompileTimeLinks)...)
			}

		case *parser.ImportNode:
			for i, imported := range n.Modules {
				if i < len(n.ResolvedModules) && n.ResolvedModules[i] != "" {
					imported = n.ResolvedModules[i]
				}
				imports = append(imports, imported)
			}
		}
	}

	if !seenModule || name == "" {
		return nil, fmt.Errorf("module declaration is missing or empty")
	}
	if (name == "std" || strings.HasPrefix(name, "std.")) && !trustedStandardLibrary {
		return nil, fmt.Errorf("module name %q is reserved for compiler-trusted standard-library sources", name)
	}
	if trustedStandardLibrary && name != "std" && !strings.HasPrefix(name, "std.") {
		return nil, fmt.Errorf("trusted standard-library source declares module %q outside the reserved %q namespace", name, "std")
	}

	return &PartialModuleInfo{
		Name:                   name,
		Imports:                imports,
		Root:                   root,
		Links:                  links,
		CompileTimeLinks:       compileTimeLinks,
		TrustedStandardLibrary: trustedStandardLibrary,
	}, nil
}

type ModuleInfo struct {
	Path                    string
	Name                    string
	Imports                 []string
	Root                    *parser.RootNode
	CompileTimeForeignLinks map[string][]attributes.Link
	TrustedStandardLibrary  bool
}

func BuildModules(partials []*PartialModuleInfo) (map[string]*ModuleInfo, error) {
	modules := map[string]*ModuleInfo{}

	for _, p := range partials {
		path := p.Path
		if path == "" {
			path = p.Name
		}
		if existing, ok := modules[path]; ok {
			if existing.Name != p.Name {
				return nil, fmt.Errorf("package %q contains both module %q and module %q", path, existing.Name, p.Name)
			}
			if existing.TrustedStandardLibrary != p.TrustedStandardLibrary {
				return nil, fmt.Errorf("cannot mix trusted and untrusted sources in package %q", path)
			}
			mergeModuleRoot(existing.Root, p.Root)
			existing.Imports = mergeImports(existing.Imports, p.Imports)
			collectCompileTimeForeignLinks(existing.CompileTimeForeignLinks, p)
		} else {
			module := &ModuleInfo{
				Path:                    path,
				Name:                    p.Name,
				Imports:                 unique(p.Imports),
				Root:                    cloneModuleRoot(p.Root),
				CompileTimeForeignLinks: make(map[string][]attributes.Link),
				TrustedStandardLibrary:  p.TrustedStandardLibrary,
			}
			collectCompileTimeForeignLinks(module.CompileTimeForeignLinks, p)
			modules[path] = module
		}
	}

	return modules, nil
}

func collectCompileTimeForeignLinks(destination map[string][]attributes.Link, partial *PartialModuleInfo) {
	for _, node := range partial.Root.Body {
		var attrs attributes.Attributes
		switch node := node.(type) {
		case *parser.FunctionDefNode:
			attrs = node.Attributes
		case *parser.DeclarationNode:
			attrs = node.Attributes
		default:
			continue
		}
		foreign, ok := attrs.Get(attributes.AttributeTypeForeign).(attributes.FunctionAttributeForeign)
		if !ok || foreign.From == "" {
			continue
		}
		for _, link := range partial.CompileTimeLinks {
			found := false
			for _, existing := range destination[foreign.From] {
				if existing == link {
					found = true
					break
				}
			}
			if !found {
				destination[foreign.From] = append(destination[foreign.From], link)
			}
		}
	}
}

func cloneModuleRoot(root *parser.RootNode) *parser.RootNode {
	return &parser.RootNode{
		Body: append([]parser.Node(nil), root.Body...),
		Loc:  root.Loc,
	}
}

func mergeModuleRoot(destination, source *parser.RootNode) {
	for _, node := range source.Body {
		if _, isModule := node.(*parser.ModuleNode); isModule {
			continue
		}
		destination.Body = append(destination.Body, node)
	}
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
