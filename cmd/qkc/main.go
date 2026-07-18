package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/codegen/llvm"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
	"github.com/marzeq/qk/types"
)

func main() {
	args, err := parseArgs()
	check(err)
	cleanupModuleObjectCache(args.verbose)

	searchPaths := buildSearchPaths(args.baseDir)

	files, err := collectSourceFiles(searchPaths, args.excludeDirs)
	check(err)

	if len(files) == 0 {
		fatal("no source files found")
	}

	var partials []*loader.PartialModuleInfo

	for _, file := range files {
		ast, err := parseFile(file)
		check(err)

		info, err := loader.CollectModuleInfo(ast)
		check(err)

		partials = append(partials, info)
	}

	if args.verbose && args.debug {
		fmt.Println("parsed and collected modules")
	}

	modules, err := loader.BuildModules(partials)
	check(err)

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

	if args.dumpIR {
		for _, moduleName := range order {
			dumpIRModule(irModules[moduleName])
		}
	}

	if args.dumpLLVM {
		for _, moduleName := range order {
			dumpLLVMModule(llvmOutputs[moduleName])
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

func parseFile(path string) (*parser.RootNode, error) {
	t, err := tokeniser.NewTokeniserFromFile(path)
	if err != nil {
		return nil, err
	}

	toks, err := t.Tokenise()
	if err != nil {
		return nil, err
	}

	p := parser.NewParser(toks)

	return p.Parse()
}

func collectSourceFiles(paths []string, exclude []string) ([]string, error) {
	seen := map[string]struct{}{}
	excluded := map[string]struct{}{}

	for _, e := range exclude {
		abs, _ := filepath.Abs(e)
		excluded[abs] = struct{}{}
	}

	var files []string

	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}

		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			abs, _ := filepath.Abs(path)

			for ex := range excluded {
				if strings.HasPrefix(abs, ex) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}

			if !d.IsDir() && filepath.Ext(path) == ".qk" {
				if _, ok := seen[abs]; !ok {
					seen[abs] = struct{}{}
					files = append(files, abs)
				}
			}

			return nil
		})
	}

	return files, nil
}

func buildSearchPaths(baseDir string) []string {
	var paths []string

	paths = append(paths, baseDir)

	home, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths,
			filepath.Join(home, ".local", "share", "qk"),
		)
	}

	paths = append(paths, filepath.Join("/usr", "local", "lib", "qk"))

	paths = append(paths, filepath.Join("/usr", "lib", "qk"))

	return paths
}

