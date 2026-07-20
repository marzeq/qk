package llvmbackend

/*
#cgo CXXFLAGS: -std=c++17
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

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

func Compile(input, output, optimizedIR string, kind OutputKind, options Options) error {
	inputCString := C.CString(input)
	defer C.free(unsafe.Pointer(inputCString))
	outputCString := C.CString(output)
	defer C.free(unsafe.Pointer(outputCString))
	optimizedIRCString := C.CString(optimizedIR)
	defer C.free(unsafe.Pointer(optimizedIRCString))
	targetCString := C.CString(options.TargetTriple)
	defer C.free(unsafe.Pointer(targetCString))
	cpuCString := C.CString(options.CPU)
	defer C.free(unsafe.Pointer(cpuCString))
	featuresCString := C.CString(options.Features)
	defer C.free(unsafe.Pointer(featuresCString))
	abiCString := C.CString(options.TargetABI)
	defer C.free(unsafe.Pointer(abiCString))
	optCString := C.CString(options.OptLevel)
	defer C.free(unsafe.Pointer(optCString))
	relocationCString := C.CString(options.RelocationModel)
	defer C.free(unsafe.Pointer(relocationCString))
	codeModelCString := C.CString(options.CodeModel)
	defer C.free(unsafe.Pointer(codeModelCString))

	var errorMessage *C.char
	result := C.qk_compile_llvm(
		inputCString,
		C.size_t(len(input)),
		outputCString,
		optimizedIRCString,
		targetCString,
		cpuCString,
		featuresCString,
		abiCString,
		optCString,
		relocationCString,
		codeModelCString,
		C.int(kind),
		C.int(boolToInt(options.Verbose)),
		&errorMessage,
	)
	if errorMessage != nil {
		defer C.qk_dispose_error(errorMessage)
	}
	if result != 0 {
		if errorMessage == nil {
			return fmt.Errorf("libLLVM compilation failed")
		}
		return fmt.Errorf("libLLVM compilation failed: %s", C.GoString(errorMessage))
	}
	return nil
}

func Link(arguments []string, verbose bool) error {
	cArguments := make([]*C.char, len(arguments))
	for index, argument := range arguments {
		cArguments[index] = C.CString(argument)
		defer C.free(unsafe.Pointer(cArguments[index]))
	}

	var argumentsPointer **C.char
	if len(cArguments) > 0 {
		argumentsPointer = &cArguments[0]
	}
	var errorMessage *C.char
	result := C.qk_link_lld(
		argumentsPointer,
		C.size_t(len(cArguments)),
		C.int(boolToInt(verbose)),
		&errorMessage,
	)
	if errorMessage != nil {
		defer C.qk_dispose_error(errorMessage)
	}
	if result != 0 {
		if errorMessage == nil {
			return fmt.Errorf("libLLD linking failed")
		}
		return fmt.Errorf("libLLD linking failed: %s", C.GoString(errorMessage))
	}
	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
