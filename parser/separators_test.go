package parser

import (
	"testing"

	"github.com/marzeq/qk/tokeniser"
)

func TestAggregateEntrySeparators(t *testing.T) {
	cases := []struct {
		name, source string
		valid        bool
	}{
		{"struct newline", "let T = struct { a: i32\n b: i32 }", true},
		{"struct comma", "let T = struct { a: i32,\n b: i32, }", true},
		{"struct mixed", "let T = struct { a: i32,\n b: i32\n c: i32 }", false},
		{"enum newline", "let T = enum { A\n B }", true},
		{"enum mixed", "let T = enum { A,\n B\n C }", false},
		{"flags newline", "let T = flags(u8) { A\n B }", true},
		{"flags mixed", "let T = flags(u8) { A,\n B\n C }", false},
		{"union newline", "let T = union { a: i32\n b: str }", true},
		{"union mixed", "let T = union { a: i32,\n b: str\n c: bool }", false},
		{"tagged union newline", "let T = union(@auto) { A\n B(i32) }", true},
		{"record literal newline", "let x = .{ a = 1\n b = 2 }", true},
		{"record literal mixed", "let x = .{ a = 1,\n b = 2\n c = 3 }", false},
		{"flags literal newline", "let x = T.{ .A\n .B }", true},
		{"flags literal mixed", "let x = T.{ .A,\n .B\n .C }", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokens, err := tokeniser.NewTokeniser(tc.source, "test.qk").Tokenise()
			if err != nil {
				t.Fatal(err)
			}
			_, err = NewParser(tokens).Parse()
			if (err == nil) != tc.valid {
				t.Fatalf("parse error = %v, want valid = %v", err, tc.valid)
			}
		})
	}
}
