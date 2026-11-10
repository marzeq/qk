package main

import (
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
