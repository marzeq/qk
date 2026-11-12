package main

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/tokeniser"
)

const builtinModule = "builtin"

//go:embed builtin.qk
var builtinSource string

//go:embed core_types.qk
var coreTypesSource string

type frontendResult struct {
	modules       map[string]*loader.ModuleInfo
	partials      []*loader.PartialModuleInfo
	order         []string
	irModules     map[string]*ir.Module
	templates     map[string][]ir.ParameterizedTemplate
	methodTargets []sema.MethodTarget
	warnings      []error
}

func runFrontend(
	args *Args,
	config loader.StageConfig,
	sources, sourcePackages map[string]string,
	trustedSources map[string]bool,
	importResolutions map[string]map[string]string,
	verbose, debug bool,
	parsedSources ...map[string]*parser.RootNode,
) (*frontendResult, error) {
	sources = cloneStringMap(sources)
	sourcePackages = cloneStringMap(sourcePackages)
	trustedSources = cloneBoolMap(trustedSources)
	const builtinOrigin = "<builtin>"
	sources[builtinOrigin] = builtinSource
	sourcePackages[builtinOrigin] = builtinModule
	trustedSources[builtinOrigin] = false
	const coreBuildOrigin = "<core.build>"
	sources[coreBuildOrigin] = coreBuildSource(config)
	sourcePackages[coreBuildOrigin] = "core.build"
	trustedSources[coreBuildOrigin] = false
	const coreTypesOrigin = "<core.types>"
	sources[coreTypesOrigin] = coreTypesSource
	sourcePackages[coreTypesOrigin] = "core.types"
	trustedSources[coreTypesOrigin] = false
	var preParsed map[string]*parser.RootNode
	if len(parsedSources) != 0 {
		preParsed = parsedSources[0]
	}
	graphModules, origins, parsed, err := sourceModuleGraph(sources, sourcePackages, trustedSources, importResolutions, args.noStdlib, preParsed)
	if err != nil {
		return nil, err
	}
	order, err := loader.ModuleDependencyOrder(graphModules, args.mainModule)
	if err != nil {
		return nil, err
	}
	result := &frontendResult{
		modules: make(map[string]*loader.ModuleInfo),
		order:   order,
	}
	var sourceOrder []string
	stageModules := make(map[string]*loader.ModuleInfo)
	needsDeferredSnapshots := false
	analyser := sema.NewAnalyser()
	loader.ConfigureDeferredStage(analyser, config, func(module string) *loader.StageEnvironment {
		current := stageModules[module]
		return &loader.StageEnvironment{
			Modules: stageModules, Order: sourceOrder,
			TrustedStandardLibrary: current != nil && current.TrustedStandardLibrary,
		}
	})
	for _, name := range order {
		partials := make([]*loader.PartialModuleInfo, 0, len(origins[name]))
		roots := make([]*parser.RootNode, 0, len(origins[name]))
		for _, origin := range origins[name] {
			root := parsed[origin]
			resolveRootImports(root, importResolutions[name])
			roots = append(roots, root)
		}
		environment := &loader.StageEnvironment{
			Modules: stageModules, Order: sourceOrder,
			TrustedStandardLibrary: len(origins[name]) != 0 && trustedSources[origins[name][0]],
		}
		if err := loader.SelectCompileTimeRootsWithEnvironment(roots, name, config, environment); err != nil {
			return nil, err
		}
		for index, root := range roots {
			origin := origins[name][index]
			partial, err := loader.CollectModuleInfo(root, trustedSources[origin])
			if err != nil {
				return nil, err
			}
			partial.Path = name
			partials = append(partials, partial)
			result.partials = append(result.partials, partial)
		}
		built, err := loader.BuildModules(partials)
		if err != nil {
			return nil, err
		}
		info := built[name]
		if info == nil {
			return nil, fmt.Errorf("module %q has no source", name)
		}
		if !args.noStdlib && name != builtinModule && name != "core.build" && name != "core.types" && name != "std" && !strings.HasPrefix(name, "std.") && !containsString(info.Imports, "std") {
			info.Imports = append(info.Imports, "std")
		}
		if moduleNeedsDeferredSnapshots(info.Root) {
			needsDeferredSnapshots = true
		}
		stageModules[name] = &loader.ModuleInfo{
			Path: name, Name: info.Name, Imports: append([]string(nil), info.Imports...),
			Root:                   info.Root,
			TrustedStandardLibrary: info.TrustedStandardLibrary,
		}
		result.modules[name] = info
		sourceOrder = append(sourceOrder, name)
	}
	if needsDeferredSnapshots {
		for _, name := range sourceOrder {
			stageModules[name].Root = parser.CloneSyntax(stageModules[name].Root).(*parser.RootNode)
		}
	}
	compiled, errs := loader.CompileSelectedModules(result.modules, args.mainModule, sourceOrder, analyser, verbose, debug)
	if len(errs) != 0 {
		return nil, errs[0]
	}
	result.irModules = compiled.IRModules
	result.templates = compiled.Templates
	result.methodTargets = compiled.MethodTargets
	result.warnings = compiled.Warnings
	return result, nil
}

