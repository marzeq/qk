package types

import (
	"slices"
	"strconv"
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
	PrimitiveI8  PrimitiveType = "i8"
	PrimitiveI16 PrimitiveType = "i16"
	PrimitiveI32 PrimitiveType = "i32"
	PrimitiveI64 PrimitiveType = "i64"

	PrimitiveU8  PrimitiveType = "u8"
	PrimitiveU16 PrimitiveType = "u16"
	PrimitiveU32 PrimitiveType = "u32"
	PrimitiveU64 PrimitiveType = "u64"

	PrimitiveF32 PrimitiveType = "f32"
	PrimitiveF64 PrimitiveType = "f64"

	PrimitiveIsz PrimitiveType = "isz"
	PrimitiveUsz PrimitiveType = "usz"

	PrimitiveVoid PrimitiveType = "void"

	PrimitiveChar PrimitiveType = "char"

	PrimitiveBool PrimitiveType = "bool"
)

func IsSigned(t Type) bool {
	return t == PrimitiveI8 || t == PrimitiveI16 || t == PrimitiveI32 || t == PrimitiveI64 || t == PrimitiveIsz
}

func IsUnsigned(t Type) bool {
	return t == PrimitiveU8 || t == PrimitiveU16 || t == PrimitiveU32 || t == PrimitiveU64 || t == PrimitiveUsz
}

func IsInteger(t Type) bool {
	return IsSigned(t) || IsUnsigned(t) || t.Equals(UntypedInt{})
}

func IsFloat(t Type) bool {
	return t == PrimitiveF32 || t == PrimitiveF64 || t.Equals(UntypedFloat{})
}

func IsNumeric(t Type) bool {
	return IsInteger(t) || IsFloat(t)
}

func IntegerRank(p PrimitiveType) int {
	switch p {
	case PrimitiveI8, PrimitiveU8:
		return 1
	case PrimitiveI16, PrimitiveU16:
		return 2
	case PrimitiveI32, PrimitiveU32:
		return 3
	case PrimitiveI64, PrimitiveU64:
		return 4
	case PrimitiveIsz, PrimitiveUsz:
		return 5
	default:
		return 0
	}
}

