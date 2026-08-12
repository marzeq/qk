package target

import "testing"

func TestCCharSigned(t *testing.T) {
	tests := []struct {
		triple string
		signed bool
	}{
		{triple: "x86_64-linux-gnu", signed: true},
		{triple: "i386-linux-gnu", signed: true},
		{triple: "arm-linux-gnueabihf", signed: false},
		{triple: "aarch64-linux-gnu", signed: false},
		{triple: "aarch64-windows-msvc", signed: true},
		{triple: "arm64-apple-darwin", signed: true},
		{triple: "wasm32-wasi", signed: true},
	}
	for _, test := range tests {
		t.Run(test.triple, func(t *testing.T) {
			if got := CCharSigned(test.triple); got != test.signed {
				t.Fatalf("CCharSigned(%q) = %t, want %t", test.triple, got, test.signed)
			}
		})
	}
}
