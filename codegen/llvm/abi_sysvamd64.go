package llvm

import (
	"fmt"

	"github.com/marzeq/qk/types"
)

type sysVAMD64ABIGenerator struct{}

type abiClass int

const (
	abiClassNone abiClass = iota
	abiClassSSE
	abiClassInteger
)

func (sysVAMD64ABIGenerator) aggregateParamChunks(e *Emitter, aggregate types.Type) []abiChunk {
	size, align := e.typeSizeAlign(aggregate)
	if size > 16 || e.hasUnalignedAggregateField(aggregate, 0) {
		return []abiChunk{{
			typeName:   "ptr",
			attributes: fmt.Sprintf(" byval(%s) align %d", e.TypeEmit(aggregate), max(8, align)),
			offset:     -1,
		}}
	}
	classes := make([]abiClass, (size+7)/8)
	floats := make([][]types.PrimitiveType, len(classes))
	e.classifySysVAggregate(aggregate, 0, classes, floats)
	chunks := make([]abiChunk, len(classes))
	for i, class := range classes {
		bytes := min(size-i*8, 8)
		chunks[i].offset = i * 8
		chunks[i].size = bytes
		switch class {
		case abiClassInteger:
			chunks[i].typeName = fmt.Sprintf("i%d", bytes*8)
		case abiClassSSE:
			floatFields := floats[i]
			switch {
			case len(floatFields) == 1 && floatFields[0] == types.PrimitiveF32:
				chunks[i].typeName = "float"
			case len(floatFields) == 1 && floatFields[0] == types.PrimitiveF64:
				chunks[i].typeName = "double"
			case len(floatFields) == 2 &&
				floatFields[0] == types.PrimitiveF32 &&
				floatFields[1] == types.PrimitiveF32:
				chunks[i].typeName = "<2 x float>"
			default:
				panic(fmt.Sprintf("unsupported C SSE aggregate chunk in %v", aggregate))
			}
		default:
			panic(fmt.Sprintf("invalid C aggregate chunk in %v", aggregate))
		}
	}
	return chunks
}

func (g sysVAMD64ABIGenerator) aggregateReturnChunks(e *Emitter, aggregate types.Type) []abiChunk {
	return g.aggregateParamChunks(e, aggregate)
}

func (sysVAMD64ABIGenerator) requiresSRet(e *Emitter, aggregate types.Type) bool {
	size, _ := e.typeSizeAlign(aggregate)
	return size > 16 || e.hasUnalignedAggregateField(aggregate, 0)
}

func (e *Emitter) classifySysVAggregate(
	ty types.Type,
	base int,
	classes []abiClass,
	floats [][]types.PrimitiveType,
) {
	ty = types.Underlying(ty)
	switch t := ty.(type) {
	case types.StructType:
		offset := 0
		for _, field := range t.Fields {
			_, align := e.typeSizeAlign(field.R)
			if !t.Packed {
				offset = alignTo(offset, align)
			}
			e.classifySysVAggregate(field.R, base+offset, classes, floats)
			size, _ := e.typeSizeAlign(field.R)
			offset += size
		}
	case types.UnionType:
		for _, field := range t.Fields {
			e.classifySysVAggregate(field.R, base, classes, floats)
		}
	case types.SliceType:
		pointerSize := e.pointerBytes()
		e.markSysVAggregateClass(base, pointerSize, abiClassInteger, classes)
		e.markSysVAggregateClass(base+pointerSize, pointerSize, abiClassInteger, classes)
	case types.ArrayType:
		elementSize, _ := e.typeSizeAlign(t.Base)
		for i := 0; i < t.Length; i++ {
			e.classifySysVAggregate(t.Base, base+i*elementSize, classes, floats)
		}
	case types.PointerType:
		e.markSysVAggregateClass(base, 8, abiClassInteger, classes)
	case types.EnumType:
		e.markSysVAggregateClass(base, 4, abiClassInteger, classes)
	case types.FlagsType:
		size, _ := e.typeSizeAlign(t.Underlying)
		e.markSysVAggregateClass(base, size, abiClassInteger, classes)
	case types.PrimitiveType:
		size, _ := e.typeSizeAlign(t)
		class := abiClassInteger
		if t == types.PrimitiveF32 || t == types.PrimitiveF64 {
			class = abiClassSSE
		}
		e.markSysVAggregateClass(base, size, class, classes)
		if class == abiClassSSE {
			floats[base/8] = append(floats[base/8], t)
		}
	default:
		panic(fmt.Sprintf("unsupported C aggregate field type %T", ty))
	}
}

func (e *Emitter) hasUnalignedAggregateField(ty types.Type, base int) bool {
	switch t := types.Underlying(ty).(type) {
	case types.StructType:
		offset := 0
		for _, field := range t.Fields {
			_, align := e.typeSizeAlign(field.R)
			if !t.Packed {
				offset = alignTo(offset, align)
			}
			if (base+offset)%align != 0 || e.hasUnalignedAggregateField(field.R, base+offset) {
				return true
			}
			size, _ := e.typeSizeAlign(field.R)
			offset += size
		}
	case types.UnionType:
		for _, field := range t.Fields {
			_, align := e.typeSizeAlign(field.R)
			if base%align != 0 || e.hasUnalignedAggregateField(field.R, base) {
				return true
			}
		}
	}
	return false
}

func (e *Emitter) markSysVAggregateClass(offset, size int, class abiClass, classes []abiClass) {
	for i := offset / 8; i <= (offset+size-1)/8; i++ {
		if class == abiClassInteger || classes[i] == abiClassNone {
			classes[i] = class
		}
	}
}
