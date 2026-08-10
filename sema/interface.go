package sema

import (
	"sort"
	"strings"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

// ModuleInterface is the stable, serializable public surface written to a
// .qkm. Type spellings are canonical compiler spellings; a future format may
// add a binary type graph without changing the enclosing container.
type ModuleInterface struct {
	Name                   string                  `json:"name"`
	TrustedStandardLibrary bool                    `json:"trusted_standard_library,omitempty"`
	Symbols                []InterfaceSymbol       `json:"symbols,omitempty"`
	Methods                []InterfaceSymbol       `json:"methods,omitempty"`
	TraitDefaults          []InterfaceTraitDefault `json:"trait_defaults,omitempty"`
}

type InterfaceTraitDefault struct {
	TraitModule string          `json:"trait_module"`
	TraitName   string          `json:"trait_name"`
	Slot        int             `json:"slot"`
	Template    InterfaceSymbol `json:"template"`
}

type InterfaceSymbol struct {
	Name               string                   `json:"name"`
	Kind               string                   `json:"kind"`
	Type               string                   `json:"type,omitempty"`
	TypeInfo           *InterfaceType           `json:"type_info,omitempty"`
	Parameters         []string                 `json:"parameters,omitempty"`
	ParameterTypes     []*InterfaceType         `json:"parameter_types,omitempty"`
	ReturnType         string                   `json:"return_type,omitempty"`
	ReturnTypeInfo     *InterfaceType           `json:"return_type_info,omitempty"`
	RequiredParameters int                      `json:"required_parameters,omitempty"`
	Variadic           bool                     `json:"variadic,omitempty"`
	TypedVariadic      bool                     `json:"typed_variadic,omitempty"`
	VariadicElement    *InterfaceType           `json:"variadic_element,omitempty"`
	GenericParameters  []string                 `json:"generic_parameters,omitempty"`
	GenericTypes       []InterfaceTypeParameter `json:"generic_types,omitempty"`
	Method             bool                     `json:"method,omitempty"`
	MethodLookupName   string                   `json:"method_lookup_name,omitempty"`
	StructuralMethod   bool                     `json:"structural_method,omitempty"`
	StaticMethod       bool                     `json:"static_method,omitempty"`
	MethodReceiver     string                   `json:"method_receiver,omitempty"`
	MethodOwner        *InterfaceType           `json:"method_owner,omitempty"`
	MethodOwnerModule  string                   `json:"method_owner_module,omitempty"`
	MethodOwnerName    string                   `json:"method_owner_name,omitempty"`
	Mutable            bool                     `json:"mutable,omitempty"`
	Comptime           bool                     `json:"comptime,omitempty"`
	InlineComptime     bool                     `json:"inline_comptime,omitempty"`
	ComptimeInteger    string                   `json:"comptime_integer,omitempty"`
	Attributes         []InterfaceAttribute     `json:"attributes,omitempty"`
}

type InterfaceTypeParameter struct {
	Owner      string         `json:"owner"`
	Name       string         `json:"name"`
	Index      int            `json:"index"`
	Constraint *InterfaceType `json:"constraint,omitempty"`
}

type InterfaceAttribute struct {
	Kind string `json:"kind"`
	From string `json:"from,omitempty"`
	ABI  string `json:"abi,omitempty"`
}

// InterfaceType is an ID-free structural type graph suitable for persistence.
// Recursive aliases terminate in an alias_ref node instead of following the
// in-memory Target pointer.
type InterfaceType struct {
	Kind          string                 `json:"kind"`
	Name          string                 `json:"name,omitempty"`
	Module        string                 `json:"module,omitempty"`
	Mutable       bool                   `json:"mutable,omitempty"`
	Packed        bool                   `json:"packed,omitempty"`
	Length        int                    `json:"length,omitempty"`
	GenericName   string                 `json:"generic_name,omitempty"`
	Base          *InterfaceType         `json:"base,omitempty"`
	Underlying    *InterfaceType         `json:"underlying,omitempty"`
	TypeArguments []*InterfaceType       `json:"type_arguments,omitempty"`
	Fields        []InterfaceField       `json:"fields,omitempty"`
	Variants      []string               `json:"variants,omitempty"`
	Values        []string               `json:"values,omitempty"`
	Parameters    []*InterfaceType       `json:"parameters,omitempty"`
	ReturnType    *InterfaceType         `json:"return_type,omitempty"`
	Types         []*InterfaceType       `json:"types,omitempty"`
	Methods       []InterfaceTraitMethod `json:"methods,omitempty"`
	Any           bool                   `json:"any,omitempty"`
	TypedVariadic bool                   `json:"typed_variadic,omitempty"`
	IdentityName  string                 `json:"identity_name,omitempty"`
	TaggedUnion   *InterfaceTaggedUnion  `json:"tagged_union,omitempty"`
	Owner         string                 `json:"owner,omitempty"`
	Index         int                    `json:"index,omitempty"`
	Constraint    *InterfaceType         `json:"constraint,omitempty"`
}

type InterfaceField struct {
	Name string         `json:"name"`
	Type *InterfaceType `json:"type"`
}

type InterfaceTraitMethod struct {
	Name              string                   `json:"name"`
	Receiver          string                   `json:"receiver"`
	Parameters        []*InterfaceType         `json:"parameters,omitempty"`
	ReturnType        *InterfaceType           `json:"return_type,omitempty"`
	GenericParameters []InterfaceTypeParameter `json:"generic_parameters,omitempty"`
	HasDefault        bool                     `json:"has_default,omitempty"`
}

type InterfaceTaggedUnion struct {
	Tag      *InterfaceType                `json:"tag"`
	Variants []InterfaceTaggedUnionVariant `json:"variants"`
	Auto     bool                          `json:"auto,omitempty"`
}

type InterfaceTaggedUnionVariant struct {
	Name     string           `json:"name"`
	TagValue string           `json:"tag_value"`
	Fields   []InterfaceField `json:"fields,omitempty"`
}

func (a *Analyser) ModuleInterfaces() map[string]ModuleInterface {
	result := make(map[string]ModuleInterface, len(a.modules))
	for path, module := range a.modules {
		iface := ModuleInterface{Name: path, TrustedStandardLibrary: module.TrustedStandardLibrary}
		for _, symbol := range module.Scope.Symbols {
			if symbol.Public {
				iface.Symbols = append(iface.Symbols, interfaceSymbol(symbol))
			}
		}
		for ownerKey, methods := range a.methods {
			for lookupName, method := range methods {
				if method.Public && method.DefinitionModule == path {
					encoded := interfaceSymbol(method)
					encoded.MethodLookupName = lookupName
					if encoded.MethodOwnerModule == "" {
						if split := strings.LastIndex(ownerKey, ":"); split >= 0 {
							encoded.MethodOwnerModule, encoded.MethodOwnerName = ownerKey[:split], ownerKey[split+1:]
						}
					}
					iface.Methods = append(iface.Methods, encoded)
				}
			}
		}
		for lookupName, method := range a.structuralMethods {
			for _, candidate := range method {
				if candidate.Public && candidate.DefinitionModule == path {
					encoded := interfaceSymbol(candidate)
					encoded.MethodLookupName = lookupName
					iface.Methods = append(iface.Methods, encoded)
				}
			}
		}
		for key, defaults := range a.traitDefaults {
			split := strings.LastIndex(key, ":")
			if split < 0 || key[:split] != path {
				continue
			}
			for slot, template := range defaults {
				if template == nil {
					continue
				}
				iface.TraitDefaults = append(iface.TraitDefaults, InterfaceTraitDefault{
					TraitModule: key[:split], TraitName: key[split+1:], Slot: slot,
					Template: interfaceSymbol(template),
				})
			}
		}
		sort.Slice(iface.Symbols, func(i, j int) bool { return iface.Symbols[i].Name < iface.Symbols[j].Name })
		sort.Slice(iface.Methods, func(i, j int) bool { return iface.Methods[i].Name < iface.Methods[j].Name })
		sort.Slice(iface.TraitDefaults, func(i, j int) bool {
			if iface.TraitDefaults[i].TraitName != iface.TraitDefaults[j].TraitName {
				return iface.TraitDefaults[i].TraitName < iface.TraitDefaults[j].TraitName
			}
			return iface.TraitDefaults[i].Slot < iface.TraitDefaults[j].Slot
		})
		result[path] = iface
	}
	return result
}

func interfaceSymbol(symbol *symbols.Symbol) InterfaceSymbol {
	out := InterfaceSymbol{
		Name:               symbol.Name,
		Kind:               symbolKindName(symbol.Kind),
		RequiredParameters: 0,
		Variadic:           symbol.Signature != nil && symbol.Signature.Variadic,
		TypedVariadic:      symbol.Signature != nil && symbol.Signature.TypedVariadic,
		Method:             symbol.Method,
		StaticMethod:       symbol.StaticMethod,
		MethodReceiver:     receiverName(symbol.MethodReceiver),
		MethodOwner:        interfaceType(symbol.MethodOwnerType),
		Mutable:            symbol.Mutable,
		Comptime:           symbol.Comptime,
		InlineComptime:     symbol.InlineComptime,
		ComptimeInteger:    symbol.ComptimeInteger,
		Attributes:         interfaceAttributes(symbol.Attributes),
	}
	if symbol.Method && symbol.MethodOwnerType != nil {
		ownerModule, ownerName, _, nominal := methodOwnerIdentity(symbol.MethodOwnerType)
		out.StructuralMethod = !nominal
		if nominal {
			out.MethodOwnerModule, out.MethodOwnerName = ownerModule, ownerName
		}
	}
	for _, parameter := range symbol.GenericParameters {
		out.GenericParameters = append(out.GenericParameters, parameter.String())
		out.GenericTypes = append(out.GenericTypes, interfaceTypeParameter(parameter))
	}
	switch symbol.Kind {
	case symbols.SymbolKindVariable:
		if symbol.Type != nil {
			out.Type = symbol.Type.String()
			out.TypeInfo = interfaceType(symbol.Type)
		}
	case symbols.SymbolKindType:
		if symbol.TypeInfo != nil {
			out.Type = symbol.TypeInfo.String()
			out.TypeInfo = interfaceType(symbol.TypeInfo)
		}
	case symbols.SymbolKindFunction:
		if symbol.Signature != nil {
			out.RequiredParameters = symbol.Signature.RequiredParameters
			for _, parameter := range symbol.Signature.Parameters {
				out.Parameters = append(out.Parameters, parameter.String())
				out.ParameterTypes = append(out.ParameterTypes, interfaceType(parameter))
			}
			if symbol.Signature.ReturnType != nil {
				out.ReturnType = symbol.Signature.ReturnType.String()
				out.ReturnTypeInfo = interfaceType(symbol.Signature.ReturnType)
			}
			out.VariadicElement = interfaceType(symbol.Signature.VariadicElement)
		}
	}
	return out
}

func interfaceTypeParameter(parameter types.TypeParameter) InterfaceTypeParameter {
	return InterfaceTypeParameter{Owner: parameter.Owner, Name: parameter.Name, Index: parameter.Index, Constraint: interfaceType(parameter.Constraint)}
}

func interfaceAttributes(values attributes.Attributes) []InterfaceAttribute {
	result := make([]InterfaceAttribute, 0, len(values))
	for _, value := range values {
		item := InterfaceAttribute{Kind: string(value.GetType())}
		switch value := value.(type) {
		case attributes.FunctionAttributeForeign:
			item.From, item.ABI = value.From, interfaceABI(value.ABI)
		case attributes.FunctionAttributeExport:
			item.From, item.ABI = value.As, interfaceABI(value.ABI)
		}
		result = append(result, item)
	}
	return result
}

func interfaceABI(abi attributes.ForeignABI) string {
	if abi == attributes.ForeignABIQK {
		return "qk"
	}
	return "c"
}

func receiverName(receiver types.TraitReceiverKind) string {
	switch receiver {
	case types.TraitReceiverPointer:
		return "pointer"
	case types.TraitReceiverMutablePointer:
		return "mutable_pointer"
	default:
		return "value"
	}
}

func interfaceTypes(values []types.Type) []*InterfaceType {
	result := make([]*InterfaceType, len(values))
	for i, value := range values {
		result[i] = interfaceType(value)
	}
	return result
}

func interfaceFields(values []shared.Pair[string, types.Type]) []InterfaceField {
	result := make([]InterfaceField, len(values))
	for i, value := range values {
		result[i] = InterfaceField{Name: value.L, Type: interfaceType(value.R)}
	}
	return result
}

func interfaceType(value types.Type) *InterfaceType {
	if value == nil {
		return nil
	}
	switch value := value.(type) {
	case types.PrimitiveType:
		return &InterfaceType{Kind: "primitive", Name: value.String()}
	case types.SelfType:
		return &InterfaceType{Kind: "self"}
	case types.TypeParameter:
		return &InterfaceType{Kind: "parameter", Owner: value.Owner, Name: value.Name, Index: value.Index, Constraint: interfaceType(value.Constraint)}
	case types.DefinedType:
		return &InterfaceType{Kind: "defined", Module: value.Module, Name: value.Name, GenericName: value.GenericName, Underlying: interfaceType(value.Underlying), TypeArguments: interfaceTypes(value.TypeArguments)}
	case *types.AliasRef:
		return &InterfaceType{Kind: "alias_ref", Module: value.Module, Name: value.Name}
	case types.PointerType:
		return &InterfaceType{Kind: "pointer", Mutable: value.Mutable, Base: interfaceType(value.Base)}
	case types.SliceType:
		return &InterfaceType{Kind: "slice", Mutable: value.Mutable, Base: interfaceType(value.Base)}
	case types.ArrayType:
		return &InterfaceType{Kind: "array", Length: value.Length, Base: interfaceType(value.Base)}
	case types.SequenceType:
		return &InterfaceType{Kind: "sequence", Length: value.Length, Base: interfaceType(value.Base)}
	case types.StructType:
		out := &InterfaceType{Kind: "struct", Packed: value.Packed, Fields: interfaceFields(value.Fields)}
		if value.TaggedUnion != nil {
			out.TaggedUnion = &InterfaceTaggedUnion{Tag: interfaceType(value.TaggedUnion.Tag), Auto: value.TaggedUnion.Auto}
			for _, variant := range value.TaggedUnion.Variants {
				out.TaggedUnion.Variants = append(out.TaggedUnion.Variants, InterfaceTaggedUnionVariant{Name: variant.Name, TagValue: variant.TagValue, Fields: interfaceFields(variant.Fields)})
			}
		}
		return out
	case types.UnionType:
		return &InterfaceType{Kind: "union", Module: value.Module, Name: value.Name, Fields: interfaceFields(value.Fields)}
	case types.EnumType:
		return &InterfaceType{Kind: "enum", Module: value.Module, Name: value.Name, IdentityName: value.IdentityName, Variants: append([]string(nil), value.Variants...), Values: append([]string(nil), value.Values...)}
	case types.FlagsType:
		return &InterfaceType{Kind: "flags", Underlying: interfaceType(value.Underlying), Variants: append([]string(nil), value.Variants...), Values: append([]string(nil), value.Values...)}
	case types.TraitType:
		out := &InterfaceType{Kind: "trait", Module: value.Module, Name: value.Name, Any: value.Any}
		for _, method := range value.Methods {
			encoded := InterfaceTraitMethod{Name: method.Name, Receiver: receiverName(method.Receiver), Parameters: interfaceTypes(method.Parameters), ReturnType: interfaceType(method.ReturnType), HasDefault: method.HasDefault}
			for _, parameter := range method.GenericParameters {
				encoded.GenericParameters = append(encoded.GenericParameters, interfaceTypeParameter(parameter))
			}
			out.Methods = append(out.Methods, encoded)
		}
		return out
	case types.TraitPointerType:
		return &InterfaceType{Kind: "trait_pointer", Mutable: value.Mutable, Base: interfaceType(value.Trait)}
	case types.FunctionType:
		return &InterfaceType{Kind: "function", Parameters: interfaceTypes(value.Parameters), ReturnType: interfaceType(value.ReturnType), Base: interfaceType(value.VariadicElement), TypedVariadic: value.TypedVariadic}
	case types.MultipleReturnType:
		return &InterfaceType{Kind: "multiple_return", Types: interfaceTypes(value.Types)}
	case types.OpaqueType:
		return &InterfaceType{Kind: "opaque"}
	case types.NoInitializerType:
		return &InterfaceType{Kind: "no_initializer"}
	case types.UntypedInt:
		return &InterfaceType{Kind: "untyped_integer"}
	case types.UntypedFloat:
		return &InterfaceType{Kind: "untyped_float"}
	case types.UnresolvedEnum:
		return &InterfaceType{Kind: "unresolved_enum"}
	case types.ErrorType:
		return &InterfaceType{Kind: "error"}
	default:
		return &InterfaceType{Kind: "unknown", Name: value.String()}
	}
}

func symbolKindName(kind symbols.SymbolKind) string {
	switch kind {
	case symbols.SymbolKindVariable:
		return "variable"
	case symbols.SymbolKindFunction:
		return "function"
	case symbols.SymbolKindType:
		return "type"
	case symbols.SymbolKindModule:
		return "module"
	default:
		return "unknown"
	}
}
