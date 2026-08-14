package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseInstallArgs(t *testing.T) {
	packageDir := t.TempDir()
	installDir := t.TempDir()

	args, err := newArgumentParser([]string{"install", packageDir, installDir}).parse()
	if err != nil {
		t.Fatalf("parse install arguments: %v", err)
	}
	if !args.install {
		t.Fatal("install command was not recorded")
	}
	if !args.release {
		t.Fatal("install command did not select release mode")
	}
	if args.outputType != OutputExecutable {
		t.Fatalf("output type: got %v, want executable", args.outputType)
	}
	wantName := filepath.Base(packageDir)
	if runtime.GOOS == "windows" {
		wantName += ".exe"
	}
	wantOutput := filepath.Join(installDir, wantName)
	if args.output != wantOutput {
		t.Fatalf("output: got %q, want %q", args.output, wantOutput)
	}
}

func TestParseInstallArgsRequiresDestination(t *testing.T) {
	_, err := newArgumentParser([]string{"install", t.TempDir()}).parse()
	if err == nil || !strings.Contains(err.Error(), "install directory") {
		t.Fatalf("error: got %v, want missing install directory", err)
	}
}

func TestParseInstallArgsRequiresDirectoryDestination(t *testing.T) {
	packageDir := t.TempDir()
	destination := filepath.Join(t.TempDir(), "missing")

	_, err := newArgumentParser([]string{"install", packageDir, destination}).parse()
	if err == nil || !strings.Contains(err.Error(), "install path is not a directory") {
		t.Fatalf("error: got %v, want invalid install directory", err)
	}
}

func TestParseInstallArgsRejectsOutputOverride(t *testing.T) {
	_, err := newArgumentParser([]string{
		"install", "-o", "custom", t.TempDir(), t.TempDir(),
	}).parse()
	if err == nil || !strings.Contains(err.Error(), "-o cannot be used with install") {
		t.Fatalf("error: got %v, want output override rejection", err)
	}
}
