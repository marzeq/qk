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
	OptLevel0       OptimisationLevel = "0"
	OptLevel1       OptimisationLevel = "1"
	OptLevel2       OptimisationLevel = "2"
	OptLevel3       OptimisationLevel = "3"
	OptLevelSize    OptimisationLevel = "s"
	OptLevelSizeMax OptimisationLevel = "z"
	OptLevelFast    OptimisationLevel = "fast"
	OptLevelDebug   OptimisationLevel = "g"
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
	libs         []string
	libraryPaths []string
	nprocs       int
}

func parseOptLevel(level string) (OptimisationLevel, error) {
	switch level {
	case "g":
		return OptLevelDebug, nil
	case "s":
		return OptLevelSize, nil
	case "z":
		return OptLevelSizeMax, nil
	case "fast":
		return OptLevelFast, nil
	default:
		n, err := strconv.Atoi(level)
		if err != nil || n < 0 {
			return "", fmt.Errorf("invalid optimisation level: %s", level)
		}
		if n > 3 {
			fmt.Fprintf(os.Stderr, "warning: optimisation level %s is equivalent to -O3\n", level)
			n = 3
		}
		return OptimisationLevel(strconv.Itoa(n)), nil
	}
}

func parseOutputType(value string) (OutputType, error) {
	switch value {
	case "exe", "executable", ".exe":
		return OutputExecutable, nil
	case "obj", "object", ".o":
		return OutputObject, nil
	case "so", "shared", "sharedlib", ".so", ".dll", ".dylib":
		return OutputSharedLib, nil
	default:
		return OutputUnspecified, fmt.Errorf("unknown output type: %s", value)
	}
}

type argumentParser struct {
	args  *Args
	input []string
	index int
}

func newArgumentParser(input []string) *argumentParser {
	return &argumentParser{
		args:  &Args{optLevel: OptLevel2, outputType: OutputUnspecified, mainModule: "main"},
		input: input,
	}
}

func (p *argumentParser) current() string {
	return p.input[p.index]
}

func (p *argumentParser) nextValue(option string) (string, error) {
	p.index++
	if p.index >= len(p.input) {
		return "", fmt.Errorf("expected value after %s", option)
	}
	value := p.input[p.index]
	p.index++
	return value, nil
}

func (p *argumentParser) gluedOrNextValue(prefix string) (string, error) {
	option := p.current()
	if len(option) > len(prefix) {
		p.index++
		return option[len(prefix):], nil
	}
	return p.nextValue(prefix)
}

func (p *argumentParser) parseSplitArgs(option string, target *[]string) error {
	value, err := p.nextValue(option)
	if err != nil {
		return err
	}
	*target = append(*target, strings.Fields(value)...)
	return nil
}

func (p *argumentParser) parse() (*Args, error) {
	for p.index < len(p.input) {
		if err := p.parseCurrent(); err != nil {
			return nil, err
		}
	}

	if err := finaliseArgs(p.args); err != nil {
		return nil, err
	}
	return p.args, nil
}

func (p *argumentParser) parseCurrent() error {
	tok := p.current()

	switch {
	case tok == "-E":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		p.args.excludeDirs = append(p.args.excludeDirs, value)

	case tok == "-o":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		p.args.output = value

	case tok == "-t":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		outputType, err := parseOutputType(value)
		if err != nil {
			return err
		}
		p.args.outputType = outputType

	case tok == "-m":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		p.args.mainModule = value

	case strings.HasPrefix(tok, "-O"):
		value, err := p.gluedOrNextValue("-O")
		if err != nil {
			return err
		}
		level, err := parseOptLevel(value)
		if err != nil {
			return err
		}
		p.args.optLevel = level

	case tok == "-v":
		p.args.verbose = true
		p.index++

	case tok == "-d":
		p.args.debug = true
		p.index++

	case tok == "-no-emit":
		p.args.noEmit = true
		p.index++

	case tok == "-dump-ir":
		p.args.dumpIR = true
		p.index++

	case tok == "-dump-llvm":
		p.args.dumpLLVM = true
		p.index++

	case tok == "-dump-asm":
		p.args.dumpAsm = true
		p.index++

	case tok == "-keep-build-dir":
		p.args.keepBuildDir = true
		p.index++

	case tok == "-static":
		p.args.static = true
		p.index++

	case tok == "-target":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		p.args.target = value

	case tok == "-sysroot":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		p.args.sysroot = value

	case tok == "-Xcompile":
		if err := p.parseSplitArgs(tok, &p.args.clangArgs); err != nil {
			return err
		}

	case tok == "-Xlink":
		if err := p.parseSplitArgs(tok, &p.args.linkArgs); err != nil {
			return err
		}

	case strings.HasPrefix(tok, "-l"):
		value, err := p.gluedOrNextValue("-l")
		if err != nil {
			return err
		}
		p.args.libs = append(p.args.libs, value)

	case strings.HasPrefix(tok, "-L"):
		value, err := p.gluedOrNextValue("-L")
		if err != nil {
			return err
		}
		p.args.libraryPaths = append(p.args.libraryPaths, value)

	case tok == "-h" || tok == "--help":
		printUsage()
		os.Exit(0)

	default:
		if strings.HasPrefix(tok, "-") {
			return fmt.Errorf("unknown argument: %s", tok)
		}
		if p.args.baseDir != "" {
			return fmt.Errorf("multiple base directories specified")
		}
		p.args.baseDir = tok
		p.index++
	}

	return nil
}

func parseArgs() (*Args, error) {
	return newArgumentParser(os.Args[1:]).parse()
}

func printUsage() {
	fmt.Printf("Usage: %s [options] <baseDir>\n", os.Args[0])
	fmt.Println("Options:")
	fmt.Println("  -E <dir>           Exclude directory or file from source file search (can specify multiple times)")
	fmt.Println("  -o <file>          Output file name")
	fmt.Println("  -m <module>        Root module name (default: main)")
	fmt.Println("  -t <type>          Output type (exe, obj, so)")
	fmt.Println("  -O <level>         Optimisation level (0, 1, 2, 3, s, z, fast, g)")
	fmt.Println("  -static            Link with static libraries")
	fmt.Println("  -l <lib>           Link with library <lib> (can specify multiple times)")
	fmt.Println("  -target <triple>   Target triple for code generation")
	fmt.Println("  -sysroot <path>    Sysroot path for target")
	fmt.Println("  -Xcompile <args>   Additional arguments to pass to clang when building module object files")
	fmt.Println("  -Xlink <args>      Additional arguments to pass to clang when linking the final executable")
	fmt.Println("  -L <path>          Add library search path (can specify multiple times)")
	fmt.Println("  -no-emit           Do not emit any output files, just check for errors")
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
