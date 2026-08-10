package stdlib

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSourcePackagePathsDeriveCanonicalDirectoryName(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "std", "io", "print.qks")
	paths, available, err := SourcePackagePaths(map[string]string{
		origin: "module std.io\n",
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	if paths[origin] != "std.io" || !available["std.io"] {
		t.Fatalf("directory package identity was not preserved: paths=%v available=%v", paths, available)
	}
}

func TestSourcePackagePathsRejectModuleDirectoryMismatch(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "std", "io", "print.qks")
	_, _, err := SourcePackagePaths(map[string]string{
		origin: "module std.strings\n",
	}, root)
	if err == nil || !strings.Contains(err.Error(), `must declare module "std.io", found "std.strings"`) {
		t.Fatalf("module-directory mismatch was accepted: %v", err)
	}
}

func TestLibrarySourcesMatchDirectoryPackages(t *testing.T) {
	root := filepath.Join("..", "libs")
	sources, err := ReadSources(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, available, err := SourcePackagePaths(sources, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(sources) || !available["std"] || !available["std.io"] {
		t.Fatalf("incomplete library package mapping: %v", available)
	}
}
