package stdlib

import (
	"sort"
	"strings"

	"github.com/marzeq/qk/comptime"
	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/tokeniser"
)

// ParseLibrarySources runs the normal QK frontend over installed library source
// text. Only std and std.* receive trusted-standard-library privileges;
// vendor modules remain ordinary QK code.
func ParseLibrarySources(sources, packagePaths map[string]string, config comptime.Config) ([]*loader.PartialModuleInfo, error) {
	origins := make([]string, 0, len(sources))
	for origin := range sources {
		origins = append(origins, origin)
	}
	sort.Strings(origins)

	partials := make([]*loader.PartialModuleInfo, 0, len(origins))
	for _, origin := range origins {
		t := tokeniser.NewTokeniser(sources[origin], origin)
		tokens, err := t.Tokenise()
		if err != nil {
			return nil, err
		}
		tokens, err = comptime.Expand(tokens, config)
		if err != nil {
			return nil, err
		}
		root, err := parser.NewParser(tokens).Parse()
		if err != nil {
			return nil, err
		}
		packagePath := packagePaths[origin]
		trusted := packagePath == "std" || strings.HasPrefix(packagePath, "std.")
		info, err := loader.CollectModuleInfo(root, trusted)
		if err != nil {
			return nil, err
		}
		info.Path = packagePath
		partials = append(partials, info)
	}
	return partials, nil
}

// Check typechecks trusted standard-library sources without generating IR or
// native output. All semantic diagnostics are returned together.
func check(sources, packagePaths map[string]string, config comptime.Config) []error {
	values, err := comptime.ResolvePackageBindings(sources, packagePaths, config)
	if err != nil {
		return []error{err}
	}
	config.ModuleBindings = values
	partials, err := ParseLibrarySources(sources, packagePaths, config)
	if err != nil {
		return []error{err}
	}
	modules, err := loader.BuildModules(partials)
	if err != nil {
		return []error{err}
	}
	graph, err := loader.BuildDependencyGraph(modules)
	if err != nil {
		return []error{err}
	}
	order, err := loader.TopoSort(graph)
	if err != nil {
		return []error{err}
	}
	analyser := sema.NewAnalyser()
	errs, _ := loader.RunSemanticPipeline(modules, analyser, order, false, false)
	return errs
}

// CheckAtRoot validates directory-derived package identities and typechecks a
// library source tree.
func CheckAtRoot(sources map[string]string, root string, config comptime.Config) []error {
	packagePaths, _, err := SourcePackagePaths(sources, root)
	if err != nil {
		return []error{err}
	}
	return check(sources, packagePaths, config)
}

// CheckRoot loads and typechecks an installed library tree.
func CheckRoot(root string, config comptime.Config) []error {
	sources, err := ReadSources(root)
	if err != nil {
		return []error{err}
	}
	return CheckAtRoot(sources, root, config)
}
