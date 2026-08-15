package loader

import (
	"testing"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
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

func TestCompileTimeBindingExpandsInsideNestedArrayType(t *testing.T) {
	source := `
module test
let $pipe_buffer_size = 32
let Pipe = type struct { x: f64 }
let GameState = type struct { pipes: [pipe_buffer_size]Pipe }
`
	tokens, err := tokeniser.NewTokeniser(source, "array_length.qk").Tokenise()
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
	state := root.Body[3].(*parser.TypeAliasNode).Type.(*parser.StructTypeNode)
	array := state.Fields[0].Type.(*parser.ArrayTypeNode)
	literal, ok := array.Length.(*parser.IntegerLiteralNode)
	if !ok || literal.Value != "32" {
		t.Fatalf("expected expanded array length 32, got %#v", array.Length)
	}
	info := &ModuleInfo{Path: "test", Name: "test", Root: root}
	if errors, _ := RunSemanticModule(info, sema.NewAnalyser(), false, false); len(errors) != 0 {
		t.Fatal(errors[0])
	}
}

func TestCompileTimeBindingsAreVisibleAcrossModuleFiles(t *testing.T) {
	parse := func(path, source string) *parser.RootNode {
		tokens, err := tokeniser.NewTokeniser(source, path).Tokenise()
		if err != nil {
			t.Fatal(err)
		}
		root, err := parser.NewParser(tokens).Parse()
		if err != nil {
			t.Fatal(err)
		}
		return root
	}
	consumer := parse("main.qk", "module test\nlet Buffer = type [pipe_buffer_size]u8\nlet use(): i32 = pipe_buffer_size\nlet make(): [pipe_buffer_size]u8 = [0; pipe_buffer_size]\n")
	provider := parse("state.qk", "module test\nlet $pipe_buffer_size = 32\n")
	if err := SelectCompileTimeRoots([]*parser.RootNode{consumer, provider}, "test", StageConfig{}); err != nil {
		t.Fatal(err)
	}
	if len(provider.Body) != 2 {
		t.Fatalf("provider retained %d nodes, want module and binding; consumer has %d", len(provider.Body), len(consumer.Body))
	}
	array := consumer.Body[1].(*parser.TypeAliasNode).Type.(*parser.ArrayTypeNode)
	literal, ok := array.Length.(*parser.IntegerLiteralNode)
	if !ok || literal.Value != "32" {
		t.Fatalf("expected cross-file array length 32, got %#v", array.Length)
	}
	partials := make([]*PartialModuleInfo, 0, 2)
	for _, root := range []*parser.RootNode{consumer, provider} {
		partial, err := CollectModuleInfo(root, false)
		if err != nil {
			t.Fatal(err)
		}
		partial.Path = "test"
		partials = append(partials, partial)
	}
	modules, err := BuildModules(partials)
	if err != nil {
		t.Fatal(err)
	}
	if len(modules["test"].Root.Body) != 5 {
		t.Fatalf("merged module has %d nodes", len(modules["test"].Root.Body))
	}
	if declaration, ok := modules["test"].Root.Body[4].(*parser.DeclarationNode); !ok || declaration.Name != "pipe_buffer_size" {
		t.Fatalf("merged provider declaration is %#v", modules["test"].Root.Body[4])
	}
	if errors, _ := RunSemanticModule(modules["test"], sema.NewAnalyser(), false, false); len(errors) != 0 {
		declaration := modules["test"].Root.Body[4].(*parser.DeclarationNode)
		function := modules["test"].Root.Body[2].(*parser.FunctionDefNode)
		identifier := function.Body.(*parser.IdentifierNode)
		t.Fatalf("%v\nprovider symbol: %#v\nuse symbol: %#v", errors[0], declaration.Symbol, identifier.Symbol)
	}
}
