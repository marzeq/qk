//go:build cgo

package llvmbackend

/*
#cgo LDFLAGS: -lclang-cpp -llldELF -llldCOFF -llldMinGW -llldMachO -llldWasm -llldCommon
#cgo !windows LDFLAGS: -lLLVM
#cgo windows LDFLAGS: -lLLVM-22 -lz -lzstd
#cgo linux LDFLAGS: -lz -lzstd
*/
import "C"
