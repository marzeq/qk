package sema

import (
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func (a *Analyser) predefineBuiltins() {
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_I8), types.PRIMITIVE_I8))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_I16), types.PRIMITIVE_I16))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_I32), types.PRIMITIVE_I32))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_I64), types.PRIMITIVE_I64))

	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_U8), types.PRIMITIVE_U8))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_U16), types.PRIMITIVE_U16))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_U32), types.PRIMITIVE_U32))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_U64), types.PRIMITIVE_U64))

	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_F32), types.PRIMITIVE_F32))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_F64), types.PRIMITIVE_F64))

	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_ISZ), types.PRIMITIVE_ISZ))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_USZ), types.PRIMITIVE_USZ))

	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_VOID), types.PRIMITIVE_VOID))

	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_CHAR), types.PRIMITIVE_CHAR))

	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_BOOL), types.PRIMITIVE_BOOL))
}
