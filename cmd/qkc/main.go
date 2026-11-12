package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"sync"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/types"
)

func main() {
	if len(os.Args) == 1 {
		printUsage()
		return
	}
	args, err := parseArgs()
	check(err)
	if args.cpuProfile != "" {
		profile, err := os.Create(args.cpuProfile)
		check(err)
		if err := pprof.StartCPUProfile(profile); err != nil {
			_ = profile.Close()
			check(err)
		}
		defer func() {
			pprof.StopCPUProfile()
			check(profile.Close())
		}()
	}
	releaseMode := loader.StageDebug
	if args.release {
		releaseMode = loader.StageRelease
	}
	comptimeConfig := loader.StageConfig{
		TargetTriple: args.target,
		ReleaseMode:  releaseMode,
	}
	libraryRoot := ""
	if args.noStdlib {
		// A genuinely library-free build must also work when no libs directory is
		// installed beside qkc.
	} else if args.libraryPath != "" {
		libraryRoot = args.libraryPath
	} else {
		var err error
		libraryRoot, err = resolveLibraryRoot()
		check(err)
	}
	searchPaths := buildSearchPaths(args.packageRoot, args.packagePaths)
	if libraryRoot != "" {
		searchPaths = append([]string{libraryRoot}, searchPaths...)
	}
	discovered, compileTimeSources, sourcePackagePaths, trustedSources, importResolutions, err := discoverSourcePackages(
		args.mainModule, args.baseDir, args.file, searchPaths, args.sourceMounts, libraryRoot, !args.noStdlib,
	)
	check(err)
	parsedSources := make(map[string]*parser.RootNode, len(compileTimeSources))
	for _, pkg := range discovered {
		for path, root := range pkg.Roots {
			parsedSources[path] = root
		}
	}
	check(applyMainModuleAttributes(args, compileTimeSources, sourcePackagePaths, parsedSources))
	comptimeConfig.Freestanding = args.freestanding
	if args.run && discovered[0].Name != "main" {
		fatal("cannot run package %q: package must declare module main", discovered[0].Name)
	}
	if !args.run && args.output == "" && args.outputType == OutputUnspecified && discovered[0].Name != "main" {
		args.outputType = OutputStaticLibrary
	}
	check(finaliseOutputArgs(args))
	if args.freestanding && args.outputType != OutputExecutable {
		fatal("freestanding mode requires executable output")
	}
	if args.outputType == OutputExecutable && discovered[0].Name != "main" {
		fatal("cannot build executable from package %q: package must declare module main", discovered[0].Name)
	}
	frontend, err := runFrontend(
		args, comptimeConfig, compileTimeSources, sourcePackagePaths, trustedSources, importResolutions, parsedSources,
	)
	check(err)
	modules, order := frontend.modules, frontend.order
	for name, info := range modules {
		if name == args.mainModule {
			continue
		}
		for _, node := range info.Root.Body {
			declaration, ok := node.(*parser.ModuleNode)
			if !ok {
				continue
			}
			if declaration.Attributes.Get(attributes.AttributeTypeEntry) != nil || declaration.Attributes.Get(attributes.AttributeTypeFreestanding) != nil {
				fatal("@entry and @freestanding are only valid on the main module")
			}
		}
	}
	irModules, parameterizedTemplates := frontend.irModules, frontend.templates
	if args.freestanding {
		foundEntry := false
		if primary := irModules[args.mainModule]; primary != nil {
			for _, fn := range primary.Functions {
				if fn.Name == args.entry && fn.Linkage == ir.LinkageExternal && fn.Visibility == ir.VisibilityDefault && len(fn.Blocks) != 0 {
					foundEntry = true
					break
				}
			}
		}
		if !foundEntry {
			fatal("freestanding entry %q must be a defined @export function in the main module", args.entry)
		}
	}
	var warnings = frontend.warnings
	for _, warning := range warnings {
		mode := args.warningMode
		if diagnostic, ok := warning.(shared.Error); ok {
			if override, exists := args.warningModes[string(diagnostic.WarningKind())]; exists {
				mode = override
			}
		}
		switch mode {
		case WarningModeShow:
			text := warning.Error()
			fmt.Println(text)
		case WarningModeError:
			if diagnostic, ok := warning.(shared.Error); ok {
				check(diagnostic.AsError())
			} else {
				check(warning)
			}
		}
	}
	specializations, err := loader.ExtractParameterizedSpecializations(irModules, parameterizedTemplates, frontend.methodTargets)
	check(err)
	if len(specializations.Functions) != 0 {
		irModules[loader.SpecializationModule] = specializations
		order = append(order, loader.SpecializationModule)
	}
	if args.outputType == OutputExecutable {
		if !args.freestanding && !args.noStdlib {
			pruneUnreachableIR(irModules, "__qk_0_3_std2_os_global_args", "__qk_0_3_std2_os_global_env")
		} else {
			pruneUnreachableIR(irModules)
		}
	}
	requestedModuleLinks := linksForUsedForeignSymbols(
		frontend.partials, irModules, args.outputType == OutputObject || args.outputType == OutputStaticLibrary,
	)
	moduleLinks := requestedModuleLinks
	// Declarations alone do not contribute anything to an object. In particular,
	// injected compile-time modules and foreign binding modules often have no
	// runtime definitions after executable reachability pruning.
	emissionOrder := make([]string, 0, len(order))
	emissionModules := make(map[string]*ir.Module, len(order))
	for _, moduleName := range order {
		module := irModules[moduleName]
		hasDefinition := len(module.Globals) != 0
		for _, fn := range module.Functions {
			if !fn.CompileTimeOnly && len(fn.Blocks) != 0 {
				hasDefinition = true
				break
			}
		}
		if !hasDefinition {
			continue
		}
		units := []*ir.Module{module}
		if args.outputType == OutputExecutable && !args.freestanding {
			units = splitEmissionModule(module)
		}
		for index, unit := range units {
			name := emissionUnitName(moduleName, index)
			emissionOrder = append(emissionOrder, name)
			emissionModules[name] = unit
		}
	}
	if len(emissionOrder) == 0 {
		// An explicit object or archive may consist entirely of type declarations.
		// Keep one empty unit so the requested artifact remains well formed.
		emissionOrder = append(emissionOrder, args.mainModule)
		emissionModules[args.mainModule] = irModules[args.mainModule]
	}

	llvmOutputs := make(map[string]string, len(irModules))
	for _, moduleName := range emissionOrder {
		llvmOutputs[moduleName] = buildLLVMModule(
			emissionModules[moduleName],
			moduleName,
			args.outputType == OutputExecutable,
			args.target,
		)
	}
	mainInitializer := ""
	userMain := ""
	if mainIR := irModules[args.mainModule]; mainIR != nil {
		mainInitializer = mainIR.Initializer
		userMain = mainIR.Entry
	}
	hostedRuntime, err := buildHostedRuntime(
		args.target, args.freestanding, args.outputType == OutputExecutable,
		!args.noStdlib, mainInitializer, userMain,
	)
	check(err)
	if hostedRuntime != "" {
		llvmOutputs["__qk.runtime"] = hostedRuntime
	}

	if args.dumpIR {
		for _, moduleName := range order {
			dumpIRModule(irModules[moduleName])
		}
	}

	if args.dumpLLVM {
		for _, moduleName := range emissionOrder {
			dumpLLVMModule(llvmOutputs[moduleName])
		}
		if hostedRuntime != "" {
			dumpLLVMModule(hostedRuntime)
		}
	}

	if _, ok := modules[args.mainModule]; !ok {
		fatal("primary package not found")
	}

	foundMain := false
	mainModule := modules[args.mainModule]
	for _, stmt := range mainModule.Root.Body {
		switch fn := stmt.(type) {
		case *parser.FunctionDefNode:
			if fn.Name == "main" && !args.freestanding {
				if len(fn.ParameterizedParameters) != 0 {
					fatal("%v", shared.NewError(fn.Loc, "main function cannot have compile-time type parameters"))
				}
				if len(fn.Args) != 0 {
					fatal("%v", shared.NewError(fn.Loc, "main function must not have arguments"))
				}
				if fn.Body == nil {
					fatal("%v", shared.NewError(fn.Loc, "main function must have a body"))
				}
				if fn.Symbol.Signature.ReturnType != types.PrimitiveVoid {
					fatal("%v", shared.NewError(fn.Loc, "main function must return void"))
				}
				if len(fn.Symbol.Attributes) != 0 {
					fatal("%v", shared.NewError(fn.Loc, "main function must not have attributes"))
				}
				foundMain = true
				break
			}
		}
	}
	if !foundMain && args.outputType == OutputExecutable && !args.freestanding {
		fatal("main function not found in primary package")
	}

	var linkRoots []string
	if args.outputType == OutputObject || args.outputType == OutputStaticLibrary || args.outputType == OutputWebAssembly {
		for _, moduleName := range order {
			for _, fn := range irModules[moduleName].Functions {
				if fn.Linkage == ir.LinkageExternal && fn.Visibility == ir.VisibilityDefault {
					linkRoots = append(linkRoots, fn.Name)
				}
			}
			for _, global := range irModules[moduleName].Globals {
				if global.Linkage == ir.LinkageExternal && global.Visibility == ir.VisibilityDefault {
					linkRoots = append(linkRoots, global.Name)
				}
			}
		}
	}

	if args.noEmit {
		if args.dumpAsm {
			for _, moduleName := range emissionOrder {
				buildDir, err := emitLLVMFile(llvmOutputs[moduleName])
				check(err)
				defer os.RemoveAll(buildDir)

				err = emitAssemblyFile(buildDir, args)
				check(err)
				err = dumpAssemblyFile(buildDir)
				check(err)
			}
			if hostedRuntime != "" {
				buildDir, err := emitLLVMFile(hostedRuntime)
				check(err)
				defer os.RemoveAll(buildDir)
				check(emitAssemblyFile(buildDir, args))
				check(dumpAssemblyFile(buildDir))
			}
		}
		return
	}

	buildDirs := make([]string, len(emissionOrder))
	objFiles := make([]string, len(emissionOrder))
	compileErrors := make([]error, len(emissionOrder))
	compileSlots := make(chan struct{}, min(runtime.GOMAXPROCS(0), 6))
	var compileJobs sync.WaitGroup
	for index, moduleName := range emissionOrder {
		buildDir, err := emitLLVMFile(llvmOutputs[moduleName])
		check(err)
		buildDirs[index] = buildDir

		if !args.keepBuildDir {
			defer os.RemoveAll(buildDir)
		}

		if args.verbose {
			fmt.Printf("emitted LLVM for module %s to %s\n", moduleName, buildDir)
		}

		if args.dumpAsm {
			err := emitAssemblyFile(buildDir, args)
			check(err)
			err = dumpAssemblyFile(buildDir)
			check(err)
		}

		objFiles[index] = filepath.Join(buildDir, "module.o")
	}
	var runtimeObject string
	var runtimeError error
	var runtimeBuildDir string
	if hostedRuntime != "" {
		buildDir, err := emitLLVMFile(hostedRuntime)
		check(err)
		runtimeBuildDir = buildDir
		buildDirs = append(buildDirs, buildDir)
		if !args.keepBuildDir {
			defer os.RemoveAll(buildDir)
		}
		if args.dumpAsm {
			check(emitAssemblyFile(buildDir, args))
			check(dumpAssemblyFile(buildDir))
		}
	}
	compileOrder := make([]int, len(emissionOrder))
	for index := range compileOrder {
		compileOrder[index] = index
	}
	sort.Slice(compileOrder, func(i, j int) bool {
		return len(llvmOutputs[emissionOrder[compileOrder[i]]]) > len(llvmOutputs[emissionOrder[compileOrder[j]]])
	})
	for _, index := range compileOrder {
		moduleName := emissionOrder[index]
		buildDir := buildDirs[index]
		compileSlots <- struct{}{}
		compileJobs.Add(1)
		go func() {
			defer compileJobs.Done()
			defer func() { <-compileSlots }()
			_, compileErrors[index] = compileLLVMModule(buildDir, moduleName, llvmOutputs[moduleName], args)
		}()
	}
	if runtimeBuildDir != "" {
		compileSlots <- struct{}{}
		compileJobs.Add(1)
		go func() {
			defer compileJobs.Done()
			defer func() { <-compileSlots }()
			runtimeObject, runtimeError = compileLLVMModule(runtimeBuildDir, "hosted runtime", hostedRuntime, args)
		}()
	}
	compileJobs.Wait()
	for _, err := range compileErrors {
		check(err)
	}
	check(runtimeError)
	if runtimeObject != "" {
		objFiles = append(objFiles, runtimeObject)
	}
	stat, err := os.Stat(args.output)

	switch {
	case os.IsNotExist(err):
	// output file doesn't exist -> good

	case err != nil:
		fatal("failed to stat %q: %v", args.output, err)

	case stat.IsDir():
		fatal("output file %s is an existing directory", args.output)

	default:
		if err := os.Remove(args.output); err != nil {
			fatal("failed to remove existing output file %q: %v", args.output, err)
		}
	}

	err = linkObjects(objFiles, moduleLinks, linkRoots, args)
	check(err)

	if args.keepBuildDir {
		for _, buildDir := range buildDirs {
			fmt.Printf("kept build directory: %s\n", buildDir)
		}
	}

	if args.run {
		output, err := filepath.Abs(args.output)
		check(err)

		cmd := exec.Command(output, args.programArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		err = cmd.Run()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				code := exitErr.ExitCode()
				fmt.Printf("\nExit code: %d\n", code)
				os.Remove(args.output)
				os.Exit(code)
			}
			check(err)
		}

		fmt.Println("\nExit code: 0")
		os.Remove(args.output)
		os.Exit(0)
	}
}