func moduleNeedsDeferredSnapshots(root *parser.RootNode) bool {
	for _, node := range root.Body {
		switch node := node.(type) {
		case *parser.FunctionDefNode:
			if node.DeferredStage {
				return true
			}
		case *parser.TypeAliasNode:
			if node.DeferredStage {
				return true
			}
			for _, method := range node.Methods {
				if method.DeferredStage {
					return true
				}
			}
		}
	}
	return false
}

func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneBoolMap(source map[string]bool) map[string]bool {
	result := make(map[string]bool, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func sourceModuleGraph(sources, sourcePackages map[string]string, trustedSources map[string]bool, importResolutions map[string]map[string]string, noStdlib bool, preParsed map[string]*parser.RootNode) (map[string]*loader.ModuleInfo, map[string][]string, map[string]*parser.RootNode, error) {
	modules := make(map[string]*loader.ModuleInfo)
	origins := make(map[string][]string)
	parsed := make(map[string]*parser.RootNode, len(sources))
	paths := make([]string, 0, len(sources))
	for origin := range sources {
		paths = append(paths, origin)
	}
	sort.Strings(paths)
	for _, origin := range paths {
		name := sourcePackages[origin]
		if name == "" {
			return nil, nil, nil, fmt.Errorf("source %s has no canonical module path", origin)
		}
		root := preParsed[origin]
		if root == nil {
			tokens, err := tokeniser.NewTokeniser(sources[origin], origin).Tokenise()
			if err != nil {
				return nil, nil, nil, err
			}
			root, err = parser.NewParser(tokens).Parse()
			if err != nil {
				return nil, nil, nil, err
			}
		}
		parsed[origin] = root
		header := sourceHeader(root)
		module := modules[name]
		if module == nil {
			module = &loader.ModuleInfo{Path: name, Name: header.Module, TrustedStandardLibrary: trustedSources[origin]}
			modules[name] = module
		} else if module.TrustedStandardLibrary != trustedSources[origin] {
			return nil, nil, nil, fmt.Errorf("module %q mixes trusted and untrusted sources", name)
		}
		for _, imported := range header.Imports {
			if resolved := importResolutions[name][imported]; resolved != "" {
				imported = resolved
			}
			if !containsString(module.Imports, imported) {
				module.Imports = append(module.Imports, imported)
			}
		}
		origins[name] = append(origins[name], origin)
	}
	for name, module := range modules {
		if name != builtinModule && !containsString(module.Imports, builtinModule) {
			module.Imports = append(module.Imports, builtinModule)
		}
		if !noStdlib && name != builtinModule && name != "core.build" && name != "core.types" && name != "std" && !strings.HasPrefix(name, "std.") && !containsString(module.Imports, "std") {
			module.Imports = append(module.Imports, "std")
		}
	}
	return modules, origins, parsed, nil
}

type sourceHeaderInfo struct {
	Module  string
	Imports []string
}

func sourceHeader(root *parser.RootNode) sourceHeaderInfo {
	var header sourceHeaderInfo
	for _, node := range root.Body {
		if module, ok := node.(*parser.ModuleNode); ok && header.Module == "" {
			header.Module = module.Name
		}
	}
	walkImports(root.Body, func(node *parser.ImportNode) {
		header.Imports = append(header.Imports, node.Modules...)
	})
	return header
}

func resolveRootImports(root *parser.RootNode, resolutions map[string]string) {
	walkImports(root.Body, func(importNode *parser.ImportNode) {
		importNode.ResolvedModules = make([]string, len(importNode.Modules))
		for i, visible := range importNode.Modules {
			resolved := resolutions[visible]
			if resolved == "" {
				resolved = visible
			}
			importNode.ResolvedModules[i] = resolved
		}
	})
}

func walkImports(body []parser.Node, visit func(*parser.ImportNode)) {
	for _, node := range body {
		switch node := node.(type) {
		case *parser.ImportNode:
			visit(node)
		case *parser.WhenNode:
			for _, branch := range node.Branches {
				walkImports(branch.Body, visit)
			}
			walkImports(node.ElseBody, visit)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
