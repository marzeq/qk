package main

import (
	"fmt"
	qktarget "github.com/marzeq/qk/target"
	"strings"
)

// Hosted builds use the platform C runtime for startup and process services.
// Freestanding builds supply their own exported entry and runtime services.
func buildHostedRuntime(targetTriple string, freestanding, executable, initializeStdlibProcessState bool, mainInitializer, userMain string) (string, error) {
	if freestanding {
		return "", nil
	}
	pointerBits, ok := qktarget.PointerBits(qktarget.EffectiveTriple(targetTriple))
	if !ok {
		return "", fmt.Errorf("cannot determine pointer width for target %q", targetTriple)
	}
	usz := fmt.Sprintf("i%d", pointerBits)
	var out strings.Builder
	out.WriteString("; qk hosted startup\n\n")
	if executable && userMain != "" && initializeStdlibProcessState {
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
  %%env.data = alloca [2 x { ptr, %[1]s }], %[1]s %%env.count.value
  %%env.with-data = insertvalue { ptr, %[1]s } zeroinitializer, ptr %%env.data, 0
  %%env.value = insertvalue { ptr, %[1]s } %%env.with-data, %[1]s %%env.count.value, 1
  store { ptr, %[1]s } %%env.value, ptr @__qk_0_3_std2_os_global_env
  %%env.empty = icmp eq %[1]s %%env.count.value, 0
  br i1 %%env.empty, label %%args.setup, label %%env.loop

env.loop:
  %%env.index = phi %[1]s [ 0, %%env.ready ], [ %%env.next, %%env.entry.ready ]
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
  br label %%env.name.loop

env.name.loop:
  %%env.name.index = phi %[1]s [ 0, %%env.strlen.end ], [ %%env.name.next, %%env.name.next-block ]
  %%env.name.done = icmp eq %[1]s %%env.name.index, %%env.entry.length
  br i1 %%env.name.done, label %%env.entry.ready, label %%env.name.byte

env.name.byte:
  %%env.name.address = getelementptr inbounds i8, ptr %%env.entry.data, %[1]s %%env.name.index
  %%env.name.character = load i8, ptr %%env.name.address
  %%env.name.separator = icmp eq i8 %%env.name.character, 61
  br i1 %%env.name.separator, label %%env.entry.ready, label %%env.name.next-block

env.name.next-block:
  %%env.name.next = add nuw %[1]s %%env.name.index, 1
  br label %%env.name.loop

env.entry.ready:
  %%env.value.has-separator = icmp ult %[1]s %%env.name.index, %%env.entry.length
  %%env.value.offset.with-separator = add nuw %[1]s %%env.name.index, 1
  %%env.value.offset = select i1 %%env.value.has-separator, %[1]s %%env.value.offset.with-separator, %[1]s %%env.entry.length
  %%env.value.data = getelementptr inbounds i8, ptr %%env.entry.data, %[1]s %%env.value.offset
  %%env.value.length = sub nuw %[1]s %%env.entry.length, %%env.value.offset
  %%env.name.with-data = insertvalue { ptr, %[1]s } zeroinitializer, ptr %%env.entry.data, 0
  %%env.name = insertvalue { ptr, %[1]s } %%env.name.with-data, %[1]s %%env.name.index, 1
  %%env.value.with-data = insertvalue { ptr, %[1]s } zeroinitializer, ptr %%env.value.data, 0
  %%env.entry.value = insertvalue { ptr, %[1]s } %%env.value.with-data, %[1]s %%env.value.length, 1
  %%env.name.slot = getelementptr inbounds [2 x { ptr, %[1]s }], ptr %%env.data, %[1]s %%env.index, i32 0
  %%env.value.slot = getelementptr inbounds [2 x { ptr, %[1]s }], ptr %%env.data, %[1]s %%env.index, i32 1
  store { ptr, %[1]s } %%env.name, ptr %%env.name.slot
  store { ptr, %[1]s } %%env.entry.value, ptr %%env.value.slot
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
	if executable && userMain != "" && !initializeStdlibProcessState {
		fmt.Fprintf(&out, "declare hidden void @%s()\n", userMain)
		if mainInitializer != "" {
			fmt.Fprintf(&out, "declare hidden void @%s()\n", mainInitializer)
		}
		out.WriteString("define i32 @main(i32 %argc, ptr %argv, ptr %envp) {\nentry:\n")
		if mainInitializer != "" {
			fmt.Fprintf(&out, "  call void @%s()\n", mainInitializer)
		}
		fmt.Fprintf(&out, "  call void @%s()\n  ret i32 0\n}\n", userMain)
	}

	panicRuntime, err := buildPlatformPanicRuntime(targetTriple, usz)
	if err != nil {
		return "", err
	}
	out.WriteString(panicRuntime)
	return out.String(), nil
}
