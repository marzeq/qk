package stdlib

import (
	"sort"

	"github.com/marzeq/qk/comptime"
	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/tokeniser"
)

// ParseTrustedSources runs the normal QK frontend over trusted standard-library
// source text and returns module fragments ready for the compilation pipeline.
func ParseTrustedSources(sources, packagePaths map[string]string, config comptime.Config) ([]*loader.PartialModuleInfo, error) {
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
		info, err := loader.CollectModuleInfo(root, true)
		if err != nil {
			return nil, err
		}
		info.Path = packagePaths[origin]
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
	partials, err := ParseTrustedSources(sources, packagePaths, config)
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

// Check typechecks trusted sources whose map keys are stable embedded origins.
// Callers supplying a directory override should use CheckAtRoot.
func Check(sources map[string]string, config comptime.Config) []error {
	return CheckAtRoot(sources, "<stdlib>", config)
}

// CheckAtRoot validates directory-derived package identities and typechecks a
// trusted standard-library source tree.
func CheckAtRoot(sources map[string]string, root string, config comptime.Config) []error {
	packagePaths, _, err := SourcePackagePaths(sources, root)
	if err != nil {
		return []error{err}
	}
	return check(sources, packagePaths, config)
}

// CheckEmbedded typechecks the standard library embedded in this build.
func CheckEmbedded(config comptime.Config) []error {
	sources, err := ReadSources()
	if err != nil {
		return []error{err}
	}
	return Check(sources, config)
}
