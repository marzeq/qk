package main

import (
	"runtime"
	"testing"
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
