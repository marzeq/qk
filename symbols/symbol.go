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
	Name     string
	Kind     SymbolKind
	Mutable  bool
	Public   bool
	Comptime bool
	// Referenced records whether source-level name resolution encountered a use
	// of this binding after its definition.
	Referenced bool
	// InlineComptime marks an untyped compile-time integer that is materialized
	// independently at each typed use instead of receiving runtime storage.
	InlineComptime  bool
	ComptimeInteger string
	Attributes      attributes.Attributes

	Type              types.Type // for SymbolKindVariable
	GenericOrigin     types.Type // pre-substitution type for values originating from a generic parameter
	StaticTraitView   *types.StaticTraitView
	Signature         *FunctionSignature      // for SymbolKindFunction
	TypeInfo          types.Type              // for SymbolKindTypeAlias
	Module            *Module                 // for SymbolKindModule
	Method            bool                    // function is attached to a type
	StaticMethod      bool                    // attached to a type without a receiver
	MethodReceiver    types.TraitReceiverKind // receiver form declared by an attached method
	MethodOwnerType   types.Type              // explicit structural owner pattern, when present
	DefinitionModule  string                  // module that owns the function implementation
	TraitRequirement  bool
	RequirementTrait  types.TraitType
	RequirementSlot   int
	RequirementAccess types.TraitReceiverKind
	// TraitDefaultTemplate and TraitDefaultSelf describe a concrete-facing
	// facade for a generic default trait method. Specialization prepends the
	// hidden Self argument and delegates to the underlying default template.
	TraitDefaultTemplate *Symbol
	TraitDefaultSelf     types.Type
	TraitDefault         bool
	GenericParameters    []types.TypeParameter
	Template             bool
	TemplateSymbol       *Symbol
	TypeArguments        []types.Type
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
