package main

import (
	"strings"
	"testing"

	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
)

func TestSelectedWhenGlobalUsesConstantInitializer(t *testing.T) {
	const path = "selected_global.qk"
	const source = `module main
import core.build
pub let Selected: i32 =
  $if core.build.os == .Linux { 17 }
  else { 20 }
let main() {}
`
	frontend, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true},
		loader.StageConfig{TargetTriple: "x86_64-unknown-linux-gnu", NoStdlib: true},
		map[string]string{path: source},
		map[string]string{path: "."},
		map[string]bool{path: false},
		map[string]map[string]string{".": {}},
		false,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}

	module := frontend.irModules["."]
	if module == nil {
		t.Fatal("main IR module was not generated")
	}
	if module.Initializer != "" {
		t.Fatalf("constant selected global generated module initializer %q", module.Initializer)
	}
	if len(module.Globals) != 1 {
		t.Fatalf("generated %d globals, want 1", len(module.Globals))
	}
	global := module.Globals[0]
	if global.Mutable {
		t.Fatal("constant selected global was emitted as mutable")
	}
	if global.Value.Kind != ir.OperandIntConst || global.Value.IntValue != "17" {
		t.Fatalf("selected global initializer is %#v, want integer constant 17", global.Value)
	}
}

func TestDirectTypeConstructorsAndFactory(t *testing.T) {
	const path = "type_values.qk"
	const source = `module main
let Pair = struct { left: i32, right: i32 }
let Byte = u8
let UserId = distinct(u64) {
  let raw(self): u64 = self.(u64)
  let from(raw: u64): UserId = raw.(UserId)
}
let Box(T: type): type = struct { value: T }
let LaterBox = Box(Later)
let Later = struct { value: i32 }
let make_number(): i32 = 3
let RuntimeResult = make_number()
let NumberTag(T: type): type = distinct(T) {
  let raw(self): T = self.(T)
}
let Tagged($T: type) = distinct(T) {
  let raw(self): T = self.(T)
}
let main() {
  let pair: Pair = .{ left=1, right=2 }
  let box: Box(i32) = .{ value=pair.left }
  let later: LaterBox = .{ value=.{ value=pair.right } }
  let user: UserId = 7.(UserId)
  let raw: u64 = user.raw()
  let made: UserId = UserId.from(raw)
  let tagged: Tagged(i32) = 8.(Tagged(i32))
  let tagged_raw: i32 = tagged.raw()
  let function_tag: NumberTag(u8) = 9.(NumberTag(u8))
  let function_raw: u8 = function_tag.raw()
  _ = made
  _ = later
  _ = RuntimeResult
  _ = tagged_raw
  _ = function_tag
  _ = function_raw
}
`
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true},
		loader.StageConfig{NoStdlib: true},
		map[string]string{path: source},
		map[string]string{path: "."},
		map[string]bool{path: false},
		map[string]map[string]string{".": {}},
		false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLexicalStructMethods(t *testing.T) {
	const path = "lexical_methods.qk"
	const source = `module main
let Counter = struct {
  value: i32
  let get(self): i32 = self.value
  let clone(self): Self = self
}

let Box(T: type): type = struct {
  value: T
  let get(self): T = self.value
  let clone(self): Self = self
}
let State = enum {
  On, Off
  let is_on(self): bool = self == State.On
}
let Bits = flags(u8) {
  First, Second
  let nonzero(self): bool = self != Bits.First
}
let Choice = union {
  number: i32
  let get(self): i32 = self.number
}
let main() {
  let counter: Counter = .{ value=3 }
  let boxed: Box(i32) = .{ value=4 }
  let a: i32 = counter.get()
  let b: i32 = boxed.get()
  let c: Counter = counter.clone()
  let d: Box(i32) = boxed.clone()
  let on: bool = State.On.is_on()
  let bits: bool = Bits.Second.nonzero()
  let choice: Choice = .{ number=5 }
  let number: i32 = choice.get()
}
`
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true}, loader.StageConfig{NoStdlib: true},
		map[string]string{path: source}, map[string]string{path: "."},
		map[string]bool{path: false}, map[string]map[string]string{".": {}}, false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestStructFieldSpread(t *testing.T) {
	const path = "field_spread.qk"
	const source = `module main
import core.types
let make_fields(): []core.types.FieldSpec = [.{ name="value", field_type=i32 }]
let Simple = struct { value: i32, other: u8 }
let Mode = enum { First, Second }
let inspect_record(): bool {
  let shape = core.types.inspect(Record)
  let members = core.types.inspect(Mode)
  return shape.kind == "struct" && len(shape.fields) == 2 && shape.fields[0].name == "value" && shape.fields[0].field_type == i32 && members.kind == "enum" && len(members.members) == 2 && members.members[1].name == "Second"
}

let Inspected: bool = $inspect_record()
let copied_fields(): []core.types.FieldSpec {
  let mut builder = core.types.StructBuilder.new()
  builder.add_field("built", i64)
  return builder.fields()
}
let Record = struct {
  ...make_fields(),
  other: u8
  let get(self): i32 = self.value
}
let make_record(): type = struct { ...make_fields() }
let generic_fields(T: type): []core.types.FieldSpec = [.{ name="item", field_type=T }]
let GenericRecord(T: type): type = struct {
  ...generic_fields(T),
  let get(self): T = self.item
}
let GenericMethod(T: type): type = struct {
  ...make_fields(),
  item: T
  let get(self): T = self.item
}
let BuiltRecord = struct { ...copied_fields() }
let main() {
  let record: Record = .{ value=7, other=1 }
  let generated: make_record() = .{ value=8 }
  let copied: BuiltRecord = .{ built=9 }
  let generic: GenericRecord(u16) = .{ item=11 }
  let generic_value: u16 = generic.get()
  let generic_method: GenericMethod(u16) = .{ value=12, item=13 }
  let generic_result: u16 = generic_method.get()
  let value: i32 = record.get()
  _ = value
  _ = generated
  _ = copied
  _ = generic
  _ = generic_value
  _ = generic_result
}
`
	frontend, err := runFrontend(&Args{mainModule: ".", noStdlib: true}, loader.StageConfig{NoStdlib: true},
		map[string]string{path: source}, map[string]string{path: "."},
		map[string]bool{path: false}, map[string]map[string]string{".": {}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range frontend.modules["."].Root.Body {
		if declaration, ok := node.(*parser.DeclarationNode); ok && declaration.Name == "Inspected" {
			if value, ok := declaration.Value.(*parser.BoolLiteralNode); !ok || value.Value != "true" {
				t.Fatalf("static inspection produced %#v", declaration.Value)
			}
			return
		}
	}
	t.Fatal("missing inspected result")
}

func TestStructFieldSpreadRejectsDuplicate(t *testing.T) {
	const path = "duplicate_spread.qk"
	const source = `module main
import core.types
let fields(): []core.types.FieldSpec = [.{ name="value", field_type=i32 }]
let Record = struct { value: i32, ...fields() }
let main() {}
`
	_, err := runFrontend(&Args{mainModule: ".", noStdlib: true}, loader.StageConfig{NoStdlib: true},
		map[string]string{path: source}, map[string]string{path: "."},
		map[string]bool{path: false}, map[string]map[string]string{".": {}}, false, false)
	if err == nil || !strings.Contains(err.Error(), "duplicate or empty struct field \"value\"") {
		t.Fatalf("expected duplicate spread field diagnostic, got %v", err)
	}
}

func TestLocalCompileTimeTypeValue(t *testing.T) {
	const path = "local_type_value.qk"
	const source = `module main
let make_type(): usz {
  let local = struct { value: i32 }
  let copy_type = local
  let sample: copy_type = .{ value=3 }
  return sizeof(local)
}
import core.types
let builder_signature(): bool {
  let mut builder = core.types.StructBuilder.new()
  builder.add_field("value", i32)
  let built = builder.finish()
  let mut copied = builder
  let more: []core.types.FieldSpec = [.{ name="extra", field_type=u8 }]
  copied.add_fields(more)
  let expanded = copied.finish()
  let mut packed = core.types.StructBuilder.new()
  packed.add_field("small", u8)
  packed.add_field("wide", u32)
  packed.set_packed(true)
  let Packed = packed.finish()
  let shape = core.types.inspect(built)
  return sizeof(built) == sizeof(i32) && expanded != built && builder.finish() == built && len(builder.fields()) == 1 && builder.fields()[0].field_type == i32 && sizeof(Packed) == 5 && shape.kind == "struct" && len(shape.fields) == 1 && shape.fields[0].field_type == i32
}
let field_size(): usz {
  let field: core.types.FieldSpec = .{ name="value", field_type=i32 }
  let field_type = field.field_type
  return sizeof(field_type)
}
let enum_and_flags(): bool {
  let mut choices = core.types.EnumBuilder.new()
  choices.add("First")
  choices.add("Second")
  let E = choices.finish()
  let mut masks = core.types.FlagsBuilder.new(u32)
  masks.add("Read")
  masks.add("Write")
  let F = masks.finish()
  return sizeof(E) > 0 && sizeof(F) == sizeof(u32) && choices.finish() == E
}
let union_builder(): bool {
  let mut raw = core.types.UnionBuilder.untagged()
  raw.add_field("number", i32)
  raw.add_field("byte", u8)
  let U = raw.finish()
  let mut tagged = core.types.UnionBuilder.auto_tagged()
  let payload: []core.types.FieldSpec = [.{ name="value", field_type=i32 }]
  tagged.add_variant(.{ name="Some", fields=payload })
  tagged.add_variant(.{ name="None", fields=[] })
  let A = tagged.finish()
  let mut recursive = core.types.UnionBuilder.untagged()
  let SelfUnion = recursive.self_type()
  recursive.add_field("next", *SelfUnion)
  let R = recursive.finish()
  return sizeof(U) == sizeof(i32) && sizeof(A) > 0 && tagged.finish() == A && sizeof(R) > 0
}
let recursive_builder(): bool {
  let mut builder = core.types.StructBuilder.new()
  let SelfType = builder.self_type()
  builder.add_field("next", *SelfType)
  let mut copy = builder
  copy.add_field("value", i32)
  let Recursive = builder.finish()
  let Expanded = copy.finish()
  return sizeof(Recursive) > 0 && Expanded != Recursive
}
let built_type(): type {
  let mut builder = core.types.StructBuilder.new()
  builder.add_field("value", i32)
  return builder.finish()
}
let built_for(T: type): type {
  let mut builder = core.types.StructBuilder.new()
  builder.add_field("value", T)
  return builder.finish()
}
let loop_built(): type {
  let mut builder = core.types.StructBuilder.new()
  for index in 0..2 {
    if index == 0 { builder.add_field("left", i32) }
    else { builder.add_field("right", i32) }
  }
  return builder.finish()
}
let same_shape(flag: bool): type {
  let mut builder = core.types.StructBuilder.new()
  builder.add_field("value", i32)
  return builder.finish()
}
let built_from_name(name: str): type {
  let mut builder = core.types.StructBuilder.new()
  builder.add_field(name, i32)
  return builder.finish()
}
let separate_inputs(): bool = same_shape(true) != same_shape(false)
let repeat_factory(): bool = same_shape(true) == same_shape(true)
let choose_type(T: type, pick: bool): type {
  if pick && sizeof(T) == 4 { return i32 }
  return i64
}
let same_type(): bool = i32 == i32 && i32 != u8
let inspect_static_type(value: i32): bool {
  let T = core.types.static_type(value / 0)
  return T == i32
}
let distinct_locations(): bool {
  let first = struct { value: i32 }
  let second = struct { value: i32 }
  return first != second
}
let local_distinct(): bool {
  let first = distinct(u64)
  let second = distinct(u64)
  let sample: first = 7.(first)
  return first != second && sizeof(first) == sizeof(u64) && core.types.static_type(sample) == first
}
let Computed: usz = $make_type()
let FieldSize: usz = $field_size()
let SameType: bool = $same_type()
let StaticType: bool = $inspect_static_type(7)
let DistinctLocations: bool = $distinct_locations()
let LocalDistinct: bool = $local_distinct()
let BuilderSignature: bool = $builder_signature()
let EnumAndFlags: bool = $enum_and_flags()
let UnionBuilderWorks: bool = $union_builder()
let RecursiveBuilderWorks: bool = $recursive_builder()
let SeparateInputs: bool = $separate_inputs()
let RepeatFactory: bool = $repeat_factory()
let Built = $built_type()
let Looped = $loop_built()
let Named = $built_from_name("label")
let main() {
  let value: Built = .{ value=1 }
  let another: built_type() = .{ value=2 }
  let generic: built_for(i32) = .{ value=3 }
  let looped: Looped = .{ left=1, right=2 }
  let named: Named = .{ label=4 }
  let direct_name: built_from_name("other") = .{ other=5 }
  let chosen: choose_type(u8, $false) = 4
  _ = value
  _ = another
  _ = generic
  _ = looped
  _ = named
  _ = direct_name
  _ = chosen
}
`
	frontend, err := runFrontend(&Args{mainModule: ".", noStdlib: true}, loader.StageConfig{NoStdlib: true},
		map[string]string{path: source}, map[string]string{path: "."}, map[string]bool{path: false},
		map[string]map[string]string{".": {}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, node := range frontend.modules["."].Root.Body {
		declaration, ok := node.(*parser.DeclarationNode)
		if !ok || (declaration.Name != "Computed" && declaration.Name != "FieldSize" && declaration.Name != "SameType" && declaration.Name != "StaticType" && declaration.Name != "DistinctLocations" && declaration.Name != "LocalDistinct" && declaration.Name != "BuilderSignature" && declaration.Name != "EnumAndFlags" && declaration.Name != "UnionBuilderWorks" && declaration.Name != "RecursiveBuilderWorks" && declaration.Name != "SeparateInputs" && declaration.Name != "RepeatFactory") {
			continue
		}
		if declaration.Name == "SameType" || declaration.Name == "StaticType" || declaration.Name == "DistinctLocations" || declaration.Name == "LocalDistinct" || declaration.Name == "BuilderSignature" || declaration.Name == "EnumAndFlags" || declaration.Name == "UnionBuilderWorks" || declaration.Name == "RecursiveBuilderWorks" || declaration.Name == "SeparateInputs" || declaration.Name == "RepeatFactory" {
			literal, ok := declaration.Value.(*parser.BoolLiteralNode)
			if !ok || literal.Value != "true" {
				t.Fatalf("compile-time type equality produced %#v", declaration.Value)
			}
			found++
			continue
		}
		literal, ok := declaration.Value.(*parser.IntegerLiteralNode)
		if !ok || literal.Value != "4" {
			t.Fatalf("compile-time sizeof(type value) produced %#v", declaration.Value)
		}
		found++
	}
	if found != 12 {
		t.Fatalf("found %d computed declarations", found)
	}
}

func TestImportedBuilderTypeFunctionInAnnotation(t *testing.T) {
	sources := map[string]string{
		"factory/factory.qk": `module factory
import core.types
pub let make(T: type): type {
  let mut builder = core.types.StructBuilder.new()
  builder.add_field("value", T)
  return builder.finish()
}
`,
		"main.qk": `module main
import factory
let main() {
  let value: factory.make(i32) = .{ value=7 }
  let nested: factory.make(Option(i32)) = ---
  _ = value
}
`,
	}
	packages := map[string]string{"factory/factory.qk": "factory", "main.qk": "."}
	trusted := map[string]bool{"factory/factory.qk": false, "main.qk": false}
	_, err := runFrontend(&Args{mainModule: ".", noStdlib: true}, loader.StageConfig{NoStdlib: true},
		sources, packages, trusted, map[string]map[string]string{"factory": {}, ".": {}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRecursiveExpressionBodyInfersReturnFromIfBranch(t *testing.T) {
	const path = "recursive_return.qk"
	const source = `module main
let fib(n: usz) =
  if n <= 1 { n }
  else { fib(n - 1) + fib(n - 2) }
let main() { let result: usz = fib(8) }
`
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true},
		loader.StageConfig{NoStdlib: true},
		map[string]string{path: source},
		map[string]string{path: "."},
		map[string]bool{path: false},
		map[string]map[string]string{".": {}},
		false,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestImportedGenericCompileTimeBranchSelectsPerInstance(t *testing.T) {
	const helperPath = "helper/helper.qk"
	const libraryPath = "choice/choice.qk"
	const mainPath = "main.qk"
	sources := map[string]string{
		helperPath: `module helper
pub let small(size: usz): bool = size == 4
`,
		libraryPath: `module choice
import helper
pub let select($T: type, value: T): T = $if helper.small(sizeof(T)) { value } else { missing_name }
`,
		mainPath: `module main
import choice
let main() { let selected: i32 = choice.select(7) }
`,
	}
	packages := map[string]string{helperPath: "helper", libraryPath: "choice", mainPath: "."}
	trusted := map[string]bool{helperPath: false, libraryPath: false, mainPath: false}
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true},
		loader.StageConfig{NoStdlib: true}, sources, packages, trusted,
		map[string]map[string]string{"helper": {}, "choice": {}, ".": {}}, false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGenericCompileTimeBranchCallsPureFunction(t *testing.T) {
	const path = "main.qk"
	const source = `module main
let small(size: usz): bool = size == 4
let select($T: type, value: T): T = $if small(sizeof(T)) { value } else { missing_name }
let main() { let selected: i32 = select(7) }
`
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true},
		loader.StageConfig{NoStdlib: true},
		map[string]string{path: source}, map[string]string{path: "."},
		map[string]bool{path: false}, map[string]map[string]string{".": {}},
		false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGenericCompilerAssertUsesConcreteType(t *testing.T) {
	const path = "main.qk"
	const source = `module main
let require_four($T: type, value: T): T = {
  $compiler_assert(sizeof(T) == 4, "four byte type required")
  value
}
let main() { let selected: i32 = require_four(7) }
`
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true},
		loader.StageConfig{NoStdlib: true},
		map[string]string{path: source}, map[string]string{path: "."},
		map[string]bool{path: false}, map[string]map[string]string{".": {}},
		false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runFrontend(
		&Args{mainModule: ".", noStdlib: true},
		loader.StageConfig{NoStdlib: true},
		map[string]string{path: strings.Replace(source, "let selected: i32 = require_four(7)", "let selected: i64 = require_four(7)", 1)},
		map[string]string{path: "."}, map[string]bool{path: false},
		map[string]map[string]string{".": {}}, false, false,
	)
	if err == nil || !strings.Contains(err.Error(), "four byte type required") {
		t.Fatalf("expected selected compiler assertion to fail, got %v", err)
	}
}

func TestFunctionContextInferenceFollowsCalls(t *testing.T) {
	const path = "main.qk"
	const definitions = `module main
let leaf(): usz = sizeof(i32)
let relay(): usz = leaf()
`
	compile := func(source string) error {
		_, err := runFrontend(
			&Args{mainModule: ".", noStdlib: true},
			loader.StageConfig{NoStdlib: true},
			map[string]string{path: source}, map[string]string{path: "."},
			map[string]bool{path: false}, map[string]map[string]string{".": {}},
			false, false,
		)
		return err
	}
	if err := compile(definitions + "let Size: usz = $relay()\nlet main() { let size: usz = Size }\n"); err != nil {
		t.Fatal(err)
	}
	if err := compile(definitions + "let main() { let size: usz = relay() }\n"); err == nil || !strings.Contains(err.Error(), "runtime entry") {
		t.Fatalf("expected runtime boundary error, got %v", err)
	}
}

func TestFunctionContextInferenceAcrossModules(t *testing.T) {
	const helperPath = "helper/helper.qk"
	const mainPath = "main.qk"
	sources := map[string]string{
		helperPath: `module helper
pub let bytes(): usz = sizeof(i32)
`,
		mainPath: `module main
import helper
let relay(): usz = helper.bytes()
let main() { let size: usz = relay() }
`,
	}
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true},
		loader.StageConfig{NoStdlib: true}, sources,
		map[string]string{helperPath: "helper", mainPath: "."},
		map[string]bool{helperPath: false, mainPath: false},
		map[string]map[string]string{"helper": {}, ".": {}}, false, false,
	)
	if err == nil || !strings.Contains(err.Error(), "runtime entry") {
		t.Fatalf("expected cross-module runtime boundary error, got %v", err)
	}
}

func TestSelectedGenericBranchInfersCompileTimeOnly(t *testing.T) {
	const path = "main.qk"
	const source = `module main
let bytes($T: type): usz = $if sizeof(T) == 4 { sizeof(T) } else { 8 }
let main() { let size: usz = bytes(i32) }
`
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true},
		loader.StageConfig{NoStdlib: true},
		map[string]string{path: source}, map[string]string{path: "."},
		map[string]bool{path: false}, map[string]map[string]string{".": {}},
		false, false,
	)
	if err == nil || !strings.Contains(err.Error(), "runtime entry") {
		t.Fatalf("expected selected specialization to require compile-time execution, got %v", err)
	}
}

func TestRetainedCompileTimeBooleanParameter(t *testing.T) {
	const path = "main.qk"
	const source = `module main
let choose($enabled: bool, value: i32): i32 = $if enabled { value } else { -value }
let main() {
  let a: i32 = choose($true, 7)
  let b: i32 = choose($false, 7)
}
`
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true},
		loader.StageConfig{NoStdlib: true},
		map[string]string{path: source}, map[string]string{path: "."},
		map[string]bool{path: false}, map[string]map[string]string{".": {}},
		false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestImportedRetainedCompileTimeParameter(t *testing.T) {
	const libraryPath = "choice/choice.qk"
	const mainPath = "main.qk"
	sources := map[string]string{
		libraryPath: `module choice
pub let choose($enabled: bool, value: i32): i32 = $if enabled { value } else { -value }
`,
		mainPath: `module main
import choice
let main() {
  let first: i32 = choice.choose($true, 7)
  let second: i32 = choice.choose($false, 7)
}
`,
	}
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true}, loader.StageConfig{NoStdlib: true},
		sources, map[string]string{libraryPath: "choice", mainPath: "."},
		map[string]bool{libraryPath: false, mainPath: false},
		map[string]map[string]string{"choice": {}, ".": {}}, false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestImportedTypeFunctionInAnnotation(t *testing.T) {
	const libraryPath = "types/types.qk"
	const mainPath = "main.qk"
	sources := map[string]string{
		libraryPath: `module types
pub let identity(T: type): type = T
pub let choose(T: type, enabled: bool): type = $if enabled { T } else { i64 }
`,
		mainPath: `module main
import types
let main() {
  let value: types.identity(i32) = 7
  let other: types.choose(i32, $false) = 9
}
`,
	}
	packages := map[string]string{libraryPath: "types", mainPath: "."}
	trusted := map[string]bool{libraryPath: false, mainPath: false}
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true},
		loader.StageConfig{NoStdlib: true}, sources, packages, trusted,
		map[string]map[string]string{"types": {}, ".": {}}, false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRetainedParameterRejectsRuntimeArgument(t *testing.T) {
	const path = "main.qk"
	const source = `module main
let choose($enabled: bool, value: i32): i32 = $if enabled { value } else { -value }
let run(enabled: bool): i32 = choose(enabled, 7)
let main() {}
`
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true}, loader.StageConfig{NoStdlib: true},
		map[string]string{path: source}, map[string]string{path: "."},
		map[string]bool{path: false}, map[string]map[string]string{".": {}}, false, false,
	)
	if err == nil || !strings.Contains(err.Error(), "must be known at compile time") {
		t.Fatalf("expected retained parameter diagnostic, got %v", err)
	}
}
