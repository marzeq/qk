package sema

import (
	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func (a *Analyser) predefineBuiltins() {
	any := types.TraitType{Name: "Any", Any: true}
	a.universe.Define(symbols.NewType("Any", any))
	a.universe.Define(symbols.NewType(string(types.PrimitiveI8), types.PrimitiveI8))
	a.universe.Define(symbols.NewType(string(types.PrimitiveI16), types.PrimitiveI16))
	a.universe.Define(symbols.NewType(string(types.PrimitiveI32), types.PrimitiveI32))
	a.universe.Define(symbols.NewType(string(types.PrimitiveI64), types.PrimitiveI64))

	a.universe.Define(symbols.NewType(string(types.PrimitiveU8), types.PrimitiveU8))
	a.universe.Define(symbols.NewType(string(types.PrimitiveU16), types.PrimitiveU16))
	a.universe.Define(symbols.NewType(string(types.PrimitiveU32), types.PrimitiveU32))
	a.universe.Define(symbols.NewType(string(types.PrimitiveU64), types.PrimitiveU64))

	a.universe.Define(symbols.NewType(string(types.PrimitiveF32), types.PrimitiveF32))
	a.universe.Define(symbols.NewType(string(types.PrimitiveF64), types.PrimitiveF64))

	a.universe.Define(symbols.NewType(string(types.PrimitiveIsz), types.PrimitiveIsz))
	a.universe.Define(symbols.NewType(string(types.PrimitiveUsz), types.PrimitiveUsz))

	a.universe.Define(symbols.NewType(string(types.PrimitiveVoid), types.PrimitiveVoid))

	a.universe.Define(symbols.NewType(string(types.PrimitiveChar), types.PrimitiveChar))

	a.universe.Define(symbols.NewType(string(types.PrimitiveBool), types.PrimitiveBool))

	a.universe.Define(symbols.NewType("str", types.StrType()))

	a.universe.Define(symbols.NewType("cstr", types.PointerType{
		Base: types.PrimitiveChar,
	}))
	panicSymbol := symbols.NewFunction("panic", &symbols.FunctionSignature{
		Parameters:         []types.Type{types.StrType()},
		RequiredParameters: 1,
		ReturnType:         types.PrimitiveVoid,
	})
	panicSymbol.Attributes = attributes.Attributes{attributes.AttributeNoReturn{}}
	a.universe.Define(panicSymbol)
}
