package target

import (
	"fmt"
	"strings"

	"github.com/marzeq/qk/types"
)

// LayoutOf returns the storage layout used by QK's LLVM representation for a
// concrete type on the selected target. It is shared by code generation and
// compile-time evaluation of layout expressions.
func LayoutOf(ty types.Type, triple string) (size, align int, err error) {
	bits, ok := PointerBits(triple)
	if !ok {
		return 0, 0, fmt.Errorf("cannot determine pointer width for target %q", triple)
	}
	pointer := bits / 8
	scalar64 := 8
	if pointer == 4 && (Arch(triple) == "386" || Arch(triple) == "i386" || Arch(triple) == "i486" || Arch(triple) == "i586" || Arch(triple) == "i686" || Arch(triple) == "x86") {
		lower := strings.ToLower(EffectiveTriple(triple))
		if !strings.Contains(lower, "windows") && !strings.Contains(lower, "win32") && !strings.Contains(lower, "mingw") && !strings.Contains(lower, "msvc") {
			scalar64 = 4
		}
	}
	var layout func(types.Type) (int, int, error)
	layout = func(value types.Type) (int, int, error) {
		if value == nil {
			return 0, 0, fmt.Errorf("type has no storage layout")
		}
		switch t := types.Underlying(value).(type) {
		case types.PrimitiveType:
			switch t {
			case types.PrimitiveI8, types.PrimitiveU8, types.PrimitiveBool:
				return 1, 1, nil
			case types.PrimitiveI16, types.PrimitiveU16:
				return 2, 2, nil
			case types.PrimitiveI32, types.PrimitiveU32, types.PrimitiveF32:
				return 4, 4, nil
			case types.PrimitiveI64, types.PrimitiveU64, types.PrimitiveF64:
				return 8, scalar64, nil
			case types.PrimitiveIsz, types.PrimitiveUsz:
				return pointer, pointer, nil
			}
		case types.PointerType:
			return pointer, pointer, nil
		case types.TraitPointerType, types.SliceType:
			return pointer * 2, pointer, nil
		case types.EnumType:
			return 4, 4, nil
		case types.FlagsType:
			return layout(t.Underlying)
		case types.ArrayType:
			size, align, err := layout(t.Base)
			return size * t.Length, align, err
		case types.StructType:
			offset, maximum := 0, 1
			for _, field := range t.Fields {
				size, align, err := layout(field.R)
				if err != nil {
					return 0, 0, err
				}
				if t.Packed {
					offset += size
				} else {
					offset = alignTo(offset, align) + size
					maximum = max(maximum, align)
				}
			}
			return alignTo(offset, maximum), maximum, nil
		case types.UnionType:
			maximumSize, maximumAlign := 0, 1
			for _, field := range t.Fields {
				size, align, err := layout(field.R)
				if err != nil {
					return 0, 0, err
				}
				maximumSize = max(maximumSize, size)
				maximumAlign = max(maximumAlign, align)
			}
			return alignTo(maximumSize, maximumAlign), maximumAlign, nil
		}
		return 0, 0, fmt.Errorf("type %v has no storage layout", value)
	}
	return layout(ty)
}

func alignTo(value, alignment int) int {
	return (value + alignment - 1) &^ (alignment - 1)
}

func OffsetOf(ty types.Type, field string, triple string) (int, error) {
	switch aggregate := types.Underlying(ty).(type) {
	case types.UnionType:
		for _, candidate := range aggregate.Fields {
			if candidate.L == field {
				return 0, nil
			}
		}
	case types.StructType:
		offset := 0
		for _, candidate := range aggregate.Fields {
			size, align, err := LayoutOf(candidate.R, triple)
			if err != nil {
				return 0, err
			}
			if !aggregate.Packed {
				offset = alignTo(offset, align)
			}
			if candidate.L == field {
				return offset, nil
			}
			offset += size
		}
	}
	return 0, fmt.Errorf("type %v has no field %q", ty, field)
}
