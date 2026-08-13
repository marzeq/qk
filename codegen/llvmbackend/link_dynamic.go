//go:build cgo && !qk_static_llvm

package llvmbackend

/*
#cgo !windows LDFLAGS: -lLLVM
#cgo windows LDFLAGS: -lLLVM-22 -lz -lzstd
#cgo linux LDFLAGS: -lz -lzstd
*/
import "C"
