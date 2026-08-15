package comptime

import (
	"strings"
	"testing"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/tokeniser"
)

func expandAndParse(t *testing.T, source string) *parser.RootNode {
	t.Helper()
	tokens, err := tokeniser.NewTokeniser(source, "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	tokens, err = Expand(tokens, Config{})
	if err != nil {
		t.Fatal(err)
	}
	root, err := parser.NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDollarBindingAndConditionalLinkCollection(t *testing.T) {
	root := expandAndParse(t, `
module test @link(
  when true { system "c", }
  else { system "unused", }
)
let $Enabled = true
when Enabled { let selected: i32 = 1 }
`)
	module := root.Body[0].(*parser.ModuleNode)
	link := module.Attributes.Get(attributes.AttributeTypeLink).(attributes.ModuleAttributeLink)
	if len(link.Links) != 1 || link.Links[0].Kind != attributes.LinkSystem || link.Links[0].Value != "c" {
		t.Fatalf("unexpected selected links: %#v", link.Links)
	}
	binding := root.Body[1].(*parser.DeclarationNode)
	if !binding.Comptime || binding.Name != "Enabled" {
		t.Fatalf("unexpected compile-time binding: %#v", binding)
	}
}

func TestUnselectedLinkBranchStillRequiresCompleteEntries(t *testing.T) {
	tokens, err := tokeniser.NewTokeniser(`module test @link(when true { system "c", } else { "fragment", })`, "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Expand(tokens, Config{})
	if err == nil || !strings.Contains(err.Error(), "complete link entry") {
		t.Fatalf("expected invalid unselected link branch, got %v", err)
	}
}

func TestWhenCannotSpliceExpressionFragments(t *testing.T) {
	tokens, err := tokeniser.NewTokeniser(`module test
let value = [when true { 1, } else { 2, }]
`, "test.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Expand(tokens, Config{})
	if err == nil || !strings.Contains(err.Error(), "complete declaration") {
		t.Fatalf("expected fragment placement error, got %v", err)
	}
}
