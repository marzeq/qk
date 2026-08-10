package comptime

import (
	"math/big"
	"strings"
	"testing"
)

func TestPackageBindingsFingerprintIsScoped(t *testing.T) {
	bindings := map[string]Value{
		"app.BufferSize":  {kind: valueInteger, integer: big.NewInt(32)},
		"dep.Feature":     {kind: valueBool, boolean: true},
		"unrelated.Value": {kind: valueInteger, integer: big.NewInt(1)},
	}
	base := PackageBindingsFingerprint(bindings, "app", []string{"dep"})

	bindings["unrelated.Value"] = Value{kind: valueInteger, integer: big.NewInt(2)}
	if got := PackageBindingsFingerprint(bindings, "app", []string{"dep"}); got != base {
		t.Fatal("an unrelated module binding invalidated the package fingerprint")
	}

	bindings["dep.Feature"] = Value{kind: valueBool, boolean: false}
	if got := PackageBindingsFingerprint(bindings, "app", []string{"dep"}); got == base {
		t.Fatal("an imported module binding did not invalidate the package fingerprint")
	}
}

func TestResolvePackageBindingsUsesImportScope(t *testing.T) {
	sources := map[string]string{
		"dep.qk":       "module dep\npub let Feature = comptime true\n",
		"unrelated.qk": "module unrelated\npub let Value = comptime true\n",
		"app.qk":       "module app\nimport dep d\nlet Enabled = comptime d.Feature\n",
	}
	packages := map[string]string{"dep.qk": "dep", "unrelated.qk": "unrelated", "app.qk": "app"}
	if _, err := ResolvePackageBindings(sources, packages, Config{}); err != nil {
		t.Fatalf("imported alias was not available to compile-time binding: %v", err)
	}

	sources["app.qk"] = "module app\nlet Enabled = comptime unrelated.Value\n"
	if _, err := ResolvePackageBindings(sources, packages, Config{}); err == nil || !strings.Contains(err.Error(), "is not in scope") {
		t.Fatalf("unimported compile-time binding was accepted: %v", err)
	}
}
