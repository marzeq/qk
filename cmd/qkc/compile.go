package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marzeq/qk/codegen/llvm"
	"github.com/marzeq/qk/codegen/llvmbackend"
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

	if args.verbose {
		fmt.Printf("> libLLVM emit object %s\n", objPath)
	}
	err := llvmbackend.Compile(
		llvmOutput,
		objPath,
		filepath.Join(buildDir, "module.opt.ll"),
		llvmbackend.OutputObject,
		llvmBackendOptions(args),
	)
	if err != nil {
		return "", err
	}
	storeModuleObject(cachePath, objPath)

	return objPath, nil
}

func emitAssemblyFile(buildDir string, args *Args) error {
	llvmOutput, err := os.ReadFile(filepath.Join(buildDir, "module.ll"))
	if err != nil {
		return err
	}
	asmPath := filepath.Join(buildDir, "module.s")
	if args.verbose {
		fmt.Printf("> libLLVM emit assembly %s\n", asmPath)
	}
	return llvmbackend.Compile(
		string(llvmOutput),
		asmPath,
		filepath.Join(buildDir, "module.opt.ll"),
		llvmbackend.OutputAssembly,
		llvmBackendOptions(args),
	)
}

func llvmBackendOptions(args *Args) llvmbackend.Options {
	relocation := args.relocation
	if relocation == "" {
		relocation = "pic"
	}
	return llvmbackend.Options{
		TargetTriple:    args.target,
		CPU:             args.cpu,
		Features:        args.features,
		TargetABI:       args.targetABI,
		OptLevel:        string(args.optLevel),
		RelocationModel: relocation,
		CodeModel:       args.codeModel,
		Verbose:         args.verbose && args.debug,
	}
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
