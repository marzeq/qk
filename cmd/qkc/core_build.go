package main

import (
	"fmt"

	"github.com/marzeq/qk/loader"
	qktarget "github.com/marzeq/qk/target"
)

// core.build is supplied by the compiler so it is available with -nostdlib.
// Its values describe the selected target, including during cross compilation.
func coreBuildSource(config loader.StageConfig) string {
	facts := qktarget.Facts(config.TargetTriple, config.Freestanding)
	mode := "Debug"
	if config.ReleaseMode == loader.StageRelease {
		mode = "Release"
	}
	return fmt.Sprintf(`module core.build
pub let OperatingSystem = enum { Unknown, Windows, Linux, MacOS, FreeBSD, OpenBSD, NetBSD, DragonFly, WASI }
pub let Architecture = enum { Unknown, X86, X86_64, ARM32, AArch64, Wasm32, Wasm64 }
pub let TargetEnvironment = enum { Unknown, GNU, MSVC, Musl }
pub let BuildMode = enum { Debug, Release }
pub let os: OperatingSystem = $.%s
pub let arch: Architecture = $.%s
pub let environment: TargetEnvironment = $.%s
pub let release_mode: BuildMode = $.%s
pub let pointer_bits: usz = $%d
pub let c_char_signed: bool = $%t
pub let freestanding: bool = $%t
pub let has_libc: bool = $%t
pub let has_filesystem: bool = $%t
pub let has_environment: bool = $%t
pub let has_process_exit: bool = $%t
`, facts.OS, facts.Architecture, facts.Environment, mode, facts.PointerBits, facts.CCharSigned, config.Freestanding,
		facts.HasHostedCapabilities, facts.HasHostedCapabilities, facts.HasHostedCapabilities, facts.HasHostedCapabilities)
}
