package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLibraryRootForGoRun(t *testing.T) {
	checkout := t.TempDir()
	if err := os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+qkModulePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	libs := filepath.Join(checkout, "libs")
	if err := os.Mkdir(libs, 0o755); err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	executable := filepath.Join(temporary, "go-build123", "b001", "exe", "qkc")
	got, err := resolveLibraryRootFor(executable, filepath.Join(checkout, "cmd", "qkc"), temporary)
	if err != nil {
		t.Fatal(err)
	}
	if got != libs {
		t.Fatalf("go run library root: got %q, want %q", got, libs)
	}
}

func TestResolveLibraryRootForInstalledBinary(t *testing.T) {
	prefix := t.TempDir()
	libs := filepath.Join(prefix, "libs")
	if err := os.Mkdir(libs, 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(prefix, "bin", "qkc")
	got, err := resolveLibraryRootFor(executable, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(libs)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("installed library root: got %q, want %q", got, want)
	}
}