func FloatRank(p PrimitiveType) int {
	switch p {
	case PrimitiveF32:
		return 1
	case PrimitiveF64:
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
		if p == PrimitiveIsz && IsSigned(otherPrimitive) || otherPrimitive == PrimitiveUsz {
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

	if p.Equals(PrimitiveBool) && IsInteger(otherPrimitive) ||
		IsInteger(p) && otherPrimitive.Equals(PrimitiveBool) {
		return true
	}

	if IsNumeric(p) && IsNumeric(otherPrimitive) {
		return true
	}

	if (p == PrimitiveChar && IsInteger(otherPrimitive)) ||
		(otherPrimitive == PrimitiveChar && IsInteger(p)) {
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

type EnumType struct {
	Module   string
	Name     string
	Variants []string
	Values   []string
}

type UnionType struct {
	Module string
	Name   string
	Fields []shared.Pair[string, Type]
}

func (u UnionType) Equals(other Type) bool {
	o, ok := other.(UnionType)
	return ok && u.Module == o.Module && u.Name == o.Name
}
func (u UnionType) CanCoerceTo(other Type) bool { return u.Equals(other) }
func (u UnionType) CanCastTo(other Type) bool   { return u.Equals(other) }
func (u UnionType) String() string {
	if u.Module == "" {
		return u.Name
	}
	return u.Module + ":" + u.Name
}

func (e EnumType) Equals(other Type) bool {
	o, ok := other.(EnumType)
	return ok && e.Module == o.Module && e.Name == o.Name
}
func (e EnumType) CanCoerceTo(other Type) bool { return e.Equals(other) }
func (e EnumType) CanCastTo(other Type) bool   { return e.Equals(other) }
func (e EnumType) String() string {
	if e.Module == "" {
		return e.Name
	}
	return e.Module + ":" + e.Name
}
func (e EnumType) VariantValue(name string) (string, bool) {
	for i, variant := range e.Variants {
		if variant == name {
			return e.Values[i], true
		}
	}
	return "", false
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
	return s.Equals(other)
}

func (s StructType) CanCastTo(other Type) bool {
	return s.CanCoerceTo(other)
}

func (s StructType) String() string {
	var result strings.Builder
	result.WriteString("struct { ")
	for i, field := range s.Fields {
		result.WriteString(field.L)
		result.WriteString(": ")
		result.WriteString(field.R.String())
		if i < len(s.Fields)-1 {
			result.WriteString(", ")
		}
	}
	result.WriteString(" }")
	return result.String()
}

type PointerType struct {
	Base    Type
	Mutable bool
}

func (p PointerType) Equals(other Type) bool {
	if otherPointer, ok := other.(PointerType); ok {
		return p.Base.Equals(otherPointer.Base) && p.Mutable == otherPointer.Mutable
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

	if !p.Mutable && otherPointer.Mutable {
		return false
	}

	if p.Base.Equals(PrimitiveVoid) || otherPointer.Base.Equals(PrimitiveVoid) {
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
		if !p.Mutable && t.Mutable {
			return false
		}
		return true
	case PrimitiveType:
		return IsInteger(t)
	}

	return false
}

func (p PointerType) String() string {
	sb := strings.Builder{}
	sb.WriteString("*")
	if p.Mutable {
		sb.WriteString("mut ")
	}
	sb.WriteString(p.Base.String())
	return sb.String()
}

func IsPointer(t Type) bool {
	_, ok := t.(PointerType)
	return ok
}

type SliceType struct {
	Base Type
	Size int
}

func (a SliceType) Equals(other Type) bool {
	otherSlice, ok := other.(SliceType)
	if !ok {
		return false
	}

	if a.Size != otherSlice.Size {
		return false
	}

	return a.Base.Equals(otherSlice.Base)
}

func (a SliceType) CanCoerceTo(other Type) bool {
	if a.Equals(other) {
		return true
	}

	if otherPointer, ok := other.(PointerType); ok {
		return a.Base.CanCoerceTo(otherPointer.Base)
	}

	otherSlice, ok := other.(SliceType)
	if !ok {
		return false
	}

	if a.Size != otherSlice.Size && a.Size != -1 && otherSlice.Size != -1 {
		return false
	}

	return a.Base.CanCoerceTo(otherSlice.Base)
}

func (a SliceType) CanCastTo(other Type) bool {
	if a.Equals(other) {
		return true
	}

	if otherPointer, ok := other.(PointerType); ok {
		if otherPointer.Base.Equals(PrimitiveVoid) {
			return true
		}
		return a.Base.CanCastTo(otherPointer.Base) || a.Base.Equals(otherPointer.Base)
	}

	return false
}

func (a SliceType) String() string {
	if a.Size == -1 {
		return "[" + a.Base.String() + "]"
	}
	return "[" + a.Base.String() + ", " + strconv.Itoa(a.Size) + "]"
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
	result.WriteString("): ")
	result.WriteString(f.ReturnType.String())
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
		return IsNumeric(t) || t.Equals(PrimitiveBool)
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

type UnresolvedEnum struct{}

func (u UnresolvedEnum) Equals(other Type) bool {
	_, ok := other.(UnresolvedEnum)
	return ok
}
func (u UnresolvedEnum) CanCoerceTo(other Type) bool { return false }
func (u UnresolvedEnum) CanCastTo(other Type) bool   { return false }
func (u UnresolvedEnum) String() string              { return "<unresolved enum>" }

func IsUntyped(t Type) bool {
	switch t.(type) {
	case UntypedInt, UntypedFloat, UnresolvedEnum:
		return true
	}
	return false
}

func HasUntyped(t Type) bool {
	if IsUntyped(t) {
		return true
	}

	switch t := t.(type) {
	case SliceType:
		return HasUntyped(t.Base)
	case PointerType:
		return HasUntyped(t.Base)
	case StructType:
		for _, field := range t.Fields {
			if HasUntyped(field.R) {
				return true
			}
		}
	case FunctionType:
		if slices.ContainsFunc(t.Parameters, HasUntyped) {
			return true
		}
		return HasUntyped(t.ReturnType)
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
		if pa == PrimitiveF64 || pb == PrimitiveF64 {
			return PrimitiveF64
		}
		return PrimitiveF32
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

type UnknownType struct{}

func (u UnknownType) Equals(other Type) bool      { return false }
func (u UnknownType) CanCoerceTo(other Type) bool { return false }
func (u UnknownType) CanCastTo(other Type) bool   { return false }
func (u UnknownType) String() string              { return "<unknown>" }
