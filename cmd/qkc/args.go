package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	baseDir       string
	file          string
	excludeDirs   []string
	output        string
	mainModule    string
	mainModuleSet bool
	optLevel      OptimisationLevel
	verbose       bool
	debug         bool
	dumpIR        bool
	dumpLLVM      bool
	dumpAsm       bool
	keepBuildDir  bool
	static        bool
	noLibc        bool
	noStdlib      bool
	stdlibPath    string
	noEmit        bool
	target        string
	sysroot       string
	cpu           string
	features      string
	targetABI     string
	relocation    string
	codeModel     string
	outputType    OutputType
	linkArgs      []string
	libs          []string
	libraryPaths  []string
	run           bool
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
	case "obj", "object", ".o", ".obj":
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

	case tok == "-file":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		p.args.file = value

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

	case tok == "-run":
		p.args.run = true
		p.index++

	case tok == "-m":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		p.args.mainModule = value
		p.args.mainModuleSet = true

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

	case tok == "-nolibc":
		p.args.noLibc = true
		p.index++

	case tok == "-nostdlib":
		p.args.noStdlib = true
		p.index++

	case tok == "-stdlib":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		p.args.stdlibPath = value

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

	case tok == "-cpu":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		p.args.cpu = value

	case tok == "-features":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		p.args.features = value

	case tok == "-target-abi":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		p.args.targetABI = value

	case tok == "-relocation-model":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		switch value {
		case "default", "static", "pic", "dynamic-no-pic":
			p.args.relocation = value
		default:
			return fmt.Errorf("invalid relocation model: %s", value)
		}

	case tok == "-code-model":
		value, err := p.nextValue(tok)
		if err != nil {
			return err
		}
		switch value {
		case "default", "tiny", "small", "kernel", "medium", "large":
			p.args.codeModel = value
		default:
			return fmt.Errorf("invalid code model: %s", value)
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

	case tok == "-v" || tok == "--version":
		printVersion()
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
	fmt.Printf("Usage: %s [options] (<baseDir> | -file <file>)\n", os.Args[0])
	fmt.Println("Options:")
	fmt.Println("  -file <file>       Compile one file as the inferred primary module")
	fmt.Println("  -E <dir>           Exclude directory or file from source file search (can specify multiple times)")
	fmt.Println("  -o <file>          Output file name")
	fmt.Println("  -run               Automatically run output executable")
	fmt.Println("  -m <module>        Root module name (default: main)")
	fmt.Println("  -t <type>          Output type (exe, obj, so)")
	fmt.Println("  -O <level>         Optimisation level (0, 1, 2, 3, s, z, fast, g)")
	fmt.Println("  -static            Link with static libraries")
	fmt.Println("  -nolibc            Do not link against the C standard library")
	fmt.Println("  -nostdlib          Do not load the embedded QK standard library")
	fmt.Println("  -stdlib <dir>      Trust and use an external QK standard-library source tree")
	fmt.Println("  -l <lib>           Link with library <lib> (can specify multiple times)")
	fmt.Println("  -target <triple>   Target triple for code generation")
	fmt.Println("  -sysroot <path>    Sysroot path for target")
	fmt.Println("  -cpu <name>        LLVM target CPU (for example: native, x86-64-v3)")
	fmt.Println("  -features <list>   LLVM target features (for example: +avx2,-sse4.1)")
	fmt.Println("  -target-abi <name> Target-specific ABI name")
	fmt.Println("  -relocation-model <model>  Relocation model (default, static, pic, dynamic-no-pic)")
	fmt.Println("  -code-model <model>        Code model (default, tiny, small, kernel, medium, large)")
	fmt.Println("  -Xlink <args>      Additional arguments for the native link")
	fmt.Println("  -L <path>          Add library search path (can specify multiple times)")
	fmt.Println("  -no-emit           Do not emit any output files, just check for errors")
}

func printVersion() {
	fmt.Println("qk compiler version (in development)")
}

func finaliseArgs(args *Args) error {
	if args.baseDir == "" && args.file == "" {
		return fmt.Errorf("no base directory or file specified")
	}
	if args.baseDir != "" && args.file != "" {
		return fmt.Errorf("base directory and -file cannot be used together")
	}

	if args.file != "" {
		info, err := os.Stat(args.file)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("source file does not exist or is not a regular file: %s", args.file)
		}
		if !strings.EqualFold(filepath.Ext(args.file), ".qk") {
			return fmt.Errorf("source file must have a .qk extension: %s", args.file)
		}
		abs, err := filepath.Abs(args.file)
		if err != nil {
			return fmt.Errorf("failed to get absolute path of source file: %v", err)
		}
		args.file = abs
	} else {
		info, err := os.Stat(args.baseDir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("base directory does not exist or is not a directory: %s", args.baseDir)
		}
		abs, err := filepath.Abs(args.baseDir)
		if err != nil {
			return fmt.Errorf("failed to get absolute path of base directory: %v", err)
		}
		args.baseDir = abs
	}

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
	if args.noStdlib && args.stdlibPath != "" {
		return fmt.Errorf("-nostdlib and -stdlib cannot be used together")
	}
	if args.stdlibPath != "" {
		info, err := os.Stat(args.stdlibPath)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("standard-library path is not a directory: %s", args.stdlibPath)
		}
		abs, err := filepath.Abs(args.stdlibPath)
		if err != nil {
			return fmt.Errorf("failed to get absolute standard-library path: %v", err)
		}
		args.stdlibPath = abs
	}

	return nil
}

func finaliseOutputArgs(args *Args) error {
	if args.output == "" {
		switch args.outputType {
		case OutputUnspecified:
			args.output = defaultExecutableName(args.mainModule, args.target)
			args.outputType = OutputExecutable
		case OutputExecutable:
			args.output = defaultExecutableName(args.mainModule, args.target)
		case OutputObject:
			args.output = defaultObjectName(args.mainModule, args.target)
		case OutputSharedLib:
			args.output = defaultSharedLibraryName(args.mainModule, args.target)
		}
	} else {
		switch args.outputType {
		case OutputUnspecified:
			ext := strings.ToLower(filepath.Ext(args.output))
			switch ext {
			case ".o", ".obj":
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

	if args.run && args.outputType != OutputExecutable {
		return fmt.Errorf("cannot run non-executable output")
	}

	return nil
}

func defaultExecutableName(module, target string) string {
	if targetIsWindows(target) {
		return module + ".exe"
	}
	return module
}

func defaultObjectName(module, target string) string {
	if targetIsWindows(target) {
		return module + ".obj"
	}
	return module + ".o"
}

func defaultSharedLibraryName(module, target string) string {
	switch {
	case targetIsWindows(target):
		return module + ".dll"
	case targetIsApple(target):
		return "lib" + module + ".dylib"
	default:
		return "lib" + module + ".so"
	}
}

func effectiveTargetName(target string) string {
	if target != "" {
		return strings.ToLower(target)
	}
	return runtime.GOARCH + "-" + runtime.GOOS
}

func targetIsWindows(target string) bool {
	target = effectiveTargetName(target)
	return strings.Contains(target, "windows") || strings.Contains(target, "mingw") || strings.Contains(target, "msvc")
}

func targetIsApple(target string) bool {
	target = effectiveTargetName(target)
	return strings.Contains(target, "darwin") || strings.Contains(target, "apple") || strings.Contains(target, "macos") || strings.Contains(target, "ios")
}
