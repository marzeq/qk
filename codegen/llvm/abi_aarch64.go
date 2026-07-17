package llvm

import (
	"fmt"

	"github.com/marzeq/qk/types"
)

type aarch64ABIGenerator struct {
	linux bool
}

func (g aarch64ABIGenerator) aggregateChunks(e *Emitter, aggregate types.Type) []abiChunk {
	if st, isStruct := aggregate.(types.StructType); isStruct {
		if element, count, ok := homogeneousFloatAggregate(st); ok && count <= 4 {
			chunk := abiChunk{typeName: fmt.Sprintf("[%d x %s]", count, element)}
			if g.linux && count > 1 {
				chunk.attributes = " alignstack(8)"
			}
			return []abiChunk{chunk}
		}
	}

	size, _ := e.typeSizeAlign(aggregate)
	switch {
	case size <= 8:
		return []abiChunk{{typeName: "i64"}}
	case size <= 16:
		return []abiChunk{{typeName: "i64"}, {typeName: "i64", offset: 8}}
	default:
		return []abiChunk{{typeName: "ptr", offset: -1}}
	}
}

func (aarch64ABIGenerator) requiresSRet(e *Emitter, aggregate types.Type) bool {
	size, _ := e.typeSizeAlign(aggregate)
	return size > 16
}

func homogeneousFloatAggregate(st types.StructType) (element string, count int, ok bool) {
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
		default:
			return false
		}
	}
	if !visit(st) || count == 0 {
		return "", 0, false
	}
	if primitive == types.PrimitiveF32 {
		return "float", count, true
	}
	return "double", count, true
}
