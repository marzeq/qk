//go:build cgo && !llvm_static

package llvmbackend

/*
#cgo LDFLAGS: -lclang-cpp -llldELF -llldCOFF -llldMinGW -llldMachO -llldCommon -lLLVM
#cgo !darwin LDFLAGS: -lstdc++
#cgo darwin LDFLAGS: -lc++
*/
import "C"
