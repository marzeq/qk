//go:build cgo

package llvmbackend

/*
#cgo !windows LDFLAGS: -lLLVM
#cgo windows LDFLAGS: -lLLVM-22 -lz -lzstd
#cgo linux LDFLAGS: -lz -lzstd
*/
import "C"
