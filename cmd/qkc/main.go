package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"

	"github.com/marzeq/qk/comptime"
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
	releaseMode := comptime.ReleaseModeDebug
	if args.release {
		releaseMode = comptime.ReleaseModeRelease
	}
	comptimeConfig := comptime.Config{
		TargetTriple: args.target,
		Sysroot:      args.sysroot,
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
	if args.run && discovered[0].Name != "main" {
		fatal("cannot run package %q: package must declare module main", discovered[0].Name)
	}
	if !args.run && args.output == "" && args.outputType == OutputUnspecified && discovered[0].Name != "main" {
		args.outputType = OutputStaticLibrary
	}
	check(finaliseOutputArgs(args))
	if args.outputType == OutputExecutable && discovered[0].Name != "main" {
		fatal("cannot build executable from package %q: package must declare module main", discovered[0].Name)
	}
	cache, cacheErr := newArtifactCache(args)
	if cacheErr != nil && args.verbose {
		fmt.Fprintf(os.Stderr, "warning: module artifact cache is unavailable: %v\n", cacheErr)
	}
	if args.dumpIR || args.dumpLLVM || args.dumpAsm {
		cache = nil
	}
	buildHash := buildInputHash(args, compileTimeSources, sourcePackagePaths)
	if cache != nil && !args.run {
		if snapshot, ok := cache.loadBuildSnapshot(buildHash); ok {
			if args.verbose {
				fmt.Println("used cached lowered QK build")
			}
			check(runCachedBuildSnapshot(cache, snapshot, args))
			return
		}
	}
	comptimeConfig.ModuleBindings, err = comptime.ResolvePackageBindingsWithImports(
		compileTimeSources, sourcePackagePaths, importResolutions, comptimeConfig,
	)
	check(err)
	frontend, err := runFrontend(
		args, comptimeConfig, compileTimeSources, sourcePackagePaths, trustedSources, importResolutions, args.verbose, args.debug,
	)
	check(err)
	modules, order := frontend.modules, frontend.order
	irModules, genericTemplates := frontend.irModules, frontend.templates
	var warnings = frontend.warnings
	var displayedWarnings []string
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
			displayedWarnings = append(displayedWarnings, text)
			fmt.Println(text)
		case WarningModeError:
			if diagnostic, ok := warning.(shared.Error); ok {
				check(diagnostic.AsError())
			} else {
				check(warning)
			}
		}
	}
	specializationUnits, err := loader.ExtractGenericSpecializationUnits(irModules, genericTemplates, frontend.interfaces)
	check(err)
	specializationMetadata := make(map[string]loader.SpecializationUnit, len(specializationUnits))
	for _, unit := range specializationUnits {
		digest := sha256.Sum256([]byte(unit.Key))
		name := loader.SpecializationModule + "." + hex.EncodeToString(digest[:8])
		irModules[name] = unit.IR
		specializationMetadata[name] = unit
		order = append(order, name)
	}
	requestedModuleLinks := linksForUsedForeignSymbols(
		frontend.partials, irModules, args.outputType == OutputObject || args.outputType == OutputStaticLibrary,
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
	artifactPaths := make(map[string]string, len(llvmOutputs))
	artifacts := make(map[string]*cachedArtifact, len(llvmOutputs))
	moduleHashes := make(map[string]string, len(llvmOutputs))
	for moduleName, llvmOutput := range llvmOutputs {
		if _, specialized := specializationMetadata[moduleName]; !specialized {
			moduleHashes[moduleName] = implementationHash(moduleName, llvmOutput)
		}
	}
	for moduleName, llvmOutput := range llvmOutputs {
		if cache == nil {
			artifacts[moduleName] = &cachedArtifact{ImplementationHash: implementationHash(moduleName, llvmOutput), Objects: make(map[string][]byte)}
			continue
		}
		var path string
		if unit, specialized := specializationMetadata[moduleName]; specialized {
			ownerHash := moduleHashes[unit.DefiningModule]
			if ownerHash == "" {
				ownerHash = specializationOwnerHash(moduleHashes)
			}
			digest := sha256.Sum256([]byte(unit.Key))
			path = cache.specializationPath(ownerHash, hex.EncodeToString(digest[:]))
		} else {
			path = cache.modulePath(moduleHashes[moduleName])
		}
		artifactPaths[moduleName] = path
		artifactHash := implementationHash(moduleName, llvmOutput)
		artifact, ok := loadCachedArtifact(path, artifactHash)
		if !ok {
			artifact = &cachedArtifact{ImplementationHash: artifactHash, Objects: make(map[string][]byte)}
		}
		artifacts[moduleName] = artifact
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
	if freestandingRuntime != "" {
		llvmOutputs["__qk.runtime"] = freestandingRuntime
		runtimeHash := implementationHash("__qk.runtime", freestandingRuntime)
		artifact := &cachedArtifact{ImplementationHash: runtimeHash, Objects: make(map[string][]byte)}
		if cache != nil {
			path := cache.modulePath(runtimeHash)
			artifactPaths["__qk.runtime"] = path
			if loaded, ok := loadCachedArtifact(path, runtimeHash); ok {
				artifact = loaded
			}
		}
		artifacts["__qk.runtime"] = artifact
	}

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
	if !foundMain && args.outputType == OutputExecutable {
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
	const objectKey = cachedNativeObjectKey
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
		artifact := artifacts[moduleName]
		if data := artifact.Objects[objectKey]; len(data) != 0 && os.WriteFile(objFile, data, 0o644) == nil {
			if args.verbose {
				fmt.Printf("used cached object for module %s\n", moduleName)
			}
		} else {
			objFile, err = compileLLVMModule(buildDir, moduleName, llvmOutputs[moduleName], args)
			check(err)
			if data, readErr := os.ReadFile(objFile); readErr == nil {
				artifact.Objects[objectKey] = data
				if path := artifactPaths[moduleName]; path != "" {
					if storeErr := storeCachedArtifact(path, artifact); storeErr != nil && args.verbose {
						fmt.Fprintf(os.Stderr, "warning: failed to update module artifact %s: %v\n", moduleName, storeErr)
					}
				}
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
		artifact := artifacts["__qk.runtime"]
		if data := artifact.Objects[objectKey]; len(data) != 0 && os.WriteFile(objFile, data, 0o644) == nil {
			if args.verbose {
				fmt.Println("used cached object for freestanding runtime")
			}
		} else {
			objFile, err = compileLLVMModule(buildDir, "freestanding runtime", freestandingRuntime, args)
			check(err)
			if data, readErr := os.ReadFile(objFile); readErr == nil {
				artifact.Objects[objectKey] = data
				if path := artifactPaths["__qk.runtime"]; path != "" {
					if storeErr := storeCachedArtifact(path, artifact); storeErr != nil && args.verbose {
						fmt.Fprintf(os.Stderr, "warning: failed to update runtime artifact: %v\n", storeErr)
					}
				}
			}
		}
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

	linkArgs := args
	if probeLibcFreeLink {
		// A failed libc-free link is an implementation detail of the probe, not
		// a user-facing linker failure. Only print the final link invocation.
		quietProbeArgs := *args
		quietProbeArgs.verbose = false
		quietProbeArgs.quietLink = true
		linkArgs = &quietProbeArgs
	}
	err = linkObjects(objFiles, moduleLinks, linkRoots, linkArgs)
	if err != nil && probeLibcFreeLink {
		if args.verbose {
			fmt.Println("libc-free link retained live libc references; retrying with the hosted runtime")
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
		if cache != nil {
			hostedRuntimeHash := implementationHash("__qk.runtime", hostedRuntime)
			hostedRuntimePath := cache.modulePath(hostedRuntimeHash)
			hostedArtifact := &cachedArtifact{ImplementationHash: hostedRuntimeHash, Objects: make(map[string][]byte)}
			if data, readErr := os.ReadFile(runtimeObject); readErr == nil {
				hostedArtifact.Objects[objectKey] = data
				if storeErr := storeCachedArtifact(hostedRuntimePath, hostedArtifact); storeErr != nil && args.verbose {
					fmt.Fprintf(os.Stderr, "warning: failed to update hosted runtime artifact: %v\n", storeErr)
				} else if storeErr == nil {
					artifactPaths["__qk.runtime"] = hostedRuntimePath
				}
			}
		}
		if len(objFiles) == 0 {
			fatal("hosted runtime retry has no runtime object to replace")
		}
		objFiles[len(objFiles)-1] = runtimeObject
		moduleLinks = requestedModuleLinks
		_ = os.Remove(args.output)
		err = linkObjects(objFiles, moduleLinks, linkRoots, args)
	}
	check(err)
	if cache != nil {
		storeSuccessfulBuildSnapshot(cache, buildHash, order, artifactPaths, moduleLinks, linkRoots, displayedWarnings, args.verbose)
	}

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
