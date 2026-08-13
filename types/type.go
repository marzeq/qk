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

// DetachForSerialization returns an equivalent type graph without runtime-only
// recursive AliasRef target pointers. Stable module/name references are enough
// to reconnect aliases when a module interface is loaded.
func DetachForSerialization(value Type) Type {
	if value == nil {
		return nil
	}
	switch value := value.(type) {
	case *AliasRef:
		return &AliasRef{Module: value.Module, Name: value.Name}
	case TypeParameter:
		value.Constraint = DetachForSerialization(value.Constraint)
		return value
	case DefinedType:
		value.Underlying = DetachForSerialization(value.Underlying)
		value.TypeArguments = detachTypes(value.TypeArguments)
		return value
	case PointerType:
		value.Base = DetachForSerialization(value.Base)
		return value
	case SliceType:
		value.Base = DetachForSerialization(value.Base)
		return value
	case ArrayType:
		value.Base = DetachForSerialization(value.Base)
		return value
	case SequenceType:
		value.Base = DetachForSerialization(value.Base)
		return value
	case StructType:
		value.Fields = detachFields(value.Fields)
		if value.TaggedUnion != nil {
			info := *value.TaggedUnion
			info.Tag = DetachForSerialization(info.Tag)
			info.Variants = append([]TaggedUnionVariant(nil), info.Variants...)
			for index := range info.Variants {
				info.Variants[index].Fields = detachFields(info.Variants[index].Fields)
			}
			value.TaggedUnion = &info
		}
		return value
	case UnionType:
		value.Fields = detachFields(value.Fields)
		return value
	case TraitType:
		value.Methods = append([]TraitMethod(nil), value.Methods...)
		for index := range value.Methods {
			value.Methods[index].Parameters = detachTypes(value.Methods[index].Parameters)
			value.Methods[index].ReturnType = DetachForSerialization(value.Methods[index].ReturnType)
			value.Methods[index].GenericParameters = append([]TypeParameter(nil), value.Methods[index].GenericParameters...)
			for parameter := range value.Methods[index].GenericParameters {
				constraint := value.Methods[index].GenericParameters[parameter].Constraint
				value.Methods[index].GenericParameters[parameter].Constraint = DetachForSerialization(constraint)
			}
		}
		return value
	case TraitPointerType:
		value.Trait = DetachForSerialization(value.Trait).(TraitType)
		return value
	case FunctionType:
		value.Parameters = detachTypes(value.Parameters)
		value.ReturnType = DetachForSerialization(value.ReturnType)
		value.VariadicElement = DetachForSerialization(value.VariadicElement)
		return value
	case MultipleReturnType:
		value.Types = detachTypes(value.Types)
		return value
	default:
		return value
	}
}

// SubstituteParameters replaces declaration-scoped generic parameters in a
// detached type graph. Bindings are keyed by TypeParameter.Key().
func SubstituteParameters(value Type, bindings map[string]Type) Type {
	if value == nil {
		return nil
	}
	if parameter, ok := value.(TypeParameter); ok {
		if replacement := bindings[parameter.Key()]; replacement != nil {
			return replacement
		}
		parameter.Constraint = SubstituteParameters(parameter.Constraint, bindings)
		return parameter
	}
	detached := DetachForSerialization(value)
	switch value := detached.(type) {
	case DefinedType:
		value.Underlying = SubstituteParameters(value.Underlying, bindings)
		value.TypeArguments = substituteTypes(value.TypeArguments, bindings)
		return value
	case PointerType:
		value.Base = SubstituteParameters(value.Base, bindings)
		return value
	case SliceType:
		value.Base = SubstituteParameters(value.Base, bindings)
		return value
	case ArrayType:
		value.Base = SubstituteParameters(value.Base, bindings)
		return value
	case SequenceType:
		value.Base = SubstituteParameters(value.Base, bindings)
		return value
	case StructType:
		value.Fields = substituteFields(value.Fields, bindings)
		if value.TaggedUnion != nil {
			value.TaggedUnion.Tag = SubstituteParameters(value.TaggedUnion.Tag, bindings)
			for index := range value.TaggedUnion.Variants {
				value.TaggedUnion.Variants[index].Fields = substituteFields(value.TaggedUnion.Variants[index].Fields, bindings)
			}
		}
		return value
	case UnionType:
		value.Fields = substituteFields(value.Fields, bindings)
		return value
	case TraitType:
		for index := range value.Methods {
			value.Methods[index].Parameters = substituteTypes(value.Methods[index].Parameters, bindings)
			value.Methods[index].ReturnType = SubstituteParameters(value.Methods[index].ReturnType, bindings)
		}
		return value
	case TraitPointerType:
		value.Trait = SubstituteParameters(value.Trait, bindings).(TraitType)
		return value
	case FunctionType:
		value.Parameters = substituteTypes(value.Parameters, bindings)
		value.ReturnType = SubstituteParameters(value.ReturnType, bindings)
		value.VariadicElement = SubstituteParameters(value.VariadicElement, bindings)
		return value
	case MultipleReturnType:
		value.Types = substituteTypes(value.Types, bindings)
		return value
	default:
		return value
	}
}

func substituteTypes(values []Type, bindings map[string]Type) []Type {
	result := make([]Type, len(values))
	for index, value := range values {
		result[index] = SubstituteParameters(value, bindings)
	}
	return result
}

