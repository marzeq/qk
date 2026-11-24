//go:build !linux || !cgo || qk_static_llvm

package ir

import (
	"fmt"
	"unsafe"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/types"
)

const HostForeignCallsAvailable = false

func callNative(symbol string, signature FunctionSignature, arguments []EvalValue, links []attributes.Link) (EvalValue, error) {
	return EvalValue{}, fmt.Errorf("host C ABI foreign calls are not implemented on this compiler host")
}

func loadNativePointer(pointerType types.Type, address unsafe.Pointer) (EvalValue, error) {
	return EvalValue{}, fmt.Errorf("host foreign pointers are not implemented on this compiler host")
}

func storeNativePointer(pointerType types.Type, address unsafe.Pointer, value EvalValue) error {
	return fmt.Errorf("host foreign pointers are not implemented on this compiler host")
}
