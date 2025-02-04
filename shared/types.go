package shared

type Type string

const (
	BUILTIN_VOID Type = "void"

	BUILTIN_UNTYPED_INT Type = "_untyped_int" // reserved for number literals that we dont yet know the type of

	BUILTIN_U8  Type = "u8"
	BUILTIN_U16 Type = "u16"
	BUILTIN_U32 Type = "u32"
	BUILTIN_U64 Type = "u64"

	BUILTIN_I8  Type = "i8"
	BUILTIN_I16 Type = "i16"
	BUILTIN_I32 Type = "i32"
	BUILTIN_I64 Type = "i64"

	BUILTIN_BOOL Type = "bool"
)

func ResolveType(name string) (Type, bool) {
	if name == "void" {
		return BUILTIN_VOID, true
	} else if name == "u8" {
		return BUILTIN_U8, true
	} else if name == "u16" {
		return BUILTIN_U16, true
	} else if name == "u32" {
		return BUILTIN_U32, true
	} else if name == "u64" {
		return BUILTIN_U64, true
	} else if name == "i8" {
		return BUILTIN_I8, true
	} else if name == "i16" {
		return BUILTIN_I16, true
	} else if name == "i32" {
		return BUILTIN_I32, true
	} else if name == "i64" {
		return BUILTIN_I64, true
	} else if name == "bool" {
		return BUILTIN_BOOL, true
	}

	return BUILTIN_VOID, false
}

func IsNumericType(t Type) bool {
	return t == BUILTIN_UNTYPED_INT ||
		t == BUILTIN_U8 ||
		t == BUILTIN_U16 ||
		t == BUILTIN_U32 ||
		t == BUILTIN_U64 ||
		t == BUILTIN_I8 ||
		t == BUILTIN_I16 ||
		t == BUILTIN_I32 ||
		t == BUILTIN_I64
}

func AreCompatibleTypes(a, b Type) (bool, Type) {
	if a == b {
		return true, a
	}

	if IsNumericType(a) && IsNumericType(b) {
		return AreCompatibleNumericTypes(a, b)
	}

	if IsUntypedNumeric(a) && IsNumericType(b) {
		return true, b
	}
	if IsUntypedNumeric(b) && IsNumericType(a) {
		return true, a
	}

	return false, BUILTIN_VOID
}

func AreCompatibleNumericTypes(a, b Type) (bool, Type) {
	if !IsNumericType(a) || !IsNumericType(b) {
		return false, BUILTIN_VOID
	}

	if a == b {
		return true, a
	}

	if IsUnsignedType(a) && IsSignedType(b) || IsSignedType(a) && IsUnsignedType(b) {
		larger := LargerNumericType(a, b)
		return true, larger
	}

	return true, LargerNumericType(a, b)
}

func IsSignedType(t Type) bool {
	return t == BUILTIN_I8 || t == BUILTIN_I16 || t == BUILTIN_I32 || t == BUILTIN_I64
}

func IsUnsignedType(t Type) bool {
	return t == BUILTIN_U8 || t == BUILTIN_U16 || t == BUILTIN_U32 || t == BUILTIN_U64
}

func LargerNumericType(a, b Type) Type {
	precedence := []Type{
		BUILTIN_U8, BUILTIN_I8,
		BUILTIN_U16, BUILTIN_I16,
		BUILTIN_U32, BUILTIN_I32,
		BUILTIN_U64, BUILTIN_I64,
	}

	aIndex := -1
	bIndex := -1
	for i, t := range precedence {
		if t == a {
			aIndex = i
		}
		if t == b {
			bIndex = i
		}
	}

	if aIndex > bIndex {
		return a
	}
	return b
}

func IsUntypedNumeric(t Type) bool {
	return t == BUILTIN_UNTYPED_INT
}

func CanCoerceTo(src, dest Type) bool {
	if src == dest {
		return true
	}
	if IsUntypedNumeric(src) && IsNumericType(dest) {
		return true
	}
	return false
}
