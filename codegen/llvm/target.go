package llvm

import (
	"fmt"
	"strings"

	qktarget "github.com/marzeq/qk/target"
	"github.com/marzeq/qk/types"
)

func (e *Emitter) integerBits(t types.PrimitiveType) int {
	switch t {
	case types.PrimitiveI8, types.PrimitiveU8:
		return 8
	case types.PrimitiveI16, types.PrimitiveU16:
		return 16
	case types.PrimitiveI32, types.PrimitiveU32:
		return 32
	case types.PrimitiveI64, types.PrimitiveU64:
		return 64
	case types.PrimitiveIsz, types.PrimitiveUsz:
		return e.pointerBits()
	default:
		return 0
	}
}

func (e *Emitter) targetArch() string {
	return qktarget.Arch(e.targetTriple())
}

func (e *Emitter) pointerBits() int {
	bits, ok := qktarget.PointerBits(e.targetTriple())
	if !ok {
		panic(fmt.Sprintf("cannot determine pointer width for target %q", e.targetTriple()))
	}
	return bits
}

func (e *Emitter) pointerBytes() int {
	return e.pointerBits() / 8
}

func (e *Emitter) pointerIntType() string {
	return fmt.Sprintf("i%d", e.pointerBits())
}

func (e *Emitter) scalar64Align() int {
	switch e.targetArch() {
	case "386", "i386", "i486", "i586", "i686", "x86":
		target := strings.ToLower(e.targetTriple())
		if strings.Contains(target, "windows") || strings.Contains(target, "win32") ||
			strings.Contains(target, "mingw") || strings.Contains(target, "msvc") {
			return 8
		}
		return 4
	default:
		return 8
	}
}
