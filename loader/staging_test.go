package loader

import (
	"strings"
	"testing"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/tokeniser"
)

func TestFreestandingDisablesHostedCapabilities(t *testing.T) {
	source := `module test
import core.build
$if core.build.has_libc || core.build.has_filesystem || core.build.has_environment || core.build.has_process_exit {
  compiler_error("hosted capability leaked into freestanding mode")
}
let retained: i32 = 7
`
	tokens, err := tokeniser.NewTokeniser(source, "freestanding.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	root, err := parser.NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if err := SelectCompileTime(root, "test", StageConfig{TargetTriple: "x86_64-unknown-linux-gnu", Freestanding: true}); err != nil {
		t.Fatal(err)
	}
}

func TestTypeReturningFunctionInAnnotation(t *testing.T) {
	source := `module test
let identity(T: type): type { return T }
let relay(T: type): type = identity(T)
let value: relay(i32) = 7
`
	tokens, err := tokeniser.NewTokeniser(source, "type_function.qk").Tokenise()
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
	partial, err := CollectModuleInfo(root, false)
	if err != nil {
		t.Fatal(err)
	}
	partial.Path = "test"
	modules, err := BuildModules([]*PartialModuleInfo{partial})
	if err != nil {
		t.Fatal(err)
	}
	if errors, _ := RunSemanticModule(modules["test"], sema.NewAnalyser(), false, false); len(errors) != 0 {
		t.Fatal(errors[0])
	}
}

func TestBareTypeParameterRequiresCompileTimeCall(t *testing.T) {
	for _, test := range []struct {
		name, call string
		wantError  bool
	}{
		{"compile time", "$identity(i32, 7)", false},
		{"runtime", "identity(i32, 7)", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "module test\nlet identity(T: type, value: T): T = value\nlet result: i32 = " + test.call + "\n"
			tokens, err := tokeniser.NewTokeniser(source, "bare_type_parameter.qk").Tokenise()
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
			partial, err := CollectModuleInfo(root, false)
			if err != nil {
				t.Fatal(err)
			}
			partial.Path = "test"
			modules, err := BuildModules([]*PartialModuleInfo{partial})
			if err != nil {
				t.Fatal(err)
			}
			errors, _ := RunSemanticModule(modules["test"], sema.NewAnalyser(), false, false)
			if test.wantError != (len(errors) != 0) {
				t.Fatalf("errors = %v, want error %t", errors, test.wantError)
			}
		})
	}
}

func TestCompileTimeOnlyFunctionsCompose(t *testing.T) {
	source := `module test
let identity(T: type, value: T): T = value
let relay(T: type, value: T): T = identity(T, value)
let result: i32 = $relay(i32, 7)
`
	tokens, err := tokeniser.NewTokeniser(source, "composed_type_parameter.qk").Tokenise()
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
	partial, err := CollectModuleInfo(root, false)
	if err != nil {
		t.Fatal(err)
	}
	partial.Path = "test"
	modules, err := BuildModules([]*PartialModuleInfo{partial})
	if err != nil {
		t.Fatal(err)
	}
	if errors, _ := RunSemanticModule(modules["test"], sema.NewAnalyser(), false, false); len(errors) != 0 {
		t.Fatal(errors[0])
	}
}

