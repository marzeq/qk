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
let (Box($T)).get(self): T = self.value
let ([]$T: PartialEq).contains(self, value: T): bool = self[0] == value
`)
	if len(root.Body) != 6 {
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
	if len(method.GenericParameters) != 1 || method.GenericParameters[0].Name != "T" {
		t.Fatalf("unexpected owner captures: %#v", method.GenericParameters)
	}
	constrained := root.Body[5].(*FunctionDefNode)
	if len(constrained.GenericParameters) != 1 || constrained.GenericParameters[0].Constraint == nil {
		t.Fatalf("missing captured constraint: %#v", constrained.GenericParameters)
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