func substituteFields(values []shared.Pair[string, Type], bindings map[string]Type) []shared.Pair[string, Type] {
	result := append([]shared.Pair[string, Type](nil), values...)
	for index := range result {
		result[index].R = SubstituteParameters(result[index].R, bindings)
	}
	return result
}

func detachTypes(values []Type) []Type {
	result := make([]Type, len(values))
	for index, value := range values {
		result[index] = DetachForSerialization(value)
	}
	return result
}

func detachFields(values []shared.Pair[string, Type]) []shared.Pair[string, Type] {
	result := append([]shared.Pair[string, Type](nil), values...)
	for index := range result {
		result[index].R = DetachForSerialization(result[index].R)
	}
	return result
}

// SelfType is the implementing type of the immediately enclosing trait. It
// remains symbolic until a structural-conformance check selects a concrete
// candidate.
type SelfType struct{}

func (SelfType) Equals(other Type) bool      { _, ok := other.(SelfType); return ok }
func (SelfType) CanCoerceTo(other Type) bool { return SelfType{}.Equals(other) }
func (SelfType) CanCastTo(other Type) bool   { return SelfType{}.Equals(other) }
func (SelfType) String() string              { return "Self" }

// SubstituteSelf replaces a trait's symbolic Self type throughout one method
// parameter or result type. Nested trait definitions introduce their own Self
// and are therefore substitution boundaries.
func SubstituteSelf(t Type, replacement Type) Type {
	if t == nil {
		return nil
	}
	switch t := t.(type) {
	case SelfType:
		return replacement
	case DefinedType:
		t.Underlying = SubstituteSelf(t.Underlying, replacement)
		t.TypeArguments = append([]Type(nil), t.TypeArguments...)
		for i := range t.TypeArguments {
			t.TypeArguments[i] = SubstituteSelf(t.TypeArguments[i], replacement)
		}
		return t
	case *AliasRef:
		return t
	case PointerType:
		t.Base = SubstituteSelf(t.Base, replacement)
		return t
	case SliceType:
		t.Base = SubstituteSelf(t.Base, replacement)
		return t
	case ArrayType:
		t.Base = SubstituteSelf(t.Base, replacement)
		return t
	case StructType:
		t.Fields = append([]shared.Pair[string, Type](nil), t.Fields...)
		for i := range t.Fields {
			t.Fields[i].R = SubstituteSelf(t.Fields[i].R, replacement)
		}
		if t.TaggedUnion != nil {
			info := *t.TaggedUnion
			info.Tag = SubstituteSelf(info.Tag, replacement)
			info.Variants = append([]TaggedUnionVariant(nil), info.Variants...)
			for i := range info.Variants {
				info.Variants[i].Fields = append([]shared.Pair[string, Type](nil), info.Variants[i].Fields...)
				for j := range info.Variants[i].Fields {
					info.Variants[i].Fields[j].R = SubstituteSelf(info.Variants[i].Fields[j].R, replacement)
				}
			}
			t.TaggedUnion = &info
		}
		return t
	case UnionType:
		t.Fields = append([]shared.Pair[string, Type](nil), t.Fields...)
		for i := range t.Fields {
			t.Fields[i].R = SubstituteSelf(t.Fields[i].R, replacement)
		}
		return t
	case FunctionType:
		t.Parameters = append([]Type(nil), t.Parameters...)
		for i := range t.Parameters {
			t.Parameters[i] = SubstituteSelf(t.Parameters[i], replacement)
		}
		t.ReturnType = SubstituteSelf(t.ReturnType, replacement)
		t.VariadicElement = SubstituteSelf(t.VariadicElement, replacement)
		return t
	case MultipleReturnType:
		t.Types = append([]Type(nil), t.Types...)
		for i := range t.Types {
			t.Types[i] = SubstituteSelf(t.Types[i], replacement)
		}
		return t
	default:
		return t
	}
}

// HasSelfType reports whether a type depends on its enclosing trait's concrete
// implementer. Referenced and nested traits own their own Self placeholders.
func HasSelfType(t Type) bool {
	switch t := t.(type) {
	case SelfType:
		return true
	case DefinedType:
		return slices.ContainsFunc(t.TypeArguments, HasSelfType)
	case PointerType:
		return HasSelfType(t.Base)
	case SliceType:
		return HasSelfType(t.Base)
	case ArrayType:
		return HasSelfType(t.Base)
	case StructType:
		for _, field := range t.Fields {
			if HasSelfType(field.R) {
				return true
			}
		}
	case UnionType:
		for _, field := range t.Fields {
			if HasSelfType(field.R) {
				return true
			}
		}
	case FunctionType:
		return slices.ContainsFunc(t.Parameters, HasSelfType) || HasSelfType(t.ReturnType)
	case MultipleReturnType:
		return slices.ContainsFunc(t.Types, HasSelfType)
	}
	return false
}

// TypeParameter is a declaration-scoped placeholder used only while describing
// a generic binding. Owner makes equally named parameters from different
// declarations distinct.
type TypeParameter struct {
	Owner      string
	Name       string
	Index      int
	Constraint Type
}

func (p TypeParameter) Equals(other Type) bool {
	o, ok := other.(TypeParameter)
	return ok && p.Owner == o.Owner && p.Index == o.Index
}
func (p TypeParameter) CanCoerceTo(other Type) bool { return p.Equals(other) }
func (p TypeParameter) CanCastTo(other Type) bool   { return p.Equals(other) }
func (p TypeParameter) String() string              { return p.Name }
func (p TypeParameter) Key() string                 { return p.Owner + "#" + strconv.Itoa(p.Index) }

