package main

import (
	"strings"
	"testing"

	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/loader"
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
