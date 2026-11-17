package formatter

import (
	"os"
	"strings"
	"testing"
)

func TestSample(t *testing.T) {
	input, err := os.ReadFile("testdata/sample.input.txt")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/sample.formatted.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Format(string(input), "sample.qk")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("format mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
	again, err := Format(got, "sample.qk")
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Error("formatter is not idempotent")
	}
	for _, line := range strings.Split(got, "\n") {
		if len([]rune(line)) > MaxWidth() {
			t.Errorf("sample line exceeds %d columns", MaxWidth())
		}
	}
}

func TestRejectInvalidSource(t *testing.T) {
	if _, err := Format("module main\nlet broken = {", "invalid.qk"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestBodyBlankLines(t *testing.T) {
	input := "module main\nlet main() {\nlet x = 1\n\n\n\nfor true { break }\nreturn\n}\n"
	got, err := Format(input, "blanks.qk")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "  let x = 1\n\n\n  for true") {
		t.Fatalf("expected two empty lines before loop:\n%s", got)
	}
	if !strings.Contains(got, "  }\n  return") {
		t.Fatalf("unexpected blank line after loop:\n%s", got)
	}
}
