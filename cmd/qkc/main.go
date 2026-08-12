package main

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"

	"github.com/marzeq/qk/comptime"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
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
	selectedStdlibSources := map[string]string{}
	stdlibSourceRoot := ""
	if args.noStdlib {
		// A genuinely library-free build must also work when no libs directory is
		// installed beside qkc.
	} else if args.stdlibPath != "" {
		stdlibSourceRoot = args.stdlibPath
		var err error
		selectedStdlibSources, err = stdlib.ReadSources(args.stdlibPath)
		check(err)
		if len(selectedStdlibSources) == 0 {
			fatal("no standard-library source files found in %s", args.stdlibPath)
		}
	} else {
		libraryRoot, err := resolveLibraryRoot()
		check(err)
		stdlibSourceRoot = libraryRoot
		selectedStdlibSources, err = stdlib.ReadSources(libraryRoot)
		check(err)
	}
	stdlibPackagePaths, availablePackages, err := stdlib.SourcePackagePaths(selectedStdlibSources, stdlibSourceRoot)
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
	comptimeConfig.ModuleBindings, err = comptime.ResolvePackageBindings(compileTimeSources, sourcePackagePaths, comptimeConfig)
	check(err)
	frontend, err := runFrontend(
		args, comptimeConfig, compileTimeSources, sourcePackagePaths, args.verbose, args.debug,
	)
	check(err)
	modules, order := frontend.modules, frontend.order
	irModules, genericTemplates := frontend.irModules, frontend.templates
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
	specializationIR, err := loader.ExtractGenericSpecializations(irModules, genericTemplates, frontend.interfaces)
	check(err)
	if len(specializationIR.Functions) != 0 {
		irModules[loader.SpecializationModule] = specializationIR
		order = append(order, loader.SpecializationModule)
	}
	requestedModuleLinks := linksForUsedForeignSymbols(
		frontend.partials, irModules, args.outputType == OutputObject,
	)
	probeLibcFreeLink := args.outputType == OutputExecutable && targetIsLinuxX8664(args.target) &&
		moduleLinksContainLibc(requestedModuleLinks)
	moduleLinks := requestedModuleLinks
	if probeLibcFreeLink {
		// IR reachability is intentionally conservative around vtables and
		// constant-specialized branches. Let the final section-GC link prove
		// whether libc is actually needed before committing to the hosted CRT.
		moduleLinks = withoutLibcLinks(requestedModuleLinks)
	}
	linksLibc := moduleLinksContainLibc(moduleLinks)

	llvmOutputs := make(map[string]string, len(irModules))
	for _, moduleName := range order {
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
		objFile, err = compileLLVMModule(buildDir, moduleName, llvmOutputs[moduleName], args)
		check(err)
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
		objFile, err = compileLLVMModule(buildDir, "freestanding runtime", freestandingRuntime, args)
		check(err)
		objFiles = append(objFiles, objFile)
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
	if err != nil && probeLibcFreeLink {
		if args.verbose {
			fmt.Printf("libc-free link retained live libc references; retrying with the hosted runtime: %v\n", err)
		}
		hostedRuntime, runtimeErr := buildFreestandingRuntime(
			args.target, true, true, !args.noStdlib, mainInitializer, userMain,
		)
		check(runtimeErr)
		buildDir, runtimeErr := emitLLVMFile(hostedRuntime)
		check(runtimeErr)
		buildDirs = append(buildDirs, buildDir)
		if !args.keepBuildDir {
			defer os.RemoveAll(buildDir)
		}
		if args.dumpAsm {
			check(emitAssemblyFile(buildDir, args))
			check(dumpAssemblyFile(buildDir))
		}
		runtimeObject, runtimeErr := compileLLVMModule(buildDir, "hosted runtime", hostedRuntime, args)
		check(runtimeErr)
		if len(objFiles) == 0 {
			fatal("hosted runtime retry has no runtime object to replace")
		}
		objFiles[len(objFiles)-1] = runtimeObject
		moduleLinks = requestedModuleLinks
		_ = os.Remove(args.output)
		err = linkObjects(objFiles, moduleLinks, linkRoots, args)
	}
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
