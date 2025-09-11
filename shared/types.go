package shared

type Type interface {
	IsPrimitive() bool
	IsStruct() bool
	IsPointer() bool
	Compare(t2 Type) bool
	String() string
}

func alignUp(x, a int) int {
	m := x % a
	if m == 0 {
		return x
	}
	return x + (a - m)
}

func GetAlignOfType(t Type) int {
	if t.Compare(PRIMITIVE_U8) || t.Compare(PRIMITIVE_I8) {
		return 4
	}
	if t.Compare(PRIMITIVE_U16) || t.Compare(PRIMITIVE_I16) {
		return 4
	}
	if t.Compare(PRIMITIVE_U32) || t.Compare(PRIMITIVE_I32) {
		return 4
	}
	if t.Compare(PRIMITIVE_U64) || t.Compare(PRIMITIVE_I64) {
		return 8
	}
	if t.Compare(PRIMITIVE_CHAR) {
		return 4
	}
	if t.Compare(PRIMITIVE_BOOL) {
		return 4
	}
	if _, ok := t.(Pointer); ok {
		return 8
	}
	if st, ok := t.(Struct); ok {
		maxAlign := 1
		for _, field := range st.Fields {
			a := GetAlignOfType(field.R)
			if a > maxAlign {
				maxAlign = a
			}
		}
		if maxAlign == 0 {
			maxAlign = 1
		}
		return maxAlign
	}
	// default conservative minimum
	return 8
}

func GetSizeOfType(t Type) int {
	if t.Compare(PRIMITIVE_U8) || t.Compare(PRIMITIVE_I8) {
		return 4
	}
	if t.Compare(PRIMITIVE_U16) || t.Compare(PRIMITIVE_I16) {
		return 4
	}
	if t.Compare(PRIMITIVE_U32) || t.Compare(PRIMITIVE_I32) {
		return 4
	}
	if t.Compare(PRIMITIVE_U64) || t.Compare(PRIMITIVE_I64) {
		return 8
	}
	if t.Compare(PRIMITIVE_CHAR) {
		return 4
	}
	if t.Compare(PRIMITIVE_BOOL) {
		return 4
	}
	if st, ok := t.(Struct); ok {
		offset := 0
		maxAlign := 1
		for _, field := range st.Fields {
			a := GetAlignOfType(field.R)
			if a > maxAlign {
				maxAlign = a
			}
			offset = alignUp(offset, a) // pad before the field if needed
			offset += GetSizeOfType(field.R)
		}
		return alignUp(offset, maxAlign)
	}
	if _, ok := t.(Pointer); ok {
		return 8
	}
	return 0
}

func IsNumericType(t Type) bool {
	return t.Compare(PRIMITIVE_U8) ||
		t.Compare(PRIMITIVE_I8) ||
		t.Compare(PRIMITIVE_U16) ||
		t.Compare(PRIMITIVE_I16) ||
		t.Compare(PRIMITIVE_U32) ||
		t.Compare(PRIMITIVE_I32) ||
		t.Compare(PRIMITIVE_U64) ||
		t.Compare(PRIMITIVE_I64) ||
		t.Compare(PRIMITIVE_UNTYPED_INT)
}

func IsUnsignedType(ts ...Type) bool {
	for _, t := range ts {
		if !(t.Compare(PRIMITIVE_U8) ||
			t.Compare(PRIMITIVE_U16) ||
			t.Compare(PRIMITIVE_U32) ||
			t.Compare(PRIMITIVE_U64)) {
			return false
		}
	}
	return true
}

func IsSignedType(ts ...Type) bool {
	for _, t := range ts {
		if !(t.Compare(PRIMITIVE_I8) ||
			t.Compare(PRIMITIVE_I16) ||
			t.Compare(PRIMITIVE_I32) ||
			t.Compare(PRIMITIVE_I64)) {
			return false
		}
	}
	return true
}

func BiggerNumericType(t1, t2 Type) Type {
	if t1.Compare(PRIMITIVE_U64) || t2.Compare(PRIMITIVE_U64) {
		return PRIMITIVE_U64
	}
	if t1.Compare(PRIMITIVE_I64) || t2.Compare(PRIMITIVE_I64) {
		return PRIMITIVE_I64
	}
	if t1.Compare(PRIMITIVE_U32) || t2.Compare(PRIMITIVE_U32) {
		return PRIMITIVE_U32
	}
	if t1.Compare(PRIMITIVE_I32) || t2.Compare(PRIMITIVE_I32) {
		return PRIMITIVE_I32
	}
	if t1.Compare(PRIMITIVE_U16) || t2.Compare(PRIMITIVE_U16) {
		return PRIMITIVE_U16
	}
	if t1.Compare(PRIMITIVE_I16) || t2.Compare(PRIMITIVE_I16) {
		return PRIMITIVE_I16
	}
	if t1.Compare(PRIMITIVE_U8) || t2.Compare(PRIMITIVE_U8) {
		return PRIMITIVE_U8
	}
	if t1.Compare(PRIMITIVE_I8) || t2.Compare(PRIMITIVE_I8) {
		return PRIMITIVE_I8
	}
	return PRIMITIVE_UNTYPED_INT
}

func CanCoerceTo(t1, t2 Type) bool {
	if t1.Compare(PRIMITIVE_UNTYPED_INT) && IsNumericType(t2) ||
		t2.Compare(PRIMITIVE_UNTYPED_INT) && IsNumericType(t1) {
		return true
	}

	if t1.Compare(t2) {
		return true
	}

	if (IsSignedType(t1, t2) || IsUnsignedType(t1, t2)) && BiggerNumericType(t1, t2).Compare(t2) {
		return true
	}

	return false
}

