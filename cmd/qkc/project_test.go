package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestSource(t *testing.T, path, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectManifestAllowsMergedSources(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"vendor-a", "vendor-b", "special"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	source := "sources vendor \"./vendor-a\"\n" +
		"sources vendor \"./vendor-b\"\n" +
		"source vendor.special \"./special\"\n" +
		"source vendor.special \"./special\"\n"
	manifest, err := parseProjectManifest(filepath.Join(root, "qk.mod"), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Mounts) != 4 {
		t.Fatalf("got %d source mounts, want 4", len(manifest.Mounts))
	}
}

func TestProjectManifestRejectsConflictingExactSources(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"first", "second"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	source := "source vendor.foo \"./first\"\nsource vendor.foo \"./second\"\n"
	_, err := parseProjectManifest(filepath.Join(root, "qk.mod"), source)
	if err == nil || !strings.Contains(err.Error(), "conflicting source mount") {
		t.Fatalf("expected conflicting source mount error, got %v", err)
	}
}

func TestDiscoverSourcePackagesMergesMounts(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "app")
	library := filepath.Join(root, "libs")
	first := filepath.Join(root, "vendor-a")
	second := filepath.Join(root, "vendor-b")
	special := filepath.Join(root, "special")
	writeTestSource(t, filepath.Join(app, "main.qk"), `module main
import vendor.builtin
import vendor.alpha
import vendor.beta
import vendor.special
import vendor.special.child
let main() {}
`)
	writeTestSource(t, filepath.Join(library, "vendor", "builtin", "main.qk"), "module vendor.builtin\n")
	writeTestSource(t, filepath.Join(first, "alpha", "main.qk"), "module alpha\n")
	writeTestSource(t, filepath.Join(second, "beta", "main.qk"), "module beta\n")
	writeTestSource(t, filepath.Join(special, "main.qk"), "module special\n")
	writeTestSource(t, filepath.Join(special, "child", "main.qk"), "module special.child\n")

	mounts := []sourceMount{
		{Kind: sourceMountCollection, Prefix: "vendor", Directory: first},
		{Kind: sourceMountCollection, Prefix: "vendor", Directory: second},
		{Kind: sourceMountExact, Prefix: "vendor.special", Directory: special, SemanticBase: "special"},
	}
	packages, _, _, _, resolutions, err := discoverSourcePackages(
		".", app, "", []string{library, app}, mounts, library, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool)
	for _, pkg := range packages {
		got[pkg.Path] = true
	}
	for _, path := range []string{".", "vendor.builtin", "alpha", "beta", "special", "special.child"} {
		if !got[path] {
			t.Errorf("module %q was not discovered", path)
		}
	}
	wantResolutions := map[string]string{
		"vendor.builtin":       "vendor.builtin",
		"vendor.alpha":         "alpha",
		"vendor.beta":          "beta",
		"vendor.special":       "special",
		"vendor.special.child": "special.child",
	}
	for visible, want := range wantResolutions {
		if got := resolutions["."][visible]; got != want {
			t.Errorf("resolution for %q = %q, want %q", visible, got, want)
		}
	}
}

func TestDiscoverSourcePackagesRejectsAmbiguousMergedSources(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "app")
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeTestSource(t, filepath.Join(app, "main.qk"), "module main\nimport vendor.foo\nlet main() {}\n")
	writeTestSource(t, filepath.Join(first, "foo", "main.qk"), "module foo\n")
	writeTestSource(t, filepath.Join(second, "foo", "main.qk"), "module foo\n")
	mounts := []sourceMount{
		{Kind: sourceMountCollection, Prefix: "vendor", Directory: first},
		{Kind: sourceMountCollection, Prefix: "vendor", Directory: second},
	}
	_, _, _, _, _, err := discoverSourcePackages(".", app, "", []string{app}, mounts, "", false)
	if err == nil || !strings.Contains(err.Error(), "is ambiguous") {
		t.Fatalf("expected ambiguous source error, got %v", err)
	}
}

func TestDiscoverSourcePackagesRejectsExactCollectionConflict(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "app")
	collection := filepath.Join(root, "collection")
	exact := filepath.Join(root, "exact")
	writeTestSource(t, filepath.Join(app, "main.qk"), "module main\nimport vendor.foo\nlet main() {}\n")
	writeTestSource(t, filepath.Join(collection, "foo", "main.qk"), "module foo\n")
	writeTestSource(t, filepath.Join(exact, "main.qk"), "module foo\n")
	mounts := []sourceMount{
		{Kind: sourceMountCollection, Prefix: "vendor", Directory: collection},
		{Kind: sourceMountExact, Prefix: "vendor.foo", Directory: exact, SemanticBase: "foo"},
	}
	_, _, _, _, _, err := discoverSourcePackages(".", app, "", []string{app}, mounts, "", false)
	if err == nil || !strings.Contains(err.Error(), "is ambiguous") {
		t.Fatalf("expected exact/collection conflict, got %v", err)
	}
}
