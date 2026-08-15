package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
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

	_, sources, sourcePackages, trustedSources, importResolutions, err := discoverSourcePackages(
		".", projectRoot, "", []string{libraryRoot, projectRoot}, nil, libraryRoot, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	config := loader.StageConfig{}
	args := &Args{mainModule: "."}
	if _, err := runFrontend(args, config, sources, sourcePackages, trustedSources, importResolutions, false, false); err != nil {
		t.Fatal(err)
	}
}

func TestCompileTimeEvaluationUsesImportedPureLibraryCode(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	libraryRoot := filepath.Join(repositoryRoot, "libs")
	projectRoot := t.TempDir()
	mainPath := filepath.Join(projectRoot, "main.qk")
	source := "module main\nimport std.math\nlet $Answer = std.math.sqrt(16.0)\nlet main() {}\n"
	if err := os.WriteFile(mainPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	_, sources, sourcePackages, trustedSources, importResolutions, err := discoverSourcePackages(
		".", projectRoot, "", []string{libraryRoot, projectRoot}, nil, libraryRoot, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	frontend, err := runFrontend(&Args{mainModule: "."}, loader.StageConfig{}, sources, sourcePackages, trustedSources, importResolutions, false, false)
	if err != nil {
		t.Fatal(err)
	}
	module := frontend.modules["."]
	if module == nil {
		t.Fatal("main module was not produced")
	}
	for _, node := range module.Root.Body {
		declaration, ok := node.(*parser.DeclarationNode)
		if !ok || declaration.Name != "Answer" {
			continue
		}
		literal, ok := declaration.Value.(*parser.FloatLiteralNode)
		if !ok || literal.Value != "4" {
			t.Fatalf("compile-time sqrt produced %#v", declaration.Value)
		}
		return
	}
	t.Fatal("compile-time answer declaration was not produced")
}

func TestCompileTimeEvaluationRejectsExecutedForeignCall(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	libraryRoot := filepath.Join(repositoryRoot, "libs")
	projectRoot := t.TempDir()
	mainPath := filepath.Join(projectRoot, "main.qk")
	source := "module main\nimport std.libc\nlet $Answer = std.libc.putchar(65)\nlet main() {}\n"
	if err := os.WriteFile(mainPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	_, sources, sourcePackages, trustedSources, importResolutions, err := discoverSourcePackages(
		".", projectRoot, "", []string{libraryRoot, projectRoot}, nil, libraryRoot, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runFrontend(&Args{mainModule: "."}, loader.StageConfig{}, sources, sourcePackages, trustedSources, importResolutions, false, false)
	if err == nil || !strings.Contains(err.Error(), "std.libc.putchar is not available in a compile-time context") {
		t.Fatalf("expected executed foreign call to be rejected, got %v", err)
	}
	if !strings.Contains(err.Error(), mainPath+":3:") {
		t.Fatalf("compile-time rejection did not point at the staged expression: %v", err)
	}
}

func TestCompileTimeEvaluationPointsAtNestedUnavailableCall(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	libraryRoot := filepath.Join(repositoryRoot, "libs")
	projectRoot := t.TempDir()
	mainPath := filepath.Join(projectRoot, "main.qk")
	source := `module main
import std.io
import std.math
let sum(x, y: i32) = {
  let result = x + y
  std.io.print("{}", result)
  result
}
let $Answer = sum(1, 2)
let $Other = std.math.sqrt(16.0)
let main() {}
`
	if err := os.WriteFile(mainPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	_, sources, sourcePackages, trustedSources, importResolutions, err := discoverSourcePackages(
		".", projectRoot, "", []string{libraryRoot, projectRoot}, nil, libraryRoot, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runFrontend(&Args{mainModule: "."}, loader.StageConfig{}, sources, sourcePackages, trustedSources, importResolutions, false, false)
	if err == nil || !strings.Contains(err.Error(), "sum cannot be evaluated at compile time") ||
		!strings.Contains(err.Error(), "evaluation reaches std.io.print, which is not available in a compile-time context") {
		t.Fatalf("expected nested print call to be rejected, got %v", err)
	}
	if !strings.Contains(err.Error(), mainPath+":9:15") || !strings.Contains(err.Error(), "Note: "+mainPath+":6:3") {
		t.Fatalf("compile-time rejection did not show the call origin and unavailable operation: %v", err)
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
