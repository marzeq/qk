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

	BUILTIN_CHAR   Type = "char"
	BUILTIN_STRING Type = "string"
)

func ResolveType(name string) (Type, bool) {
	if name == string(BUILTIN_VOID) {
		return BUILTIN_VOID, true
	} else if name == string(BUILTIN_U8) {
		return BUILTIN_U8, true
	} else if name == string(BUILTIN_U16) {
		return BUILTIN_U16, true
	} else if name == string(BUILTIN_U32) {
		return BUILTIN_U32, true
	} else if name == string(BUILTIN_U64) {
		return BUILTIN_U64, true
	} else if name == string(BUILTIN_I8) {
		return BUILTIN_I8, true
	} else if name == string(BUILTIN_I16) {
		return BUILTIN_I16, true
	} else if name == string(BUILTIN_I32) {
		return BUILTIN_I32, true
	} else if name == string(BUILTIN_I64) {
		return BUILTIN_I64, true
	} else if name == string(BUILTIN_BOOL) {
		return BUILTIN_BOOL, true
	} else if name == string(BUILTIN_CHAR) {
		return BUILTIN_CHAR, true
	} else if name == string(BUILTIN_STRING) {
		return BUILTIN_STRING, true
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

	if (a == BUILTIN_BOOL || a == BUILTIN_CHAR) && IsNumericType(b) {
		return true, b
	}
	if (b == BUILTIN_BOOL || b == BUILTIN_CHAR) && IsNumericType(a) {
		return true, a
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
