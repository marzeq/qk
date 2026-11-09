package main

import "fmt"

// Panic reporting in hosted builds uses the target C runtime.
func buildPlatformPanicRuntime(target, usz string) (string, error) {
	if targetIsWebAssembly(target) {
		return fmt.Sprintf(`declare void @llvm.trap() noreturn

define hidden void @__qk_panic({ ptr, %s } %%message) noreturn {
entry:
  call void @llvm.trap()
  unreachable
}

define hidden void @__qk_assert(i1 %%condition, { ptr, %[1]s } %%message) {
entry:
  br i1 %%condition, label %%passed, label %%failed
failed:
  call void @llvm.trap()
  unreachable
passed:
  ret void
}
`, usz), nil
	}
	writer := "write"
	if targetIsWindows(target) {
		writer = "_write"
	}
	lengthConversion := fmt.Sprintf("  %%length = trunc %s %%length.native to i32", usz)
	if usz == "i32" {
		lengthConversion = "  %length = add i32 %length.native, 0"
	}
	return fmt.Sprintf(`declare i32 @%[1]s(i32, ptr, i32)
declare void @exit(i32) noreturn

@__qk_panic_prefix = private constant [7 x i8] c"panic: "
@__qk_assert_prefix = private constant [18 x i8] c"assertion failed: "
@__qk_panic_newline = private constant [1 x i8] c"\0A"

define hidden void @__qk_panic({ ptr, %[2]s } %%message) noreturn {
entry:
  %%data = extractvalue { ptr, %[2]s } %%message, 0
  %%length.native = extractvalue { ptr, %[2]s } %%message, 1
%[3]s
  %%prefix = call i32 @%[1]s(i32 2, ptr @__qk_panic_prefix, i32 7)
  %%written = call i32 @%[1]s(i32 2, ptr %%data, i32 %%length)
  %%newline = call i32 @%[1]s(i32 2, ptr @__qk_panic_newline, i32 1)
  call void @exit(i32 101)
  unreachable
}

define hidden void @__qk_assert(i1 %%condition, { ptr, %[2]s } %%message) {
entry:
  br i1 %%condition, label %%passed, label %%failed
failed:
  %%data = extractvalue { ptr, %[2]s } %%message, 0
  %%length.native = extractvalue { ptr, %[2]s } %%message, 1
%[3]s
  %%prefix = call i32 @%[1]s(i32 2, ptr @__qk_assert_prefix, i32 18)
  %%written = call i32 @%[1]s(i32 2, ptr %%data, i32 %%length)
  %%newline = call i32 @%[1]s(i32 2, ptr @__qk_panic_newline, i32 1)
  call void @exit(i32 101)
  unreachable
passed:
  ret void
}
`, writer, usz, lengthConversion), nil
}