func HasTypeParameter(t Type) bool {
	switch t := t.(type) {
	case TypeParameter:
		return true
	case DefinedType:
		return slices.ContainsFunc(t.TypeArguments, HasTypeParameter)
	case PointerType:
		return HasTypeParameter(t.Base)
	case SliceType:
		return HasTypeParameter(t.Base)
	case ArrayType:
		return HasTypeParameter(t.Base)
	case StructType:
		for _, field := range t.Fields {
			if HasTypeParameter(field.R) {
				return true
			}
		}
	case UnionType:
		for _, field := range t.Fields {
			if HasTypeParameter(field.R) {
				return true
			}
		}
	case FunctionType:
		return slices.ContainsFunc(t.Parameters, HasTypeParameter) || HasTypeParameter(t.ReturnType)
	case MultipleReturnType:
		return slices.ContainsFunc(t.Types, HasTypeParameter)
	}
	return false
}

// NoInitializerType is a contextual marker used only while validating `---`.
type NoInitializerType struct{}

func (NoInitializerType) Equals(other Type) bool { _, ok := other.(NoInitializerType); return ok }
func (NoInitializerType) CanCoerceTo(Type) bool  { return false }
func (NoInitializerType) CanCastTo(Type) bool    { return false }
func (NoInitializerType) String() string         { return "<no initializer>" }

// DefinedType is a nominal user-defined type. Its underlying type determines
// representation and explicit cast compatibility, but never implicit coercion.
type DefinedType struct {
	Module        string
	Name          string
	Underlying    Type
	GenericName   string
	TypeArguments []Type
}

// StrType returns the nominal builtin string type. Its representation is a
// dynamic byte slice, but it is intentionally distinct from []u8.
func StrType() DefinedType {
	return DefinedType{
		Name:       "str",
		Underlying: SliceType{Base: PrimitiveU8},
	}
}

// AliasRef represents a recursive reference to a named type while that type is
// being resolved. It is only constructed for cycles behind pointer indirection.
type AliasRef struct {
	Module string
	Name   string
	Target *Type
}

func (r *AliasRef) Equals(other Type) bool {
	if o, ok := other.(*AliasRef); ok {
		if r.Module == o.Module && r.Name == o.Name {
			return true
		}
	}
	if d, ok := other.(DefinedType); ok {
		if r.Module == d.Module && r.Name == d.Name {
			return true
		}
	}
	// Recursive resolution can leave a provisional reference to a transparent
	// alias in an already-built aggregate. Once the alias target is available,
	// compare through it. Nominal recursive types remain distinct because their
	// target is the nominal DefinedType rather than its underlying structure.
	if r.Target != nil && *r.Target != nil {
		target := *r.Target
		if reference, same := target.(*AliasRef); !same || reference != r {
			return target.Equals(other)
		}
	}
	return false
}
func (r *AliasRef) CanCoerceTo(other Type) bool { return r.Equals(other) }
func (r *AliasRef) CanCastTo(other Type) bool   { return castCompatible(r, other) }
func (r *AliasRef) String() string {
	if r.Module == "" {
		return r.Name
	}
	return r.Module + "." + r.Name
}

func (d DefinedType) Equals(other Type) bool {
	switch o := other.(type) {
	case DefinedType:
		return d.Module == o.Module && d.Name == o.Name
	case *AliasRef:
		return o.Equals(d)
	default:
		return false
	}
}
func (d DefinedType) CanCoerceTo(other Type) bool { return d.Equals(other) }
func (d DefinedType) CanCastTo(other Type) bool {
	return castCompatible(d.Underlying, Underlying(other))
}
func (d DefinedType) String() string {
	if d.Module == "" {
		return d.Name
	}
	return d.Module + "." + d.Name
}

func Underlying(t Type) Type {
	for {
		switch d := t.(type) {
		case DefinedType:
			t = d.Underlying
		case *AliasRef:
			if d.Target == nil || *d.Target == nil {
				return d
			}
			t = *d.Target
		default:
			return t
		}
	}
}

