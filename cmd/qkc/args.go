package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type OutputType int

const (
	OutputUnspecified OutputType = iota
	OutputExecutable
	OutputObject
	OutputSharedLib
)

type OptimisationLevel string

const (
	OptLevel0    OptimisationLevel = "0"
	OptLevel1    OptimisationLevel = "1"
	OptLevel2    OptimisationLevel = "2"
	OptLevel3    OptimisationLevel = "3"
	OptLevelSize OptimisationLevel = "s"
	OptLevelFast OptimisationLevel = "fast"
)

type Args struct {
	baseDir      string
	excludeDirs  []string
	output       string
	mainModule   string
	optLevel     OptimisationLevel
	verbose      bool
	debug        bool
	dumpIR       bool
	dumpLLVM     bool
	dumpAsm      bool
	keepBuildDir bool
	static       bool
	noEmit       bool
	target       string
	sysroot      string
	outputType   OutputType
	clangArgs    []string
	linkArgs     []string
}

func parseArgs() (*Args, error) {
	a := &Args{optLevel: OptLevel2, outputType: OutputUnspecified, mainModule: "main"}
	args := os.Args[1:]
	i := 0

	for i < len(args) {
		tok := args[i]

		switch {
		case tok == "-E":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after %s", tok)
			}
			a.excludeDirs = append(a.excludeDirs, args[i])
			i++

		case tok == "-o":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after %s", tok)
			}
			a.output = args[i]
			i++

		case tok == "-t":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after %s", tok)
			}
			switch args[i] {
			case "exe", "executable", ".exe":
				a.outputType = OutputExecutable
			case "obj", "object", ".o":
				a.outputType = OutputObject
			case "so", "shared", "sharedlib", ".so", ".dll", ".dylib":
				a.outputType = OutputSharedLib
			default:
				return nil, fmt.Errorf("unknown output type: %s", args[i])
			}
			i++

		case tok == "-m":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after %s", tok)
			}
			a.mainModule = args[i]
			i++

		case len(tok) >= 2 && tok[0:2] == "-O":
			i++
			tok = tok[2:]
			if tok == "" {
				return nil, fmt.Errorf("expected value after -O")
			}
			if tok == "s" {
				a.optLevel = OptLevelSize
			} else if tok == "fast" {
				a.optLevel = OptLevelFast
			} else if _, err := strconv.Atoi(tok); err == nil {
				a.optLevel = OptimisationLevel(tok)
			} else {
				return nil, fmt.Errorf("invalid optimisation level: %s", tok)
			}

		case tok == "-v":
			a.verbose = true
			i++

		case tok == "-d":
			a.debug = true
			i++

		case tok == "-no-emit":
			a.noEmit = true
			i++

		case tok == "-dump-ir":
			a.dumpIR = true
			i++

		case tok == "-dump-llvm":
			a.dumpLLVM = true
			i++

		case tok == "-dump-asm":
			a.dumpAsm = true
			i++

		case tok == "-keep-build-dir":
			a.keepBuildDir = true
			i++

		case tok == "-static":
			a.static = true
			i++

		case tok == "-target":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after -target")
			}
			a.target = args[i]
			i++

		case tok == "-sysroot":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after -sysroot")
			}
			a.sysroot = args[i]
			i++

		case tok == "-C":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after %s", tok)
			}
			parts := strings.Fields(args[i])
			a.clangArgs = append(a.clangArgs, parts...)
			i++

		case tok == "-L":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after %s", tok)
			}
			parts := strings.Fields(args[i])
			a.linkArgs = append(a.linkArgs, parts...)
			i++

		case tok == "-h" || tok == "--help":
			fmt.Printf("Usage: %s [options] <baseDir>\n", os.Args[0])
			fmt.Println("Options:")
			fmt.Println("  -E <dir>           Exclude directory from source file search (can specify multiple times)")
			fmt.Println("  -o <file>          Output file name")
			fmt.Println("  -m <module>        Root module name (default: main)")
			fmt.Println("  -t <type>          Output type (exe, obj, so)")
			fmt.Println("  -O<level>          Optimisation level (0, 1, 2, 3, s, fast)")
			fmt.Println("  -static            Link with static libraries")
			fmt.Println("  -target <triple>   Target triple for code generation")
			fmt.Println("  -sysroot <path>    Sysroot path for target")
			fmt.Println("  -C <args>          Additional arguments to pass to clang when building module object files")
			fmt.Println("  -L <args>          Additional arguments to pass to linker")
			fmt.Println("  -no-emit           Do not emit any output files, just check for errors")
			os.Exit(0)

		default:
			if tok[0] == '-' {
				return nil, fmt.Errorf("unknown argument: %s", tok)
			}
			if a.baseDir != "" {
				return nil, fmt.Errorf("multiple base directories specified")
			}
			a.baseDir = tok
			i++
		}
	}

	if err := finaliseArgs(a); err != nil {
		return nil, err
	}
	return a, nil
}

func finaliseArgs(args *Args) error {
	if args.baseDir == "" {
		return fmt.Errorf("no base directory specified")
	}

	if _, err := os.Stat(args.baseDir); os.IsNotExist(err) {
		return fmt.Errorf("base directory does not exist: %s", args.baseDir)
	}

	abs, err := filepath.Abs(args.baseDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path of base directory: %v", err)
	}
	args.baseDir = abs

	for i, e := range args.excludeDirs {
		if _, err := os.Stat(e); os.IsNotExist(err) {
			return fmt.Errorf("exclude path does not exist: %s", e)
		}
		abs, err := filepath.Abs(e)
		if err != nil {
			return fmt.Errorf("failed to get absolute path of exclude directory: %v", err)
		}
		args.excludeDirs[i] = abs
	}

	if args.sysroot != "" {
		if _, err := os.Stat(args.sysroot); os.IsNotExist(err) {
			return fmt.Errorf("sysroot path does not exist: %s", args.sysroot)
		}
		abs, err := filepath.Abs(args.sysroot)
		if err != nil {
			return fmt.Errorf("failed to get absolute path of sysroot: %v", err)
		}
		args.sysroot = abs
	}

	if args.output == "" {
		switch args.outputType {
		case OutputUnspecified:
			args.output = args.mainModule
			args.outputType = OutputExecutable
		case OutputExecutable:
			args.output = args.mainModule
		case OutputObject:
			args.output = args.mainModule + ".o"
		case OutputSharedLib:
			args.output = "lib" + args.mainModule + ".so"
		}
	} else {
		switch args.outputType {
		case OutputUnspecified:
			ext := filepath.Ext(args.output)
			switch ext {
			case ".o":
				args.outputType = OutputObject
			case ".so", ".dll", ".dylib":
				args.outputType = OutputSharedLib
			case "", ".exe":
				args.outputType = OutputExecutable
			default:
				return fmt.Errorf("cannot infer output type from extension: %s", ext)
			}
		}
	}

	return nil
}
