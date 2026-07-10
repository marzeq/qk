package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

	if args.verbose {
		fmt.Println("parsed and collected modules")
	}

	modules, err := loader.BuildModules(partials)
	check(err)

	analyser := sema.NewAnalyser()

	order, errs := loader.ComputeModuleOrder(modules)
	checkErrs(errs)

	errs = loader.RunSemanticPipeline(modules, analyser, order, args.verbose, args.debug)
	checkErrs(errs)

	if args.verbose {
		fmt.Println("semantic analysis completed successfully")
	}

	irModules, errs := loader.GenerateIRModules(modules, args.mainModule, order, args.verbose)
	checkErrs(errs)

	llvmOutputs := buildLLVMModules(irModules, args.mainModule, order, args.outputType == OutputExecutable)

	if args.dumpIR {
		dumpIRModules(irModules)
	}

	if args.dumpLLVM {
		dumpLLVMModules(llvmOutputs, order)
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
		fatal("main function not found in main module (%s)", args.mainModule)
	}

	if args.noEmit {
		fmt.Println("typecheck successful, no output emitted due to -no-emit flag")
		return
	}

	buildDir, err := emitLLVMFiles(llvmOutputs, order)
	check(err)
	if !args.keepBuildDir {
		defer os.RemoveAll(buildDir)
	}

	if args.verbose {
		fmt.Printf("emitted LLVM files to %s\n", buildDir)
	}

	objFiles, err := compileLLVMModules(buildDir, order, args.optLevel, args.verbose, args.target, args.sysroot, args.clangArgs)
	check(err)

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

	os.Remove(args.output)

	err = linkObjects(objFiles, args.output, args.outputType, args.static, args.verbose, args.target, args.sysroot, args.linkArgs)
	check(err)

	if args.keepBuildDir {
		fmt.Printf("kept build directory: %s\n", buildDir)
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

func buildLLVMModules(mods map[string]*ir.Module, mainModule string, order []string, isExecutable bool) map[string]string {
	outputs := make(map[string]string, len(mods))

	for _, name := range order {
		mod := mods[name]
		if mod == nil {
			continue
		}

		emitter := &llvm.Emitter{ModuleName: name, Executable: isExecutable, MainModule: mainModule}
		var output strings.Builder
		emitter.EmitModule(&output, mod)
		outputs[name] = output.String()
	}

	return outputs
}

func dumpLLVMModules(mods map[string]string, order []string) {
	for i, name := range order {
		output, ok := mods[name]
		if !ok {
			continue
		}

		if i > 0 {
			fmt.Println()
		}
		fmt.Print(output)
		if !strings.HasSuffix(output, "\n") {
			fmt.Println()
		}
	}
}

func emitLLVMFiles(mods map[string]string, order []string) (string, error) {
	buildDir, err := os.MkdirTemp("/tmp", "qk-build-")
	if err != nil {
		return "", err
	}

	for _, name := range order {
		output, ok := mods[name]
		if !ok {
			continue
		}

		llPath := filepath.Join(buildDir, safeModuleFileName(name)+".ll")
		if err := os.WriteFile(llPath, []byte(output), 0o644); err != nil {
			return "", err
		}
	}

	return buildDir, nil
}

func compileLLVMModules(buildDir string, order []string, optLevel OptimisationLevel, verbose bool, target string, sysroot string, extraClangArgs []string) ([]string, error) {
	objFiles := make([]string, 0, len(order))

	for _, name := range order {
		llPath := filepath.Join(buildDir, safeModuleFileName(name)+".ll")
		if _, err := os.Stat(llPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		objPath := filepath.Join(buildDir, safeModuleFileName(name)+".o")
		clangArgs := []string{"-c", llPath, "-o", objPath, fmt.Sprintf("-O%s", optLevel)}
		if target != "" {
			clangArgs = append([]string{"-target", target}, clangArgs...)
		}
		if sysroot != "" {
			clangArgs = append(clangArgs, "--sysroot="+sysroot)
		}
		if len(extraClangArgs) > 0 {
			clangArgs = append(clangArgs, extraClangArgs...)
		}

		if verbose {
			fmt.Printf("> clang %s\n", strings.Join(clangArgs, " "))
		}
		cmd := exec.Command("clang", clangArgs...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("clang failed for module %q: %w\n%s", name, err, string(out))
		}

		objFiles = append(objFiles, objPath)
	}

	if len(objFiles) == 0 {
		return nil, fmt.Errorf("no object files were produced")
	}

	return objFiles, nil
}

func linkObjects(objFiles []string, output string, outputType OutputType, static, verbose bool, target string, sysroot string, extraLinkArgs []string) error {
	args, err := buildLinkArgs(objFiles, output, outputType, static, target, sysroot, extraLinkArgs)
	if err != nil {
		return err
	}

	if verbose {
		fmt.Printf("> clang %s\n", strings.Join(args, " "))
	}
	cmd := exec.Command("clang", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("linking failed: %w\n%s", err, string(out))
	}

	return nil
}

func buildLinkArgs(objFiles []string, output string, outputType OutputType, static bool, target string, sysroot string, extraLinkArgs []string) ([]string, error) {
	args := append([]string{}, objFiles...)

	switch outputType {
	case OutputExecutable:
	case OutputObject:
		args = append(args, "-r")
	case OutputSharedLib:
		if static {
			return nil, fmt.Errorf("cannot use --static with shared lib output")
		}
		args = append(args, "-shared", "-fPIC")
	default:
		return nil, fmt.Errorf("unknown output type")
	}

	if static {
		args = append(args, "-static")
	}

	if sysroot != "" {
		args = append([]string{"--sysroot=" + sysroot}, args...)
	}

	if target != "" {
		args = append([]string{"-target", target}, args...)
	}

	if len(extraLinkArgs) > 0 {
		args = append(args, extraLinkArgs...)
	}

	args = append(args, "-o", output)
	args = append(args, "-fuse-ld=lld")

	return args, nil
}

func safeModuleFileName(name string) string {
	if name == "" {
		return "module"
	}

	var b strings.Builder
	b.Grow(len(name))
	for i, r := range name {
		valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (r >= '0' && r <= '9')
		if !valid {
			b.WriteByte('_')
			continue
		}
		if i == 0 && r >= '0' && r <= '9' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}

	if b.Len() == 0 {
		return "module"
	}

	return b.String()
}
