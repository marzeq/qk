package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/marzeq/qk/comptime"
)

func TestLibraryTreeThroughCompilerFrontend(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	libraryRoot := filepath.Join(repositoryRoot, "libs")
	packages, err := libraryPackages(libraryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) == 0 {
		t.Fatal("library tree contains no QK packages")
	}

	projectRoot := t.TempDir()
	var source strings.Builder
	source.WriteString("module main\n\n")
	for _, name := range packages {
		source.WriteString("import ")
		source.WriteString(name)
		source.WriteByte('\n')
	}
	source.WriteString("\nlet main() {}\n")
	mainPath := filepath.Join(projectRoot, "main.qk")
	if err := os.WriteFile(mainPath, []byte(source.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	_, sources, sourcePackages, trustedSources, err := discoverSourcePackages(
		".", projectRoot, "", []string{libraryRoot, projectRoot}, libraryRoot, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	config := comptime.Config{}
	config.ModuleBindings, err = comptime.ResolvePackageBindings(sources, sourcePackages, config)
	if err != nil {
		t.Fatal(err)
	}
	args := &Args{mainModule: "."}
	if _, err := runFrontend(args, config, sources, sourcePackages, trustedSources, false, false); err != nil {
		t.Fatal(err)
	}
}

func TestStandardLibraryTrustComesFromLibraryPath(t *testing.T) {
	libraryRoot := t.TempDir()
	standardPackage := filepath.Join(libraryRoot, "std", "io")
	ordinaryPackage := filepath.Join(libraryRoot, "vendor", "example")
	for _, directory := range []string{standardPackage, ordinaryPackage} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	standardSource := filepath.Join(standardPackage, "io.qk")
	ordinarySource := filepath.Join(ordinaryPackage, "example.qk")
	for _, source := range []string{standardSource, ordinarySource} {
		if err := os.WriteFile(source, []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !trustedStandardLibrarySource(standardSource, libraryRoot) {
		t.Fatal("package beneath <libspath>/std was not trusted")
	}
	if trustedStandardLibrarySource(ordinarySource, libraryRoot) {
		t.Fatal("package outside <libspath>/std was trusted")
	}
}

func libraryPackages(root string) ([]string, error) {
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".qk") {
			return nil
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		seen[strings.ReplaceAll(filepath.ToSlash(relative), "/", ".")] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	packages := make([]string, 0, len(seen))
	for name := range seen {
		packages = append(packages, name)
	}
	sort.Strings(packages)
	return packages, nil
}
