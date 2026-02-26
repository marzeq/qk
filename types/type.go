package types

import "github.com/marzeq/qk/shared"

type Type interface {
	Equals(Type) bool
	CanCoerceTo(Type) bool
	CanCastTo(Type) bool
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

	if p.IsInteger() && otherPrimitive.IsInteger() {
		return IntegerRank(p) <= IntegerRank(otherPrimitive)
	}

	if p.IsInteger() && otherPrimitive.IsFloat() {
		return true
	}

	if p.IsFloat() && otherPrimitive.IsFloat() {
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

	if p.IsNumeric() && otherPrimitive.IsNumeric() {
		return true
	}

	if (p == PRIMITIVE_CHAR && otherPrimitive.IsInteger()) ||
		(otherPrimitive == PRIMITIVE_CHAR && p.IsInteger()) {
		return true
	}

	if p.IsInteger() {
		if _, ok := other.(PointerType); ok {
			return true
		}
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

func (s StructType) CanCastTo(other Type) bool {
	return s.Equals(other)
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
		return t.IsInteger()
	}

	return false
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

type UntypedInt struct{}

func (u UntypedInt) Equals(other Type) bool {
	_, ok := other.(UntypedInt)
	return ok
}

func (u UntypedInt) CanCoerceTo(other Type) bool {
	switch t := other.(type) {
	case PrimitiveType:
		return t.IsInteger() || t.IsFloat()
	case UntypedInt:
		return true
	case UntypedFloat:
		return true
	default:
		return false
	}
}

type UntypedFloat struct{}

func (u UntypedFloat) Equals(other Type) bool {
	_, ok := other.(UntypedFloat)
	return ok
}

func (u UntypedFloat) CanCoerceTo(other Type) bool {
	switch t := other.(type) {
	case PrimitiveType:
		return t.IsFloat()
	case UntypedFloat:
		return true
	default:
		return false
	}
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

	if pa.IsFloat() || pb.IsFloat() {
		if pa == PRIMITIVE_F64 || pb == PRIMITIVE_F64 {
			return PRIMITIVE_F64
		}
		return PRIMITIVE_F32
	}

	return WiderInteger(pa, pb)
}

func WiderInteger(a, b PrimitiveType) PrimitiveType {
	rank := func(p PrimitiveType) int {
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

	if rank(a) >= rank(b) {
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

func (u UntypedInt) CanCastTo(other Type) bool {
	switch t := other.(type) {
	case PrimitiveType:
		return t.IsNumeric()
	default:
		return false
	}
}

func (u UntypedFloat) CanCastTo(other Type) bool {
	switch t := other.(type) {
	case PrimitiveType:
		return t.IsFloat() || t.IsInteger()
	default:
		return false
	}
}
