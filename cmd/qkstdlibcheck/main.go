package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/marzeq/qk/comptime"
	"github.com/marzeq/qk/stdlib"
)

func main() {
	targetTriple := flag.String("target", "", "target triple used for compile-time selection")
	noLibc := flag.Bool("nolibc", false, "select NoLibc compile-time branches")
	release := flag.Bool("release", false, "select the .Release compile-time mode")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: qkstdlibcheck [-target triple] [-nolibc] [-release]")
		os.Exit(2)
	}

	releaseMode := comptime.ReleaseModeDebug
	if *release {
		releaseMode = comptime.ReleaseModeRelease
	}
	errs := stdlib.CheckEmbedded(comptime.Config{
		TargetTriple: *targetTriple,
		NoLibc:       *noLibc,
		ReleaseMode:  releaseMode,
	})
	for _, err := range errs {
		fmt.Fprintln(os.Stderr, err)
	}
	if len(errs) != 0 {
		os.Exit(1)
	}
	fmt.Println("standard library typecheck passed")
}
