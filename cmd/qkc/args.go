package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Args struct {
	baseDir      string
	excludeDirs  []string
	output       string
	optLevel     int
	verbose      bool
	debug        bool
	dumpIR       bool
	dumpLLVM     bool
	keepBuildDir bool
	static       bool
	target       string
	sysroot      string
	ClangArgs    []string
	LinkArgs     []string
}

func parseArgs() (*Args, error) {
	a := &Args{optLevel: -1}
	args := os.Args[1:]
	i := 0

	for i < len(args) {
		tok := args[i]

		switch {
		case tok == "-E" || tok == "--exclude":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after %s", tok)
			}
			a.excludeDirs = append(a.excludeDirs, args[i])
			i++

		case tok == "-o" || tok == "--output":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after %s", tok)
			}
			a.output = args[i]
			i++

		case len(tok) >= 2 && tok[0:2] == "-O":
			// allow -O3 or -O 3
			if tok == "-O" {
				i++
				if i >= len(args) {
					return nil, fmt.Errorf("expected value after -O")
				}
				tok = args[i]
				i++
			} else {
				i++
				tok = tok[2:]
			}
			lvl, err := strconv.Atoi(tok)
			if err != nil || lvl < 0 || lvl > 3 {
				return nil, fmt.Errorf("invalid optimization level: %s", tok)
			}
			if a.optLevel != -1 {
				return nil, fmt.Errorf("optimization level specified multiple times")
			}
			a.optLevel = lvl

		case tok == "-v" || tok == "--verbose":
			a.verbose = true
			i++

		case tok == "-d" || tok == "--debug":
			a.debug = true
			i++

		case tok == "--dump-ir":
			a.dumpIR = true
			i++

		case tok == "--dump-llvm":
			a.dumpLLVM = true
			i++

		case tok == "--keep-build-dir":
			a.keepBuildDir = true
			i++

		case tok == "--static":
			a.static = true
			i++

		case tok == "--target":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after --target")
			}
			a.target = args[i]
			i++

		case tok == "--sysroot":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after --sysroot")
			}
			a.sysroot = args[i]
			i++

		case tok == "--clang-arg" || tok == "--clang-args":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after %s", tok)
			}
			// allow quoted multiple args; split into fields
			parts := strings.Fields(args[i])
			a.ClangArgs = append(a.ClangArgs, parts...)
			i++

		case tok == "--link-arg" || tok == "--link-args":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("expected value after %s", tok)
			}
			parts := strings.Fields(args[i])
			a.LinkArgs = append(a.LinkArgs, parts...)
			i++

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
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		args.baseDir = cwd
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

	if args.optLevel == -1 {
		args.optLevel = 2
	}

	if args.output == "" {
		args.output = "a.out"
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

	return nil
}
