package main

import (
	"fmt"
	"os"
)

func check(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func checkErrs(errs []error) {
	if len(errs) > 0 {
		for _, err := range errs {
			fmt.Println(err)
		}
		os.Exit(1)
	}
}

func fatal(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
	os.Exit(1)
}
