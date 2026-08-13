package main

import (
	"reflect"
	"testing"

	"github.com/marzeq/qk/attributes"
)

func TestUnwrapLinkerArgs(t *testing.T) {
	got := unwrapLinkerArgs([]string{"--allow-undefined", "-Wl,--export=foo,--strip-all"})
	want := []string{"--allow-undefined", "--export=foo", "--strip-all"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unwrapped linker arguments: got %q, want %q", got, want)
	}
}

func TestOrderedModuleLinksPutArchivesBeforeDependencies(t *testing.T) {
	links := []attributes.Link{
		{Kind: attributes.LinkSystem, Value: "X11"},
		{Kind: attributes.LinkSearchPath, Value: "/native/lib"},
		{Kind: attributes.LinkFramework, Value: "Cocoa"},
		{Kind: attributes.LinkPath, Value: "/native/libraylib.a"},
		{Kind: attributes.LinkSystem, Value: "GL"},
	}
	want := []attributes.Link{
		{Kind: attributes.LinkSearchPath, Value: "/native/lib"},
		{Kind: attributes.LinkPath, Value: "/native/libraylib.a"},
		{Kind: attributes.LinkSystem, Value: "X11"},
		{Kind: attributes.LinkSystem, Value: "GL"},
		{Kind: attributes.LinkFramework, Value: "Cocoa"},
	}
	if got := orderedModuleLinks(links); !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered module links: got %v, want %v", got, want)
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
