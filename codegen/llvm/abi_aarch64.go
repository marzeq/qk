package llvm

import (
	"fmt"

	"github.com/marzeq/qk/types"
)

type aarch64ABIGenerator struct {
	linux bool
}

func (g aarch64ABIGenerator) aggregateParamChunks(e *Emitter, aggregate types.Type) []abiChunk {
	if element, count, ok := homogeneousFloatAggregate(aggregate); ok && count <= 4 {
		size, _ := e.typeSizeAlign(aggregate)
		chunk := abiChunk{typeName: fmt.Sprintf("[%d x %s]", count, element), size: size}
		if g.linux && count > 1 {
			chunk.attributes = " alignstack(8)"
		}
		return []abiChunk{chunk}
	}

	size, _ := e.typeSizeAlign(aggregate)
	switch {
	case size <= 8:
		return []abiChunk{{typeName: "i64", size: 8}}
	case size <= 16:
		return []abiChunk{{typeName: "[2 x i64]", size: 16}}
	default:
		return []abiChunk{{typeName: "ptr", offset: -1}}
	}
}

func (g aarch64ABIGenerator) aggregateReturnChunks(e *Emitter, aggregate types.Type) []abiChunk {
	if element, count, ok := homogeneousFloatAggregate(aggregate); ok && count <= 4 {
		size, _ := e.typeSizeAlign(aggregate)
		return []abiChunk{{typeName: fmt.Sprintf("[%d x %s]", count, element), size: size}}
	}

	size, _ := e.typeSizeAlign(aggregate)
	switch {
	case size <= 8:
		return []abiChunk{{typeName: fmt.Sprintf("i%d", size*8), size: size}}
	case size <= 16:
		return []abiChunk{{typeName: "[2 x i64]", size: 16}}
	default:
		return []abiChunk{{typeName: "ptr", offset: -1}}
	}
}

func (aarch64ABIGenerator) requiresSRet(e *Emitter, aggregate types.Type) bool {
	size, _ := e.typeSizeAlign(aggregate)
	return size > 16
}

func homogeneousFloatAggregate(aggregate types.Type) (element string, count int, ok bool) {
	var primitive types.PrimitiveType
	var visit func(types.Type) bool
	visit = func(ty types.Type) bool {
		ty = types.Underlying(ty)
		switch t := ty.(type) {
		case types.PrimitiveType:
			if t != types.PrimitiveF32 && t != types.PrimitiveF64 {
				return false
			}
			if primitive != "" && primitive != t {
				return false
			}
			primitive = t
			count++
			return true
		case types.StructType:
			for _, field := range t.Fields {
				if !visit(field.R) {
					return false
				}
			}
			return true
		case types.ArrayType:
			for range t.Length {
				if !visit(t.Base) {
					return false
				}
			}
			return true
		default:
			return false
		}
	}
	if !visit(aggregate) || count == 0 {
		return "", 0, false
	}
	if primitive == types.PrimitiveF32 {
		return "float", count, true
	}
	return "double", count, true
}
