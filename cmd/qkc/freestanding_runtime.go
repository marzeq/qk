package main

import (
	"fmt"
	"strings"

	qktarget "github.com/marzeq/qk/target"
)

func buildFreestandingRuntime(
	targetTriple string,
	noLibc, noStdlib, executable bool,
	mainInitializer, userMain string,
) (string, error) {
	pointerBits, ok := qktarget.PointerBits(qktarget.EffectiveTriple(targetTriple))
	if !ok {
		return "", fmt.Errorf("cannot determine pointer width for target %q", targetTriple)
	}
	usz := fmt.Sprintf("i%d", pointerBits)

	var out strings.Builder
	out.WriteString("; qk thin runtime\n\n")
	target := strings.ToLower(qktarget.EffectiveTriple(targetTriple))
	linuxX8664 := strings.Contains(target, "linux") && (qktarget.Arch(targetTriple) == "x86_64" || qktarget.Arch(targetTriple) == "amd64")
	if noLibc && executable && !linuxX8664 {
		return "", fmt.Errorf("freestanding -nolibc executables are currently supported only for Linux x86-64, not target %q", target)
	}
	if noLibc && linuxX8664 {
		if executable && userMain != "" {
			initializerDeclaration := ""
			initializerCall := ""
			if mainInitializer != "" {
				initializerDeclaration = fmt.Sprintf("declare hidden void @%s()\n", mainInitializer)
				initializerCall = fmt.Sprintf("  call void @%s()\n", mainInitializer)
			}
			fmt.Fprintf(&out, `declare hidden void @%s()
%s
define void @_start() noreturn nounwind {
entry:
%s  call void @%s()
  %%exit = call i64 asm sideeffect "syscall", "={rax},{rax},{rdi},~{rcx},~{r11},~{memory}"(i64 60, i32 0)
  unreachable
}

`, userMain, initializerDeclaration, initializerCall, userMain)
		}
		fmt.Fprintf(&out, `define hidden void @__qk_panic({ ptr, %[1]s } %%message) #1 {
entry:
  %%data = extractvalue { ptr, %[1]s } %%message, 0
  %%length = extractvalue { ptr, %[1]s } %%message, 1
  %%prefix = call i64 asm sideeffect "syscall", "={rax},{rax},{rdi},{rsi},{rdx},~{rcx},~{r11},~{memory}"(i64 1, i64 2, ptr @__qk_panic_prefix, %[1]s 7)
  %%written = call i64 asm sideeffect "syscall", "={rax},{rax},{rdi},{rsi},{rdx},~{rcx},~{r11},~{memory}"(i64 1, i64 2, ptr %%data, %[1]s %%length)
  %%newline = call i64 asm sideeffect "syscall", "={rax},{rax},{rdi},{rsi},{rdx},~{rcx},~{r11},~{memory}"(i64 1, i64 2, ptr @__qk_panic_newline, %[1]s 1)
  %%exit = call i64 asm sideeffect "syscall", "={rax},{rax},{rdi},~{rcx},~{r11},~{memory}"(i64 60, i64 101)
  unreachable
}

@__qk_panic_prefix = private constant [7 x i8] c"panic: "
@__qk_panic_newline = private constant [1 x i8] c"\0A"

`, usz)
	} else {
		if executable && userMain != "" {
			fmt.Fprintf(&out, "declare hidden void @%s()\n", userMain)
			initializerCall := ""
			if mainInitializer != "" {
				fmt.Fprintf(&out, "declare hidden void @%s()\n", mainInitializer)
				initializerCall = fmt.Sprintf("  call void @%s()\n", mainInitializer)
			}
			if !noStdlib {
				fmt.Fprintf(&out, `declare %s @strlen(ptr)
@__qk_std_global_Args = external hidden global { ptr, %s }

define i32 @main(i32 %%argc, ptr %%argv) {
entry:
`, usz, usz)
				argc := "%argc"
				if pointerBits != 32 {
					fmt.Fprintf(&out, "  %%argc.usz = zext i32 %%argc to %s\n", usz)
					argc = "%argc.usz"
				}
				fmt.Fprintf(&out, `  %%args.data = alloca { ptr, %[1]s }, %[1]s %[2]s
  %%args.with-data = insertvalue { ptr, %[1]s } zeroinitializer, ptr %%args.data, 0
  %%args = insertvalue { ptr, %[1]s } %%args.with-data, %[1]s %[2]s, 1
  store { ptr, %[1]s } %%args, ptr @__qk_std_global_Args
  %%args.empty = icmp eq i32 %%argc, 0
  br i1 %%args.empty, label %%run, label %%args.loop

args.loop:
  %%arg.index = phi %[1]s [ 0, %%entry ], [ %%arg.next, %%args.loop ]
  %%argv.slot = getelementptr inbounds ptr, ptr %%argv, %[1]s %%arg.index
  %%arg.data = load ptr, ptr %%argv.slot
  %%arg.length = call %[1]s @strlen(ptr %%arg.data)
  %%arg.with-data = insertvalue { ptr, %[1]s } zeroinitializer, ptr %%arg.data, 0
  %%arg = insertvalue { ptr, %[1]s } %%arg.with-data, %[1]s %%arg.length, 1
  %%arg.slot = getelementptr inbounds { ptr, %[1]s }, ptr %%args.data, %[1]s %%arg.index
  store { ptr, %[1]s } %%arg, ptr %%arg.slot
  %%arg.next = add nuw %[1]s %%arg.index, 1
  %%args.finished = icmp eq %[1]s %%arg.next, %[2]s
  br i1 %%args.finished, label %%run, label %%args.loop

run:
%[4]s  call void @%[3]s()
  ret i32 0
}

`, usz, argc, userMain, initializerCall)
			} else {
				fmt.Fprintf(&out, `
define i32 @main(i32 %%argc, ptr %%argv) {
entry:
%s  call void @%s()
  ret i32 0
}

`, initializerCall, userMain)
			}
		}
		fmt.Fprintf(&out, `declare %[1]s @write(i32, ptr, %[1]s)
declare void @abort() noreturn

define hidden void @__qk_panic({ ptr, %[1]s } %%message) #1 {
entry:
  %%data = extractvalue { ptr, %[1]s } %%message, 0
  %%length = extractvalue { ptr, %[1]s } %%message, 1
  %%prefix.result = call %[1]s @write(i32 2, ptr @__qk_panic_prefix, %[1]s 7)
  %%empty = icmp eq %[1]s %%length, 0
  br i1 %%empty, label %%newline, label %%write

write:
  %%remaining = phi %[1]s [ %%length, %%entry ], [ %%next.remaining, %%write.continue ]
  %%cursor = phi ptr [ %%data, %%entry ], [ %%next.cursor, %%write.continue ]
  %%written = call %[1]s @write(i32 2, ptr %%cursor, %[1]s %%remaining)
  %%failed = icmp sle %[1]s %%written, 0
  br i1 %%failed, label %%terminate, label %%write.continue

write.continue:
  %%next.remaining = sub %[1]s %%remaining, %%written
  %%next.cursor = getelementptr i8, ptr %%cursor, %[1]s %%written
  %%finished = icmp eq %[1]s %%next.remaining, 0
  br i1 %%finished, label %%newline, label %%write

newline:
  %%newline.result = call %[1]s @write(i32 2, ptr @__qk_panic_newline, %[1]s 1)
  br label %%terminate

terminate:
  call void @abort()
  unreachable
}

@__qk_panic_prefix = private constant [7 x i8] c"panic: "
@__qk_panic_newline = private constant [1 x i8] c"\0A"

`, usz)
	}
	if !noLibc {
		out.WriteString("attributes #1 = { cold noinline noreturn nounwind }\n")
		return out.String(), nil
	}
	fmt.Fprintf(&out, `

define hidden ptr @memset(ptr %%dest, i32 %%value, %[1]s %%length) #0 {
entry:
  %%byte = trunc i32 %%value to i8
  %%empty = icmp eq %[1]s %%length, 0
  br i1 %%empty, label %%done, label %%loop

loop:
  %%index = phi %[1]s [ 0, %%entry ], [ %%next, %%loop ]
  %%address = getelementptr inbounds i8, ptr %%dest, %[1]s %%index
  store i8 %%byte, ptr %%address
  %%next = add nuw %[1]s %%index, 1
  %%finished = icmp eq %[1]s %%next, %%length
  br i1 %%finished, label %%done, label %%loop

done:
  ret ptr %%dest
}

define hidden ptr @memcpy(ptr %%dest, ptr %%src, %[1]s %%length) #0 {
entry:
  %%empty = icmp eq %[1]s %%length, 0
  br i1 %%empty, label %%done, label %%loop

loop:
  %%index = phi %[1]s [ 0, %%entry ], [ %%next, %%loop ]
  %%source = getelementptr inbounds i8, ptr %%src, %[1]s %%index
  %%byte = load i8, ptr %%source
  %%target = getelementptr inbounds i8, ptr %%dest, %[1]s %%index
  store i8 %%byte, ptr %%target
  %%next = add nuw %[1]s %%index, 1
  %%finished = icmp eq %[1]s %%next, %%length
  br i1 %%finished, label %%done, label %%loop

done:
  ret ptr %%dest
}

define hidden ptr @memmove(ptr %%dest, ptr %%src, %[1]s %%length) #0 {
entry:
  %%empty = icmp eq %[1]s %%length, 0
  br i1 %%empty, label %%done, label %%direction

direction:
  %%source.end = getelementptr i8, ptr %%src, %[1]s %%length
  %%before = icmp ult ptr %%dest, %%src
  %%after = icmp uge ptr %%dest, %%source.end
  %%forward = or i1 %%before, %%after
  br i1 %%forward, label %%forward.loop, label %%backward.loop

forward.loop:
  %%forward.index = phi %[1]s [ 0, %%direction ], [ %%forward.next, %%forward.loop ]
  %%forward.source = getelementptr inbounds i8, ptr %%src, %[1]s %%forward.index
  %%forward.byte = load i8, ptr %%forward.source
  %%forward.target = getelementptr inbounds i8, ptr %%dest, %[1]s %%forward.index
  store i8 %%forward.byte, ptr %%forward.target
  %%forward.next = add nuw %[1]s %%forward.index, 1
  %%forward.finished = icmp eq %[1]s %%forward.next, %%length
  br i1 %%forward.finished, label %%done, label %%forward.loop

backward.loop:
  %%backward.index = phi %[1]s [ %%length, %%direction ], [ %%backward.next, %%backward.loop ]
  %%backward.next = sub nuw %[1]s %%backward.index, 1
  %%backward.source = getelementptr inbounds i8, ptr %%src, %[1]s %%backward.next
  %%backward.byte = load i8, ptr %%backward.source
  %%backward.target = getelementptr inbounds i8, ptr %%dest, %[1]s %%backward.next
  store i8 %%backward.byte, ptr %%backward.target
  %%backward.finished = icmp eq %[1]s %%backward.next, 0
  br i1 %%backward.finished, label %%done, label %%backward.loop

done:
  ret ptr %%dest
}

attributes #0 = { noinline nounwind optnone nobuiltin }
attributes #1 = { cold noinline noreturn nounwind }
`, usz)
	return out.String(), nil
}
