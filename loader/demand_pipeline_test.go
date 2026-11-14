package loader

import (
	"strings"
	"testing"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/tokeniser"
)

func demandTestModules(t *testing.T, call string) map[string]*ModuleInfo {
	t.Helper()
	parse := func(source, file string) *parser.RootNode {
		tokens, err := tokeniser.NewTokeniser(source, file).Tokenise()
		if err != nil {
			t.Fatal(err)
		}
		root, err := parser.NewParser(tokens).Parse()
		if err != nil {
			t.Fatal(err)
		}
		return root
	}
	library := parse(`module std.test
pub let used(): i32 { return 7 }
pub let unused(): i32 { return missing }
`, "library.qk")
	main := parse("module main\nimport std.test\nlet main() { let _ = std.test."+call+"() }\n", "main.qk")
	return map[string]*ModuleInfo{
		"std.test": {Path: "std.test", Name: "std.test", Root: library, TrustedStandardLibrary: true},
		"main":     {Path: "main", Name: "main", Root: main, Imports: []string{"std.test"}},
	}
}

func TestExecutableDefersUnusedLibraryBody(t *testing.T) {
	order := []string{"std.test", "main"}
	modules := demandTestModules(t, "used")
	compiled, errors := CompileSelectedModulesLazyTemplates(modules, "main", order, sema.NewAnalyser(), true)
	if len(errors) != 0 {
		t.Fatalf("unexpected errors: %v", errors)
	}
	if len(compiled.IRModules["std.test"].Functions) != 1 {
		t.Fatalf("expected only the used library function, got %d", len(compiled.IRModules["std.test"].Functions))
	}

	modules = demandTestModules(t, "used")
	_, errors = CompileSelectedModules(modules, "main", order, sema.NewAnalyser())
	if len(errors) == 0 || !strings.Contains(errors[0].Error(), "missing") {
		t.Fatalf("full library check should reject unused invalid body, got %v", errors)
	}

	modules = demandTestModules(t, "unused")
	_, errors = CompileSelectedModulesLazyTemplates(modules, "main", order, sema.NewAnalyser(), true)
	if len(errors) == 0 || !strings.Contains(errors[0].Error(), "missing") {
		t.Fatalf("demanded library body should be checked, got %v", errors)
	}
}
