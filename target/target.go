package target

import (
	"runtime"
	"strings"
)

func EffectiveTriple(triple string) string {
	if triple != "" {
		return triple
	}
	return runtime.GOARCH + "-" + runtime.GOOS
}

func Arch(triple string) string {
	triple = strings.ToLower(EffectiveTriple(triple))
	if arch, _, ok := strings.Cut(triple, "-"); ok {
		return arch
	}
	return triple
}

func PointerBits(triple string) (int, bool) {
	switch Arch(triple) {
	case "386", "i386", "i486", "i586", "i686", "x86",
		"arm", "armv6", "armv7", "armv7a", "armv7l", "thumb", "thumbv7", "thumbv7a",
		"mips", "mipsel", "powerpc", "ppc", "riscv32", "wasm32":
		return 32, true
	case "x86_64", "amd64", "aarch64", "arm64", "mips64", "mips64el",
		"powerpc64", "powerpc64le", "ppc64", "ppc64le", "riscv64", "s390x", "sparcv9", "wasm64":
		return 64, true
	default:
		return 0, false
	}
}
