package main

import (
	"fmt"
	"os"

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

func main() {
	t, err := tokeniser.NewTokeniserFromFile("test.qk")
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
	os.WriteFile("build/test.ir", []byte(ir), 0644)
}
