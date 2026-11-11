package parser

import (
	"testing"

	"github.com/marzeq/qk/tokeniser"
)

func TestWhenParsesThroughOrdinaryGrammar(t *testing.T) {
	source := `
module test
let choose(value: i32): i32 {
  $if value > 0 { return value }
  else { return 0 }
}
$if true {
  let Selected = struct { value: i32 }
} else {
  this is deliberately invalid
}
let Integer = $if true { i32 } else { u32 }
let value = $if true { 1 } else { 2 }
`
	tokens, err := tokeniser.NewTokeniser(source, "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewParser(tokens).Parse()
	if err == nil {
		t.Fatal("expected invalid unselected branch to be parsed and rejected")
	}
}

func TestValidWhenContextsParse(t *testing.T) {
	source := `
module test @link(
  $if true { system "c", }
  else { compiler_error("unsupported") }
)
$if true { let selected: i32 = 1 } else { let selected: i32 = 2 }
let Integer = $if true { i32 } else { u32 }
let value = $if true { 1 } else { 2 }
let choose(): i32 {
  $if true { return value } else { return 0 }
}
`
	tokens, err := tokeniser.NewTokeniser(source, "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	root, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Body) != 5 {
		t.Fatalf("got %d top-level nodes", len(root.Body))
	}
}

func TestCompileTimeIfContextsParse(t *testing.T) {
	source := `
module test @link(
  $if true { system "c", }
  else if false { system "m", }
  else { compiler_error("unsupported") }
)
$if true { let selected: i32 = 1 } else if false { let selected: i32 = 2 }
let Integer = $if true { i32 } else { u32 }
let value = $if true { 1 } else { 2 }
let choose(): i32 {
  $if true { return value } else { return 0 }
}
`
	tokens, err := tokeniser.NewTokeniser(source, "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	root, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Body) != 5 {
		t.Fatalf("got %d top-level nodes", len(root.Body))
	}
}

func TestUnprefixedIntrinsicsParse(t *testing.T) {
	source := `
module test
let Raw: type = repr(Token)
let bytes = $sizeof(i32)
let alignment = $alignof(i32)
let count = len("hello")
let value = repr(token)
let data = $embed("data.txt")
`
	tokens, err := tokeniser.NewTokeniser(source, "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	root, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Body) != 7 {
		t.Fatalf("got %d top-level nodes", len(root.Body))
	}
}

func TestCompilerDirectiveRequiresCompileTimeContext(t *testing.T) {
	tokens, err := tokeniser.NewTokeniser("module test\ncompiler_error(\"message\")\n", "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(tokens).Parse(); err == nil {
		t.Fatal("bare compiler_error must require compile-time context")
	}
}
