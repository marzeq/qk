package llvm

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/marzeq/qk/types"
)

type abiChunk struct {
	typeName   string
	attributes string
	offset     int
}

type abiClass int

const (
	abiClassNone abiClass = iota
	abiClassSSE
	abiClassInteger
)

type foreignABIGenerator interface {
	aggregateChunks(*Emitter, types.StructType) []abiChunk
	requiresSRet(*Emitter, types.StructType) bool
}

type sysVAMD64ABIGenerator struct{}
type win64ABIGenerator struct{}
type aarch64ABIGenerator struct {
	linux bool
}

func (e *Emitter) foreignABIChunks(ty types.Type) []abiChunk {
	ty = types.Underlying(ty)
	st, ok := ty.(types.StructType)
	if !ok {
		return nil
	}
	generator := e.foreignABIGenerator()
	if generator == nil {
		panic(fmt.Sprintf("C aggregate ABI lowering is not implemented for target %q", e.targetTriple()))
	}
	return generator.aggregateChunks(e, st)
}

func (sysVAMD64ABIGenerator) aggregateChunks(e *Emitter, st types.StructType) []abiChunk {
	size, _ := e.typeSizeAlign(st)
	if size > 16 {
		panic(fmt.Sprintf("C aggregate %v is larger than 16 bytes; byval/sret lowering is not implemented", st))
	}
	classes := make([]abiClass, (size+7)/8)
	floats := make([][]types.PrimitiveType, len(classes))
	e.classifyAggregate(st, 0, classes, floats)
	chunks := make([]abiChunk, len(classes))
	for i, class := range classes {
		bytes := min(size-i*8, 8)
		chunks[i].offset = i * 8
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
				panic(fmt.Sprintf("unsupported C SSE aggregate chunk in %v", st))
			}
		default:
			panic(fmt.Sprintf("invalid C aggregate chunk in %v", st))
		}
	}
	return chunks
}

func (sysVAMD64ABIGenerator) requiresSRet(e *Emitter, st types.StructType) bool {
	size, _ := e.typeSizeAlign(st)
	return size > 16
}

func (win64ABIGenerator) aggregateChunks(e *Emitter, st types.StructType) []abiChunk {
	size, _ := e.typeSizeAlign(st)
	switch size {
	case 1, 2, 4, 8:
		return []abiChunk{{typeName: fmt.Sprintf("i%d", size*8)}}
	default:
		return []abiChunk{{typeName: "ptr", offset: -1}}
	}
}

func (win64ABIGenerator) requiresSRet(e *Emitter, st types.StructType) bool {
	size, _ := e.typeSizeAlign(st)
	return size != 1 && size != 2 && size != 4 && size != 8
}

func (g aarch64ABIGenerator) aggregateChunks(e *Emitter, st types.StructType) []abiChunk {
	if element, count, ok := e.homogeneousFloatAggregate(st); ok && count <= 4 {
		chunk := abiChunk{typeName: fmt.Sprintf("[%d x %s]", count, element)}
		if g.linux && count > 1 {
			chunk.attributes = " alignstack(8)"
		}
		return []abiChunk{chunk}
	}

	size, _ := e.typeSizeAlign(st)
	switch {
	case size <= 8:
		return []abiChunk{{typeName: "i64"}}
	case size <= 16:
		return []abiChunk{{typeName: "i64"}, {typeName: "i64", offset: 8}}
	default:
		return []abiChunk{{typeName: "ptr", offset: -1}}
	}
}

func (aarch64ABIGenerator) requiresSRet(e *Emitter, st types.StructType) bool {
	size, _ := e.typeSizeAlign(st)
	return size > 16
}

func (e *Emitter) foreignABIParamTypes(ty types.Type) []string {
	chunks := e.foreignABIChunks(ty)
	if len(chunks) == 0 {
		return []string{e.TypeEmit(ty)}
	}
	result := make([]string, len(chunks))
	for i := range chunks {
		result[i] = chunks[i].typeName + chunks[i].attributes
	}
	return result
}