func TestCompileTimeLayoutUsesSelectedTarget(t *testing.T) {
	source := "module test\nlet Header = type struct @packed { tag: u8, length: u32 }\nlet size(T: type): usz = sizeof(T)\nlet pointer_bytes: usz = $size(usz)\nlet pointer_alignment: usz = $alignof(usz)\nlet length_offset: usz = $offsetof(Header, length)\n"
	tokens, err := tokeniser.NewTokeniser(source, "layout_target.qk").Tokenise()
	if err != nil {
		t.Fatal(err)
	}
	root, err := parser.NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if err := SelectCompileTime(root, "test", StageConfig{TargetTriple: "wasm32-wasi"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"pointer_bytes": "4", "pointer_alignment": "4", "length_offset": "1"}
	for _, raw := range root.Body[3:] {
		declaration := raw.(*parser.DeclarationNode)
		value, ok := declaration.Value.(*parser.IntegerLiteralNode)
		if !ok || value.Value != want[declaration.Name] {
			t.Fatalf("%s = %#v, want %s", declaration.Name, declaration.Value, want[declaration.Name])
		}
	}
}

func TestBareLayoutOperationRequiresCompileTimeContext(t *testing.T) {
	source := "module test\nlet value: usz = sizeof(i32)\n"
	tokens, err := tokeniser.NewTokeniser(source, "bare_layout.qk").Tokenise()
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
	partial, err := CollectModuleInfo(root, false)
	if err != nil {
		t.Fatal(err)
	}
	partial.Path = "test"
	modules, err := BuildModules([]*PartialModuleInfo{partial})
	if err != nil {
		t.Fatal(err)
	}
	errors, _ := RunSemanticModule(modules["test"], sema.NewAnalyser(), false, false)
	if len(errors) == 0 || !strings.Contains(errors[0].Error(), "compile-time context") {
		t.Fatalf("expected compile-time context diagnostic, got %v", errors)
	}
}

func TestGenericCompileTimeBranchDefersUntilSpecialization(t *testing.T) {
	source := "module test\nlet size_class($T: type): i32 = $if sizeof(T) == 4 { 1 } else { 2 }\nlet small: i32 = size_class(i32)\nlet large: i32 = size_class(i64)\n"
	tokens, err := tokeniser.NewTokeniser(source, "generic_branch.qk").Tokenise()
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
	function := root.Body[1].(*parser.FunctionDefNode)
	if !function.DeferredStage {
		t.Fatal("generic $if was selected before its type argument was known")
	}
	partial, err := CollectModuleInfo(root, false)
	if err != nil {
		t.Fatal(err)
	}
	partial.Path = "test"
	modules, err := BuildModules([]*PartialModuleInfo{partial})
	if err != nil {
		t.Fatal(err)
	}
	analyser := sema.NewAnalyser()
	ConfigureDeferredStage(analyser, StageConfig{})
	if errors, _ := RunSemanticModule(modules["test"], analyser, false, false); len(errors) != 0 {
		t.Fatal(errors[0])
	}
	if len(modules["test"].Root.Body) < 6 {
		t.Fatalf("expected two concrete instances, got %d nodes", len(modules["test"].Root.Body))
	}
	irModules, irErrors := GenerateIRModules(modules, "test", []string{"test"}, false, false)
	if len(irErrors) != 0 {
		t.Fatal(irErrors[0])
	}
	templates := map[string][]ir.GenericTemplate{"test": GenerateModuleGenericTemplateIR(modules["test"])}
	specializations, err := ExtractGenericSpecializations(irModules, templates, analyser.ModuleInterfaces())
	if err != nil {
		t.Fatal(err)
	}
	evaluator := ir.NewEvaluator(irModules["test"], specializations)
	if _, err := evaluator.Run(irModules["test"].Initializer); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int64{"small": 1, "large": 2} {
		value, ok := findEvaluatedGlobal(evaluator.Globals, name)
		if !ok || value.Integer == nil || value.Integer.Int64() != want {
			t.Fatalf("%s = %#v, want %d", name, value, want)
		}
	}
}

func TestCompileTimeFunctionRunsThroughRegularIR(t *testing.T) {
	source := `
module test @link(
  $if twice(1) == 2 { system "selected", }
  else { compiler_error("wrong conditional link branch") }
)
import core.build
let twice(value: i32): i32 = value * 2
let Answer: i32 = $twice(21)
$if Answer == 42 {
  let selected: i32 = Answer
} else {
  compiler_error("regular IR evaluation returned the wrong value")
}

$compiler_assert(twice(2) == 4, "ordinary function call failed")
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
			t.Fatal("compile-time selection left a $if node in the module")
		}
	}
	answer, ok := root.Body[3].(*parser.DeclarationNode)
	if !ok {
		t.Fatalf("expected answer declaration, got %T", root.Body[3])
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

func TestCompileTimePrefixInNestedValuePositions(t *testing.T) {
	source := `module test
let twice(value: i32): i32 = value * 2
let global: i32 = 1 + $twice(20)
let main(): i32 = twice($twice(2)) + $(3 + 4)
`
	tokens, err := tokeniser.NewTokeniser(source, "nested_prefix.qk").Tokenise()
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
	global := root.Body[2].(*parser.DeclarationNode).Value.(*parser.BinaryOpNode)
	if literal, ok := global.Operand2.(*parser.IntegerLiteralNode); !ok || literal.Value != "40" {
		t.Fatalf("unexpected global operand: %#v", global.Operand2)
	}
	main := root.Body[3].(*parser.FunctionDefNode).Body.(*parser.BinaryOpNode)
	call := main.Operand1.(*parser.FunctionCallNode)
	if literal, ok := call.Args[0].(*parser.IntegerLiteralNode); !ok || literal.Value != "4" {
		t.Fatalf("unexpected call argument: %#v", call.Args[0])
	}
	if literal, ok := main.Operand2.(*parser.IntegerLiteralNode); !ok || literal.Value != "7" {
		t.Fatalf("unexpected binary operand: %#v", main.Operand2)
	}
}

func TestCompileTimeBindingExpandsInsideNestedArrayType(t *testing.T) {
	source := `
module test
let pipe_buffer_size = $32
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
	provider := parse("state.qk", "module test\nlet pipe_buffer_size = $32\n")
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

func TestLocalCompileTimeCallDoesNotStageUnrelatedImportedRuntimeCode(t *testing.T) {
	source := `
module test
import std.io
let sum(x, y: i32): i32 = x + y
let main() {
  let x = $sum(1, 2)
  std.io.print("The sum is: {}\n", x)
}
`
	tokens, err := tokeniser.NewTokeniser(source, "local_staging.qk").Tokenise()
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
	main := root.Body[3].(*parser.FunctionDefNode)
	binding := main.Body.(*parser.BlockNode).Body[0].(*parser.DeclarationNode)
	literal, ok := binding.Value.(*parser.IntegerLiteralNode)
	if !ok || literal.Value != "3" {
		t.Fatalf("expected evaluated local binding 3, got %#v", binding.Value)
	}
	bindingType, ok := binding.TypeNode.(*parser.NamedTypeNode)
	if !ok || bindingType.Name != "i32" {
		t.Fatalf("expected evaluated local binding to retain i32, got %#v", binding.TypeNode)
	}
}
