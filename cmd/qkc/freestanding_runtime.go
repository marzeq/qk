package main

import (
	"fmt"
	"strings"

	qktarget "github.com/marzeq/qk/target"
)

func buildFreestandingRuntime(targetTriple string) (string, error) {
	pointerBits, ok := qktarget.PointerBits(qktarget.EffectiveTriple(targetTriple))
	if !ok {
		return "", fmt.Errorf("cannot determine pointer width for target %q", targetTriple)
	}
	usz := fmt.Sprintf("i%d", pointerBits)

	var out strings.Builder
	fmt.Fprintf(&out, `; qk freestanding runtime

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
`, usz)
	return out.String(), nil
}
