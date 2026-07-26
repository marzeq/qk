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
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: qkstdlibcheck [-target triple] [-nolibc]")
		os.Exit(2)
	}

	errs := stdlib.CheckEmbedded(comptime.Config{TargetTriple: *targetTriple, NoLibc: *noLibc})
	for _, err := range errs {
		fmt.Fprintln(os.Stderr, err)
	}
	if len(errs) != 0 {
		os.Exit(1)
	}
	fmt.Println("standard library typecheck passed")
}
