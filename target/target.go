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

// CCharSigned reports whether plain C char uses the signed representation for
// the target ABI. ARM ABIs default to unsigned char, except for the Apple and
// Windows variants, which define plain char as signed.
func CCharSigned(triple string) bool {
	target := strings.ToLower(EffectiveTriple(triple))
	switch Arch(target) {
	case "arm", "armv6", "armv7", "armv7a", "armv7l", "thumb", "thumbv7", "thumbv7a",
		"aarch64", "arm64":
		return strings.Contains(target, "apple") || strings.Contains(target, "darwin") ||
			strings.Contains(target, "macos") || strings.Contains(target, "windows") ||
			strings.Contains(target, "mingw") || strings.Contains(target, "msvc") ||
			strings.Contains(target, "win32")
	default:
		return true
	}
}

// BuildFacts describes the selected target rather than the compiler host.
type BuildFacts struct {
	OS, Architecture, Environment string
	PointerBits                   int
	CCharSigned                   bool
	HasHostedCapabilities         bool
}

func Facts(triple string, freestanding bool) BuildFacts {
	triple = EffectiveTriple(triple)
	lower := strings.ToLower(triple)
	facts := BuildFacts{OS: "Unknown", Architecture: "Unknown", Environment: "Unknown"}
	switch {
	case strings.Contains(lower, "windows"), strings.Contains(lower, "mingw"), strings.Contains(lower, "msvc"):
		facts.OS = "Windows"
	case strings.Contains(lower, "linux"):
		facts.OS = "Linux"
	case strings.Contains(lower, "darwin"), strings.Contains(lower, "apple"):
		facts.OS = "MacOS"
	case strings.Contains(lower, "freebsd"):
		facts.OS = "FreeBSD"
	case strings.Contains(lower, "openbsd"):
		facts.OS = "OpenBSD"
	case strings.Contains(lower, "netbsd"):
		facts.OS = "NetBSD"
	case strings.Contains(lower, "dragonfly"):
		facts.OS = "DragonFly"
	case strings.Contains(lower, "wasi"):
		facts.OS = "WASI"
	}
	switch Arch(triple) {
	case "386", "i386", "i486", "i586", "i686", "x86":
		facts.Architecture = "X86"
	case "x86_64", "amd64":
		facts.Architecture = "X86_64"
	case "arm", "armv6", "armv7", "armv7a", "armv7l", "thumb", "thumbv7", "thumbv7a":
		facts.Architecture = "ARM32"
	case "aarch64", "arm64":
		facts.Architecture = "AArch64"
	case "wasm32":
		facts.Architecture = "Wasm32"
	case "wasm64":
		facts.Architecture = "Wasm64"
	}
	switch {
	case strings.Contains(lower, "msvc"):
		facts.Environment = "MSVC"
	case strings.Contains(lower, "musl"):
		facts.Environment = "Musl"
	case strings.Contains(lower, "gnu"), strings.Contains(lower, "mingw"):
		facts.Environment = "GNU"
	}
	facts.PointerBits, _ = PointerBits(triple)
	facts.CCharSigned = CCharSigned(triple)
	facts.HasHostedCapabilities = !freestanding && (facts.OS == "Windows" || facts.OS == "Linux" || facts.OS == "MacOS" || facts.OS == "FreeBSD" || facts.OS == "OpenBSD" || facts.OS == "NetBSD" || facts.OS == "DragonFly")
	return facts
}
