package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/marzeq/qk/comptime"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/tokeniser"
)

type frontendResult struct {
	modules    map[string]*loader.ModuleInfo
	partials   []*loader.PartialModuleInfo
	order      []string
	irModules  map[string]*ir.Module
	templates  map[string][]ir.GenericTemplate
	interfaces map[string]sema.ModuleInterface
	warnings   []error
}

func runFrontend(
	args *Args,
	config comptime.Config,
	sources, sourcePackages map[string]string,
	verbose, debug bool,
) (*frontendResult, error) {
	graphModules, origins, err := sourceModuleGraph(sources, sourcePackages, config, args.noStdlib)
	if err != nil {
		return nil, err
	}
	order, errs := loader.ComputeModuleOrder(graphModules, args.mainModule)
	if len(errs) != 0 {
		return nil, errs[0]
	}
	result := &frontendResult{
		modules: make(map[string]*loader.ModuleInfo), irModules: make(map[string]*ir.Module),
		templates: make(map[string][]ir.GenericTemplate), interfaces: make(map[string]sema.ModuleInterface),
		order: order,
	}
	analyser := sema.NewAnalyser()
	var sourceOrder []string
	for _, name := range order {
		partials := make([]*loader.PartialModuleInfo, 0, len(origins[name]))
		for _, origin := range origins[name] {
			fileConfig := config
			fileConfig.PackagePath = name
			root, err := parseSource(origin, sources[origin], fileConfig)
			if err != nil {
				return nil, err
			}
			partial, err := loader.CollectModuleInfo(root, name == "std" || strings.HasPrefix(name, "std."))
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
		if !args.noStdlib && name != "std" && !strings.HasPrefix(name, "std.") && !containsString(info.Imports, "std") {
			info.Imports = append(info.Imports, "std")
		}
		analyser.DeclareModule(info.Root, name, info.TrustedStandardLibrary)
		if len(analyser.Errors()) != 0 {
			return nil, analyser.Errors()[0]
		}
		analyser.AnalyseModuleBody(info.Root, name)
		if len(analyser.Errors()) != 0 {
			return nil, analyser.Errors()[0]
		}
		result.modules[name] = info
		sourceOrder = append(sourceOrder, name)
	}
	if errs, warnings := loader.RunSemanticModuleBodies(result.modules, analyser, sourceOrder, verbose, debug); len(errs) != 0 {
		return nil, errs[0]
	} else {
		result.warnings = append(result.warnings, warnings...)
	}
	loader.PropagateSpecializationDemands(result.modules, sourceOrder)
	allInterfaces := analyser.ModuleInterfaces()
	for _, name := range sourceOrder {
		info := result.modules[name]
		dependencyInitializers := make([]string, 0, len(info.Imports))
		for _, imported := range info.Imports {
			if dependency := result.irModules[imported]; dependency != nil && dependency.Initializer != "" {
				dependencyInitializers = append(dependencyInitializers, dependency.Initializer)
			}
		}
		moduleIR := loader.GenerateIRModule(info, args.mainModule, dependencyInitializers)
		result.irModules[name] = moduleIR
		result.templates[name] = loader.GenerateModuleGenericTemplateIR(info)
		iface := allInterfaces[name]
		result.interfaces[name] = iface
	}
	return result, nil
}

func sourceModuleGraph(sources, sourcePackages map[string]string, config comptime.Config, noStdlib bool) (map[string]*loader.ModuleInfo, map[string][]string, error) {
	modules := make(map[string]*loader.ModuleInfo)
	origins := make(map[string][]string)
	paths := make([]string, 0, len(sources))
	for origin := range sources {
		paths = append(paths, origin)
	}
	sort.Strings(paths)
	for _, origin := range paths {
		name := sourcePackages[origin]
		if name == "" {
			return nil, nil, fmt.Errorf("source %s has no canonical module path", origin)
		}
		tokens, err := tokeniser.NewTokeniser(sources[origin], origin).Tokenise()
		if err != nil {
			return nil, nil, err
		}
		fileConfig := config
		fileConfig.PackagePath = name
		tokens, err = comptime.Expand(tokens, fileConfig)
		if err != nil {
			return nil, nil, err
		}
		header, err := parser.ScanSourceHeader(tokens)
		if err != nil {
			return nil, nil, err
		}
		module := modules[name]
		if module == nil {
			module = &loader.ModuleInfo{Path: name, Name: header.Module, TrustedStandardLibrary: name == "std" || strings.HasPrefix(name, "std.")}
			modules[name] = module
		}
		for _, imported := range header.Imports {
			if !containsString(module.Imports, imported) {
				module.Imports = append(module.Imports, imported)
			}
		}
		origins[name] = append(origins[name], origin)
	}
	for name, module := range modules {
		if !noStdlib && name != "std" && !strings.HasPrefix(name, "std.") && !containsString(module.Imports, "std") {
			module.Imports = append(module.Imports, "std")
		}
	}
	return modules, origins, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
