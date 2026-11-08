package main

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/marzeq/qk/attributes"
)

func TestCrossHostFinalLinkIsRejected(t *testing.T) {
	target := "arm64-apple-macosx"
	if runtime.GOOS == "darwin" {
		target = "x86_64-unknown-linux-gnu"
	}
	if err := validateExternalLinkTarget(target, "/target-sysroot"); err == nil {
		t.Fatalf("cross-host final link for %q was accepted", target)
	}
}

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

func TestWindowsHostDefaultsToMSVC(t *testing.T) {
	if got := defaultTargetForHost("windows"); got != "x86_64-pc-windows-msvc" {
		t.Fatalf("Windows default target: got %q", got)
	}
	if got := defaultTargetForHost("linux"); got != "" {
		t.Fatalf("Linux default target override: got %q", got)
	}
}

func TestWindowsGNUTargetRequiresExplicitSysroot(t *testing.T) {
	target := "x86_64-w64-windows-gnu"
	if _, err := hostWindowsToolchainArgs(target, ""); err == nil {
		t.Fatal("Windows GNU target without a sysroot was accepted")
	}
	if _, err := hostWindowsToolchainArgs(target, t.TempDir()); err != nil {
		t.Fatalf("Windows GNU target with a sysroot was rejected: %v", err)
	}
}

func TestTargetSystemLibrary(t *testing.T) {
	tests := []struct {
		target  string
		library string
		want    string
	}{
		{"x86_64-pc-windows-msvc", "c", "ucrt"},
		{"x86_64-w64-windows-gnu", "c", "msvcrt"},
		{"amd64-windows", "c", "msvcrt"},
		{"x86_64-pc-windows-msvc", "kernel32", "kernel32"},
		{"x86_64-linux-gnu", "c", "c"},
	}
	for _, test := range tests {
		if got := targetSystemLibrary(test.target, test.library); got != test.want {
			t.Errorf("targetSystemLibrary(%q, %q) = %q, want %q", test.target, test.library, got, test.want)
		}
	}
}

func TestIsCLibrary(t *testing.T) {
	for _, library := range []string{"c", "System", "msvcrt", "ucrt"} {
		if !isCLibrary(library) {
			t.Errorf("isCLibrary(%q) = false", library)
		}
	}
	if isCLibrary("kernel32") {
		t.Error("isCLibrary(\"kernel32\") = true")
	}
}

func TestMSVCCRTIsRecognizedAsLibc(t *testing.T) {
	if !targetIsWindowsMSVC("x86_64-pc-windows-msvc") {
		t.Fatal("MSVC target was not recognized")
	}
	for _, library := range []string{"c", "msvcrt", "ucrt"} {
		if !isCLibrary(library) {
			t.Fatalf("MSVC CRT library %q was not recognized as libc", library)
		}
	}
}

func TestMSVCLinkArgumentsUseNativeSyntax(t *testing.T) {
	config := &Args{output: "app.exe", outputType: OutputExecutable, libs: []string{"user32"}}
	links := []attributes.Link{{Kind: attributes.LinkSystem, Value: "c"}}
	got, err := buildMSVCLinkArgs([]string{"one.obj", "two.obj"}, links, config)
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := []string{"/DEFAULTLIB:libcmt", "user32.lib", "kernel32.lib", "one.obj", "two.obj"}
	if len(got) < len(wantSuffix) || !reflect.DeepEqual(got[len(got)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("MSVC linker arguments: got %q, want suffix %q", got, wantSuffix)
	}
}

func TestMSVCLinkArgumentsKeepCRTStartupWithoutCLibraryImport(t *testing.T) {
	config := &Args{output: "app.exe", outputType: OutputExecutable}
	got, err := buildMSVCLinkArgs([]string{"main.obj"}, nil, config)
	if err != nil {
		t.Fatal(err)
	}
	for _, argument := range got {
		if argument == "/DEFAULTLIB:libcmt" {
			return
		}
	}
	t.Fatalf("MSVC linker arguments omit CRT startup: %q", got)
}
