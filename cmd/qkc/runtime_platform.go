package main

import (
	"fmt"
	"strings"

	qktarget "github.com/marzeq/qk/target"
)

// buildPlatformPanicRuntime deliberately uses only the target kernel ABI.  It
// must remain usable when no C library was selected by the program.
func buildPlatformPanicRuntime(target, usz string) (string, error) {
	arch := qktarget.Arch(target)
	switch {
	case targetIsWindows(target):
		return windowsPanicRuntime(usz), nil
	case arch == "wasm32" || arch == "wasm64":
		return wasmPanicRuntime(usz), nil
	case strings.Contains(target, "linux"):
		return unixPanicRuntime("Linux", arch, usz, 1, 60)
	case strings.Contains(target, "freebsd"):
		return unixPanicRuntime("FreeBSD", arch, usz, 4, 1)
	case strings.Contains(target, "openbsd"):
		return unixPanicRuntime("OpenBSD", arch, usz, 4, 1)
	case strings.Contains(target, "netbsd"):
		return unixPanicRuntime("NetBSD", arch, usz, 4, 1)
	case strings.Contains(target, "dragonfly"):
		return unixPanicRuntime("DragonFly BSD", arch, usz, 4, 1)
	case targetIsApple(target):
		return unixPanicRuntime("Darwin", arch, usz, 0x2000004, 0x2000001)
	default:
		return "", fmt.Errorf("the QK runtime has no libc-free process I/O implementation for target %q", target)
	}
}

func wasmPanicRuntime(usz string) string {
	return fmt.Sprintf(`define hidden void @__qk_panic({ ptr, %s } %%message) #1 {
entry:
  unreachable
}

`, usz)
}

func targetIsLinuxX8664(target string) bool {
	target = strings.ToLower(qktarget.EffectiveTriple(target))
	arch := qktarget.Arch(target)
	return strings.Contains(target, "linux") && (arch == "x86_64" || arch == "amd64")
}

func unixPanicRuntime(osName, arch, usz string, writeNumber, exitNumber uint64) (string, error) {
	if arch == "aarch64" || arch == "arm64" {
		return arm64UnixPanicRuntime(usz, writeNumber, exitNumber, osName == "Darwin"), nil
	}
	if arch != "x86_64" && arch != "amd64" {
		return "", fmt.Errorf("the %s libc-free runtime is not implemented for architecture %q", osName, arch)
	}
	return fmt.Sprintf(`define hidden void @__qk_panic({ ptr, %[1]s } %%message) #1 {
entry:
  %%data = extractvalue { ptr, %[1]s } %%message, 0
  %%length = extractvalue { ptr, %[1]s } %%message, 1
  %%prefix.result = call i64 asm sideeffect "syscall", "={rax},{rax},{rdi},{rsi},{rdx},~{rcx},~{r11},~{memory}"(i64 %[2]d, i64 2, ptr @__qk_panic_prefix, %[1]s 7)
  %%message.result = call i64 asm sideeffect "syscall", "={rax},{rax},{rdi},{rsi},{rdx},~{rcx},~{r11},~{memory}"(i64 %[2]d, i64 2, ptr %%data, %[1]s %%length)
  %%newline.result = call i64 asm sideeffect "syscall", "={rax},{rax},{rdi},{rsi},{rdx},~{rcx},~{r11},~{memory}"(i64 %[2]d, i64 2, ptr @__qk_panic_newline, %[1]s 1)
  %%exit.result = call i64 asm sideeffect "syscall", "={rax},{rax},{rdi},~{rcx},~{r11},~{memory}"(i64 %[3]d, i64 101)
  unreachable
}

@__qk_panic_prefix = private constant [7 x i8] c"panic: "
@__qk_panic_newline = private constant [1 x i8] c"\0A"

`, usz, writeNumber, exitNumber), nil
}

func arm64UnixPanicRuntime(usz string, writeNumber, exitNumber uint64, darwin bool) string {
	numberRegister, trap := "x8", "svc #0"
	if darwin {
		numberRegister, trap = "x16", "svc #0x80"
	}
	return fmt.Sprintf(`define hidden void @__qk_panic({ ptr, %[1]s } %%message) #1 {
entry:
  %%data = extractvalue { ptr, %[1]s } %%message, 0
  %%length = extractvalue { ptr, %[1]s } %%message, 1
  %%prefix.result = call i64 asm sideeffect "%[5]s", "={x0},{%[4]s},{x0},{x1},{x2},~{memory}"(i64 %[2]d, i64 2, ptr @__qk_panic_prefix, %[1]s 7)
  %%message.result = call i64 asm sideeffect "%[5]s", "={x0},{%[4]s},{x0},{x1},{x2},~{memory}"(i64 %[2]d, i64 2, ptr %%data, %[1]s %%length)
  %%newline.result = call i64 asm sideeffect "%[5]s", "={x0},{%[4]s},{x0},{x1},{x2},~{memory}"(i64 %[2]d, i64 2, ptr @__qk_panic_newline, %[1]s 1)
  %%exit.result = call i64 asm sideeffect "%[5]s", "={x0},{%[4]s},{x0},~{memory}"(i64 %[3]d, i64 101)
  unreachable
}

@__qk_panic_prefix = private constant [7 x i8] c"panic: "
@__qk_panic_newline = private constant [1 x i8] c"\0A"

`, usz, writeNumber, exitNumber, numberRegister, trap)
}

// Windows does not publish a stable direct-system-call ABI. Kernel32 is the
// stable kernel-facing ABI and, unlike the CRT, is present for every process.
func windowsPanicRuntime(usz string) string {
	return fmt.Sprintf(`declare dllimport ptr @GetStdHandle(i32)
declare dllimport i32 @WriteFile(ptr, ptr, i32, ptr, ptr)
declare dllimport void @ExitProcess(i32) noreturn

define hidden void @__qk_panic({ ptr, %[1]s } %%message) #1 {
entry:
  %%data = extractvalue { ptr, %[1]s } %%message, 0
  %%length.native = extractvalue { ptr, %[1]s } %%message, 1
  %%length = trunc %[1]s %%length.native to i32
  %%stderr = call ptr @GetStdHandle(i32 -12)
  %%prefix.result = call i32 @WriteFile(ptr %%stderr, ptr @__qk_panic_prefix, i32 7, ptr null, ptr null)
  %%message.result = call i32 @WriteFile(ptr %%stderr, ptr %%data, i32 %%length, ptr null, ptr null)
  %%newline.result = call i32 @WriteFile(ptr %%stderr, ptr @__qk_panic_newline, i32 1, ptr null, ptr null)
  call void @ExitProcess(i32 101)
  unreachable
}

@__qk_panic_prefix = private constant [7 x i8] c"panic: "
@__qk_panic_newline = private constant [1 x i8] c"\0A"

`, usz)
}