func check(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func checkErrs(errs []error) {
	if len(errs) > 0 {
		for _, err := range errs {
			fmt.Println(err)
		}
		os.Exit(1)
	}
}

func fatal(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
	os.Exit(1)
}

func buildLLVMModule(mod *ir.Module, moduleName string, mainModule string, isExecutable bool, targetTriple string) string {
	emitter := &llvm.Emitter{ModuleName: moduleName, Executable: isExecutable, MainModule: mainModule, TargetTriple: targetTriple}
	var output strings.Builder
	emitter.EmitModule(&output, mod)
	return output.String()
}

func dumpLLVMModule(output string) {
	fmt.Print(output)
	if !strings.HasSuffix(output, "\n") {
		fmt.Println()
	}
}

func emitLLVMFile(output string) (string, error) {
	buildDir, err := os.MkdirTemp("/tmp", "qk-build-")
	if err != nil {
		return "", err
	}

	llPath := filepath.Join(buildDir, "module.ll")
	if err := os.WriteFile(llPath, []byte(output), 0o644); err != nil {
		return "", err
	}

	return buildDir, nil
}

func compileLLVMModule(buildDir, moduleName, llvmOutput string, args *Args) (string, error) {
	objPath := filepath.Join(buildDir, "module.o")
	cachePath := moduleObjectCachePath(llvmOutput, args)
	if cached, err := os.ReadFile(cachePath); err == nil {
		if err := os.WriteFile(objPath, cached, 0o644); err == nil {
			now := time.Now()
			_ = os.Chtimes(cachePath, now, now)
			if args.verbose {
				fmt.Printf("used cached module %s\n", moduleName)
			}
			return objPath, nil
		}
	}

	optimizedPath, err := optimizeLLVMModule(buildDir, args)
	if err != nil {
		return "", err
	}

	clangArgs := []string{
		"-c", optimizedPath, "-o", objPath,
		fmt.Sprintf("-O%s", args.optLevel),
		"-ffunction-sections",
		"-fdata-sections",
	}
	if args.target != "" {
		clangArgs = append([]string{"-target", args.target}, clangArgs...)
	}
	if args.sysroot != "" {
		clangArgs = append(clangArgs, "--sysroot="+args.sysroot)
	}
	if len(args.clangArgs) > 0 {
		clangArgs = append(clangArgs, args.clangArgs...)
	}

	if args.verbose {
		fmt.Printf("> clang %s\n", strings.Join(clangArgs, " "))
	}
	out, err := exec.Command("clang", clangArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("clang failed: %w\n%s", err, string(out))
	}
	storeModuleObject(cachePath, objPath)

	return objPath, nil
}

const (
	moduleCacheMaxAge  = 30 * 24 * time.Hour
	moduleCacheMaxSize = int64(512 << 20)
)

type moduleCacheEntry struct {
	path    string
	modTime time.Time
	size    int64
}

func cleanupModuleObjectCache(verbose bool) {
	cacheHome, err := os.UserCacheDir()
	if err != nil {
		return
	}
	cacheDir := filepath.Join(cacheHome, "qk", "modules")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-moduleCacheMaxAge)
	var cached []moduleCacheEntry
	var totalSize int64
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".o" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(cacheDir, entry.Name())
		if info.ModTime().Before(cutoff) {
			if os.Remove(path) == nil {
				removed++
			}
			continue
		}
		cached = append(cached, moduleCacheEntry{path: path, modTime: info.ModTime(), size: info.Size()})
		totalSize += info.Size()
	}

	sort.Slice(cached, func(i, j int) bool {
		return cached[i].modTime.Before(cached[j].modTime)
	})
	for _, entry := range cached {
		if totalSize <= moduleCacheMaxSize {
			break
		}
		if os.Remove(entry.path) == nil {
			totalSize -= entry.size
			removed++
		}
	}
	if verbose && removed > 0 {
		fmt.Printf("removed %d stale cached modules\n", removed)
	}
}

func moduleObjectCachePath(llvmOutput string, args *Args) string {
	cacheHome, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	hash := sha256.New()
	for _, value := range append([]string{
		"qk-module-object-v1",
		llvmOutput,
		string(args.optLevel),
		args.target,
		args.sysroot,
	}, args.clangArgs...) {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	key := hex.EncodeToString(hash.Sum(nil))
	return filepath.Join(cacheHome, "qk", "modules", key+".o")
}

func storeModuleObject(cachePath, objPath string) {
	if cachePath == "" {
		return
	}
	data, err := os.ReadFile(objPath)
	if err != nil {
		return
	}
	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}
	temp, err := os.CreateTemp(cacheDir, "module-*.o")
	if err != nil {
		return
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return
	}
	if err := temp.Close(); err != nil {
		return
	}
	_ = os.Rename(tempPath, cachePath)
}

func optimizeLLVMModule(buildDir string, args *Args) (string, error) {
	llPath := filepath.Join(buildDir, "module.ll")
	optimizedPath := filepath.Join(buildDir, "module.opt.ll")
	optArgs := []string{"-passes=globaldce", "-S", llPath, "-o", optimizedPath}
	if args.verbose {
		fmt.Printf("> opt %s\n", strings.Join(optArgs, " "))
	}
	out, err := exec.Command("opt", optArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("LLVM global dead-code elimination failed: %w\n%s", err, string(out))
	}
	return optimizedPath, nil
}

func linkObjects(objFiles []string, moduleLinks []attributes.Link, roots []string, config *Args) error {
	args, err := buildLinkArgs(objFiles, moduleLinks, roots, config)
	if err != nil {
		return err
	}

	if config.verbose {
		fmt.Printf("> clang %s\n", strings.Join(args, " "))
	}
	cmd := exec.Command("clang", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("linking failed: %w\n%s", err, string(out))
	}

	return nil
}