// Substitute replaces declaration-scoped type parameters throughout a type.
func Substitute(t Type, arguments map[string]Type) Type {
	if t == nil {
		return nil
	}
	switch t := t.(type) {
	case TypeParameter:
		if replacement, ok := arguments[t.Key()]; ok {
			return replacement
		}
		return t
	case DefinedType:
		t.Underlying = Substitute(t.Underlying, arguments)
		t.TypeArguments = append([]Type(nil), t.TypeArguments...)
		for i := range t.TypeArguments {
			t.TypeArguments[i] = Substitute(t.TypeArguments[i], arguments)
		}
		return t
	case *AliasRef:
		return t
	case PointerType:
		t.Base = Substitute(t.Base, arguments)
		return t
	case SliceType:
		t.Base = Substitute(t.Base, arguments)
		return t
	case ArrayType:
		t.Base = Substitute(t.Base, arguments)
		return t
	case StructType:
		t.Fields = append([]shared.Pair[string, Type](nil), t.Fields...)
		for i := range t.Fields {
			t.Fields[i].R = Substitute(t.Fields[i].R, arguments)
		}
		if t.TaggedUnion != nil {
			info := *t.TaggedUnion
			info.Tag = Substitute(info.Tag, arguments)
			info.Variants = append([]TaggedUnionVariant(nil), info.Variants...)
			for i := range info.Variants {
				info.Variants[i].Fields = append([]shared.Pair[string, Type](nil), info.Variants[i].Fields...)
				for j := range info.Variants[i].Fields {
					info.Variants[i].Fields[j].R = Substitute(info.Variants[i].Fields[j].R, arguments)
				}
			}
			t.TaggedUnion = &info
		}
		return t
	case UnionType:
		t.Fields = append([]shared.Pair[string, Type](nil), t.Fields...)
		for i := range t.Fields {
			t.Fields[i].R = Substitute(t.Fields[i].R, arguments)
		}
		return t
	case FunctionType:
		t.Parameters = append([]Type(nil), t.Parameters...)
		for i := range t.Parameters {
			t.Parameters[i] = Substitute(t.Parameters[i], arguments)
		}
		t.ReturnType = Substitute(t.ReturnType, arguments)
		t.VariadicElement = Substitute(t.VariadicElement, arguments)
		return t
	case MultipleReturnType:
		t.Types = append([]Type(nil), t.Types...)
		for i := range t.Types {
			t.Types[i] = Substitute(t.Types[i], arguments)
		}
		return t
	case TraitPointerType:
		substituted := Substitute(t.Trait, arguments)
		if trait, ok := substituted.(TraitType); ok {
			t.Trait = trait
		}
		return t
	case TraitType:
		t.Methods = append([]TraitMethod(nil), t.Methods...)
		for i := range t.Methods {
			t.Methods[i].GenericParameters = append([]TypeParameter(nil), t.Methods[i].GenericParameters...)
			t.Methods[i].Parameters = append([]Type(nil), t.Methods[i].Parameters...)
			for j := range t.Methods[i].GenericParameters {
				t.Methods[i].GenericParameters[j].Constraint = Substitute(
					t.Methods[i].GenericParameters[j].Constraint,
					arguments,
				)
			}
			for j := range t.Methods[i].Parameters {
				t.Methods[i].Parameters[j] = Substitute(t.Methods[i].Parameters[j], arguments)
			}
			t.Methods[i].ReturnType = Substitute(t.Methods[i].ReturnType, arguments)
		}
		return t
	default:
		return t
	}
}

// Identity returns a deterministic, declaration-sensitive encoding suitable
// for specialization caches and symbol mangling.
func Identity(t Type) string {
	if t == nil {
		return "<nil>"
	}
	switch t := t.(type) {
	case SelfType:
		return "trait-self"
	case TypeParameter:
		return "param(" + t.Key() + ")"
	case DefinedType:
		if t.GenericName != "" {
			arguments := make([]string, len(t.TypeArguments))
			for i, argument := range t.TypeArguments {
				arguments[i] = Identity(argument)
			}
			return "defined(" + t.Module + ":" + t.GenericName + "<" + strings.Join(arguments, ",") + ">)"
		}
		return "defined(" + t.Module + ":" + t.Name + ")"
	case *AliasRef:
		return "alias(" + t.Module + ":" + t.Name + ")"
	case PointerType:
		mutable := ""
		if t.Mutable {
			mutable = "mut:"
		}
		return "ptr(" + mutable + Identity(t.Base) + ")"
	case SliceType:
		mutable := ""
		if t.Mutable {
			mutable = "mut:"
		}
		return "slice(" + mutable + Identity(t.Base) + ")"
	case ArrayType:
		return "array(" + strconv.Itoa(t.Length) + ":" + Identity(t.Base) + ")"
	case FunctionType:
		parameters := make([]string, len(t.Parameters))
		for i, parameter := range t.Parameters {
			parameters[i] = Identity(parameter)
		}
		return "fn(" + strings.Join(parameters, ",") + ")->" + Identity(t.ReturnType)
	case MultipleReturnType:
		items := make([]string, len(t.Types))
		for i, item := range t.Types {
			items[i] = Identity(item)
		}
		return "multi(" + strings.Join(items, ",") + ")"
	case PrimitiveType:
		return "primitive(" + string(t) + ")"
	case StructType:
		fields := make([]string, len(t.Fields))
		for i, field := range t.Fields {
			fields[i] = field.L + ":" + Identity(field.R)
		}
		packed := ""
		if t.Packed {
			packed = "packed:"
		}
		return "struct(" + packed + strings.Join(fields, ",") + ")"
	case UnionType:
		return "union(" + t.Module + ":" + t.Name + ")"
	case EnumType:
		if t.IdentityName != "" {
			return "enum(" + t.IdentityName + ")"
		}
		return "enum(" + t.Module + ":" + t.Name + ")"
	case FlagsType:
		return "flags(" + t.String() + ")"
	case TraitType:
		return "trait(" + t.String() + ")"
	case TraitPointerType:
		mutable := ""
		if t.Mutable {
			mutable = "mut:"
		}
		return "dyn(" + mutable + Identity(t.Trait) + ")"
	default:
		return t.String()
	}
}

func castCompatible(from, to Type) bool {
	from = Underlying(from)
	to = Underlying(to)
	if upgradesMutableAccess(from, to) {
		return false
	}
	return from.Equals(to) || from.CanCastTo(to) || to.CanCastTo(from)
}

