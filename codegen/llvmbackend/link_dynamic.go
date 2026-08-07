//go:build cgo

package llvmbackend

/*
#cgo LDFLAGS: -lclang-cpp -llldELF -llldCOFF -llldMinGW -llldMachO -llldWasm -llldCommon -lLLVM
*/
import "C"
