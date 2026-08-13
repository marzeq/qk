package main

import (
	"reflect"
	"testing"
)

func TestAppleRelocatableLinkArgs(t *testing.T) {
	got := appleRelocatableLinkArgs([]string{
		"one.o",
		"two.o",
		"-r",
		"-nostdlib",
		"-target", "arm64-apple-macosx",
		"--sysroot=/MacOSX.sdk",
		"-Wl,-u,_entry",
		"-o", "result.o",
	})
	want := []string{
		"one.o",
		"two.o",
		"-r",
		"-syslibroot", "/MacOSX.sdk",
		"-u", "_entry",
		"-o", "result.o",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Apple relocatable linker arguments: got %q, want %q", got, want)
	}
}