func upgradesMutableAccess(from, to Type) bool {
	switch source := from.(type) {
	case PointerType:
		switch target := to.(type) {
		case PointerType:
			return target.Mutable && !source.Mutable
		case SliceType:
			return target.Mutable && !source.Mutable
		}
	case SliceType:
		switch target := to.(type) {
		case PointerType:
			return target.Mutable && !source.Mutable
		case SliceType:
			return target.Mutable && !source.Mutable
		}
	}
	return false
}

func CanExplicitCast(from, to Type) bool {
	if from.CanCastTo(to) {
		return true
	}
	_, fromDefined := from.(DefinedType)
	_, toDefined := to.(DefinedType)
	return (fromDefined || toDefined) && castCompatible(from, to)
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

	PrimitiveBool PrimitiveType = "bool"
)

func IsSigned(t Type) bool {
	t = Underlying(t)
	return t == PrimitiveI8 || t == PrimitiveI16 || t == PrimitiveI32 || t == PrimitiveI64 || t == PrimitiveIsz
}

func IsUnsigned(t Type) bool {
	t = Underlying(t)
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
	if IsInteger(p) {
		if _, ok := other.(PointerType); ok {
			return true
		}
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

	return false
}

func (p PrimitiveType) String() string {
	return string(p)
}

type StructType struct {
	Fields      []shared.Pair[string, Type]
	Packed      bool
	TaggedUnion *TaggedUnionInfo
}

type TaggedUnionInfo struct {
	Tag      Type
	Variants []TaggedUnionVariant
	Auto     bool
}

type TaggedUnionVariant struct {
	Name     string
	TagValue string
	Fields   []shared.Pair[string, Type]
}

func TaggedUnion(t Type) (*TaggedUnionInfo, bool) {
	st, ok := Underlying(t).(StructType)
	if !ok || st.TaggedUnion == nil {
		return nil, false
	}
	return st.TaggedUnion, true
}

// TaggedUnionRepr returns the ordinary source-visible struct and union type
// that has the same layout as an explicitly tagged union. Auto tags remain
// compiler-private and intentionally have no raw representation view.
func TaggedUnionRepr(t Type) (StructType, bool) {
	storage, ok := Underlying(t).(StructType)
	if !ok || storage.TaggedUnion == nil || storage.TaggedUnion.Auto {
		return StructType{}, false
	}
	payload := make([]shared.Pair[string, Type], 0, len(storage.TaggedUnion.Variants))
	for _, variant := range storage.TaggedUnion.Variants {
		if len(variant.Fields) == 0 {
			continue
		}
		fields := make([]shared.Pair[string, Type], len(variant.Fields))
		for i, field := range variant.Fields {
			name := field.L
			if _, positional := strconv.Atoi(name); positional == nil {
				name = "_" + name
			}
			fields[i] = shared.Pair[string, Type]{L: name, R: field.R}
		}
		payload = append(payload, shared.Pair[string, Type]{
			L: variant.Name, R: StructType{Fields: fields},
		})
	}
	return StructType{
		Packed: storage.Packed,
		Fields: []shared.Pair[string, Type]{
			{L: "tag", R: storage.TaggedUnion.Tag},
			{L: "payload", R: UnionType{Fields: payload}},
		},
	}, true
}

func (i *TaggedUnionInfo) Variant(name string) (TaggedUnionVariant, int, bool) {
	if i == nil {
		return TaggedUnionVariant{}, -1, false
	}
	for index, variant := range i.Variants {
		if variant.Name == name {
			return variant, index, true
		}
	}
	return TaggedUnionVariant{}, -1, false
}

type TraitMethod struct {
	Name              string
	GenericParameters []TypeParameter
	Receiver          TraitReceiverKind
	Parameters        []Type
	ReturnType        Type
	HasDefault        bool
}

type TraitReceiverKind uint8

const (
	TraitReceiverValue TraitReceiverKind = iota
	TraitReceiverPointer
	TraitReceiverMutablePointer
)

// TraitType is an unsized, nominal set of method requirements. Values exist
// only through TraitPointerType descriptors.
type TraitType struct {
	Module  string
	Name    string
	Methods []TraitMethod
	Any     bool
}

func (t TraitType) DynamicIncompatibility() (method string, reason string, incompatible bool) {
	for _, method := range t.Methods {
		if len(method.GenericParameters) != 0 {
			return method.Name, "is generic", true
		}
		if HasSelfType(method.ReturnType) || slices.ContainsFunc(method.Parameters, HasSelfType) {
			return method.Name, "uses Self outside its receiver", true
		}
	}
	return "", "", false
}

func (t TraitType) Equals(other Type) bool {
	o, ok := Underlying(other).(TraitType)
	return ok && t.Module == o.Module && t.Name == o.Name && t.Any == o.Any
}
func (t TraitType) CanCoerceTo(other Type) bool { return t.Equals(other) }
func (t TraitType) CanCastTo(other Type) bool   { return t.Equals(other) }
func (t TraitType) String() string {
	if t.Any {
		return "Any"
	}
	if t.Module == "" {
		return t.Name
	}
	return t.Module + "." + t.Name
}

// StaticTraitView describes the method surface of a concrete value that was
// reinterpreted as a trait without erasing its representation.
type StaticTraitView struct {
	Trait  TraitType
	Access TraitReceiverKind
}

func (v StaticTraitView) String() string {
	switch v.Access {
	case TraitReceiverPointer:
		return "*" + v.Trait.String()
	case TraitReceiverMutablePointer:
		return "*mut " + v.Trait.String()
	default:
		return v.Trait.String()
	}
}

type TraitPointerType struct {
	Trait   TraitType
	Mutable bool
}

func (p TraitPointerType) Equals(other Type) bool {
	o, ok := other.(TraitPointerType)
	return ok && p.Mutable == o.Mutable && p.Trait.Equals(o.Trait)
}
func (p TraitPointerType) CanCoerceTo(other Type) bool {
	o, ok := other.(TraitPointerType)
	return ok && (p.Mutable || !o.Mutable) && p.Trait.Equals(o.Trait)
}
func (p TraitPointerType) CanCastTo(other Type) bool { return p.CanCoerceTo(other) }
func (p TraitPointerType) String() string {
	if p.Mutable {
		return "*mut dyn " + p.Trait.String()
	}
	return "*dyn " + p.Trait.String()
}

// OpaqueType is an incomplete type with no known value representation. It is
// used as the underlying type of a nominal DefinedType and may only be used
// behind pointer indirection.
type OpaqueType struct{}

func (OpaqueType) Equals(other Type) bool        { _, ok := other.(OpaqueType); return ok }
func (o OpaqueType) CanCoerceTo(other Type) bool { return o.Equals(other) }
func (o OpaqueType) CanCastTo(other Type) bool   { return o.Equals(other) }
func (OpaqueType) String() string                { return "opaque" }

func IsOpaque(t Type) bool {
	_, ok := Underlying(t).(OpaqueType)
	return ok
}

// IsComplete reports whether a type has a known by-value representation.
// Pointer representation never depends on the completeness of its base type.
func IsComplete(t Type) bool {
	return isComplete(t, make(map[string]bool), make(map[*AliasRef]bool))
}

func isComplete(t Type, defined map[string]bool, aliases map[*AliasRef]bool) bool {
	switch t := t.(type) {
	case DefinedType:
		key := Identity(t)
		if defined[key] {
			return false
		}
		defined[key] = true
		complete := isComplete(t.Underlying, defined, aliases)
		delete(defined, key)
		return complete
	case *AliasRef:
		if aliases[t] || t.Target == nil || *t.Target == nil {
			return false
		}
		aliases[t] = true
		complete := isComplete(*t.Target, defined, aliases)
		delete(aliases, t)
		return complete
	case OpaqueType:
		return false
	case TraitType:
		return false
	case SelfType:
		return false
	case StructType:
		for _, field := range t.Fields {
			if !isComplete(field.R, defined, aliases) {
				return false
			}
		}
	case UnionType:
		for _, field := range t.Fields {
			if !isComplete(field.R, defined, aliases) {
				return false
			}
		}
	case SliceType:
		return isComplete(t.Base, defined, aliases)
	case ArrayType:
		return isComplete(t.Base, defined, aliases)
	case MultipleReturnType:
		for _, item := range t.Types {
			if !isComplete(item, defined, aliases) {
				return false
			}
		}
	}
	return true
}

type EnumType struct {
	Module       string
	Name         string
	IdentityName string
	Variants     []string
	Values       []string
}

type FlagsType struct {
	Underlying PrimitiveType
	Variants   []string
	Values     []string
}

func (f FlagsType) Equals(other Type) bool {
	o, ok := other.(FlagsType)
	return ok && f.Underlying == o.Underlying && slices.Equal(f.Variants, o.Variants) && slices.Equal(f.Values, o.Values)
}
func (f FlagsType) CanCoerceTo(other Type) bool { return f.Equals(other) }
func (f FlagsType) CanCastTo(other Type) bool {
	return other.Equals(f.Underlying) || f.Equals(other)
}
func (f FlagsType) String() string { return "flags(" + f.Underlying.String() + ")" }
func (f FlagsType) VariantValue(name string) (string, bool) {
	for i, variant := range f.Variants {
		if variant == name {
			return f.Values[i], true
		}
	}
	return "", false
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
	if u.Name != "" {
		if u.Module == "" {
			return u.Name
		}
		return u.Module + "." + u.Name
	}
	var result strings.Builder
	result.WriteString("union { ")
	writeFields(&result, u.Fields)
	result.WriteString(" }")
	return result.String()
}

func (e EnumType) Equals(other Type) bool {
	o, ok := other.(EnumType)
	if !ok {
		return false
	}
	if e.IdentityName != "" || o.IdentityName != "" {
		return e.IdentityName != "" && e.IdentityName == o.IdentityName
	}
	return e.Module == o.Module && e.Name == o.Name
}
func (e EnumType) CanCoerceTo(other Type) bool { return e.Equals(other) }
func (e EnumType) CanCastTo(other Type) bool   { return e.Equals(other) }
func (e EnumType) String() string {
	if e.Name != "" {
		if e.Module == "" {
			return e.Name
		}
		return e.Module + "." + e.Name
	}
	return "enum { " + strings.Join(e.Variants, ", ") + " }"
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

	if s.Packed != otherStruct.Packed {
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
	result.WriteString("struct ")
	if s.Packed {
		result.WriteString("@packed ")
	}
	result.WriteString("{ ")
	writeFields(&result, s.Fields)
	result.WriteString(" }")
	return result.String()
}

func writeFields(result *strings.Builder, fields []shared.Pair[string, Type]) {
	for i, field := range fields {
		if field.L != "" {
			result.WriteString(field.L)
			result.WriteString(": ")
		}
		result.WriteString(field.R.String())
		if i < len(fields)-1 {
			result.WriteString(", ")
		}
	}
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
	t = Underlying(t)
	_, ok := t.(PointerType)
	return ok
}

type SliceType struct {
	Base    Type
	Mutable bool
}

func (a SliceType) Equals(other Type) bool {
	otherSlice, ok := other.(SliceType)
	if !ok {
		return false
	}

	if a.Mutable != otherSlice.Mutable {
		return false
	}

	return a.Base.Equals(otherSlice.Base)
}

func (a SliceType) CanCoerceTo(other Type) bool {
	if a.Equals(other) {
		return true
	}
	otherSlice, ok := other.(SliceType)
	if !ok {
		return false
	}
	if otherSlice.Mutable && !a.Mutable {
		return false
	}

	return a.Base.Equals(otherSlice.Base)
}

func (a SliceType) CanCastTo(other Type) bool {
	if a.Equals(other) {
		return true
	}

	if otherSlice, ok := other.(SliceType); ok {
		if otherSlice.Mutable && !a.Mutable {
			return false
		}
		return a.Base.Equals(otherSlice.Base)
	}
	if otherArray, ok := other.(ArrayType); ok {
		return a.Base.Equals(otherArray.Base)
	}

	return false
}

func (a SliceType) String() string {
	mutable := ""
	if a.Mutable {
		mutable = "mut "
	}
	return "[]" + mutable + a.Base.String()
}

type ArrayType struct {
	Base   Type
	Length int
}

func (a ArrayType) Equals(other Type) bool {
	o, ok := other.(ArrayType)
	return ok && a.Length == o.Length && a.Base.Equals(o.Base)
}

func (a ArrayType) CanCoerceTo(other Type) bool {
	if a.Equals(other) {
		return true
	}
	s, ok := other.(SliceType)
	return ok && !s.Mutable && a.Base.Equals(s.Base)
}

func (a ArrayType) CanCastTo(other Type) bool {
	if a.Equals(other) {
		return true
	}
	s, ok := other.(SliceType)
	return ok && a.Base.Equals(s.Base)
}

func (a ArrayType) String() string {
	return "[" + strconv.Itoa(a.Length) + "]" + a.Base.String()
}

// SequenceType is the provisional type of an array/slice literal before an
// expected type selects its storage representation. It must not reach IR.
type SequenceType struct {
	Base   Type
	Length int
}

func (s SequenceType) Equals(other Type) bool {
	o, ok := other.(SequenceType)
	return ok && s.Length == o.Length && s.Base.Equals(o.Base)
}
func (s SequenceType) CanCoerceTo(Type) bool { return false }
func (s SequenceType) CanCastTo(Type) bool   { return false }
func (s SequenceType) String() string        { return "<array or slice literal>" }

type FunctionType struct {
	Parameters      []Type
	ReturnType      Type
	TypedVariadic   bool
	VariadicElement Type
}

// MultipleReturnType is an ABI result bundle, not a source-level tuple.
type MultipleReturnType struct{ Types []Type }

func (m MultipleReturnType) Equals(other Type) bool {
	o, ok := other.(MultipleReturnType)
	if !ok || len(m.Types) != len(o.Types) {
		return false
	}
	for i := range m.Types {
		if !m.Types[i].Equals(o.Types[i]) {
			return false
		}
	}
	return true
}
func (m MultipleReturnType) CanCoerceTo(other Type) bool {
	o, ok := other.(MultipleReturnType)
	if !ok || len(m.Types) != len(o.Types) {
		return false
	}
	for i := range m.Types {
		if !m.Types[i].CanCoerceTo(o.Types[i]) {
			return false
		}
	}
	return true
}
func (m MultipleReturnType) CanCastTo(other Type) bool { return m.Equals(other) }
func (m MultipleReturnType) String() string {
	parts := make([]string, len(m.Types))
	for i, t := range m.Types {
		parts[i] = t.String()
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func (f FunctionType) Equals(other Type) bool {
	otherFunction, ok := other.(FunctionType)
	if !ok {
		return false
	}

	if len(f.Parameters) != len(otherFunction.Parameters) {
		return false
	}
	if f.TypedVariadic != otherFunction.TypedVariadic {
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
	if f.TypedVariadic != otherFunction.TypedVariadic {
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
		if f.TypedVariadic && i == len(f.Parameters)-1 {
			result.WriteString("...")
			result.WriteString(f.VariadicElement.String())
		} else {
			result.WriteString(param.String())
		}
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
	case ArrayType:
		return HasUntyped(t.Base)
	case SequenceType:
		return HasUntyped(t.Base)
	case PointerType:
		return HasUntyped(t.Base)
	case TraitPointerType:
		return false
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
	case MultipleReturnType:
		return slices.ContainsFunc(t.Types, HasUntyped)
	}

	return false
}

// HasError reports whether semantic failure has poisoned any part of a type.
// Composite types containing ErrorType are erroneous as a whole and must not
// participate in follow-on compatibility diagnostics.
func HasError(t Type) bool {
	return hasError(t, make(map[string]bool), make(map[*AliasRef]bool))
}

func hasError(t Type, defined map[string]bool, aliases map[*AliasRef]bool) bool {
	if t == nil {
		return false
	}
	switch t := t.(type) {
	case ErrorType:
		return true
	case DefinedType:
		key := Identity(t)
		if defined[key] {
			return false
		}
		defined[key] = true
		for _, argument := range t.TypeArguments {
			if hasError(argument, defined, aliases) {
				return true
			}
		}
		return hasError(t.Underlying, defined, aliases)
	case *AliasRef:
		if aliases[t] || t.Target == nil || *t.Target == nil {
			return false
		}
		aliases[t] = true
		return hasError(*t.Target, defined, aliases)
	case SliceType:
		return hasError(t.Base, defined, aliases)
	case ArrayType:
		return hasError(t.Base, defined, aliases)
	case SequenceType:
		return hasError(t.Base, defined, aliases)
	case PointerType:
		return hasError(t.Base, defined, aliases)
	case TraitPointerType:
		return hasError(t.Trait, defined, aliases)
	case TypeParameter:
		key := Identity(t)
		if defined[key] {
			return false
		}
		defined[key] = true
		return hasError(t.Constraint, defined, aliases)
	case StructType:
		for _, field := range t.Fields {
			if hasError(field.R, defined, aliases) {
				return true
			}
		}
		if t.TaggedUnion != nil {
			if hasError(t.TaggedUnion.Tag, defined, aliases) {
				return true
			}
			for _, variant := range t.TaggedUnion.Variants {
				for _, field := range variant.Fields {
					if hasError(field.R, defined, aliases) {
						return true
					}
				}
			}
		}
	case UnionType:
		for _, field := range t.Fields {
			if hasError(field.R, defined, aliases) {
				return true
			}
		}
	case FunctionType:
		for _, parameter := range t.Parameters {
			if hasError(parameter, defined, aliases) {
				return true
			}
		}
		return hasError(t.ReturnType, defined, aliases)
	case MultipleReturnType:
		for _, item := range t.Types {
			if hasError(item, defined, aliases) {
				return true
			}
		}
	case TraitType:
		key := Identity(t)
		if defined[key] {
			return false
		}
		defined[key] = true
		for _, method := range t.Methods {
			for _, parameter := range method.GenericParameters {
				if hasError(parameter.Constraint, defined, aliases) {
					return true
				}
			}
			for _, parameter := range method.Parameters {
				if hasError(parameter, defined, aliases) {
					return true
				}
			}
			if hasError(method.ReturnType, defined, aliases) {
				return true
			}
		}
	}
	return false
}

func HasTraitPointer(t Type) bool {
	switch t := Underlying(t).(type) {
	case TraitPointerType:
		return true
	case StructType:
		for _, field := range t.Fields {
			if HasTraitPointer(field.R) {
				return true
			}
		}
	case UnionType:
		for _, field := range t.Fields {
			if HasTraitPointer(field.R) {
				return true
			}
		}
	case SliceType:
		return HasTraitPointer(t.Base)
	case ArrayType:
		return HasTraitPointer(t.Base)
	}
	return false
}

// HasTaggedUnion reports whether a nominal tagged union occurs anywhere in a
// type, including behind pointer or slice indirection.
func HasTaggedUnion(t Type) bool {
	return hasTaggedUnion(t, make(map[string]bool), make(map[*AliasRef]bool))
}

func hasTaggedUnion(t Type, defined map[string]bool, aliases map[*AliasRef]bool) bool {
	switch t := t.(type) {
	case DefinedType:
		key := Identity(t)
		if defined[key] {
			return false
		}
		defined[key] = true
		return hasTaggedUnion(t.Underlying, defined, aliases)
	case *AliasRef:
		if aliases[t] || t.Target == nil || *t.Target == nil {
			return false
		}
		aliases[t] = true
		return hasTaggedUnion(*t.Target, defined, aliases)
	case PointerType:
		return hasTaggedUnion(t.Base, defined, aliases)
	case SliceType:
		return hasTaggedUnion(t.Base, defined, aliases)
	case ArrayType:
		return hasTaggedUnion(t.Base, defined, aliases)
	case StructType:
		if t.TaggedUnion != nil {
			return true
		}
		for _, field := range t.Fields {
			if hasTaggedUnion(field.R, defined, aliases) {
				return true
			}
		}
	case UnionType:
		for _, field := range t.Fields {
			if hasTaggedUnion(field.R, defined, aliases) {
				return true
			}
		}
	case FunctionType:
		for _, parameter := range t.Parameters {
			if hasTaggedUnion(parameter, defined, aliases) {
				return true
			}
		}
		return hasTaggedUnion(t.ReturnType, defined, aliases)
	case MultipleReturnType:
		for _, item := range t.Types {
			if hasTaggedUnion(item, defined, aliases) {
				return true
			}
		}
	}
	return false
}

func PromoteNumeric(a, b Type) Type {
	if a.Equals(b) {
		return a
	}

	_, aUntypedFloat := a.(UntypedFloat)
	_, bUntypedFloat := b.(UntypedFloat)
	if aUntypedFloat || bUntypedFloat {
		other := b
		if bUntypedFloat {
			other = a
		}

		if _, ok := other.(UntypedInt); ok {
			return UntypedFloat{}
		}
		if primitive, ok := other.(PrimitiveType); ok {
			if IsFloat(primitive) {
				return primitive
			}
			if IsInteger(primitive) {
				return PrimitiveF64
			}
		}
		return ErrorType{}
	}

	if _, ok := a.(UntypedInt); ok {
		return b
	}
	if _, ok := b.(UntypedInt); ok {
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

// CommonType finds the type shared by values in aggregate literals.
func CommonType(a, b Type) Type {
	if a.Equals(b) {
		return a
	}

	return PromoteNumeric(a, b)
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
