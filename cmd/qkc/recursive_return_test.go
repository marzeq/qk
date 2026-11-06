package main

import (
	"testing"

	"github.com/marzeq/qk/loader"
)

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
