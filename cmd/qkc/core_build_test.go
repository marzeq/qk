package main

import (
	"testing"

	"github.com/marzeq/qk/loader"
)

func TestCoreBuildIsAvailableWithoutStdlib(t *testing.T) {
	for _, target := range []struct {
		triple string
		member string
		arch   string
	}{
		{"x86_64-unknown-linux-gnu", "Linux", "X86_64"},
		{"x86_64-pc-windows-msvc", "Windows", "X86_64"},
		{"aarch64-apple-darwin", "MacOS", "AArch64"},
	} {
		source := `module main
import core.build
let selected_os = $core.build.os
$if selected_os != .` + target.member + ` {
  compiler_error("retained OS mismatch")
}
$if core.build.os == .` + target.member + ` && core.build.pointer_bits == 64 {
  let selected: i32 = 1
} else {
  compiler_error("wrong target")
}
$if core.build.arch != .` + target.arch + ` {
  compiler_error("wrong architecture")
}
let main(): i32 = selected
`
		_, err := runFrontend(
			&Args{mainModule: ".", noStdlib: true},
			loader.StageConfig{TargetTriple: target.triple},
			map[string]string{"main.qk": source},
			map[string]string{"main.qk": "."},
			map[string]bool{"main.qk": false},
			nil, false, false,
		)
		if err != nil {
			t.Errorf("%s: %v", target.triple, err)
		}
	}
}
