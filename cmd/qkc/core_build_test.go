package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/ir"
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
			nil,
		)
		if err != nil {
			t.Errorf("%s: %v", target.triple, err)
		}
	}
}

func TestCoreBuildContextIsCompileTimeInsideEvaluation(t *testing.T) {
	source := `module main
import core.build
let in_compile_time(): bool = core.build.context == .CompileTime
let selected: bool = $in_compile_time()
$if !selected { compiler_error("compile-time evaluator used runtime context") }
let main(): void {}
`
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true}, loader.StageConfig{},
		map[string]string{"main.qk": source}, map[string]string{"main.qk": "."},
		map[string]bool{"main.qk": false}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRaylibSelectsSharedLinkForCompileTime(t *testing.T) {
	libraryRoot := filepath.Clean(filepath.Join("..", "..", "libs"))
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "main.qk"), []byte("module main\nimport vendor.raylib\nlet main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, sources, packages, trusted, resolutions, err := discoverSourcePackages(
		".", projectRoot, "", []string{libraryRoot, projectRoot}, nil, libraryRoot, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	frontend, err := runFrontend(&Args{mainModule: "."}, loader.StageConfig{}, sources, packages, trusted, resolutions)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontend.modules["vendor.raylib"].CompileTimeForeignLinks["IsWindowReady"]) == 0 {
		t.Fatalf("raylib foreign link map is empty: %#v", frontend.modules["vendor.raylib"].CompileTimeForeignLinks)
	}
	for _, partial := range frontend.partials {
		if partial.Path != "vendor.raylib" || len(partial.CompileTimeLinks) == 0 {
			continue
		}
		if len(partial.CompileTimeLinks) != 1 || partial.CompileTimeLinks[0] != (attributes.Link{Kind: attributes.LinkSystem, Value: "raylib"}) {
			t.Fatalf("compile-time raylib links = %#v", partial.CompileTimeLinks)
		}
		for _, link := range partial.Links {
			if link.Kind == attributes.LinkPath && filepath.Base(link.Value) == "libraylib.a" {
				return
			}
		}
		t.Fatalf("runtime raylib links lack the static archive: %#v", partial.Links)
	}
	t.Fatal("raylib link attributes were not collected")
}

func TestRaylibCompileTimeForeignCall(t *testing.T) {
	if !ir.HostForeignCallsAvailable {
		t.Skip("host foreign calls are unavailable in this build")
	}
	if _, err := os.Stat("/usr/lib/libraylib.so"); err != nil {
		t.Skip("host raylib shared library is not installed")
	}
	libraryRoot := filepath.Clean(filepath.Join("..", "..", "libs"))
	projectRoot := t.TempDir()
	source := `module main
import vendor.raylib
let Ready = $vendor.raylib.is_window_ready()
$if Ready { compiler_error("raylib unexpectedly has an open window") }
let Packed = $vendor.raylib.color_to_int(vendor.raylib.get_color(0x11223344))
$if Packed != 0x11223344 { compiler_error("raylib Color ABI round trip failed") }
let main() {}
`
	if err := os.WriteFile(filepath.Join(projectRoot, "main.qk"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	_, sources, packages, trusted, resolutions, err := discoverSourcePackages(
		".", projectRoot, "", []string{libraryRoot, projectRoot}, nil, libraryRoot, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runFrontend(&Args{mainModule: "."}, loader.StageConfig{}, sources, packages, trusted, resolutions); err != nil {
		t.Fatal(err)
	}
}

func TestCompileTimeForeignLinkFailureNamesLibrary(t *testing.T) {
	if !ir.HostForeignCallsAvailable {
		t.Skip("host foreign calls are unavailable in this build")
	}
	source := `module main @link(system "qk_missing_compile_time_library_758133")
let native(x: f64): f64 @foreign(symbol "cos")
let Answer = $native(0.0)
let main() {}
`
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true}, loader.StageConfig{},
		map[string]string{"main.qk": source}, map[string]string{"main.qk": "."},
		map[string]bool{"main.qk": false}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "qk_missing_compile_time_library_758133") ||
		!strings.Contains(err.Error(), "main.qk:3:") {
		t.Fatalf("expected source-located library load failure, got %v", err)
	}
}

func TestCompileTimeTopLevelCallRunsOnce(t *testing.T) {
	source := `module main
let mut counter: i32 = 0
let next(): i32 {
  counter += 1
  return counter
}
let Answer = $next()
$if Answer != 1 { compiler_error("compile-time call executed more than once") }
let main() {}
`
	_, err := runFrontend(
		&Args{mainModule: ".", noStdlib: true}, loader.StageConfig{},
		map[string]string{"main.qk": source}, map[string]string{"main.qk": "."},
		map[string]bool{"main.qk": false}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
}
