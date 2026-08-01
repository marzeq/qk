package llvm

import (
	"fmt"
	"strings"

	qktarget "github.com/marzeq/qk/target"
	"github.com/marzeq/qk/types"
)

type abiChunk struct {
	typeName   string
	attributes string
	offset     int
	size       int
}

type foreignABIGenerator interface {
	aggregateParamChunks(*Emitter, types.Type) []abiChunk
	aggregateReturnChunks(*Emitter, types.Type) []abiChunk
	requiresSRet(*Emitter, types.Type) bool
}

func (e *Emitter) foreignABIAggregate(ty types.Type) (types.Type, foreignABIGenerator) {
	ty = types.Underlying(ty)
	switch ty.(type) {
	case types.StructType, types.UnionType, types.SliceType, types.ArrayType:
	default:
		return nil, nil
	}
	generator := e.foreignABIGenerator()
	if generator == nil {
		panic(fmt.Sprintf("C aggregate ABI lowering is not implemented for target %q", e.targetTriple()))
	}
	return ty, generator
}

func (e *Emitter) foreignABIChunks(ty types.Type) []abiChunk {
	aggregate, generator := e.foreignABIAggregate(ty)
	if generator == nil {
		return nil
	}
	return generator.aggregateParamChunks(e, aggregate)
}

func (e *Emitter) foreignABIReturnChunks(ty types.Type) []abiChunk {
	aggregate, generator := e.foreignABIAggregate(ty)
	if generator == nil {
		return nil
	}
	return generator.aggregateReturnChunks(e, aggregate)
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
	if e.foreignABIReturnUsesSRet(ty) {
		return "void"
	}
	chunks := e.foreignABIReturnChunks(ty)
	if len(chunks) == 0 {
		return e.TypeEmit(ty)
	}
	if len(chunks) == 1 {
		return chunks[0].typeName
	}
	return fmt.Sprintf("{ %s, %s }", chunks[0].typeName, chunks[1].typeName)
}

func (e *Emitter) foreignABIReturnUsesSRet(ty types.Type) bool {
	aggregate := types.Underlying(ty)
	switch aggregate.(type) {
	case types.StructType, types.UnionType, types.SliceType, types.ArrayType:
	default:
		return false
	}
	generator := e.foreignABIGenerator()
	return generator != nil && generator.requiresSRet(e, aggregate)
}

func (e *Emitter) foreignABISRetArgument(ty types.Type, value string) string {
	_, align := e.typeSizeAlign(ty)
	argument := fmt.Sprintf("ptr sret(%s) align %d", e.TypeEmit(ty), align)
	if value != "" {
		argument += " " + value
	}
	return argument
}

func (e *Emitter) abiScratchAllocation(ty types.Type, chunks []abiChunk) (string, bool) {
	actualSize, _ := e.typeSizeAlign(ty)
	scratchSize := actualSize
	for _, chunk := range chunks {
		if chunk.offset >= 0 {
			scratchSize = max(scratchSize, chunk.offset+chunk.size)
		}
	}
	if scratchSize == actualSize {
		return e.TypeEmit(ty), false
	}
	return fmt.Sprintf("[%d x i8]", scratchSize), true
}

func (e *Emitter) targetTriple() string {
	return qktarget.EffectiveTriple(e.TargetTriple)
}

func (e *Emitter) foreignABIGenerator() foreignABIGenerator {
	target := strings.ToLower(e.targetTriple())
	if strings.Contains(target, "aarch64") || strings.Contains(target, "arm64") {
		return aarch64ABIGenerator{linux: strings.Contains(target, "linux")}
	}
	switch e.targetArch() {
	case "386", "i386", "i486", "i586", "i686", "x86":
		if strings.Contains(target, "windows") || strings.Contains(target, "win32") ||
			strings.Contains(target, "mingw") || strings.Contains(target, "msvc") {
			return win32ABIGenerator{}
		}
		return sysVI386ABIGenerator{}
	case "x86_64", "amd64":
	default:
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
		case types.PrimitiveI8, types.PrimitiveU8, types.PrimitiveBool:
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
	case types.TraitPointerType:
		return e.pointerBytes() * 2, e.pointerBytes()
	case types.SliceType:
		// Slices are represented as { data pointer, length }.
		return e.pointerBytes() * 2, e.pointerBytes()
	case types.ArrayType:
		elementSize, elementAlign := e.typeSizeAlign(t.Base)
		return elementSize * t.Length, elementAlign
	case types.EnumType:
		return 4, 4
	case types.FlagsType:
		return e.typeSizeAlign(t.Underlying)
	case types.StructType:
		if t.Packed {
			size := 0
			for _, field := range t.Fields {
				fieldSize, _ := e.typeSizeAlign(field.R)
				size += fieldSize
			}
			return size, 1
		}
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
