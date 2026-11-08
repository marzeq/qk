package parser

import (
	"testing"

	"github.com/marzeq/qk/tokeniser"
)

func parseTestRoot(t *testing.T, source string) *RootNode {
	t.Helper()
	tokens, err := tokeniser.NewTokeniser(source, "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	root, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestComptimeTypeParametersAndOwnerCaptures(t *testing.T) {
	root := parseTestRoot(t, `
let Box($T: type) = type struct { value: T }
let Size($T: type) = @sizeof(T)
let identity($T: type, value: T): T = value
let make_box($T: type, value: T): Box(T) = Box(T).{ value=value }
let Box($).get(self): T = self.value
let Box($Foo).renamed(self): Foo = self.value
let ([]$T: PartialEq).contains(self, value: T): bool = self[0] == value
`)
	if len(root.Body) != 7 {
		t.Fatalf("got %d declarations", len(root.Body))
	}
	box := root.Body[0].(*TypeAliasNode)
	if len(box.GenericParameters) != 1 || box.GenericParameters[0].Name != "T" {
		t.Fatalf("unexpected type constructor parameters: %#v", box.GenericParameters)
	}
	size := root.Body[1].(*FunctionDefNode)
	if len(size.GenericParameters) != 1 || len(size.Args) != 0 {
		t.Fatalf("unexpected type-level function: %#v", size)
	}
	identity := root.Body[2].(*FunctionDefNode)
	if len(identity.GenericParameters) != 1 || len(identity.Args) != 1 {
		t.Fatalf("unexpected function parameters: %#v, args=%d", identity.GenericParameters, len(identity.Args))
	}
	method := root.Body[4].(*FunctionDefNode)
	if method.MethodOwnerType == nil || len(method.GenericParameters) != 0 {
		t.Fatalf("unexpected implicit owner capture: %#v", method)
	}
	renamed := root.Body[5].(*FunctionDefNode)
	if len(renamed.GenericParameters) != 1 || renamed.GenericParameters[0].Name != "Foo" {
		t.Fatalf("unexpected renamed owner capture: %#v", renamed.GenericParameters)
	}
	constrained := root.Body[6].(*FunctionDefNode)
	if len(constrained.GenericParameters) != 1 || constrained.GenericParameters[0].Constraint == nil {
		t.Fatalf("missing captured constraint: %#v", constrained.GenericParameters)
	}
}

func TestParameterizedNominalMethodOwnerRejectsOuterParentheses(t *testing.T) {
	tokens, err := tokeniser.NewTokeniser("let (Box($T)).get(self): T = self.value", "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(tokens).Parse(); err == nil {
		t.Fatal("expected outer parentheses around a nominal owner to be rejected")
	}
}

func TestTypeParametersRequireDollarBinders(t *testing.T) {
	tokens, err := tokeniser.NewTokeniser("let Box(T: type) = type struct { value: T }", "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(tokens).Parse(); err == nil {
		t.Fatal("expected an unmarked type parameter to be rejected")
	}
}

func TestAngleBracketGenericDeclarationsAreRejected(t *testing.T) {
	tokens, err := tokeniser.NewTokeniser("let Box<T> = type struct { value: T }", "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(tokens).Parse(); err == nil {
		t.Fatal("expected legacy angle-bracket declaration to be rejected")
	}
}
