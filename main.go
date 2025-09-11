package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"

	"github.com/marzeq/quokka/codegen"
	"github.com/marzeq/quokka/modules"
	"github.com/marzeq/quokka/parser"
	"github.com/marzeq/quokka/shared"
	"github.com/marzeq/quokka/tokeniser"
	"github.com/marzeq/quokka/typechecker"
)

func _check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	prog := path.Base(os.Args[0])
	fmt.Printf("Usage: %s -o <output file> [-lib] [-lf ...] <src files>\n", prog)
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
	outFile, linkerFlags, sources, irOutput, asmOutput, buildDir := parseArgs()
	if len(sources) != 1 {
		fmt.Println("for now, only one source file is supported")
		os.Exit(1)
	}
	srcFile := sources[0]
	outBaseName := strings.TrimSuffix(path.Base(outFile), path.Ext(outFile))
	pathExt := path.Ext(outFile)

	var tmpDir string
	if buildDir == "" {
		td, err := os.MkdirTemp("", "quokka_build_*")
		_check(err)
		tmpDir = td
		defer os.RemoveAll(tmpDir)
	} else {
		tmpDir = buildDir
		os.MkdirAll(tmpDir, 0755)
	}

	t, err := tokeniser.NewTokeniserFromFile(srcFile)
	_check(err)

	toks, err := t.Tokenise()
	_check(err)

	p := parser.NewParser(toks)
	ast, err := p.Parse()
	_check(err)

	dg, err := modules.NewDepGraph(srcFile, ast)
	_check(err)

	files, err := dg.TopoSort()
	_check(err)

	ms := make(typechecker.ModulesSignatures)
	moduleToFiles := make(map[string][]string)

	for _, f := range files {
		mod, err := ms.CollectSignaturesFromRootNode(dg.ASTs[f])
		_check(err)
		moduleToFiles[mod] = append(moduleToFiles[mod], f)
	}

	processedModules := map[string]struct{}{}

	for mod, filesInMod := range moduleToFiles {
		if _, ok := processedModules[mod]; ok {
			continue
		}

		tc := typechecker.NewTypeChecker(mod, ms)

		for _, f := range filesInMod {
			ast, err := tc.TypeCheck(dg.ASTs[f])
			_check(err)
			dg.ASTs[f] = ast
		}

		processedModules[mod] = struct{}{}
	}

	ir := ""
	for _, f := range files {
		mod := ""
		for m, fs := range moduleToFiles {
			if slices.Contains(fs, f) {
				mod = m
			}
			if mod != "" {
				break
			}
		}

		cg := codegen.NewCodeGen(dg.ASTs[f], mod, ms)
		fileIr, err := cg.EmitIR()
		_check(err)
		ir += fileIr + "\n"
	}

	switch pathExt {
	case "":
		mainSig, ok := ms.LookupFunction("", "main", "")
		if !ok {
			fmt.Println("no main function found in the default module")
			os.Exit(1)
		}
		if len(mainSig.ArgTypes) != 0 {
			fmt.Println("main function must take no arguments")
			os.Exit(1)
		}
		if !mainSig.RetType.Compare(shared.PRIMITIVE_VOID) {
			fmt.Println("main function must return void")
			os.Exit(1)
		}
		ir += "\nexport function w $main() {\n"
		ir += "@start\n"
		ir += "  call $_main()\n"
		ir += "  ret 0\n"
		ir += "}\n"
	}

	ssaPath := path.Join(tmpDir, outBaseName+".ssa")
	_check(os.WriteFile(ssaPath, []byte(ir), 0644))
	if irOutput != "" {
		_check(copyFile(ssaPath, irOutput))
	}

	sPath := path.Join(tmpDir, outBaseName+".s")
	_check(runCmd("qbe", "-o", sPath, ssaPath))
	if asmOutput != "" {
		_check(copyFile(sPath, asmOutput))
	}

	oPath := path.Join(tmpDir, outBaseName+".o")
	_check(runCmd("cc", "-c", "-o", oPath, sPath))

	switch pathExt {
	case ".o":
		_check(copyFile(oPath, outFile))
	case ".a":
		_check(runCmd("ar", "rcs", outFile, oPath))
	case ".so":
		args := append([]string{"cc", "-shared", "-o", outFile, oPath}, linkerFlags...)
		_check(runCmd(args...))
	case "":
		if outFile == "" {
			outFile = "a.out"
		}
		args := append([]string{"cc", "-o", outFile, oPath}, linkerFlags...)
		_check(runCmd(args...))
	default:
		fmt.Printf("unknown output file extension: %s\n", pathExt)
	}
}
