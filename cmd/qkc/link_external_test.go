package main

import (
	"reflect"
	"testing"
)

func TestUnwrapLinkerArgs(t *testing.T) {
	got := unwrapLinkerArgs([]string{"--allow-undefined", "-Wl,--export=foo,--strip-all"})
	want := []string{"--allow-undefined", "--export=foo", "--strip-all"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unwrapped linker arguments: got %q, want %q", got, want)
	}
}

func TestStaticLibraryNames(t *testing.T) {
	if got := defaultStaticLibraryName("math", "x86_64-linux-gnu"); got != "libmath.a" {
		t.Fatalf("ELF static library name: got %q", got)
	}
	if got := defaultStaticLibraryName("math", "x86_64-pc-windows-msvc"); got != "math.lib" {
		t.Fatalf("Windows static library name: got %q", got)
	}
	if got := defaultStaticLibraryName("math", "x86_64-w64-windows-gnu"); got != "libmath.a" {
		t.Fatalf("MinGW static library name: got %q", got)
	}
}
