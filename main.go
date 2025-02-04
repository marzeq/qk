package main

import (
	"fmt"
	"os"

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
	ast, err = tc.TypeCheck(ast)
	_check(err)
}
