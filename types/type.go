package types

import (
	"strings"

	"github.com/marzeq/qk/shared"
)

type Type interface {
	Equals(Type) bool
	CanCoerceTo(Type) bool
	CanCastTo(Type) bool
	String() string
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

	PRIMITIVE_BOOL PrimitiveType = "bool"
)

func IsSigned(t Type) bool {
	return t == PRIMITIVE_I8 || t == PRIMITIVE_I16 || t == PRIMITIVE_I32 || t == PRIMITIVE_I64 || t == PRIMITIVE_ISZ
}

func IsUnsigned(t Type) bool {
	return t == PRIMITIVE_U8 || t == PRIMITIVE_U16 || t == PRIMITIVE_U32 || t == PRIMITIVE_U64 || t == PRIMITIVE_USZ
}

func IsInteger(t Type) bool {
	return IsSigned(t) || IsUnsigned(t) || t.Equals(UntypedInt{})
}

func IsFloat(t Type) bool {
	return t == PRIMITIVE_F32 || t == PRIMITIVE_F64 || t.Equals(UntypedFloat{})
}

func IsNumeric(t Type) bool {
	return IsInteger(t) || IsFloat(t)
}

func IntegerRank(p PrimitiveType) int {
	switch p {
	case PRIMITIVE_I8, PRIMITIVE_U8:
		return 1
	case PRIMITIVE_I16, PRIMITIVE_U16:
		return 2
	case PRIMITIVE_I32, PRIMITIVE_U32:
		return 3
	case PRIMITIVE_I64, PRIMITIVE_U64:
		return 4
	case PRIMITIVE_ISZ, PRIMITIVE_USZ:
		return 5
	default:
		return 0
	}
}

func FloatRank(p PrimitiveType) int {
	switch p {
	case PRIMITIVE_F32:
		return 1
	case PRIMITIVE_F64:
		return 2
	default:
		return 0
	}
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

	if IsInteger(p) && IsInteger(otherPrimitive) {
		if p == PRIMITIVE_ISZ && IsSigned(otherPrimitive) || otherPrimitive == PRIMITIVE_USZ {
			return true
		}

		if IsSigned(p) && IsSigned(otherPrimitive) {
			return IntegerRank(p) <= IntegerRank(otherPrimitive)
		}
		if IsUnsigned(p) && IsUnsigned(otherPrimitive) {
			return IntegerRank(p) <= IntegerRank(otherPrimitive)
		}
		return false
	}

	if IsInteger(p) && IsFloat(otherPrimitive) {
		return true
	}

	if IsFloat(p) && IsFloat(otherPrimitive) {
		return FloatRank(p) <= FloatRank(otherPrimitive)
	}

	return false
}

func (p PrimitiveType) CanCastTo(other Type) bool {
	if p.Equals(other) {
		return true
	}

	otherPrimitive, ok := other.(PrimitiveType)
	if !ok {
		return false
	}

	if p.Equals(PRIMITIVE_BOOL) && IsInteger(otherPrimitive) ||
		IsInteger(p) && otherPrimitive.Equals(PRIMITIVE_BOOL) {
		return true
	}

	if IsNumeric(p) && IsNumeric(otherPrimitive) {
		return true
	}

	if (p == PRIMITIVE_CHAR && IsInteger(otherPrimitive)) ||
		(otherPrimitive == PRIMITIVE_CHAR && IsInteger(p)) {
		return true
	}

	if IsInteger(p) {
		if _, ok := other.(PointerType); ok {
			return true
		}
	}

	return false
}

