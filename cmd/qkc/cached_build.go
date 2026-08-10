package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func runCachedQKM(cached *qkmFile, inputs qkmInputs, args *Args) error {
	for _, warning := range cached.Warnings {
		fmt.Println(warning)
	}
	for _, moduleName := range cached.Order {
		module := cached.Modules[moduleName]
		if module == nil || module.LLVM == "" {
			return fmt.Errorf("cached QK module %q has no implementation", moduleName)
		}
		if args.dumpLLVM {
			dumpLLVMModule(module.LLVM)
		}
	}
	if args.dumpLLVM && cached.RuntimeLLVM != "" {
		dumpLLVMModule(cached.RuntimeLLVM)
	}

	if args.noEmit {
		if args.dumpAsm {
			for _, moduleName := range cached.Order {
				buildDir, err := emitLLVMFile(cached.Modules[moduleName].LLVM)
				if err != nil {
					return err
				}
				defer os.RemoveAll(buildDir)
				if err := emitAssemblyFile(buildDir, args); err != nil {
					return err
				}
				if err := dumpAssemblyFile(buildDir); err != nil {
					return err
				}
			}
			if cached.RuntimeLLVM != "" {
				buildDir, err := emitLLVMFile(cached.RuntimeLLVM)
				if err != nil {
					return err
				}
				defer os.RemoveAll(buildDir)
				if err := emitAssemblyFile(buildDir, args); err != nil {
					return err
				}
				if err := dumpAssemblyFile(buildDir); err != nil {
					return err
				}
			}
		}
		return nil
	}

	objectKey := qkmObjectKey(args)
	objectsChanged := false
	var buildDirs []string
	var objectFiles []string
	for _, moduleName := range cached.Order {
		module := cached.Modules[moduleName]
		buildDir, err := emitLLVMFile(module.LLVM)
		if err != nil {
			return err
		}
		buildDirs = append(buildDirs, buildDir)
		if !args.keepBuildDir {
			defer os.RemoveAll(buildDir)
		}
		if args.dumpAsm {
			if err := emitAssemblyFile(buildDir, args); err != nil {
				return err
			}
			if err := dumpAssemblyFile(buildDir); err != nil {
				return err
			}
		}
		objectPath := filepath.Join(buildDir, "module.o")
		if materializeQKMObject(module, objectKey, objectPath) {
			if args.verbose {
				fmt.Printf("used cached object for module %s\n", moduleName)
			}
		} else {
			objectPath, err = compileLLVMModule(buildDir, moduleName, module.LLVM, args)
			if err != nil {
				return err
			}
			if rememberQKMObject(module, objectKey, objectPath) == nil {
				objectsChanged = true
			}
		}
		objectFiles = append(objectFiles, objectPath)
	}
	if cached.RuntimeLLVM != "" {
		buildDir, err := emitLLVMFile(cached.RuntimeLLVM)
		if err != nil {
			return err
		}
		buildDirs = append(buildDirs, buildDir)
		if !args.keepBuildDir {
			defer os.RemoveAll(buildDir)
		}
		if args.dumpAsm {
			if err := emitAssemblyFile(buildDir, args); err != nil {
				return err
			}
			if err := dumpAssemblyFile(buildDir); err != nil {
				return err
			}
		}
		objectPath := filepath.Join(buildDir, "module.o")
		if data := cached.RuntimeObjects[objectKey]; len(data) != 0 && os.WriteFile(objectPath, data, 0o644) == nil {
			if args.verbose {
				fmt.Println("used cached object for freestanding runtime")
			}
		} else {
			objectPath, err = compileLLVMModule(buildDir, "freestanding runtime", cached.RuntimeLLVM, args)
			if err != nil {
				return err
			}
			data, readErr := os.ReadFile(objectPath)
			if readErr == nil {
				if cached.RuntimeObjects == nil {
					cached.RuntimeObjects = make(map[string][]byte)
				}
				cached.RuntimeObjects[objectKey] = data
				objectsChanged = true
			}
		}
		objectFiles = append(objectFiles, objectPath)
	}
	if objectsChanged {
		if err := storeQKM(inputs, cached); err != nil && args.verbose {
			fmt.Fprintf(os.Stderr, "warning: failed to update QK module cache: %v\n", err)
		}
	}

	if err := prepareOutputPath(args.output); err != nil {
		return err
	}
	if err := linkObjects(objectFiles, cached.Links, cached.LinkRoots, args); err != nil {
		return err
	}
	if args.keepBuildDir {
		for _, buildDir := range buildDirs {
			fmt.Printf("kept build directory: %s\n", buildDir)
		}
	}
	return runOutput(args)
}

func prepareOutputPath(output string) error {
	stat, err := os.Stat(output)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("failed to stat %q: %w", output, err)
	case stat.IsDir():
		return fmt.Errorf("output file %s is an existing directory", output)
	default:
		if err := os.Remove(output); err != nil {
			return fmt.Errorf("failed to remove existing output file %q: %w", output, err)
		}
		return nil
	}
}

func runOutput(args *Args) error {
	if !args.run {
		return nil
	}
	output, err := filepath.Abs(args.output)
	if err != nil {
		return err
	}
	command := exec.Command(output, args.programArgs...)
	command.Stdout, command.Stderr, command.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := command.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			fmt.Printf("\nExit code: %d\n", code)
			_ = os.Remove(args.output)
			os.Exit(code)
		}
		return err
	}
	fmt.Println("\nExit code: 0")
	_ = os.Remove(args.output)
	os.Exit(0)
	return nil
}