func (e *Emitter) foreignABIReturnType(ty types.Type) string {
	if st, ok := types.Underlying(ty).(types.StructType); ok {
		if generator := e.foreignABIGenerator(); generator != nil && generator.requiresSRet(e, st) {
			panic(fmt.Sprintf("Win64 aggregate return %v requires sret lowering", ty))
		}
	}
	chunks := e.foreignABIChunks(ty)
	if len(chunks) == 0 {
		return e.TypeEmit(ty)
	}
	if len(chunks) == 1 {
		return chunks[0].typeName
	}
	return fmt.Sprintf("{ %s, %s }", chunks[0].typeName, chunks[1].typeName)
}

func (e *Emitter) targetTriple() string {
	if e.TargetTriple != "" {
		return e.TargetTriple
	}
	return runtime.GOARCH + "-" + runtime.GOOS
}

func (e *Emitter) foreignABIGenerator() foreignABIGenerator {
	target := strings.ToLower(e.targetTriple())
	if strings.Contains(target, "aarch64") || strings.Contains(target, "arm64") {
		return aarch64ABIGenerator{linux: strings.Contains(target, "linux")}
	}
	if !strings.Contains(target, "x86_64") && !strings.Contains(target, "amd64") {
		return nil
	}
	if strings.Contains(target, "windows") || strings.Contains(target, "win32") {
		return win64ABIGenerator{}
	}
	return sysVAMD64ABIGenerator{}
}

func (e *Emitter) homogeneousFloatAggregate(st types.StructType) (element string, count int, ok bool) {
	var primitive types.PrimitiveType
	var visit func(types.Type) bool
	visit = func(ty types.Type) bool {
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

func (e *Emitter) typeSizeAlign(ty types.Type) (int, int) {
	ty = types.Underlying(ty)
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
		case types.PrimitiveIsz, types.PrimitiveUsz:
			return e.pointerBytes(), e.pointerBytes()
		case types.PrimitiveI64, types.PrimitiveU64, types.PrimitiveF64:
			return 8, e.scalar64Align()
		default:
			panic(fmt.Sprintf("unsupported primitive type %v", t))
		}
	case types.PointerType:
		return e.pointerBytes(), e.pointerBytes()
	case types.SliceType:
		// Slices are represented as { data pointer, length }.
		return e.pointerBytes() * 2, e.pointerBytes()
	case types.EnumType:
		return 4, 4
	case types.StructType:
		offset, maxAlign := 0, 1
		for _, field := range t.Fields {
			size, align := e.typeSizeAlign(field.R)
			offset = alignTo(offset, align) + size
			maxAlign = max(maxAlign, align)
		}
		return alignTo(offset, maxAlign), maxAlign
	case types.UnionType:
		maxSize, maxAlign := 0, 1
		for _, field := range t.Fields {
			size, align := e.typeSizeAlign(field.R)
			maxSize = max(maxSize, size)
			maxAlign = max(maxAlign, align)
		}
		return alignTo(maxSize, maxAlign), maxAlign
	default:
		panic(fmt.Sprintf("unsupported C ABI type %T", ty))
	}
}

func alignTo(v, a int) int {
	return (v + a - 1) &^ (a - 1)
}

func (e *Emitter) classifyAggregate(
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
			offset = alignTo(offset, align)
			e.classifyAggregate(field.R, base+offset, classes, floats)
			size, _ := e.typeSizeAlign(field.R)
			offset += size
		}
	case types.UnionType:
		for _, field := range t.Fields {
			e.classifyAggregate(field.R, base, classes, floats)
		}
	case types.PointerType:
		e.markAggregateClass(base, 8, abiClassInteger, classes)
	case types.EnumType:
		e.markAggregateClass(base, 4, abiClassInteger, classes)
	case types.PrimitiveType:
		size, _ := e.typeSizeAlign(t)
		class := abiClassInteger
		if t == types.PrimitiveF32 || t == types.PrimitiveF64 {
			class = abiClassSSE
		}
		e.markAggregateClass(base, size, class, classes)
		if class == abiClassSSE {
			floats[base/8] = append(floats[base/8], t)
		}
	default:
		panic(fmt.Sprintf("unsupported C aggregate field type %T", ty))
	}
}

func (e *Emitter) markAggregateClass(offset, size int, class abiClass, classes []abiClass) {
	for i := offset / 8; i <= (offset+size-1)/8; i++ {
		if class == abiClassInteger || classes[i] == abiClassNone {
			classes[i] = class
		}
	}
}
