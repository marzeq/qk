package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/marzeq/quokka/codegen"
	"github.com/marzeq/quokka/parser"
	"github.com/marzeq/quokka/tokeniser"
	"github.com/marzeq/quokka/typechecker"
)

func _check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCmd(args ...string) error {
	if len(args) == 0 {
		return nil
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return err
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: quokka [src file] [output binary]")
		return
	}

	srcfile := os.Args[1]
	binfile := os.Args[2]
	rest := os.Args[3:]
	binname := path.Base(binfile)

	t, err := tokeniser.NewTokeniserFromFile(srcfile)
	_check(err)

	toks, err := t.Tokenise()
	_check(err)

	p := parser.NewParser(toks)

	ast, err := p.Parse()
	_check(err)

	tc := typechecker.NewTypeChecker()
	ast, funcTable, err := tc.TypeCheck(ast)
	_check(err)

	cg := codegen.NewCodeGen(ast, funcTable)
	ir, err := cg.EmitIR()
	_check(err)

	os.Mkdir("build", 0755)
	os.WriteFile(fmt.Sprintf("build/%s.ssa", binname), []byte(ir), 0644)

	_check(runCmd("qbe", "-o", "build/"+binname+".s", "build/"+binname+".ssa"))
	args := append([]string{"cc", "-o", binfile, "build/" + binname + ".s"}, rest...)
	_check(runCmd(args...))
}
