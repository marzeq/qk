package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/marzeq/qk/attributes"
)

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
	args = append(args, config.linkArgs...)

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

	args = append(args, "-o", config.output, "-fuse-ld=lld")
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
