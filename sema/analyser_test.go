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

func TestCompilerImplDeclarationsAreReservedForCoreNamespace(t *testing.T) {
	for _, module := range []string{"core", "core.extra", "main"} {
		t.Run(module, func(t *testing.T) {
			source := "module " + module + "\nlet supplied(value: i32): i32 @compiler_impl\n"
			tokens, err := tokeniser.NewTokeniser(source, "compiler_impl.qk").Tokenise()
			if err != nil {
				t.Fatal(err)
			}
			root, err := parser.NewParser(tokens).Parse()
			if err != nil {
				t.Fatal(err)
			}
			analyser := NewAnalyser()
			analyser.AnalyseModule(root, module, false)
			if errs := analyser.Errors(); len(errs) != 0 {
				t.Fatal(errs)
			}
			attributor := analyser.NewAttributor()
			attributor.AttributeModule(root)
			if errs := attributor.Errors(); len(errs) != 0 {
				t.Fatal(errs)
			}
			validator := analyser.NewValidator()
			validator.ValidateModule(root)
			if got := len(validator.Errors()); (got == 0) != (module != "main") {
				t.Fatalf("validation errors for %s: %v", module, validator.Errors())
			}
			if warnings := validator.Warnings(); len(warnings) != 0 {
				t.Fatalf("compiler supplied declaration warned about parameters: %v", warnings)
			}
		})
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
