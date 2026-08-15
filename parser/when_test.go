package parser

import (
	"testing"

	"github.com/marzeq/qk/tokeniser"
)

func TestWhenParsesThroughOrdinaryGrammar(t *testing.T) {
	source := `
module test
let choose(value: i32): i32 {
  when value > 0 { return value }
  else { return 0 }
}
when true {
  let Selected = type struct { value: i32 }
} else {
  this is deliberately invalid
}
let Integer = type when true { i32 } else { u32 }
let value = when true { 1 } else { 2 }
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
  when true { system "c", }
  else { @compiler_error("unsupported") }
)
when true { let selected: i32 = 1 } else { let selected: i32 = 2 }
let Integer = type when true { i32 } else { u32 }
let value = when true { 1 } else { 2 }
let choose(): i32 {
  when true { return value } else { return 0 }
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
