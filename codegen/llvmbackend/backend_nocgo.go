//go:build !cgo

package llvmbackend

import "fmt"

type OutputKind int

const (
	OutputObject OutputKind = iota
	OutputAssembly
)

type Options struct {
	TargetTriple    string
	CPU             string
	Features        string
	TargetABI       string
	OptLevel        string
	RelocationModel string
	CodeModel       string
	Verbose         bool
}

func Compile(string, string, string, OutputKind, Options) error {
	return fmt.Errorf("qkc was built without cgo; native code generation is unavailable")
}

func Link([]string, bool) error {
	return fmt.Errorf("qkc was built without cgo; native linking is unavailable")
}
