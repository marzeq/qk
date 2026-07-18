package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/marzeq/qk/codegen/llvm"
	"github.com/marzeq/qk/ir"
)

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
