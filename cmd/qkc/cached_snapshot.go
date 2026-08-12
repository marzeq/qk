package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const cachedNativeObjectKey = "native-object-v1"

func runCachedBuildSnapshot(cache *artifactCache, snapshot *cachedBuildSnapshot, args *Args) error {
	for _, warning := range snapshot.Warnings {
		fmt.Println(warning)
	}
	if args.noEmit && !args.dumpLLVM {
		return nil
	}
	var buildDirs []string
	var objectFiles []string
	materialize := func(name, relativePath string) error {
		artifact, ok := loadCachedArtifactAny(filepath.Join(cache.root, relativePath))
		if !ok {
			return fmt.Errorf("cached artifact %s is unavailable", name)
		}
		if args.dumpLLVM {
			dumpLLVMModule(artifact.LLVM)
		}
		if args.noEmit {
			return nil
		}
		object := artifact.Objects[cachedNativeObjectKey]
		if len(object) == 0 {
			return fmt.Errorf("cached artifact %s has no native object", name)
		}
		buildDir, err := os.MkdirTemp("", "qk-cached-build-*")
		if err != nil {
			return err
		}
		buildDirs = append(buildDirs, buildDir)
		path := filepath.Join(buildDir, "module.o")
		if err := os.WriteFile(path, object, 0o644); err != nil {
			return err
		}
		objectFiles = append(objectFiles, path)
		if args.verbose {
			fmt.Printf("used cached object for module %s\n", name)
		}
		return nil
	}
	for _, module := range snapshot.Modules {
		if err := materialize(module.Name, module.Path); err != nil {
			return err
		}
	}
	if snapshot.Runtime != "" {
		if err := materialize("freestanding runtime", snapshot.Runtime); err != nil {
			return err
		}
	}
	for _, buildDir := range buildDirs {
		if !args.keepBuildDir {
			defer os.RemoveAll(buildDir)
		}
	}
	if args.noEmit {
		return nil
	}
	if err := prepareCachedOutputPath(args.output); err != nil {
		return err
	}
	if err := linkObjects(objectFiles, snapshot.Links, snapshot.LinkRoots, args); err != nil {
		return err
	}
	if args.keepBuildDir {
		for _, buildDir := range buildDirs {
			fmt.Printf("kept build directory: %s\n", buildDir)
		}
	}
	return runCachedOutput(args)
}

func prepareCachedOutputPath(path string) error {
	stat, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("failed to stat %q: %w", path, err)
	case stat.IsDir():
		return fmt.Errorf("output file %s is an existing directory", path)
	default:
		return os.Remove(path)
	}
}

func runCachedOutput(args *Args) error {
	if !args.run {
		return nil
	}
	output, err := filepath.Abs(args.output)
	if err != nil {
		return err
	}
	command := exec.Command(output, args.programArgs...)
	command.Stdout, command.Stderr, command.Stdin = os.Stdout, os.Stderr, os.Stdin
	return command.Run()
}
