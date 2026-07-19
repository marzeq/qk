package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/preprocessor"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/stdlib"
	"github.com/marzeq/qk/types"
)

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func main() {
	args, err := parseArgs()
	check(err)
	cleanupModuleObjectCache(args.verbose)

	searchPaths := buildSearchPaths(args.baseDir)
	preprocessorConfig := preprocessor.Config{TargetTriple: args.target, NoLibc: args.noLibc, NoStdlib: args.noStdlib}
	embeddedStdlibSources, err := stdlib.ReadSources()
	check(err)
	selectedStdlibSources := embeddedStdlibSources
	var stdlibFiles []string
	if args.stdlibPath != "" {
		stdlibFiles, err = collectSourceFiles([]string{args.stdlibPath}, nil)
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
	capabilitySources := selectedStdlibSources
	if args.noStdlib {
		capabilitySources = embeddedStdlibSources
	}
	preprocessorConfig.Capabilities, err = preprocessor.ResolveCapabilities(capabilitySources, preprocessorConfig, args.noStdlib)
	check(err)

	excludes := append([]string(nil), args.excludeDirs...)
	if args.stdlibPath != "" {
		excludes = append(excludes, args.stdlibPath)
	}
	files, err := collectSourceFiles(searchPaths, excludes)
	check(err)

	if len(files) == 0 {
		fatal("no source files found")
	}

	var partials []*loader.PartialModuleInfo

	for _, file := range files {
		ast, err := parseFile(file, preprocessorConfig)
		check(err)

		info, err := loader.CollectModuleInfo(ast, false)
		check(err)

		partials = append(partials, info)
	}
	if !args.noStdlib {
		trustedConfig := preprocessorConfig
		trustedConfig.TrustedStandardLibrary = true
		if args.stdlibPath != "" {
			for _, file := range stdlibFiles {
				ast, err := parseFile(file, trustedConfig)
				check(err)
				info, err := loader.CollectModuleInfo(ast, true)
				check(err)
				partials = append(partials, info)
			}
		} else {
			stdlibPartials, err := stdlib.ParseTrustedSources(selectedStdlibSources, trustedConfig)
			check(err)
			partials = append(partials, stdlibPartials...)
		}
	}

	if args.verbose && args.debug {
		fmt.Println("parsed and collected modules")
	}

	modules, err := loader.BuildModules(partials)
	check(err)
	if !args.noStdlib {
		for name, module := range modules {
			if name != "std" && !containsString(module.Imports, "std") {
				module.Imports = append(module.Imports, "std")
			}
		}
	}

	analyser := sema.NewAnalyser()

	order, errs := loader.ComputeModuleOrder(modules, args.mainModule)
	checkErrs(errs)

	var warnings []error
	errs, warnings = loader.RunSemanticPipeline(modules, analyser, order, args.verbose, args.debug)
	checkErrs(errs)
	for _, w := range warnings {
		fmt.Println(w)
	}

	if args.verbose && args.debug {
		fmt.Println("semantic analysis completed successfully")
	}

	irModules, errs := loader.GenerateIRModules(modules, args.mainModule, order, args.verbose, args.debug)
	checkErrs(errs)

	llvmOutputs := make(map[string]string, len(irModules))
	for _, moduleName := range order {
		llvmOutputs[moduleName] = buildLLVMModule(irModules[moduleName], moduleName, args.mainModule, args.outputType == OutputExecutable, args.target)
	}
	mainInitializer := ""
	if mainIR := irModules[args.mainModule]; mainIR != nil {
		mainInitializer = mainIR.Initializer
	}
	freestandingRuntime, err := buildFreestandingRuntime(args.target, args.noLibc, args.outputType == OutputExecutable, mainInitializer)
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
		fatal("main module (%s) not found", args.mainModule)
	}

	foundMain := false
	mainModule := modules[args.mainModule]
	for _, root := range mainModule.Roots {
		for _, stmt := range root.Body {
			switch fn := stmt.(type) {
			case *parser.FunctionDefNode:
				if fn.Name == "main" {
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
	}
	if !foundMain && args.outputType == OutputExecutable {
		fatal("main function not found in primary module \"%s\"", args.mainModule)
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

		cmd := exec.Command(output)
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
