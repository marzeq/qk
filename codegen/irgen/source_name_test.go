package irgen

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
)

func TestSourceCallNamePreservesRuneOffsets(t *testing.T) {
	for _, source := range []string{
		"// prefix\nfirst()\nsecond()",
		"// π\nfirst()\nsecond()",
	} {
		generator := &Generator{}
		for _, name := range []string{"first", "second"} {
			byteOffset := strings.Index(source, name)
			start := utf8.RuneCountInString(source[:byteOffset])
			identifier := &parser.IdentifierNode{Loc: shared.Location{
				FilePath: "input.qk", SourceText: source,
				Offset: start, EndOffset: start + utf8.RuneCountInString(name),
			}}
			if got := generator.sourceCallName(identifier); got != name {
				t.Fatalf("source %q: got %q, want %q", source, got, name)
			}
		}
	}
}
