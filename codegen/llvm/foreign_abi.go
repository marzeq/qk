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

type foreignABIGenerator interface {
	aggregateChunks(*Emitter, types.StructType) []abiChunk
	requiresSRet(*Emitter, types.StructType) bool
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
