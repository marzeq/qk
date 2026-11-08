package sema

import (
	"testing"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/tokeniser"
	"github.com/marzeq/qk/types"
)

func TestParameterizedNominalMethodOwnerSyntax(t *testing.T) {
	source := `
let Box($T: type) = type struct { value: T }
let Box($).get(self): T = self.value
let Box($Foo).renamed(self): Foo = self.value
let Pair($A: type, $B: type) = type struct { first: A, second: B }
let Pair($).swap(self): Pair(B, A) = Pair(B, A).{ first=self.second, second=self.first }
let Box($).keep(self, $U: type, value: U): U = value
`
	tokens, err := tokeniser.NewTokeniser(source, "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	root, err := parser.NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	analyser := NewAnalyser()
	analyser.AnalyseModule(root, "test", false)
	if len(analyser.Errors()) != 0 {
		t.Fatalf("unexpected semantic errors: %v", analyser.Errors())
	}
	for _, test := range []struct {
		index int
		name  string
	}{{1, "T"}, {2, "Foo"}} {
		method := root.Body[test.index].(*parser.FunctionDefNode)
		if method.Symbol == nil || len(method.Symbol.GenericParameters) != 1 || method.Symbol.GenericParameters[0].Name != test.name {
			t.Fatalf("method %d did not capture %q: %#v", test.index, test.name, method.Symbol)
		}
	}
	pairMethod := root.Body[4].(*parser.FunctionDefNode)
	if len(pairMethod.Symbol.GenericParameters) != 2 || pairMethod.Symbol.GenericParameters[1].Name != "B" {
		t.Fatalf("method did not capture both owner parameters: %#v", pairMethod.Symbol.GenericParameters)
	}
	keep := root.Body[5].(*parser.FunctionDefNode)
	if len(keep.Symbol.GenericParameters) != 2 || keep.Symbol.GenericParameters[0].Name != "T" || keep.Symbol.GenericParameters[1].Name != "U" {
		t.Fatalf("method did not preserve its own type parameter after owner captures: %#v", keep.Symbol.GenericParameters)
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
