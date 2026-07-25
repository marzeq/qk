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

	Type              types.Type // for SymbolKindVariable
	GenericOrigin     types.Type // pre-substitution type for values originating from a generic parameter
	StaticTraitView   *types.StaticTraitView
	Signature         *FunctionSignature      // for SymbolKindFunction
	TypeInfo          types.Type              // for SymbolKindTypeAlias
	Module            *Module                 // for SymbolKindModule
	Method            bool                    // function is attached to a type
	StaticMethod      bool                    // attached to a type without a receiver
	MethodReceiver    types.TraitReceiverKind // receiver form declared by an attached method
	DefinitionModule  string                  // module that owns the function implementation
	GenericParameters []types.TypeParameter
	Template          bool
	TemplateSymbol    *Symbol
	TypeArguments     []types.Type
}

type FunctionSignature struct {
	Parameters         []types.Type
	RequiredParameters int
	ReturnType         types.Type
	Variadic           bool
	TypedVariadic      bool
	VariadicElement    types.Type
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
