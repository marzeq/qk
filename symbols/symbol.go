package symbols

import (
	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/types"
)

type SymbolKind int

const (
	SymbolKindVariable SymbolKind = iota
	SymbolKindFunction
	SymbolKindType
	SymbolKindModule
)

type Symbol struct {
	Name       string
	Kind       SymbolKind
	Mutable    bool
	Public     bool
	Attributes attributes.Attributes

	Type      types.Type         // for SymbolKindVariable
	Signature *FunctionSignature // for SymbolKindFunction
	TypeInfo  types.Type         // for SymbolKindTypeAlias
	Module    *Module            // for SymbolKindModule
}

type FunctionSignature struct {
	Parameters []types.Type
	ReturnType types.Type
	Variadic   bool
}

func NewVariable(name string, typ types.Type) *Symbol {
	return &Symbol{
		Name: name,
		Kind: SymbolKindVariable,
		Type: typ,
	}
}

func NewFunction(name string, signature *FunctionSignature) *Symbol {
	return &Symbol{
		Name:      name,
		Kind:      SymbolKindFunction,
		Signature: signature,
	}
}

func NewType(name string, typeInfo types.Type) *Symbol {
	return &Symbol{
		Name:     name,
		Kind:     SymbolKindType,
		TypeInfo: typeInfo,
	}
}