func (p PrimitiveType) String() string {
	return string(p)
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

func (s StructType) CanCastTo(other Type) bool {
	return s.Equals(other)
}

func (s StructType) String() string {
	var result strings.Builder
	result.WriteString("struct { ")
	for i, field := range s.Fields {
		result.WriteString(field.L + ": " + field.R.String())
		if i < len(s.Fields)-1 {
			result.WriteString(", ")
		}
	}
	result.WriteString(" }")
	return result.String()
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

func (p PointerType) CanCastTo(other Type) bool {
	if p.Equals(other) {
		return true
	}

	switch t := other.(type) {
	case PointerType:
		return true
	case PrimitiveType:
		return IsInteger(t)
	}

	return false
}

func (p PointerType) String() string {
	return "*" + p.Base.String()
}

func IsPointer(t Type) bool {
	_, ok := t.(PointerType)
	return ok
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

func (a ArrayType) CanCastTo(other Type) bool {
	return a.Equals(other)
}

func (a ArrayType) String() string {
	if a.Size == -1 {
		return "[" + a.Base.String() + "]"
	}
	return "[" + a.Base.String() + ", " + string(a.Size) + "]"
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

func (f FunctionType) CanCastTo(other Type) bool {
	return f.Equals(other)
}

func (f FunctionType) String() string {
	var result strings.Builder
	result.WriteString("(")
	for i, param := range f.Parameters {
		result.WriteString(param.String())
		if i < len(f.Parameters)-1 {
			result.WriteString(", ")
		}
	}
	result.WriteString("): " + f.ReturnType.String())
	return result.String()
}

type ErrorType struct{}

func (e ErrorType) Equals(other Type) bool {
	_, ok := other.(ErrorType)
	return ok
}

func (e ErrorType) CanCoerceTo(other Type) bool {
	return true
}

func (e ErrorType) CanCastTo(other Type) bool {
	return true
}

func (e ErrorType) String() string {
	return "<error>"
}

type UntypedInt struct{}

func (u UntypedInt) Equals(other Type) bool {
	_, ok := other.(UntypedInt)
	return ok
}

func (u UntypedInt) CanCoerceTo(other Type) bool {
	switch t := other.(type) {
	case PrimitiveType:
		return IsInteger(t) || IsFloat(t)
	case UntypedInt:
		return true
	case UntypedFloat:
		return true
	default:
		return false
	}
}

func (u UntypedInt) CanCastTo(other Type) bool {
	switch t := other.(type) {
	case PrimitiveType:
		return IsNumeric(t) || t.Equals(PRIMITIVE_BOOL)
	default:
		return false
	}
}

func (u UntypedInt) String() string {
	return "<untyped int>"
}

type UntypedFloat struct{}

func (u UntypedFloat) Equals(other Type) bool {
	_, ok := other.(UntypedFloat)
	return ok
}

func (u UntypedFloat) CanCoerceTo(other Type) bool {
	switch t := other.(type) {
	case PrimitiveType:
		return IsFloat(t)
	case UntypedFloat:
		return true
	default:
		return false
	}
}

func (u UntypedFloat) CanCastTo(other Type) bool {
	switch t := other.(type) {
	case PrimitiveType:
		return IsFloat(t) || IsInteger(t)
	default:
		return false
	}
}

func (u UntypedFloat) String() string {
	return "<untyped float>"
}

func IsUntyped(t Type) bool {
	switch t.(type) {
	case UntypedInt, UntypedFloat:
		return true
	}
	return false
}

func PromoteNumeric(a, b Type) Type {
	if a.Equals(b) {
		return a
	}

	if _, ok := a.(UntypedInt); ok {
		return b
	}
	if _, ok := b.(UntypedInt); ok {
		return a
	}

	if _, ok := a.(UntypedFloat); ok {
		return b
	}
	if _, ok := b.(UntypedFloat); ok {
		return a
	}

	pa, aok := a.(PrimitiveType)
	pb, bok := b.(PrimitiveType)

	if !aok || !bok {
		return ErrorType{}
	}

	if IsFloat(pa) || IsFloat(pb) {
		if pa == PRIMITIVE_F64 || pb == PRIMITIVE_F64 {
			return PRIMITIVE_F64
		}
		return PRIMITIVE_F32
	}

	return PromoteIntegers(pa, pb)
}

func PromoteIntegers(a, b PrimitiveType) Type {
	if IsSigned(a) && IsSigned(b) {
		return widerSigned(a, b)
	}

	if IsUnsigned(a) && IsUnsigned(b) {
		return widerUnsigned(a, b)
	}

	return ErrorType{}
}

func widerSigned(a, b PrimitiveType) PrimitiveType {
	if !IsSigned(a) || !IsSigned(b) {
		panic("widerSigned called with non-signed types")
	}

	if IntegerRank(a) >= IntegerRank(b) {
		return a
	}
	return b
}

func widerUnsigned(a, b PrimitiveType) PrimitiveType {
	if !IsUnsigned(a) || !IsUnsigned(b) {
		panic("widerUnsigned called with non-unsigned types")
	}

	if IntegerRank(a) >= IntegerRank(b) {
		return a
	}
	return b
}

func DefaultUntyped(t Type) Type {
	switch t.(type) {
	case UntypedInt:
		return PRIMITIVE_I32
	case UntypedFloat:
		return PRIMITIVE_F32
	default:
		return t
	}
}

type UnknownType struct{}

func (u UnknownType) Equals(other Type) bool      { return false }
func (u UnknownType) CanCoerceTo(other Type) bool { return false }
func (u UnknownType) CanCastTo(other Type) bool   { return false }
func (u UnknownType) String() string              { return "<unknown>" }
