package shared

type Type interface {
  IsPrimitive() bool
  IsStruct() bool
  IsPointer() bool
}

func GetSizeOfType(t Type) int {
  if t == PRIMITIVE_U8 || t == PRIMITIVE_I8 {
    return 4
  }
  if t == PRIMITIVE_U16 || t == PRIMITIVE_I16 {
    return 4
  }
  if t == PRIMITIVE_U32 || t == PRIMITIVE_I32 {
    return 4
  }
  if t == PRIMITIVE_U64 || t == PRIMITIVE_I64 {
    return 8
  }
  if t == PRIMITIVE_CHAR {
    return 4
  }
  if t == PRIMITIVE_BOOL {
    return 4
  }
  if t == PRIMITIVE_CSTRING {
    return 8
  }
  if st, ok := t.(Struct); ok {
    maxSize := 0
    for _, field := range st.Fields {
      size := GetSizeOfType(field.R)
      if size > maxSize {
        maxSize = size
      }
    }
    return maxSize * len(st.Fields)
  }
  if _, ok := t.(Pointer); ok {
    return 8
  }
  return 0
}

func IsNumericType(t Type) bool {
  return t == PRIMITIVE_U8 ||
    t == PRIMITIVE_I8 ||
    t == PRIMITIVE_U16 ||
    t == PRIMITIVE_I16 ||
    t == PRIMITIVE_U32 ||
    t == PRIMITIVE_I32 ||
    t == PRIMITIVE_U64 ||
    t == PRIMITIVE_I64 ||
    t == PRIMITIVE_UNTYPED_INT
}

func IsUnsignedType(ts ...Type) bool {
  for _, t := range ts {
    if !(t == PRIMITIVE_U8 ||
      t == PRIMITIVE_U16 ||
      t == PRIMITIVE_U32 ||
      t == PRIMITIVE_U64) {
      return false
    }
  }
  return true
}

func IsSignedType(ts ...Type) bool {
  for _, t := range ts {
    if !(t == PRIMITIVE_I8 ||
      t == PRIMITIVE_I16 ||
      t == PRIMITIVE_I32 ||
      t == PRIMITIVE_I64) {
      return false
    }
  }
  return true
}

func BiggerNumericType(t1, t2 Type) Type {
  if t1 == PRIMITIVE_U64 || t2 == PRIMITIVE_U64 {
    return PRIMITIVE_U64
  }
  if t1 == PRIMITIVE_I64 || t2 == PRIMITIVE_I64 {
    return PRIMITIVE_I64
  }
  if t1 == PRIMITIVE_U32 || t2 == PRIMITIVE_U32 {
    return PRIMITIVE_U32
  }
  if t1 == PRIMITIVE_I32 || t2 == PRIMITIVE_I32 {
    return PRIMITIVE_I32
  }
  if t1 == PRIMITIVE_U16 || t2 == PRIMITIVE_U16 {
    return PRIMITIVE_U16
  }
  if t1 == PRIMITIVE_I16 || t2 == PRIMITIVE_I16 {
    return PRIMITIVE_I16
  }
  if t1 == PRIMITIVE_U8 || t2 == PRIMITIVE_U8 {
    return PRIMITIVE_U8
  }
  if t1 == PRIMITIVE_I8 || t2 == PRIMITIVE_I8 {
    return PRIMITIVE_I8
  }
  return PRIMITIVE_UNTYPED_INT
}

func CanCoerceTo(t1, t2 Type) bool {
  if t1 == PRIMITIVE_UNTYPED_INT && IsNumericType(t2) {
    return true
  }

  if t1 == t2 {
    return true
  }

  if (IsSignedType(t1, t2) || IsUnsignedType(t1, t2)) && BiggerNumericType(t1, t2) == t2 {
    return true
  }

  return false
}

func CanCastTo(t1, t2 Type) bool {
  if t1 == t2 {
    return true
  }

  if t1 == PRIMITIVE_CHAR && t2 == PRIMITIVE_U8 || t1 == PRIMITIVE_U8 && t2 == PRIMITIVE_CHAR {
    return true
  }

  if IsNumericType(t1) && IsNumericType(t2) {
    return true
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

  PRIMITIVE_CHAR    Primitive = "char"
  PRIMITIVE_CSTRING Primitive = "cstring"
)

func (pt Primitive) IsPrimitive() bool { return true }
func (pt Primitive) IsStruct() bool    { return false }
func (pt Primitive) IsPointer() bool   { return false }

type Struct struct {
  Fields []Pair[string, Type]
}

func (st Struct) IsPrimitive() bool { return false }
func (st Struct) IsStruct() bool    { return true }
func (st Struct) IsPointer() bool   { return false }

type Pointer struct {
  To    Type
  Const bool
}

func (pt Pointer) IsPrimitive() bool { return false }
func (pt Pointer) IsStruct() bool    { return false }
func (pt Pointer) IsPointer() bool   { return true }

type TypeTable map[string]Type

func NewTypeTable() TypeTable {
  tt := TypeTable{}

  tt.Define(string(PRIMITIVE_VOID), PRIMITIVE_VOID)
  tt.Define(string(PRIMITIVE_UNTYPED_INT), PRIMITIVE_UNTYPED_INT)

  tt.Define(string(PRIMITIVE_U8), PRIMITIVE_U8)
  tt.Define(string(PRIMITIVE_U16), PRIMITIVE_U16)
  tt.Define(string(PRIMITIVE_U32), PRIMITIVE_U32)
  tt.Define(string(PRIMITIVE_U64), PRIMITIVE_U64)

  tt.Define(string(PRIMITIVE_I8), PRIMITIVE_I8)
  tt.Define(string(PRIMITIVE_I16), PRIMITIVE_I16)
  tt.Define(string(PRIMITIVE_I32), PRIMITIVE_I32)
  tt.Define(string(PRIMITIVE_I64), PRIMITIVE_I64)

  tt.Define(string(PRIMITIVE_BOOL), PRIMITIVE_BOOL)

  tt.Define(string(PRIMITIVE_CHAR), PRIMITIVE_CHAR)
  tt.Define(string(PRIMITIVE_CSTRING), PRIMITIVE_CSTRING)

  tt.Define("string", Struct{Fields: []Pair[string, Type]{
    {"data", PRIMITIVE_CSTRING},
    {"len", PRIMITIVE_U64},
  }})

  return tt
}

func (tt TypeTable) Define(name string, t Type) {
  tt[name] = t
}

func (tt TypeTable) Lookup(name string) (Type, bool) {
  t, ok := tt[name]
  return t, ok
}
