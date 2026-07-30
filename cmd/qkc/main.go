package main

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

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
	cleanupModuleObjectCache(args.verbose)

	searchPaths := buildSearchPaths(args.packageRoot)
	comptimeConfig := comptime.Config{TargetTriple: args.target, NoLibc: args.noLibc, NoStdlib: args.noStdlib}
	embeddedStdlibSources, err := stdlib.ReadSources()
	check(err)
	selectedStdlibSources := embeddedStdlibSources
	if args.stdlibPath != "" {
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
	if args.noStdlib {
		availablePackages = map[string]bool{}
	}
	discovered, compileTimeSources, sourcePackagePaths, err := discoverSourcePackages(
		args.mainModule, args.baseDir, args.file, searchPaths, availablePackages,
	)
	check(err)
	if !args.noStdlib {
		maps.Copy(compileTimeSources, selectedStdlibSources)
		maps.Copy(sourcePackagePaths, stdlibPackagePaths)
	}
	comptimeConfig.ModuleBindings, err = comptime.ResolvePackageBindings(compileTimeSources, sourcePackagePaths, comptimeConfig)
	check(err)

	var partials []*loader.PartialModuleInfo
	discoveredByPath := make(map[string]*sourcePackage, len(discovered))
	for _, pkg := range discovered {
		discoveredByPath[pkg.Path] = pkg
	}
	pending := []*sourcePackage{discovered[0]}
	parsedPackages := map[string]bool{}
	for len(pending) != 0 {
		pkg := pending[0]
		pending = pending[1:]
		if parsedPackages[pkg.Path] {
			continue
		}
		parsedPackages[pkg.Path] = true
		for _, file := range pkg.Files {
			config := comptimeConfig
			config.PackagePath = pkg.Path
			ast, err := parseSource(file, compileTimeSources[file], config)
			check(err)
			info, err := loader.CollectModuleInfo(ast, false)
			check(err)
			info.Path = pkg.Path
			partials = append(partials, info)
			for _, imported := range info.Imports {
				if dependency := discoveredByPath[imported]; dependency != nil {
					pending = append(pending, dependency)
				}
			}
		}
	}
	if !args.noStdlib {
		stdlibPartials, err := stdlib.ParseTrustedSources(selectedStdlibSources, comptimeConfig)
		check(err)
		partials = append(partials, stdlibPartials...)
	}

	if args.verbose && args.debug {
		fmt.Println("parsed and collected modules")
	}
	if args.run && discovered[0].Name != "main" {
		fatal("cannot run package %q: package must declare module main", discovered[0].Name)
	}
	if !args.run && args.outputType == OutputUnspecified && discovered[0].Name != "main" {
		args.outputType = OutputObject
	}
	check(finaliseOutputArgs(args))
	if args.outputType == OutputExecutable && discovered[0].Name != "main" {
		fatal("cannot build executable from package %q: package must declare module main", discovered[0].Name)
	}

	modules, err := loader.BuildModules(partials)
	check(err)
	if !args.noStdlib {
		for name, module := range modules {
			if name != "std" && !strings.HasPrefix(name, "std.") && !slices.Contains(module.Imports, "std") {
				module.Imports = append(module.Imports, "std")
			}
		}
	}

	analyser := sema.NewAnalyser()

	order, errs := loader.ComputeModuleOrder(modules, args.mainModule)
	checkErrs(errs)
	for _, path := range order {
		module := modules[path]
		if path == args.mainModule || module.TrustedStandardLibrary {
			continue
		}
		if module.Name == "main" {
			fatal("package %q declares module main; command packages cannot be imported", path)
		}
		if module.Name != path {
			fatal("package %q must declare module %q, found %q", path, path, module.Name)
		}
	}

	var warnings []error
	errs, warnings = loader.RunSemanticPipeline(modules, analyser, order, args.verbose, args.debug)
	for _, warning := range warnings {
		mode := args.warningMode
		if diagnostic, ok := warning.(shared.Error); ok {
			if override, exists := args.warningModes[string(diagnostic.WarningKind())]; exists {
				mode = override
			}
		}
		switch mode {
		case WarningModeShow:
			fmt.Println(warning)
		case WarningModeError:
			if diagnostic, ok := warning.(shared.Error); ok {
				errs = append(errs, diagnostic.AsError())
			} else {
				errs = append(errs, warning)
			}
		}
	}
	checkErrs(errs)

	if args.verbose && args.debug {
		fmt.Println("semantic analysis completed successfully")
	}

	irModules, errs := loader.GenerateIRModules(modules, args.mainModule, order, args.verbose, args.debug)
	checkErrs(errs)

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
		args.noLibc,
		args.noStdlib,
		args.outputType == OutputExecutable,
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

		objFile, err := compileLLVMModule(buildDir, moduleName, llvmOutputs[moduleName], args)
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
		objFile, err := compileLLVMModule(buildDir, "freestanding runtime", freestandingRuntime, args)
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

	var moduleLinks []attributes.Link
	for _, moduleName := range order {
		if module := modules[moduleName]; module != nil {
			moduleLinks = append(moduleLinks, module.Links...)
		}
	}
	var linkRoots []string
	if args.outputType == OutputObject {
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
