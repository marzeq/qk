package llvm

import (
	"fmt"

	"github.com/marzeq/qk/types"
)

// abiChunk is one System V AMD64 eightbyte used to pass an aggregate.  LLVM
// IR function types are already ABI-lowered, so foreign C declarations must
// use these chunks rather than the source-level struct type.
type abiChunk struct {
	typeName string
	offset   int
}

type abiClass uint8

const (
	abiClassNone abiClass = iota
	abiClassSSE
	abiClassInteger
)

func (e *Emitter) foreignABIChunks(ty types.Type) []abiChunk {
	st, ok := ty.(types.StructType)
	if !ok {
		return nil
	}

	size, align := e.typeSizeAlign(st)
	if size == 0 {
		return nil
	}
	if size > 16 {
		panic(fmt.Sprintf("foreign aggregate %v is larger than 16 bytes; sret/byval lowering is not implemented", ty))
	}

	classes := make([]abiClass, (size+7)/8)
	floatFields := make([][]types.PrimitiveType, len(classes))
	e.classifyAggregate(st, 0, classes, floatFields)

	chunks := make([]abiChunk, len(classes))
	for i, class := range classes {
		bytes := size - i*8
		if bytes > 8 {
			bytes = 8
		}
		chunks[i] = abiChunk{offset: i * 8}
		switch class {
		case abiClassInteger:
			chunks[i].typeName = fmt.Sprintf("i%d", bytes*8)
		case abiClassSSE:
			fields := floatFields[i]
			switch {
			case len(fields) == 1 && fields[0] == types.PrimitiveF32:
				chunks[i].typeName = "float"
			case len(fields) == 1 && fields[0] == types.PrimitiveF64:
				chunks[i].typeName = "double"
			case len(fields) == 2 && fields[0] == types.PrimitiveF32 && fields[1] == types.PrimitiveF32:
				chunks[i].typeName = "<2 x float>"
			default:
				panic(fmt.Sprintf("unsupported foreign SSE aggregate chunk in %v", ty))
			}
		default:
			panic(fmt.Sprintf("invalid foreign aggregate chunk in %v", ty))
		}
	}
	_ = align
	return chunks
}

func (e *Emitter) foreignABIParamTypes(ty types.Type) []string {
	chunks := e.foreignABIChunks(ty)
	if len(chunks) == 0 {
		return []string{e.TypeEmit(ty)}
	}
	result := make([]string, len(chunks))
	for i, chunk := range chunks {
		result[i] = chunk.typeName
	}
	return result
}

func (e *Emitter) foreignABIReturnType(ty types.Type) string {
	chunks := e.foreignABIChunks(ty)
	switch len(chunks) {
	case 0:
		return e.TypeEmit(ty)
	case 1:
		return chunks[0].typeName
	case 2:
		return fmt.Sprintf("{ %s, %s }", chunks[0].typeName, chunks[1].typeName)
	default:
		panic("aggregate cannot have more than two System V AMD64 eightbytes")
	}
}

func (e *Emitter) typeSizeAlign(ty types.Type) (int, int) {
	switch t := ty.(type) {
	case types.PrimitiveType:
		switch t {
		case types.PrimitiveVoid:
			return 0, 1
		case types.PrimitiveI8, types.PrimitiveU8, types.PrimitiveChar, types.PrimitiveBool:
			return 1, 1
		case types.PrimitiveI16, types.PrimitiveU16:
			return 2, 2
		case types.PrimitiveI32, types.PrimitiveU32, types.PrimitiveF32:
			return 4, 4
		default:
			return 8, 8
		}
	case types.PointerType:
		return 8, 8
	case types.SliceType:
		return 16, 8
	case types.StructType:
		offset, maxAlign := 0, 1
		for _, field := range t.Fields {
			size, align := e.typeSizeAlign(field.R)
			offset = alignTo(offset, align)
			offset += size
			if align > maxAlign {
				maxAlign = align
			}
		}
		return alignTo(offset, maxAlign), maxAlign
	default:
		panic(fmt.Sprintf("unsupported foreign ABI type %T", ty))
	}
}

func alignTo(value, align int) int { return (value + align - 1) &^ (align - 1) }

func (e *Emitter) classifyAggregate(ty types.Type, base int, classes []abiClass, floatFields [][]types.PrimitiveType) {
	switch t := ty.(type) {
	case types.StructType:
		offset := 0
		for _, field := range t.Fields {
			_, align := e.typeSizeAlign(field.R)
			offset = alignTo(offset, align)
			e.classifyAggregate(field.R, base+offset, classes, floatFields)
			size, _ := e.typeSizeAlign(field.R)
			offset += size
		}
	case types.SliceType, types.PointerType:
		e.markAggregateClass(base, 8, abiClassInteger, classes)
		if _, ok := t.(types.SliceType); ok {
			e.markAggregateClass(base+8, 8, abiClassInteger, classes)
		}
	case types.PrimitiveType:
		size, _ := e.typeSizeAlign(t)
		class := abiClassInteger
		if t == types.PrimitiveF32 || t == types.PrimitiveF64 {
			class = abiClassSSE
		}
		e.markAggregateClass(base, size, class, classes)
		if class == abiClassSSE {
			floatFields[base/8] = append(floatFields[base/8], t)
		}
	default:
		panic(fmt.Sprintf("unsupported foreign aggregate field type %T", ty))
	}
}

func (e *Emitter) markAggregateClass(offset, size int, class abiClass, classes []abiClass) {
	for i := offset / 8; i <= (offset+size-1)/8; i++ {
		if class == abiClassInteger || classes[i] == abiClassNone {
			classes[i] = class
		}
	}
}
