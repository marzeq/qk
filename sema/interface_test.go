package sema

import (
	"testing"

	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/types"
)

func TestInterfaceTypePreservesStructuralInformation(t *testing.T) {
	input := types.DefinedType{
		Module: "example", Name: "Result",
		Underlying: types.StructType{
			Packed: true,
			Fields: []shared.Pair[string, types.Type]{{L: "value", R: types.PointerType{Base: types.PrimitiveU8, Mutable: true}}},
			TaggedUnion: &types.TaggedUnionInfo{
				Tag:      types.PrimitiveU8,
				Variants: []types.TaggedUnionVariant{{Name: "ok", TagValue: "1", Fields: []shared.Pair[string, types.Type]{{L: "0", R: types.PrimitiveI32}}}},
			},
		},
	}
	encoded := interfaceType(input)
	if encoded.Kind != "defined" || encoded.Module != "example" || encoded.Underlying == nil || !encoded.Underlying.Packed {
		t.Fatalf("nominal/structural information was lost: %#v", encoded)
	}
	if encoded.Underlying.TaggedUnion == nil || encoded.Underlying.TaggedUnion.Variants[0].TagValue != "1" {
		t.Fatalf("tagged-union information was lost: %#v", encoded.Underlying)
	}
	pointer := encoded.Underlying.Fields[0].Type
	if pointer.Kind != "pointer" || !pointer.Mutable || pointer.Base.Name != "u8" {
		t.Fatalf("field type was not preserved: %#v", pointer)
	}
}

func TestInterfaceTypeDoesNotFollowRecursiveAliasTarget(t *testing.T) {
	var target types.Type
	reference := &types.AliasRef{Module: "example", Name: "Node", Target: &target}
	target = types.DefinedType{Module: "example", Name: "Node", Underlying: types.PointerType{Base: reference}}
	encoded := interfaceType(reference)
	if encoded.Kind != "alias_ref" || encoded.Module != "example" || encoded.Name != "Node" {
		t.Fatalf("recursive alias was not encoded as a reference: %#v", encoded)
	}
}
