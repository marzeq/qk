package main

import (
	"strings"
	"testing"
)

func TestMainModuleFreestandingAttributes(t *testing.T) {
	args := &Args{mainModule: "."}
	sources := map[string]string{"main.qk": "module main @freestanding @entry(\"_start\")\nlet start(): void @export(\"_start\") {}\n"}
	packages := map[string]string{"main.qk": "."}
	if err := applyMainModuleAttributes(args, sources, packages); err != nil {
		t.Fatal(err)
	}
	if !args.freestanding || args.entry != "_start" {
		t.Fatalf("mode = %v, entry = %q", args.freestanding, args.entry)
	}
	args = &Args{mainModule: ".", entry: "boot"}
	if err := applyMainModuleAttributes(args, sources, packages); err != nil {
		t.Fatal(err)
	}
	if args.entry != "boot" {
		t.Fatalf("-entry did not override @entry: %q", args.entry)
	}
}

func TestFreestandingLinkSelectsEntryAndSuppressesCRT(t *testing.T) {
	args := &Args{target: "x86_64-unknown-linux-gnu", outputType: OutputExecutable, output: "app", freestanding: true, entry: "boot"}
	linkArgs, err := buildLinkArgs([]string{"app.o"}, nil, args)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(linkArgs, " ")
	if !strings.Contains(joined, "-nostdlib") || !strings.Contains(joined, "-Wl,-e,boot") {
		t.Fatalf("freestanding link flags missing: %q", joined)
	}
	args.freestanding = false
	linkArgs, err = buildLinkArgs([]string{"app.o"}, nil, args)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(linkArgs, " "), "-nostdlib") {
		t.Fatalf("hosted link suppresses libc: %q", linkArgs)
	}
}