func CanCastTo(t1, t2 Type) bool {
	if t1.Compare(t2) {
		return true
	}

	// char <-> u8
	if t1.Compare(PRIMITIVE_CHAR) && t2.Compare(PRIMITIVE_U8) ||
		t2.Compare(PRIMITIVE_CHAR) && t1.Compare(PRIMITIVE_U8) {
		return true
	}

	// numeric <-> numeric
	if IsNumericType(t1) && IsNumericType(t2) {
		return true
	}

	// pointer <-> *void
	if t1.IsPointer() && t2.IsPointer() {
		if t1.(Pointer).To.Compare(PRIMITIVE_VOID) || t2.(Pointer).To.Compare(PRIMITIVE_VOID) {
			return true
		}
	}

	return false
}

type Primitive string

const (
	PRIMITIVE_VOID Primitive = "void"

	PRIMITIVE_UNTYPED_INT Primitive = "_untyped_int"

	PRIMITIVE_U8  Primitive = "u8"
	PRIMITIVE_U16 Primitive = "u16"
	PRIMITIVE_U32 Primitive = "u32"
	PRIMITIVE_U64 Primitive = "u64"

	PRIMITIVE_I8  Primitive = "i8"
	PRIMITIVE_I16 Primitive = "i16"
	PRIMITIVE_I32 Primitive = "i32"
	PRIMITIVE_I64 Primitive = "i64"

	PRIMITIVE_BOOL Primitive = "bool"

	PRIMITIVE_CHAR Primitive = "char"
)

func (pt Primitive) IsPrimitive() bool { return true }
func (pt Primitive) IsStruct() bool    { return false }
func (pt Primitive) IsPointer() bool   { return false }
func (pt Primitive) Compare(t2 Type) bool {
	if !t2.IsPrimitive() {
		return false
	}
	return pt == t2.(Primitive)
}
func (pt Primitive) String() string { return string(pt) }

func GetPrimitive(name string) (Primitive, bool) {
	switch name {
	case string(PRIMITIVE_VOID):
		return PRIMITIVE_VOID, true
	case string(PRIMITIVE_U8):
		return PRIMITIVE_U8, true
	case string(PRIMITIVE_U16):
		return PRIMITIVE_U16, true
	case string(PRIMITIVE_U32):
		return PRIMITIVE_U32, true
	case string(PRIMITIVE_U64):
		return PRIMITIVE_U64, true
	case string(PRIMITIVE_I8):
		return PRIMITIVE_I8, true
	case string(PRIMITIVE_I16):
		return PRIMITIVE_I16, true
	case string(PRIMITIVE_I32):
		return PRIMITIVE_I32, true
	case string(PRIMITIVE_I64):
		return PRIMITIVE_I64, true
	case string(PRIMITIVE_BOOL):
		return PRIMITIVE_BOOL, true
	case string(PRIMITIVE_CHAR):
		return PRIMITIVE_CHAR, true
	}
	return "", false
}

type StructLayout struct {
	Size    int
	Align   int
	Offsets []int
}

type Struct struct {
	Fields []Pair[string, Type]
	Layout StructLayout
}

func (st *Struct) GetLayout() StructLayout {
	if st.Layout.Size != 0 {
		return st.Layout
	}

	offsets := make([]int, len(st.Fields))
	offset := 0
	maxAlign := 1

	for i, f := range st.Fields {
		a := GetAlignOfType(f.R)
		if a > maxAlign {
			maxAlign = a
		}
		offset = alignUp(offset, a)
		offsets[i] = offset
		offset += GetSizeOfType(f.R)
	}

	size := alignUp(offset, maxAlign)
	st.Layout = StructLayout{Size: size, Align: maxAlign, Offsets: offsets}
	return st.Layout
}

func (st Struct) IsPrimitive() bool { return false }
func (st Struct) IsStruct() bool    { return true }
func (st Struct) IsPointer() bool   { return false }
func (st1 Struct) Compare(t2 Type) bool {
	if !t2.IsStruct() {
		return false
	}

	st2 := t2.(Struct)

	if len(st1.Fields) != len(st2.Fields) {
		return false
	}
	for i, f1 := range st1.Fields {
		f2 := st2.Fields[i]
		if f1.R.IsStruct() && f2.R.IsStruct() {
			if !f1.R.(Struct).Compare(f2.R.(Struct)) {
				return false
			}
		} else if f1.R != f2.R {
			return false
		}
	}
	return true
}
func (st Struct) String() string {
	s := "struct { "
	for i, f := range st.Fields {
		if i > 0 {
			s += ", "
		}
		s += f.L + ": " + f.R.String()
	}
	s += " }"
	return s
}

type Pointer struct {
	To    Type
	Const bool
}

func (pt Pointer) IsPrimitive() bool { return false }
func (pt Pointer) IsStruct() bool    { return false }
func (pt Pointer) IsPointer() bool   { return true }
func (pt Pointer) Compare(t2 Type) bool {
	if !t2.IsPointer() {
		return false
	}
	if pt.To.Compare(PRIMITIVE_VOID) {
		return true
	}
	return pt.To.Compare(t2.(Pointer).To)
}
func (pt Pointer) String() string {
	if pt.Const {
		return "*" + pt.To.String() + "(const)"
	}
	return "*" + pt.To.String()
}

type TypeTable map[string]Type

func (tt TypeTable) Define(name string, t Type) bool {
	if _, exists := tt[name]; exists {
		return false
	}
	tt[name] = t
	return true
}

func (tt TypeTable) Lookup(name string) (Type, bool) {
	t, ok := tt[name]
	return t, ok
}
