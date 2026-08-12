package sema

import (
	"reflect"
	"testing"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/types"
)

func TestInlineAsmTemplateOperands(t *testing.T) {
	got := inlineAsmTemplateOperands("add $2, ${0:q}; $$literal; use ${12}")
	want := []int{2, 0, 12}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operands: got %v, want %v", got, want)
	}
}

func TestInlineAsmTiedOutput(t *testing.T) {
	if index, ok := inlineAsmTiedOutput("12"); !ok || index != 12 {
		t.Fatalf("valid tie: index=%d ok=%v", index, ok)
	}
	for _, invalid := range []string{"", "r", "1r", "-1"} {
		if _, ok := inlineAsmTiedOutput(invalid); ok {
			t.Fatalf("accepted invalid tie %q", invalid)
		}
	}
}

func TestInlineAsmCommonConstraintCompatibility(t *testing.T) {
	for _, test := range []struct {
		constraint string
		type_      types.Type
		want       bool
	}{
		{"r", types.PrimitiveI32, true},
		{"r", types.PointerType{Base: types.PrimitiveU8}, true},
		{"r", types.PrimitiveF64, false},
		{"f", types.PrimitiveF32, true},
		{"f", types.PrimitiveU64, false},
		{"i", types.PrimitiveI32, true},
		{"x", types.PrimitiveF32, true}, // target-specific: defer to LLVM
	} {
		if got := inlineAsmConstraintAcceptsType(test.constraint, test.type_); got != test.want {
			t.Errorf("constraint %q with %v: got %v, want %v", test.constraint, test.type_, got, test.want)
		}
	}
}

func TestInlineAsmIntegerConstant(t *testing.T) {
	if !inlineAsmIntegerConstant(&parser.IntegerLiteralNode{}) || inlineAsmIntegerConstant(&parser.IdentifierNode{}) {
		t.Fatal("integer constant classification is incorrect")
	}
}

func TestInlineAsmClobberNames(t *testing.T) {
	for _, valid := range []string{"memory", "cc", "xmm0", "flags-register"} {
		if !inlineAsmClobberName(valid) {
			t.Errorf("rejected valid clobber %q", valid)
		}
	}
	for _, invalid := range []string{"", "~{cc}", "xmm 0", "cc,flags"} {
		if inlineAsmClobberName(invalid) {
			t.Errorf("accepted invalid clobber %q", invalid)
		}
	}
}
