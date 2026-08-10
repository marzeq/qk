package main

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"slices"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/comptime"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/stdlib"
	"github.com/marzeq/qk/types"
)

func main() {
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
	searchPaths := buildSearchPaths(args.packageRoot, args.packagePaths)
	releaseMode := comptime.ReleaseModeDebug
	if args.release {
		releaseMode = comptime.ReleaseModeRelease
	}
	comptimeConfig := comptime.Config{
		TargetTriple: args.target,
		Sysroot:      args.sysroot,
		ReleaseMode:  releaseMode,
	}
	embeddedStdlibSources, err := stdlib.ReadSources()
	check(err)
	selectedStdlibSources := embeddedStdlibSources
	if args.noStdlib {
		selectedStdlibSources = map[string]string{}
	} else if args.stdlibPath != "" {
		stdlibFiles, err := collectSourceFiles([]string{args.stdlibPath}, nil)
		check(err)
		if len(stdlibFiles) == 0 {
			fatal("no standard-library source files found in %s", args.stdlibPath)
		}
		selectedStdlibSources = make(map[string]string, len(stdlibFiles))
		for _, file := range stdlibFiles {
			data, err := os.ReadFile(file)
			check(err)
			selectedStdlibSources[file] = string(data)
		}
	}
	stdlibPackagePaths, availablePackages, err := virtualSourcePackagePaths(selectedStdlibSources)
	check(err)
	discovered, compileTimeSources, sourcePackagePaths, err := discoverSourcePackages(
		args.mainModule, args.baseDir, args.file, searchPaths, availablePackages,
	)
	check(err)
	maps.Copy(compileTimeSources, selectedStdlibSources)
	maps.Copy(sourcePackagePaths, stdlibPackagePaths)
	if args.run && discovered[0].Name != "main" {
		fatal("cannot run package %q: package must declare module main", discovered[0].Name)
	}
	if !args.run && args.output == "" && args.outputType == OutputUnspecified && discovered[0].Name != "main" {
		args.outputType = OutputObject
	}
	check(finaliseOutputArgs(args))
	if args.outputType == OutputExecutable && discovered[0].Name != "main" {
		fatal("cannot build executable from package %q: package must declare module main", discovered[0].Name)
	}
	qkmInputs, qkmErr := makeQKMInputs(args, compileTimeSources, sourcePackagePaths)
	var cachedQKM *qkmFile
	if qkmErr == nil && !args.dumpIR {
		cachedQKM, _ = loadQKM(qkmInputs)
		if cachedQKM != nil {
			if args.verbose {
				fmt.Printf("used cached QK modules from %s\n", qkmInputs.Path)
			}
			check(runCachedQKM(cachedQKM, qkmInputs, args))
			return
		}
	}
	comptimeConfig.ModuleBindings, err = comptime.ResolvePackageBindings(compileTimeSources, sourcePackagePaths, comptimeConfig)
	check(err)
	qkmInputs = withQKMComptimeVariant(qkmInputs, comptime.BindingsFingerprint(comptimeConfig.ModuleBindings))
	frontend, err := runIncrementalFrontend(args, comptimeConfig, compileTimeSources, sourcePackagePaths, qkmInputs, args.verbose, args.debug)
	check(err)
	modules, partials, order := frontend.modules, frontend.partials, frontend.order
	irModules, genericTemplates := frontend.irModules, frontend.templates
	for _, warning := range frontend.cachedWarnings {
		fmt.Println(warning)
	}
	var warnings = frontend.warnings
	var cachedWarningText []string
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
			cachedWarningText = append(cachedWarningText, text)
			fmt.Println(text)
		case WarningModeError:
			if diagnostic, ok := warning.(shared.Error); ok {
				check(diagnostic.AsError())
			} else {
				check(warning)
			}
		}
	}
	specializationIR, err := loader.ExtractGenericSpecializations(irModules, genericTemplates, frontend.interfaces)
	check(err)
	if len(specializationIR.Functions) != 0 {
		irModules[loader.SpecializationModule] = specializationIR
		order = append(order, loader.SpecializationModule)
	}
	moduleLinks := frontend.moduleLinks
	for _, link := range linksForUsedForeignSymbols(partials, irModules, args.outputType == OutputObject) {
		if !slices.Contains(moduleLinks, link) {
			moduleLinks = append(moduleLinks, link)
		}
	}
	linksLibc := moduleLinksContainLibc(moduleLinks)

	llvmOutputs := make(map[string]string, len(irModules))
	for _, moduleName := range order {
		if cachedModule := frontend.cachedModules[moduleName]; cachedModule != nil {
			llvmOutputs[moduleName] = cachedModule.LLVM
			continue
		}
		llvmOutputs[moduleName] = buildLLVMModule(
			irModules[moduleName],
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
	freestandingRuntime, err := buildFreestandingRuntime(
		args.target,
		linksLibc,
		args.outputType == OutputExecutable,
		!args.noStdlib,
		mainInitializer,
		userMain,
	)
	check(err)

	if args.dumpIR {
		for _, moduleName := range order {
			dumpIRModule(irModules[moduleName])
		}
	}

	if args.dumpLLVM {
		for _, moduleName := range order {
			dumpLLVMModule(llvmOutputs[moduleName])
		}
		if freestandingRuntime != "" {
			dumpLLVMModule(freestandingRuntime)
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
			if fn.Name == "main" {
				if len(fn.GenericParameters) != 0 {
					fatal("%v", shared.NewError(fn.Loc, "main function must not be generic"))
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
	if !foundMain && args.outputType == OutputExecutable {
		fatal("main function not found in primary package")
	}

	var linkRoots []string
	if args.outputType == OutputObject || args.outputType == OutputWebAssembly {
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

	moduleInterfaces := frontend.interfaces
	moduleSourceHashes := qkmSourceHashes(compileTimeSources, sourcePackagePaths)
	activeQKM := &qkmFile{
		Primary: args.mainModule, Order: append([]string(nil), order...),
		Modules: make(map[string]*qkmModule, len(order)), RuntimeLLVM: freestandingRuntime,
		Links: append([]attributes.Link(nil), moduleLinks...), LinkRoots: append([]string(nil), linkRoots...),
		Interfaces: moduleInterfaces, InterfaceHashes: qkmInterfaceHashes(moduleInterfaces), Warnings: cachedWarningText,
	}
	activeQKM.RuntimeObjects = findQKMRuntimeObjects(qkmInputs, freestandingRuntime)
	for _, moduleName := range order {
		encodedTemplates, encodeErr := encodeQKMTemplates(genericTemplates[moduleName])
		check(encodeErr)
		encodedIR, encodeErr := encodeQKMIR(irModules[moduleName])
		check(encodeErr)
		var imports []string
		var links []attributes.Link
		var objects map[string][]byte
		if module := modules[moduleName]; module != nil {
			imports = append([]string(nil), module.Imports...)
			links = append([]attributes.Link(nil), module.Links...)
		} else if cachedModule := frontend.cachedModules[moduleName]; cachedModule != nil {
			imports = append([]string(nil), cachedModule.Imports...)
			links = append([]attributes.Link(nil), cachedModule.Links...)
			objects = cachedModule.Objects
		}
		if objects == nil {
			objects = findQKMImplementationObjects(qkmInputs, moduleName, llvmOutputs[moduleName])
		}
		importInterfaces := make(map[string]string, len(imports))
		for _, imported := range imports {
			importInterfaces[imported] = activeQKM.InterfaceHashes[imported]
		}
		iface, hasInterface := moduleInterfaces[moduleName]
		var interfacePtr *sema.ModuleInterface
		if hasInterface {
			interfaceCopy := iface
			interfacePtr = &interfaceCopy
		}
		activeQKM.Modules[moduleName] = &qkmModule{
			SourceHash: moduleSourceHashes[moduleName], Imports: imports,
			ImportInterfaces: importInterfaces, Interface: interfacePtr,
			Templates: encodedTemplates, IR: encodedIR, LLVM: llvmOutputs[moduleName], Links: links, Objects: objects,
		}
	}
	if qkmErr == nil {
		if err := storeQKM(qkmInputs, activeQKM); err == nil {
			pruneOldQKMFiles(qkmInputs.Path)
		} else if args.verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to write QK module cache: %v\n", err)
		}
	}

	if args.noEmit {
		if args.dumpAsm {
			for _, moduleName := range order {
				buildDir, err := emitLLVMFile(llvmOutputs[moduleName])
				check(err)
				defer os.RemoveAll(buildDir)

				err = emitAssemblyFile(buildDir, args)
				check(err)
				err = dumpAssemblyFile(buildDir)
				check(err)
			}
			if freestandingRuntime != "" {
				buildDir, err := emitLLVMFile(freestandingRuntime)
				check(err)
				defer os.RemoveAll(buildDir)
				check(emitAssemblyFile(buildDir, args))
				check(dumpAssemblyFile(buildDir))
			}
		}
		return
	}

	var buildDirs []string
	var objFiles []string
	objectKey := qkmObjectKey(args)
	qkmObjectsChanged := false
	for _, moduleName := range order {
		buildDir, err := emitLLVMFile(llvmOutputs[moduleName])
		check(err)
		buildDirs = append(buildDirs, buildDir)

		if !args.keepBuildDir {
			defer os.RemoveAll(buildDir)
		}

		if args.verbose && args.debug {
			fmt.Printf("emitted LLVM for module %s to %s\n", moduleName, buildDir)
		}

		if args.dumpAsm {
			err := emitAssemblyFile(buildDir, args)
			check(err)
			err = dumpAssemblyFile(buildDir)
			check(err)
		}

		objFile := filepath.Join(buildDir, "module.o")
		if materializeQKMObject(activeQKM.Modules[moduleName], objectKey, objFile) {
			if args.verbose {
				fmt.Printf("used cached object for module %s\n", moduleName)
			}
		} else {
			objFile, err = compileLLVMModule(buildDir, moduleName, llvmOutputs[moduleName], args)
			check(err)
			if rememberQKMObject(activeQKM.Modules[moduleName], objectKey, objFile) == nil {
				qkmObjectsChanged = true
			}
		}
		objFiles = append(objFiles, objFile)
	}
	if freestandingRuntime != "" {
		buildDir, err := emitLLVMFile(freestandingRuntime)
		check(err)
		buildDirs = append(buildDirs, buildDir)
		if !args.keepBuildDir {
			defer os.RemoveAll(buildDir)
		}
		if args.dumpAsm {
			check(emitAssemblyFile(buildDir, args))
			check(dumpAssemblyFile(buildDir))
		}
		objFile := filepath.Join(buildDir, "module.o")
		if data := activeQKM.RuntimeObjects[objectKey]; len(data) != 0 && os.WriteFile(objFile, data, 0o644) == nil {
			if args.verbose {
				fmt.Println("used cached object for freestanding runtime")
			}
		} else {
			objFile, err = compileLLVMModule(buildDir, "freestanding runtime", freestandingRuntime, args)
			check(err)
			if data, readErr := os.ReadFile(objFile); readErr == nil {
				if activeQKM.RuntimeObjects == nil {
					activeQKM.RuntimeObjects = make(map[string][]byte)
				}
				activeQKM.RuntimeObjects[objectKey] = data
				qkmObjectsChanged = true
			}
		}
		objFiles = append(objFiles, objFile)
	}
	if qkmObjectsChanged && qkmErr == nil {
		if err := storeQKM(qkmInputs, activeQKM); err != nil && args.verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to update QK module cache: %v\n", err)
		}
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
