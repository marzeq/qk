package llvm

import (
	"fmt"
	"strings"

	qktarget "github.com/marzeq/qk/target"
)

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
