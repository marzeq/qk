package main

import (
	"fmt"
	"strings"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/codegen/llvmbackend"
)

func linkObjects(objFiles []string, moduleLinks []attributes.Link, roots []string, config *Args) error {
	args, err := buildLinkArgs(objFiles, moduleLinks, roots, config)
	if err != nil {
		return err
	}

	return llvmbackend.Link(args, config.verbose)
}

func buildLinkArgs(objFiles []string, moduleLinks []attributes.Link, roots []string, config *Args) ([]string, error) {
	args := append([]string{}, objFiles...)

	switch config.outputType {
	case OutputExecutable:
	case OutputObject:
		if targetIsWindows(config.target) {
			return nil, fmt.Errorf("relocatable object output for Windows targets is unavailable")
		}
		args = append(args, "-r")
	case OutputSharedLib:
		if config.static {
			return nil, fmt.Errorf("cannot use --static with shared lib output")
		}
		args = append(args, "-shared")
	default:
		return nil, fmt.Errorf("unknown output type")
	}
	if config.outputType != OutputObject || !isWindowsGNUTarget(config.target) {
		args = append(args, deadStripLinkerFlag(config.target))
	}
	if config.outputType == OutputObject {
		for _, root := range roots {
			args = append(args, linkerUndefinedFlag(config.target, root))
		}
	}

	if config.static {
		args = append(args, "-static")
	}
	if config.noLibc {
		if config.outputType == OutputExecutable {
			args = append(args, "-nostdlib", "-Wl,-e,_start")
		} else {
			args = append(args, "-nolibc")
		}
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

func isWindowsGNUTarget(target string) bool {
	target = effectiveTargetName(target)
	windows := strings.Contains(target, "windows") || strings.Contains(target, "mingw")
	return windows && (strings.Contains(target, "gnu") || strings.Contains(target, "mingw"))
}

func deadStripLinkerFlag(target string) string {
	target = effectiveTargetName(target)
	switch {
	case targetIsApple(target):
		return "-Wl,-dead_strip"
	case strings.Contains(target, "windows"), strings.Contains(target, "mingw"), strings.Contains(target, "msvc"):
		return "-Wl,/OPT:REF"
	default:
		return "-Wl,--gc-sections"
	}
}

func linkerUndefinedFlag(target, symbol string) string {
	target = effectiveTargetName(target)
	switch {
	case targetIsApple(target):
		return "-Wl,-u,_" + symbol
	case isWindowsGNUTarget(target):
		return "-Wl,-u," + symbol
	case strings.Contains(target, "windows"), strings.Contains(target, "msvc"):
		return "-Wl,/INCLUDE:" + symbol
	default:
		return "-Wl,-u," + symbol
	}
}