func buildLinkArgs(objFiles []string, moduleLinks []attributes.Link, roots []string, config *Args) ([]string, error) {
	args := append([]string{}, objFiles...)

	switch config.outputType {
	case OutputExecutable:
	case OutputObject:
		args = append(args, "-r")
	case OutputSharedLib:
		if config.static {
			return nil, fmt.Errorf("cannot use --static with shared lib output")
		}
		args = append(args, "-shared")
	default:
		return nil, fmt.Errorf("unknown output type")
	}
	args = append(args, deadStripLinkerFlag(config.target))
	if config.outputType == OutputObject {
		for _, root := range roots {
			args = append(args, linkerUndefinedFlag(config.target, root))
		}
	}

	if config.static {
		args = append(args, "-static")
	}
	if config.noLibc {
		args = append(args, "-nolibc")
	}

	if config.sysroot != "" {
		args = append([]string{"--sysroot=" + config.sysroot}, args...)
	}

	if config.target != "" {
		args = append([]string{"-target", config.target}, args...)
	}

	if len(config.linkArgs) > 0 {
		args = append(args, config.linkArgs...)
	}
	for _, link := range moduleLinks {
		switch link.Kind {
		case attributes.LinkSystem:
			args = append(args, "-l"+link.Value)
		case attributes.LinkPath:
			args = append(args, link.Value)
		case attributes.LinkSearchPath:
			args = append(args, "-L"+link.Value)
		case attributes.LinkFramework:
			args = append(args, "-framework", link.Value)
		default:
			return nil, fmt.Errorf("unknown module link kind %d", link.Kind)
		}
	}

	for _, lib := range config.libs {
		args = append(args, "-l"+lib)
	}

	for _, path := range config.libraryPaths {
		args = append(args, "-L"+path)
	}

	args = append(args, "-o", config.output)
	args = append(args, "-fuse-ld=lld")

	return args, nil
}

func deadStripLinkerFlag(target string) string {
	target = strings.ToLower(target)
	switch {
	case strings.Contains(target, "darwin"), strings.Contains(target, "apple"), strings.Contains(target, "macos"), strings.Contains(target, "ios"):
		return "-Wl,-dead_strip"
	case strings.Contains(target, "windows"), strings.Contains(target, "mingw"), strings.Contains(target, "msvc"):
		return "-Wl,/OPT:REF"
	default:
		return "-Wl,--gc-sections"
	}
}

func linkerUndefinedFlag(target, symbol string) string {
	target = strings.ToLower(target)
	switch {
	case strings.Contains(target, "darwin"), strings.Contains(target, "apple"), strings.Contains(target, "macos"), strings.Contains(target, "ios"):
		return "-Wl,-u,_" + symbol
	case strings.Contains(target, "windows"), strings.Contains(target, "mingw"), strings.Contains(target, "msvc"):
		return "-Wl,/INCLUDE:" + symbol
	default:
		return "-Wl,-u," + symbol
	}
}

func emitAssemblyFile(buildDir string, args *Args) error {
	llPath, err := optimizeLLVMModule(buildDir, args)
	if err != nil {
		return err
	}
	asmPath := filepath.Join(buildDir, "module.s")
	clangArgs := []string{"-S", llPath, "-o", asmPath, fmt.Sprintf("-O%s", args.optLevel)}

	if args.target != "" {
		clangArgs = append([]string{"-target", args.target}, clangArgs...)
	}
	if args.sysroot != "" {
		clangArgs = append(clangArgs, "--sysroot="+args.sysroot)
	}
	if len(args.clangArgs) > 0 {
		clangArgs = append(clangArgs, args.clangArgs...)
	}
	if args.verbose {
		fmt.Printf("> clang %s\n", strings.Join(clangArgs, " "))
	}
	out, err := exec.Command("clang", clangArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("clang failed while generating assembly: %w\n%s", err, string(out))
	}
	return nil
}

func dumpAssemblyFile(buildDir string) error {
	data, err := os.ReadFile(filepath.Join(buildDir, "module.s"))
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		fmt.Println()
	}
	return nil
}
