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
	release := flag.Bool("release", false, "select the .Release compile-time mode")
	libraryRoot := flag.String("libs", "libs", "library source root")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: qkstdlibcheck [-target triple] [-release] [-libs directory]")
		os.Exit(2)
	}

	releaseMode := comptime.ReleaseModeDebug
	if *release {
		releaseMode = comptime.ReleaseModeRelease
	}
	errs := stdlib.CheckRoot(*libraryRoot, comptime.Config{
		TargetTriple:   *targetTriple,
		CheckingStdlib: true,
		ReleaseMode:    releaseMode,
	})
	for _, err := range errs {
		fmt.Fprintln(os.Stderr, err)
	}
	if len(errs) != 0 {
		os.Exit(1)
	}
	fmt.Println("standard library typecheck passed")
}
