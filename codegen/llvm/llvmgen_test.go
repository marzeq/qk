package llvm

import "testing"

func TestEncodeLLVMString(t *testing.T) {
	got := encodeLLVMString("plain text \"\\\x00\xff")
	want := "plain text \\22\\5C\\00\\FF"
	if got != want {
		t.Fatalf("encodeLLVMString() = %q, want %q", got, want)
	}
}
