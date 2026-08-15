package loader

import (
	"testing"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/tokeniser"
)

func TestCompileTimeFunctionRunsThroughRegularIR(t *testing.T) {
	source := `
module test @link(
  when twice(1) == 2 { system "selected", }
  else { @compiler_error("wrong conditional link branch") }
)
let twice(value: i32): i32 = value * 2
let $Answer: i32 = twice(21)
when Answer == 42 {
  let selected: i32 = Answer
} else {
  @compiler_error("regular IR evaluation returned the wrong value")
}
@compiler_assert(twice(2) == 4, "ordinary function call failed")
`
	tokens, err := tokeniser.NewTokeniser(source, "staging.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	root, err := parser.NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if err := SelectCompileTime(root, "test", StageConfig{}); err != nil {
		t.Fatal(err)
	}
	for _, node := range root.Body {
		if _, unresolved := node.(*parser.WhenNode); unresolved {
			t.Fatal("compile-time selection left a when node in the module")
		}
	}
	answer, ok := root.Body[2].(*parser.DeclarationNode)
	if !ok {
		t.Fatalf("expected answer declaration, got %T", root.Body[2])
	}
	literal, ok := answer.Value.(*parser.IntegerLiteralNode)
	if !ok || literal.Value != "42" {
		t.Fatalf("expected evaluated literal 42, got %#v", answer.Value)
	}
	module := root.Body[0].(*parser.ModuleNode)
	link := module.Attributes.Get(attributes.AttributeTypeLink).(attributes.ModuleAttributeLink)
	if len(link.Links) != 1 || link.Links[0].Value != "selected" {
		t.Fatalf("unexpected selected links: %#v", link.Links)
	}
}
