//go:build cgo

package llvmbackend

/*
#cgo LDFLAGS: -lclang-cpp -llldELF -llldCOFF -llldMinGW -llldMachO -llldCommon -lLLVM
*/
import "C"
