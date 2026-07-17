package llvm

import (
	"fmt"
	"strings"
)

func (e *Emitter) targetArch() string {
	target := strings.ToLower(e.targetTriple())
	if arch, _, ok := strings.Cut(target, "-"); ok {
		return arch
	}
	return target
}

func (e *Emitter) pointerBits() int {
	arch := e.targetArch()
	switch arch {
	case "386", "i386", "i486", "i586", "i686", "x86",
		"arm", "armv6", "armv7", "armv7a", "armv7l", "thumb", "thumbv7", "thumbv7a",
		"mips", "mipsel", "powerpc", "ppc", "riscv32", "wasm32":
		return 32
	case "x86_64", "amd64", "aarch64", "arm64", "mips64", "mips64el",
		"powerpc64", "powerpc64le", "ppc64", "ppc64le", "riscv64", "s390x", "sparcv9", "wasm64":
		return 64
	default:
		panic(fmt.Sprintf("cannot determine pointer width for target %q", e.targetTriple()))
	}
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
		return 4
	default:
		return 8
	}
}
