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

func TestImportedTypeFunctionInAnnotation(t *testing.T) {
	const libraryPath = "types/types.qk"
	const mainPath = "main.qk"
	sources := map[string]string{
		libraryPath: `module types
pub let identity(T: type): type = T
`,
		mainPath: `module main
import types
let main() { let value: types.identity(i32) = 7 }
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
