package sema

import (
	"testing"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/tokeniser"
	"github.com/marzeq/qk/types"
)

func TestParenthesizedNominalMethodOwnerRequiresLexicalDeclaration(t *testing.T) {
	const source = "module main\nlet Counter = struct { value: i32 }\nlet (Counter).get(self): i32 = self.value\n"
	tokens, err := tokeniser.NewTokeniser(source, "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	root, err := parser.NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	analyser := NewAnalyser()
	analyser.AnalyseModule(root, "main", false)
	if len(analyser.Errors()) == 0 {
		t.Fatal("accepted external nominal method")
	}
}

func TestRecursiveExpressionBodyInfersReturnFromIfBranch(t *testing.T) {
	const source = `module main
let fib(n: usz) =
  if n <= 1 { n }
  else { fib(n - 1) + fib(n - 2) }
let main() { let result: usz = fib(8) }
`
	tokens, err := tokeniser.NewTokeniser(source, "recursive_return.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	root, err := parser.NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	analyser := NewAnalyser()
	analyser.AnalyseModule(root, "main", false)
	if errs := analyser.Errors(); len(errs) != 0 {
		t.Fatal(errs)
	}
	attributor := analyser.NewAttributor()
	attributor.AttributeModule(root)
	if errs := attributor.Errors(); len(errs) != 0 {
		t.Fatal(errs)
	}
	fib := root.Body[1].(*parser.FunctionDefNode)
	if got := fib.Symbol.Signature.ReturnType; !got.Equals(types.PrimitiveUsz) {
		t.Fatalf("inferred return type = %v, want usz", got)
	}
	validator := analyser.NewValidator()
	validator.ValidateModule(root)
	if errs := validator.Errors(); len(errs) != 0 {
		t.Fatal(errs)
	}
}
