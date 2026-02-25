package types

import "github.com/marzeq/qk/shared"

type Type interface {
	Equals(Type) bool
	CanCoerceTo(Type) bool
}

type PrimitiveType string

const (
	PRIMITIVE_I8  PrimitiveType = "i8"
	PRIMITIVE_I16 PrimitiveType = "i16"
	PRIMITIVE_I32 PrimitiveType = "i32"
	PRIMITIVE_I64 PrimitiveType = "i64"

	PRIMITIVE_U8  PrimitiveType = "u8"
	PRIMITIVE_U16 PrimitiveType = "u16"
	PRIMITIVE_U32 PrimitiveType = "u32"
	PRIMITIVE_U64 PrimitiveType = "u64"

	PRIMITIVE_F32 PrimitiveType = "f32"
	PRIMITIVE_F64 PrimitiveType = "f64"

	PRIMITIVE_ISZ PrimitiveType = "isz"
	PRIMITIVE_USZ PrimitiveType = "usz"

	PRIMITIVE_VOID PrimitiveType = "void"

	PRIMITIVE_CHAR PrimitiveType = "char"
)

func (p PrimitiveType) IsInteger() bool {
	return p == PRIMITIVE_I8 || p == PRIMITIVE_I16 || p == PRIMITIVE_I32 || p == PRIMITIVE_I64 ||
		p == PRIMITIVE_U8 || p == PRIMITIVE_U16 || p == PRIMITIVE_U32 || p == PRIMITIVE_U64 ||
		p == PRIMITIVE_ISZ || p == PRIMITIVE_USZ
}

func (p PrimitiveType) IsFloat() bool {
	return p == PRIMITIVE_F32 || p == PRIMITIVE_F64
}

func (p PrimitiveType) IsNumeric() bool {
	return p.IsInteger() || p.IsFloat()
}

func (p PrimitiveType) Equals(other Type) bool {
	if otherPrimitive, ok := other.(PrimitiveType); ok {
		return p == otherPrimitive
	}
	return false
}

func (p PrimitiveType) CanCoerceTo(other Type) bool {
	if p.Equals(other) {
		return true
	}

	otherPrimitive, ok := other.(PrimitiveType)
	if !ok {
		return false
	}

	if p.IsNumeric() && otherPrimitive.IsNumeric() {
		return true
	}

	if p.Equals(PRIMITIVE_CHAR) && otherPrimitive.Equals(PRIMITIVE_U8) ||
		p.Equals(PRIMITIVE_U8) && otherPrimitive.Equals(PRIMITIVE_CHAR) {
		return true
	}

	return false
}

type StructType struct {
	Fields []shared.Pair[string, Type]
}

func (s StructType) Equals(other Type) bool {
	otherStruct, ok := other.(StructType)
	if !ok {
		return false
	}

	if len(s.Fields) != len(otherStruct.Fields) {
		return false
	}

	for i, field := range s.Fields {
		otherField := otherStruct.Fields[i]
		if field.L != otherField.L || !field.R.Equals(otherField.R) {
			return false
		}
	}

	return true
}

func (s StructType) CanCoerceTo(other Type) bool {
	if s.Equals(other) {
		return true
	}
	return false
}

type PointerType struct {
	Base Type
}

func (p PointerType) Equals(other Type) bool {
	if otherPointer, ok := other.(PointerType); ok {
		return p.Base.Equals(otherPointer.Base)
	}
	return false
}

func (p PointerType) CanCoerceTo(other Type) bool {
	if p.Equals(other) {
		return true
	}

	otherPointer, ok := other.(PointerType)
	if !ok {
		return false
	}

	if p.Base.Equals(PRIMITIVE_VOID) || otherPointer.Base.Equals(PRIMITIVE_VOID) {
		return true
	}
	return p.Base.CanCoerceTo(otherPointer.Base)
}

type ArrayType struct {
	Base Type
	Size int
}

func (a ArrayType) Equals(other Type) bool {
	otherArray, ok := other.(ArrayType)
	if !ok {
		return false
	}

	if a.Size != otherArray.Size {
		return false
	}

	return a.Base.Equals(otherArray.Base)
}

func (a ArrayType) CanCoerceTo(other Type) bool {
	if a.Equals(other) {
		return true
	}

	otherArray, ok := other.(ArrayType)
	if !ok {
		return false
	}

	if a.Size != otherArray.Size {
		return false
	}

	return a.Base.CanCoerceTo(otherArray.Base)
}

type FunctionType struct {
	Parameters []Type
	ReturnType Type
}

func (f FunctionType) Equals(other Type) bool {
	otherFunction, ok := other.(FunctionType)
	if !ok {
		return false
	}

	if len(f.Parameters) != len(otherFunction.Parameters) {
		return false
	}

	for i, param := range f.Parameters {
		if !param.Equals(otherFunction.Parameters[i]) {
			return false
		}
	}

	return f.ReturnType.Equals(otherFunction.ReturnType)
}

func (f FunctionType) CanCoerceTo(other Type) bool {
	otherFunction, ok := other.(FunctionType)
	if !ok {
		return false
	}

	if len(f.Parameters) != len(otherFunction.Parameters) {
		return false
	}

	for i := range f.Parameters {
		if !otherFunction.Parameters[i].CanCoerceTo(f.Parameters[i]) {
			return false
		}
	}

	return f.ReturnType.CanCoerceTo(otherFunction.ReturnType)
}

type ErrorType struct{}

func (e ErrorType) Equals(other Type) bool {
	_, ok := other.(ErrorType)
	return ok
}

func (e ErrorType) CanCoerceTo(other Type) bool {
	return e.Equals(other)
}
