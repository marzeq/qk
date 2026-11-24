package main

import (
	"strings"
	"testing"
)

func TestHostedRuntimeUsesDiscoveredProcessGlobalNames(t *testing.T) {
	llvm, err := buildHostedRuntime("x86_64-unknown-linux-gnu", false, true, "args_symbol", "env_symbol", "", "user_main")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"args_symbol", "env_symbol"} {
		if !strings.Contains(llvm, "@"+name) {
			t.Fatalf("hosted runtime does not reference %s", name)
		}
	}
	if strings.Contains(llvm, "__qk_0_3_std2_os_global_") {
		t.Fatal("hosted runtime contains a hard-coded std.os symbol")
	}
}
