package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"runtime"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/tokeniser"
)

func _check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	prog := path.Base(os.Args[0])
	fmt.Printf("Usage: %s -o <output file> [-lf ...] <src files>\n", prog)
}

func runCmd(args ...string) error {
	if len(args) == 0 {
		return nil
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return out.Sync()
}

func parseArgs() (
	outfile string,
	linkerFlags []string,
	sources []string,
	irOutput string,
	asmOutput string,
	buildDir string,
) {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if len(arg) > 2 && arg[0] == '-' && arg[1] == '-' {
			arg = arg[1:]
		}
		switch arg {
		case "-o", "-output", "-out":
			if i+1 >= len(args) {
				fmt.Println("missing argument for output file")
				os.Exit(1)
			}
			outfile = args[i+1]
			i++
		case "-lf", "-lflags", "-linkerflags":
			if i+1 >= len(args) {
				fmt.Println("missing argument for linker flags")
			}
			linkerFlags = append(linkerFlags, args[i+1])
			i++
		case "-h", "-help":
			usage()
			os.Exit(0)
		// flags for debugging, do not document
		case "-iro", "-irout", "-iroutput":
			if i+1 >= len(args) {
				fmt.Println("missing argument for ir output file")
			}
			irOutput = args[i+1]
			i++
		case "-asmo", "-asmout", "-asmoutput":
			if i+1 >= len(args) {
				fmt.Println("missing argument for asm output file")
			}
			asmOutput = args[i+1]
			i++
		case "-bd", "-builddir":
			if i+1 >= len(args) {
				fmt.Println("missing argument for build dir")
			}
			buildDir = args[i+1]
			i++
		default:
			if len(arg) > 0 && arg[0] == '-' {
				fmt.Printf("unknown argument: %s\n", arg)
				os.Exit(1)
			}
			sources = append(sources, args[i])
		}
	}

	if len(sources) == 0 {
		usage()
		os.Exit(1)
	}
	return
}

func main() {
	_, _, sources, _, _, _ := parseArgs()
	if len(sources) != 1 {
		fmt.Println("for now, only one source file is supported")
		os.Exit(1)
	}
	srcFile := sources[0]

	t, err := tokeniser.NewTokeniserFromFile(srcFile)
	_check(err)

	toks, err := t.Tokenise()
	_check(err)

	p := parser.NewParser(toks)
	ast, err := p.Parse()
	_check(err)

	_ = ast
	runtime.Breakpoint()
}
