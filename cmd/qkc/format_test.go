package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunFormatPaths(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "a.qk")
	second := filepath.Join(nested, "b.qk")
	source := "module main\nlet value:i32=1\n"
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := runFormat([]string{root}); err != nil {
		t.Fatal(err)
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(a), "value: i32 = 1") {
		t.Fatalf("directory file was not formatted: %s", a)
	}
	if string(b) != source {
		t.Fatal("directory format recursed unexpectedly")
	}
	if err := runFormat([]string{filepath.Join(root, "...")}); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "value: i32 = 1") {
		t.Fatalf("recursive file was not formatted: %s", b)
	}
	if err := runFormat([]string{first}); err != nil {
		t.Fatal(err)
	}
}
