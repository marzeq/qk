//go:build (linux || darwin || windows) && cgo && !qk_static_llvm

package ir

/*
#cgo pkg-config: libffi
#include <ffi.h>
#include <stdint.h>
#include <stdlib.h>

static ffi_type *qk_make_struct_type(ffi_type **elements) {
    ffi_type *result = calloc(1, sizeof(ffi_type));
    if (!result) return NULL;
    result->type = FFI_TYPE_STRUCT;
    result->elements = elements;
    return result;
}
static int qk_struct_offsets(ffi_type *type, size_t *offsets) {
    return (int)ffi_get_struct_offsets(FFI_DEFAULT_ABI, type, offsets);
}

static int qk_call_foreign(void *symbol, ffi_type *result_type, ffi_type **argument_types,
                           void **argument_values, unsigned argument_count, unsigned fixed_count,
                           void *result) {
    ffi_cif cif;
    ffi_status status = fixed_count == argument_count
        ? ffi_prep_cif(&cif, FFI_DEFAULT_ABI, argument_count, result_type, argument_types)
        : ffi_prep_cif_var(&cif, FFI_DEFAULT_ABI, fixed_count, argument_count,
                           result_type, argument_types);
    if (status != FFI_OK) return (int)status;
    ffi_call(&cif, FFI_FN(symbol), result, argument_values);
    return 0;
}
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"unsafe"

	"github.com/marzeq/qk/attributes"
	qktarget "github.com/marzeq/qk/target"
	"github.com/marzeq/qk/types"
)

const HostForeignCallsAvailable = true

func callNative(symbol string, signature FunctionSignature, arguments []EvalValue, links []attributes.Link) (EvalValue, error) {
	if len(arguments) < len(signature.ParamTypes) || !signature.Variadic && len(arguments) != len(signature.ParamTypes) {
		return EvalValue{}, fmt.Errorf("foreign call %q has %d arguments for %d fixed parameters", symbol, len(arguments), len(signature.ParamTypes))
	}
	address, err := foreignSymbol(symbol, links)
	if err != nil {
		return EvalValue{}, err
	}
	var allocations []unsafe.Pointer
	defer func() {
		for _, allocation := range allocations {
			C.free(allocation)
		}
	}()
	returnType, returnSize, err := nativeFFIType(signature.ReturnType, &allocations)
	if err != nil {
		return EvalValue{}, fmt.Errorf("foreign return type: %w", err)
	}
	argumentCount := len(arguments)
	typeMemory := C.malloc(C.size_t(max(argumentCount, 1)) * C.size_t(unsafe.Sizeof(uintptr(0))))
	valueMemory := C.malloc(C.size_t(max(argumentCount, 1)) * C.size_t(unsafe.Sizeof(uintptr(0))))
	if typeMemory == nil || valueMemory == nil {
		C.free(typeMemory)
		C.free(valueMemory)
		return EvalValue{}, fmt.Errorf("could not allocate foreign call arguments")
	}
	defer C.free(typeMemory)
	defer C.free(valueMemory)
	typePointers := unsafe.Slice((**C.ffi_type)(typeMemory), argumentCount)
	valuePointers := unsafe.Slice((*unsafe.Pointer)(valueMemory), argumentCount)
	for index, argument := range arguments {
		parameterType := argument.Type
		if index < len(signature.ParamTypes) {
			parameterType = signature.ParamTypes[index]
		}
		ffiType, size, err := nativeFFIType(parameterType, &allocations)
		if err != nil {
			return EvalValue{}, fmt.Errorf("foreign argument %d: %w", index+1, err)
		}
		typePointers[index] = ffiType
		buffer := C.calloc(1, C.size_t(max(size, 1)))
		if buffer == nil {
			return EvalValue{}, fmt.Errorf("could not allocate foreign argument %d", index+1)
		}
		allocations = append(allocations, buffer)
		if err := writeNativeValue(parameterType, argument, buffer, &allocations); err != nil {
			return EvalValue{}, fmt.Errorf("foreign argument %d: %w", index+1, err)
		}
		valuePointers[index] = buffer
	}
	resultMemory := C.calloc(1, C.size_t(max(returnSize, 16)))
	if resultMemory == nil {
		return EvalValue{}, fmt.Errorf("could not allocate foreign result")
	}
	defer C.free(resultMemory)
	if status := C.qk_call_foreign(address, returnType, (**C.ffi_type)(typeMemory),
		(*unsafe.Pointer)(valueMemory), C.uint(argumentCount), C.uint(len(signature.ParamTypes)), resultMemory); status != 0 {
		return EvalValue{}, fmt.Errorf("libffi could not prepare foreign call (status %d)", int(status))
	}
	for index, argument := range arguments {
		parameterType := argument.Type
		if index < len(signature.ParamTypes) {
			parameterType = signature.ParamTypes[index]
		}
		pointerType, ok := types.Underlying(parameterType).(types.PointerType)
		if !ok || !pointerType.Mutable || argument.pointer == nil {
			continue
		}
		buffer := *(*unsafe.Pointer)(valuePointers[index])
		if err := copyBackNativePointer(argument.pointer, buffer); err != nil {
			return EvalValue{}, fmt.Errorf("foreign argument %d: %w", index+1, err)
		}
	}
	result, err := readNativeValue(signature.ReturnType, resultMemory)
	if err != nil {
		return EvalValue{}, err
	}
	// A C function may return an argument or a byte inside one (for example
	// memset or strchr). The argument is a temporary C copy of evaluator
	// storage, so rebind it before freeing the copy.
	if result.nativePointer != nil {
		for index, argument := range arguments {
			if argument.pointer == nil {
				continue
			}
			base := *(*unsafe.Pointer)(valuePointers[index])
			if base == nil {
				continue
			}
			if result.nativePointer == base {
				result.pointer, result.nativePointer = argument.pointer, nil
				break
			}
			if data, err := nativePointedBytes(argument.pointer); err == nil {
				offset := uintptr(result.nativePointer) - uintptr(base)
				if uintptr(result.nativePointer) > uintptr(base) && offset < uintptr(len(data)) {
					pointer := *argument.pointer
					pointer.path = append([]evalPath(nil), argument.pointer.path...)
					if len(pointer.path) == 0 {
						pointer.path = append(pointer.path, evalPath{index: int(offset), typ: types.PrimitiveU8})
					} else {
						pointer.path[len(pointer.path)-1].index += int(offset)
					}
					result.pointer, result.nativePointer = &pointer, nil
					break
				}
			} else if pointed, loadErr := argument.pointer.load(); loadErr == nil {
				if size, _, layoutErr := qktarget.LayoutOf(pointed.Type, ""); layoutErr == nil &&
					uintptr(result.nativePointer) > uintptr(base) && uintptr(result.nativePointer)-uintptr(base) < uintptr(size) {
					return EvalValue{}, fmt.Errorf("foreign return points inside temporary argument %d; only byte-array offsets can be retained", index+1)
				}
			}
		}
	}
	return result, nil
}

func nativeFFIType(typ types.Type, allocations *[]unsafe.Pointer) (*C.ffi_type, int, error) {
	if typ == nil {
		return nil, 0, fmt.Errorf("missing type")
	}
	switch value := types.Underlying(typ).(type) {
	case types.PrimitiveType:
		switch value {
		case types.PrimitiveVoid:
			return &C.ffi_type_void, 0, nil
		case types.PrimitiveBool, types.PrimitiveU8:
			return &C.ffi_type_uint8, 1, nil
		case types.PrimitiveI8:
			return &C.ffi_type_sint8, 1, nil
		case types.PrimitiveU16:
			return &C.ffi_type_uint16, 2, nil
		case types.PrimitiveI16:
			return &C.ffi_type_sint16, 2, nil
		case types.PrimitiveU32:
			return &C.ffi_type_uint32, 4, nil
		case types.PrimitiveI32:
			return &C.ffi_type_sint32, 4, nil
		case types.PrimitiveUsz:
			if unsafe.Sizeof(uintptr(0)) == 4 {
				return &C.ffi_type_uint32, 4, nil
			}
			return &C.ffi_type_uint64, 8, nil
		case types.PrimitiveIsz:
			if unsafe.Sizeof(uintptr(0)) == 4 {
				return &C.ffi_type_sint32, 4, nil
			}
			return &C.ffi_type_sint64, 8, nil
		case types.PrimitiveU64:
			return &C.ffi_type_uint64, 8, nil
		case types.PrimitiveI64:
			return &C.ffi_type_sint64, 8, nil
		case types.PrimitiveF32:
			return &C.ffi_type_float, 4, nil
		case types.PrimitiveF64:
			return &C.ffi_type_double, 8, nil
		}
	case types.PointerType:
		return &C.ffi_type_pointer, int(unsafe.Sizeof(uintptr(0))), nil
	case types.EnumType:
		return &C.ffi_type_sint32, 4, nil
	case types.FlagsType:
		return nativeFFIType(value.Underlying, allocations)
	case types.StructType:
		if value.Packed || len(value.Fields) == 0 {
			return nil, 0, fmt.Errorf("packed or empty struct %s is not supported by the host C ABI bridge", typ)
		}
		elementMemory := C.calloc(C.size_t(len(value.Fields)+1), C.size_t(unsafe.Sizeof(uintptr(0))))
		if elementMemory == nil {
			return nil, 0, fmt.Errorf("could not allocate foreign struct layout")
		}
		*allocations = append(*allocations, elementMemory)
		elements := unsafe.Slice((**C.ffi_type)(elementMemory), len(value.Fields)+1)
		for index, field := range value.Fields {
			fieldType, _, err := nativeFFIType(field.R, allocations)
			if err != nil {
				return nil, 0, err
			}
			elements[index] = fieldType
		}
		ffiType := C.qk_make_struct_type((**C.ffi_type)(elementMemory))
		if ffiType == nil {
			return nil, 0, fmt.Errorf("could not allocate foreign struct type")
		}
		*allocations = append(*allocations, unsafe.Pointer(ffiType))
		offsetsMemory := C.calloc(C.size_t(len(value.Fields)), C.size_t(unsafe.Sizeof(C.size_t(0))))
		if offsetsMemory == nil {
			return nil, 0, fmt.Errorf("could not allocate foreign struct offsets")
		}
		*allocations = append(*allocations, offsetsMemory)
		if status := C.qk_struct_offsets(ffiType, (*C.size_t)(offsetsMemory)); status != 0 {
			return nil, 0, fmt.Errorf("libffi could not calculate struct layout (status %d)", int(status))
		}
		size, alignment, err := qktarget.LayoutOf(typ, "")
		if err != nil {
			return nil, 0, err
		}
		if int(ffiType.size) != size || int(ffiType.alignment) != alignment {
			return nil, 0, fmt.Errorf("host C struct layout for %s differs from QK layout", typ)
		}
		offsets := unsafe.Slice((*C.size_t)(offsetsMemory), len(value.Fields))
		for index, field := range value.Fields {
			expected, err := qktarget.OffsetOf(typ, field.L, "")
			if err != nil {
				return nil, 0, err
			}
			if int(offsets[index]) != expected {
				return nil, 0, fmt.Errorf("host C field layout for %s.%s differs from QK layout", typ, field.L)
			}
		}
		return ffiType, size, nil
	}
	return nil, 0, fmt.Errorf("type %s is not supported by the host C ABI bridge", typ)
}

func writeNativeValue(typ types.Type, value EvalValue, destination unsafe.Pointer, allocations *[]unsafe.Pointer) error {
	if structure, ok := types.Underlying(typ).(types.StructType); ok {
		if len(value.Aggregate) != len(structure.Fields) {
			return fmt.Errorf("foreign struct %s has %d fields, got %d", typ, len(structure.Fields), len(value.Aggregate))
		}
		for index, field := range structure.Fields {
			offset, err := qktarget.OffsetOf(typ, field.L, "")
			if err != nil {
				return err
			}
			if err := writeNativeValue(field.R, value.Aggregate[index], unsafe.Add(destination, offset), allocations); err != nil {
				return err
			}
		}
		return nil
	}
	if _, pointer := types.Underlying(typ).(types.PointerType); pointer {
		if value.functionName != "" || len(value.Aggregate) != 0 ||
			value.Integer != nil && value.Integer.Sign() != 0 && value.nativePointer == nil && value.pointer == nil {
			return fmt.Errorf("pointer value cannot be passed to the host C ABI bridge")
		}
		address := value.nativePointer
		if value.pointer != nil {
			data, byteErr := nativePointedBytes(value.pointer)
			pointed, loadErr := value.pointer.load()
			if loadErr != nil {
				return loadErr
			}
			size := len(data)
			if byteErr != nil {
				var err error
				size, _, err = qktarget.LayoutOf(pointed.Type, "")
				if err != nil {
					return err
				}
			}
			buffer := C.calloc(1, C.size_t(max(size, 1)))
			if buffer == nil {
				return fmt.Errorf("could not allocate pointed-to value")
			}
			*allocations = append(*allocations, buffer)
			if byteErr == nil {
				copy(unsafe.Slice((*byte)(buffer), len(data)), data)
			} else if err := writeNativeValue(pointed.Type, pointed, buffer, allocations); err != nil {
				return err
			}
			address = buffer
		}
		*(*unsafe.Pointer)(destination) = address
		return nil
	}
	size, _, err := qktarget.LayoutOf(typ, "")
	if err != nil {
		return err
	}
	bytes := unsafe.Slice((*byte)(destination), size)
	switch types.Underlying(typ) {
	case types.PrimitiveF32:
		if value.Float == nil {
			return fmt.Errorf("expected floating-point value")
		}
		binary.NativeEndian.PutUint32(bytes, math.Float32bits(float32(*value.Float)))
	case types.PrimitiveF64:
		if value.Float == nil {
			return fmt.Errorf("expected floating-point value")
		}
		binary.NativeEndian.PutUint64(bytes, math.Float64bits(*value.Float))
	case types.PrimitiveBool:
		if value.Boolean {
			bytes[0] = 1
		}
	default:
		if value.Integer == nil {
			return fmt.Errorf("expected integer value")
		}
		switch size {
		case 1:
			bytes[0] = byte(value.Integer.Uint64())
		case 2:
			binary.NativeEndian.PutUint16(bytes, uint16(value.Integer.Uint64()))
		case 4:
			binary.NativeEndian.PutUint32(bytes, uint32(value.Integer.Uint64()))
		case 8:
			binary.NativeEndian.PutUint64(bytes, value.Integer.Uint64())
		}
	}
	return nil
}

func copyBackNativePointer(pointer *evalPointer, source unsafe.Pointer) error {
	if source == nil {
		return nil
	}
	if data, err := nativePointedBytes(pointer); err == nil {
		container := pointer
		start := 0
		if len(pointer.path) != 0 {
			last := pointer.path[len(pointer.path)-1]
			container = &evalPointer{root: pointer.root, globalName: pointer.globalName,
				evaluator: pointer.evaluator, path: pointer.path[:len(pointer.path)-1]}
			start = last.index
		}
		storage, err := container.load()
		if err != nil {
			return err
		}
		for index, byteValue := range unsafe.Slice((*byte)(source), len(data)) {
			storage.Aggregate[start+index] = EvalInteger(big.NewInt(int64(byteValue)), types.PrimitiveU8)
		}
		return container.store(storage)
	}
	pointed, err := pointer.load()
	if err != nil {
		return err
	}
	result, err := readNativeValue(pointed.Type, source)
	if err != nil {
		return err
	}
	return pointer.store(result)
}

func nativePointedBytes(pointer *evalPointer) ([]byte, error) {
	container := pointer
	start := 0
	if len(pointer.path) != 0 {
		last := pointer.path[len(pointer.path)-1]
		container = &evalPointer{root: pointer.root, globalName: pointer.globalName,
			evaluator: pointer.evaluator, path: pointer.path[:len(pointer.path)-1]}
		start = last.index
	}
	pointed, err := container.load()
	if err != nil {
		return nil, err
	}
	array, ok := types.Underlying(pointed.Type).(types.ArrayType)
	if !ok || !array.Base.Equals(types.PrimitiveU8) || start < 0 || start > len(pointed.Aggregate) {
		return nil, fmt.Errorf("pointer target %s is not supported by the host C ABI bridge", pointed.Type)
	}
	result := make([]byte, len(pointed.Aggregate)-start)
	for index, item := range pointed.Aggregate[start:] {
		if item.Integer == nil {
			return nil, fmt.Errorf("pointed-to byte %d is unavailable", index)
		}
		result[index] = byte(item.Integer.Uint64())
	}
	return result, nil
}

func readNativeValue(typ types.Type, source unsafe.Pointer) (EvalValue, error) {
	if typ.Equals(types.PrimitiveVoid) {
		return EvalValue{Type: typ}, nil
	}
	if _, pointer := types.Underlying(typ).(types.PointerType); pointer {
		return EvalValue{Type: typ, nativePointer: *(*unsafe.Pointer)(source)}, nil
	}
	if structure, ok := types.Underlying(typ).(types.StructType); ok {
		value := EvalValue{Type: typ, Aggregate: make([]EvalValue, len(structure.Fields))}
		for index, field := range structure.Fields {
			offset, err := qktarget.OffsetOf(typ, field.L, "")
			if err != nil {
				return EvalValue{}, err
			}
			fieldValue, err := readNativeValue(field.R, unsafe.Add(source, offset))
			if err != nil {
				return EvalValue{}, err
			}
			value.Aggregate[index] = fieldValue
		}
		return value, nil
	}
	size, _, err := qktarget.LayoutOf(typ, "")
	if err != nil {
		return EvalValue{}, err
	}
	bytes := unsafe.Slice((*byte)(source), size)
	switch types.Underlying(typ) {
	case types.PrimitiveF32:
		return EvalFloat(float64(math.Float32frombits(binary.NativeEndian.Uint32(bytes))), typ), nil
	case types.PrimitiveF64:
		return EvalFloat(math.Float64frombits(binary.NativeEndian.Uint64(bytes)), typ), nil
	case types.PrimitiveBool:
		return EvalBool(bytes[0] != 0), nil
	}
	var integer uint64
	switch size {
	case 1:
		integer = uint64(bytes[0])
	case 2:
		integer = uint64(binary.NativeEndian.Uint16(bytes))
	case 4:
		integer = uint64(binary.NativeEndian.Uint32(bytes))
	case 8:
		integer = binary.NativeEndian.Uint64(bytes)
	}
	result := new(big.Int).SetUint64(integer)
	if types.IsSigned(typ) && integer&(uint64(1)<<(size*8-1)) != 0 {
		result.Sub(result, new(big.Int).Lsh(big.NewInt(1), uint(size*8)))
	}
	return EvalInteger(result, typ), nil
}

func loadNativePointer(pointerType types.Type, address unsafe.Pointer) (EvalValue, error) {
	pointer, ok := types.Underlying(pointerType).(types.PointerType)
	if !ok || address == nil {
		return EvalValue{}, fmt.Errorf("foreign pointer is null or has no pointee type")
	}
	return readNativeValue(pointer.Base, address)
}

func storeNativePointer(pointerType types.Type, address unsafe.Pointer, value EvalValue) error {
	pointer, ok := types.Underlying(pointerType).(types.PointerType)
	if !ok || address == nil {
		return fmt.Errorf("foreign pointer is null or has no pointee type")
	}
	var allocations []unsafe.Pointer
	defer func() {
		for _, allocation := range allocations {
			C.free(allocation)
		}
	}()
	return writeNativeValue(pointer.Base, value, address, &allocations)
}
