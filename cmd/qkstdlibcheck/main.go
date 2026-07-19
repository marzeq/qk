package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/marzeq/qk/stdlib"
)

func main() {
	targetTriple := flag.String("target", "", "target triple used for compile-time selection")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: qkstdlibcheck [-target triple]")
		os.Exit(2)
	}

	errs := stdlib.CheckEmbedded(*targetTriple)
	for _, err := range errs {
		fmt.Fprintln(os.Stderr, err)
	}
	if len(errs) != 0 {
		os.Exit(1)
	}
	fmt.Println("standard library typecheck passed")
}
