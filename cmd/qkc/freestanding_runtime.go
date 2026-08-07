package main

import (
	"fmt"
	"strings"

	qktarget "github.com/marzeq/qk/target"
)

func buildFreestandingRuntime(
	targetTriple string,
	linksLibc, executable bool,
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
	libcFreeHosted := !linksLibc && executable && linuxX8664
	if libcFreeHosted && linuxX8664 {
		if executable && userMain != "" {
			initializerDeclaration := ""
			initializerCall := ""
			if mainInitializer != "" {
				initializerDeclaration = fmt.Sprintf("declare hidden void @%s()\n", mainInitializer)
				initializerCall = fmt.Sprintf("  call void @%s()\n", mainInitializer)
			}
			fmt.Fprintf(&out, `declare hidden void @%[2]s()
%[3]s
@__qk_0_3_std2_os_global_args = external hidden global { ptr, %[1]s }
@__qk_0_3_std2_os_global_env = external hidden global { ptr, %[1]s }

module asm ".text"
module asm ".globl _start"
module asm ".type _start,@function"
module asm "_start:"
module asm "movq %%rsp, %%rdi"
module asm "andq $-16, %%rsp"
module asm "subq $8, %%rsp"
module asm "jmp __qk_start"
module asm ".size _start, .-_start"

define hidden void @__qk_start(ptr %%stack) noreturn nounwind {
entry:
	%%argc = load %[1]s, ptr %%stack
	%%argv = getelementptr inbounds %[1]s, ptr %%stack, %[1]s 1
	%%envp.offset = add nuw %[1]s %%argc, 1
	%%envp = getelementptr inbounds ptr, ptr %%argv, %[1]s %%envp.offset
	br label %%env.count

env.count:
	%%env.count.index = phi %[1]s [ 0, %%entry ], [ %%env.count.next, %%env.count.next-block ]
	%%env.count.slot = getelementptr inbounds ptr, ptr %%envp, %[1]s %%env.count.index
	%%env.count.data = load ptr, ptr %%env.count.slot
	%%env.count.done = icmp eq ptr %%env.count.data, null
	br i1 %%env.count.done, label %%env.ready, label %%env.count.next-block

env.count.next-block:
	%%env.count.next = add nuw %[1]s %%env.count.index, 1
	br label %%env.count

env.ready:
	%%env.count.value = phi %[1]s [ %%env.count.index, %%env.count ]
	%%env.data = alloca { ptr, %[1]s }, %[1]s %%env.count.value
	%%env.with-data = insertvalue { ptr, %[1]s } zeroinitializer, ptr %%env.data, 0
	%%env.value = insertvalue { ptr, %[1]s } %%env.with-data, %[1]s %%env.count.value, 1
	store { ptr, %[1]s } %%env.value, ptr @__qk_0_3_std2_os_global_env
	%%env.empty = icmp eq %[1]s %%env.count.value, 0
	br i1 %%env.empty, label %%args.setup, label %%env.loop

env.loop:
	%%env.index = phi %[1]s [ 0, %%env.ready ], [ %%env.next, %%env.strlen.end ]
	%%envp.slot = getelementptr inbounds ptr, ptr %%envp, %[1]s %%env.index
	%%env.entry.data = load ptr, ptr %%envp.slot
	br label %%env.strlen.loop

env.strlen.loop:
	%%env.strlen.index = phi %[1]s [ 0, %%env.loop ], [ %%env.strlen.next, %%env.strlen.loop ]
	%%env.strlen.address = getelementptr inbounds i8, ptr %%env.entry.data, %[1]s %%env.strlen.index
	%%env.strlen.byte = load i8, ptr %%env.strlen.address
	%%env.strlen.done = icmp eq i8 %%env.strlen.byte, 0
	%%env.strlen.next = add nuw %[1]s %%env.strlen.index, 1
	br i1 %%env.strlen.done, label %%env.strlen.end, label %%env.strlen.loop

env.strlen.end:
	%%env.entry.length = phi %[1]s [ %%env.strlen.index, %%env.strlen.loop ]
	%%env.entry.with-data = insertvalue { ptr, %[1]s } zeroinitializer, ptr %%env.entry.data, 0
	%%env.entry = insertvalue { ptr, %[1]s } %%env.entry.with-data, %[1]s %%env.entry.length, 1
	%%env.slot = getelementptr inbounds { ptr, %[1]s }, ptr %%env.data, %[1]s %%env.index
	store { ptr, %[1]s } %%env.entry, ptr %%env.slot
	%%env.next = add nuw %[1]s %%env.index, 1
	%%env.finished = icmp eq %[1]s %%env.next, %%env.count.value
	br i1 %%env.finished, label %%args.setup, label %%env.loop

args.setup:
	%%args.data = alloca { ptr, %[1]s }, %[1]s %%argc
	%%args.with-data = insertvalue { ptr, %[1]s } zeroinitializer, ptr %%args.data, 0
	%%args = insertvalue { ptr, %[1]s } %%args.with-data, %[1]s %%argc, 1
	store { ptr, %[1]s } %%args, ptr @__qk_0_3_std2_os_global_args
	%%args.empty = icmp eq %[1]s %%argc, 0
	br i1 %%args.empty, label %%run, label %%args.loop

args.loop:
	%%arg.index = phi %[1]s [ 0, %%args.setup ], [ %%arg.next, %%strlen.end ]
	%%argv.slot = getelementptr inbounds ptr, ptr %%argv, %[1]s %%arg.index
	%%arg.data = load ptr, ptr %%argv.slot
	br label %%strlen.loop

strlen.loop:
	%%strlen.index = phi %[1]s [ 0, %%args.loop ], [ %%strlen.next, %%strlen.loop ]
	%%strlen.address = getelementptr inbounds i8, ptr %%arg.data, %[1]s %%strlen.index
	%%strlen.byte = load i8, ptr %%strlen.address
	%%strlen.done = icmp eq i8 %%strlen.byte, 0
	%%strlen.next = add nuw %[1]s %%strlen.index, 1
	br i1 %%strlen.done, label %%strlen.end, label %%strlen.loop

strlen.end:
	%%arg.length = phi %[1]s [ %%strlen.index, %%strlen.loop ]
	%%arg.with-data = insertvalue { ptr, %[1]s } zeroinitializer, ptr %%arg.data, 0
	%%arg = insertvalue { ptr, %[1]s } %%arg.with-data, %[1]s %%arg.length, 1
	%%arg.slot = getelementptr inbounds { ptr, %[1]s }, ptr %%args.data, %[1]s %%arg.index
	store { ptr, %[1]s } %%arg, ptr %%arg.slot
	%%arg.next = add nuw %[1]s %%arg.index, 1
	%%args.finished = icmp eq %[1]s %%arg.next, %%argc
	br i1 %%args.finished, label %%run, label %%args.loop

run:
%[4]s  call void @%[2]s()
  %%exit = call i64 asm sideeffect "syscall", "={rax},{rax},{rdi},~{rcx},~{r11},~{memory}"(i64 60, i32 0)
  unreachable
}

`, usz, userMain, initializerDeclaration, initializerCall)
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
			fmt.Fprintf(&out, `
@__qk_0_3_std2_os_global_args = external hidden global { ptr, %s }
@__qk_0_3_std2_os_global_env = external hidden global { ptr, %s }

define i32 @main(i32 %%argc, ptr %%argv, ptr %%envp) {
entry:
`, usz, usz)
			argc := "%argc"
			if pointerBits != 32 {
				fmt.Fprintf(&out, "  %%argc.usz = zext i32 %%argc to %s\n", usz)
				argc = "%argc.usz"
			}
			fmt.Fprintf(&out, `  br label %%env.count

env.count:
  %%env.count.index = phi %[1]s [ 0, %%entry ], [ %%env.count.next, %%env.count.next-block ]
  %%env.count.slot = getelementptr inbounds ptr, ptr %%envp, %[1]s %%env.count.index
  %%env.count.data = load ptr, ptr %%env.count.slot
  %%env.count.done = icmp eq ptr %%env.count.data, null
  br i1 %%env.count.done, label %%env.ready, label %%env.count.next-block

env.count.next-block:
  %%env.count.next = add nuw %[1]s %%env.count.index, 1
  br label %%env.count

env.ready:
  %%env.count.value = phi %[1]s [ %%env.count.index, %%env.count ]
  %%env.data = alloca { ptr, %[1]s }, %[1]s %%env.count.value
  %%env.with-data = insertvalue { ptr, %[1]s } zeroinitializer, ptr %%env.data, 0
  %%env.value = insertvalue { ptr, %[1]s } %%env.with-data, %[1]s %%env.count.value, 1
  store { ptr, %[1]s } %%env.value, ptr @__qk_0_3_std2_os_global_env
  %%env.empty = icmp eq %[1]s %%env.count.value, 0
  br i1 %%env.empty, label %%args.setup, label %%env.loop

env.loop:
  %%env.index = phi %[1]s [ 0, %%env.ready ], [ %%env.next, %%env.strlen.end ]
  %%envp.slot = getelementptr inbounds ptr, ptr %%envp, %[1]s %%env.index
  %%env.entry.data = load ptr, ptr %%envp.slot
  br label %%env.strlen.loop

env.strlen.loop:
  %%env.strlen.index = phi %[1]s [ 0, %%env.loop ], [ %%env.strlen.next, %%env.strlen.loop ]
  %%env.strlen.address = getelementptr inbounds i8, ptr %%env.entry.data, %[1]s %%env.strlen.index
  %%env.strlen.byte = load i8, ptr %%env.strlen.address
  %%env.strlen.done = icmp eq i8 %%env.strlen.byte, 0
  %%env.strlen.next = add nuw %[1]s %%env.strlen.index, 1
  br i1 %%env.strlen.done, label %%env.strlen.end, label %%env.strlen.loop

env.strlen.end:
  %%env.entry.length = phi %[1]s [ %%env.strlen.index, %%env.strlen.loop ]
  %%env.entry.with-data = insertvalue { ptr, %[1]s } zeroinitializer, ptr %%env.entry.data, 0
  %%env.entry = insertvalue { ptr, %[1]s } %%env.entry.with-data, %[1]s %%env.entry.length, 1
  %%env.slot = getelementptr inbounds { ptr, %[1]s }, ptr %%env.data, %[1]s %%env.index
  store { ptr, %[1]s } %%env.entry, ptr %%env.slot
  %%env.next = add nuw %[1]s %%env.index, 1
  %%env.finished = icmp eq %[1]s %%env.next, %%env.count.value
  br i1 %%env.finished, label %%args.setup, label %%env.loop

args.setup:
  %%args.data = alloca { ptr, %[1]s }, %[1]s %[2]s
  %%args.with-data = insertvalue { ptr, %[1]s } zeroinitializer, ptr %%args.data, 0
  %%args = insertvalue { ptr, %[1]s } %%args.with-data, %[1]s %[2]s, 1
  store { ptr, %[1]s } %%args, ptr @__qk_0_3_std2_os_global_args
  %%args.empty = icmp eq i32 %%argc, 0
  br i1 %%args.empty, label %%run, label %%args.loop

args.loop:
  %%arg.index = phi %[1]s [ 0, %%args.setup ], [ %%arg.next, %%strlen.end ]
  %%argv.slot = getelementptr inbounds ptr, ptr %%argv, %[1]s %%arg.index
  %%arg.data = load ptr, ptr %%argv.slot
  br label %%strlen.loop

strlen.loop:
  %%strlen.index = phi %[1]s [ 0, %%args.loop ], [ %%strlen.next, %%strlen.loop ]
  %%strlen.address = getelementptr inbounds i8, ptr %%arg.data, %[1]s %%strlen.index
  %%strlen.byte = load i8, ptr %%strlen.address
  %%strlen.done = icmp eq i8 %%strlen.byte, 0
  %%strlen.next = add nuw %[1]s %%strlen.index, 1
  br i1 %%strlen.done, label %%strlen.end, label %%strlen.loop

strlen.end:
  %%arg.length = phi %[1]s [ %%strlen.index, %%strlen.loop ]
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
		}
		panicRuntime, err := buildPlatformPanicRuntime(target, usz)
		if err != nil {
			return "", err
		}
		out.WriteString(panicRuntime)
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

define hidden %[1]s @strlen(ptr %%text) #0 {
entry:
  br label %%loop

loop:
  %%length = phi %[1]s [ 0, %%entry ], [ %%next, %%loop ]
  %%address = getelementptr inbounds i8, ptr %%text, %[1]s %%length
  %%byte = load i8, ptr %%address
  %%finished = icmp eq i8 %%byte, 0
  %%next = add nuw %[1]s %%length, 1
  br i1 %%finished, label %%done, label %%loop

done:
  ret %[1]s %%length
}

attributes #0 = { noinline nounwind optnone nobuiltin }
attributes #1 = { cold noinline noreturn nounwind }
`, usz)
	return out.String(), nil
}
