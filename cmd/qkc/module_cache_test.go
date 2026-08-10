package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/types"
)

func TestQKMRoundTrip(t *testing.T) {
	t.Setenv("QK_CACHE_DIR", t.TempDir())
	args := &Args{mainModule: "app", target: "arm64-test", outputType: OutputExecutable}
	inputs, err := makeQKMInputs(args, map[string]string{"main.qk": "module main\n"}, map[string]string{"main.qk": "app"})
	if err != nil {
		t.Fatal(err)
	}
	want := &qkmFile{
		Primary: "app",
		Order:   []string{"std", "app"},
		Modules: map[string]*qkmModule{
			"std": {LLVM: "; std", Objects: map[string][]byte{"target": {1, 2, 3}}},
			"app": {LLVM: "; app"},
		},
		Interfaces: map[string]sema.ModuleInterface{"app": {Name: "app"}},
	}
	if err := storeQKM(inputs, want); err != nil {
		t.Fatal(err)
	}
	got, ok := loadQKM(inputs)
	if !ok {
		t.Fatal("stored .qkm was not loadable")
	}
	if got.Primary != want.Primary || got.Modules["app"].LLVM != "; app" ||
		!bytes.Equal(got.Modules["std"].Objects["target"], []byte{1, 2, 3}) {
		t.Fatalf("unexpected round trip: %#v", got)
	}
	if filepath.Ext(inputs.Path) != ".qkm" {
		t.Fatalf("cache path %q is not a .qkm", inputs.Path)
	}
}

func TestQKMGenericTemplateRoundTripAndInstantiation(t *testing.T) {
	parameter := types.TypeParameter{Owner: "box", Name: "T", Index: 0}
	function := ir.NewFunction("box", ir.LinkageExternal, nil)
	function.Signature = ir.FunctionSignature{ParamTypes: []types.Type{parameter}, ReturnType: parameter}
	function.Values[1] = parameter
	function.Blocks = []*ir.Block{{ID: 1, Instr: []ir.Instr{ir.Return{HasValue: true, Value: ir.ValueOperand(1, parameter)}}}}
	input := []ir.GenericTemplate{{Module: "example", Name: "box", Parameters: []types.TypeParameter{parameter}, Functions: []*ir.Function{function}}}
	encoded, err := encodeQKMTemplates(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeQKMTemplates(encoded)
	if err != nil {
		t.Fatal(err)
	}
	instances, err := ir.InstantiateGenericTemplate(decoded[0], []types.Type{types.PrimitiveI32})
	if err != nil {
		t.Fatal(err)
	}
	instance := instances[0]
	if !instance.Signature.ParamTypes[0].Equals(types.PrimitiveI32) || !instance.Signature.ReturnType.Equals(types.PrimitiveI32) {
		t.Fatalf("template types were not substituted: %#v", instance.Signature)
	}
	returned := instance.Blocks[0].Instr[0].(ir.Return)
	if !returned.Value.Type.Equals(types.PrimitiveI32) {
		t.Fatalf("instruction operand was not substituted: %v", returned.Value.Type)
	}
}

func TestQKMInputInvalidation(t *testing.T) {
	t.Setenv("QK_CACHE_DIR", t.TempDir())
	args := &Args{mainModule: "app", target: "arm64-test", outputType: OutputExecutable}
	base, err := makeQKMInputs(args, map[string]string{"main.qk": "module main\n"}, map[string]string{"main.qk": "app"})
	if err != nil {
		t.Fatal(err)
	}
	changedSource, _ := makeQKMInputs(args, map[string]string{"main.qk": "module main\n// changed\n"}, map[string]string{"main.qk": "app"})
	changedArgs := *args
	changedArgs.optLevel = OptLevel3
	sameFrontend, _ := makeQKMInputs(&changedArgs, map[string]string{"main.qk": "module main\n"}, map[string]string{"main.qk": "app"})
	changedTarget := *args
	changedTarget.target = "x86_64-test"
	differentFrontend, _ := makeQKMInputs(&changedTarget, map[string]string{"main.qk": "module main\n"}, map[string]string{"main.qk": "app"})
	if base.Hash == changedSource.Hash {
		t.Fatal("source change did not invalidate frontend cache")
	}
	if base.Hash != sameFrontend.Hash {
		t.Fatal("optimization level unnecessarily invalidated frontend cache")
	}
	if base.Hash == differentFrontend.Hash {
		t.Fatal("target change did not invalidate frontend cache")
	}
	if qkmObjectKey(args) == qkmObjectKey(&changedArgs) {
		t.Fatal("optimization level did not invalidate object variant")
	}
}

func TestQKMSourceHashesArePerModule(t *testing.T) {
	sources := map[string]string{"a/one.qk": "one", "a/two.qk": "two", "b/main.qk": "main"}
	packages := map[string]string{"a/one.qk": "a", "a/two.qk": "a", "b/main.qk": "b"}
	base := qkmSourceHashes(sources, packages)
	sources["b/main.qk"] = "changed"
	changed := qkmSourceHashes(sources, packages)
	if base["a"] != changed["a"] {
		t.Fatal("changing module b invalidated module a source identity")
	}
	if base["b"] == changed["b"] {
		t.Fatal("changing module b did not invalidate its source identity")
	}
}

func TestQKMModuleVariantRequiresMatchingDependencyInterfaces(t *testing.T) {
	candidate := &qkmModule{ComptimeHash: "bindings-a", ImportInterfaces: map[string]string{"dep": "interface-a"}}
	if matchingQKMModule([]*qkmModule{candidate}, map[string]string{"dep": "interface-b"}, "bindings-a") != nil {
		t.Fatal("module variant was reused with a different dependency interface")
	}
	if matchingQKMModule([]*qkmModule{candidate}, map[string]string{"dep": "interface-a"}, "bindings-b") != nil {
		t.Fatal("module variant was reused with different scoped compile-time bindings")
	}
	if matchingQKMModule([]*qkmModule{candidate}, map[string]string{"dep": "interface-a"}, "bindings-a") != candidate {
		t.Fatal("module variant was not reused with the same dependency interface")
	}
}

func TestCorruptQKMFallsBackToCompilation(t *testing.T) {
	t.Setenv("QK_CACHE_DIR", t.TempDir())
	inputs, err := makeQKMInputs(&Args{mainModule: "app"}, map[string]string{"x": "source"}, map[string]string{"x": "app"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(inputs.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputs.Path, []byte("not a qkm"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadQKM(inputs); ok {
		t.Fatal("corrupt .qkm was accepted")
	}
}
