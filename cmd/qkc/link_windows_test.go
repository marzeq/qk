package main

import "testing"

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
