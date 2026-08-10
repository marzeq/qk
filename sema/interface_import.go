package sema

import (
	"fmt"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

// DeclareModuleInterface installs a source-free module interface into the
// analyser. Concrete generic bodies are instantiated later from the symbolic
// IR templates stored alongside this interface in the QKM artifact.
func (a *Analyser) DeclareModuleInterface(iface ModuleInterface) error {
	if iface.Name == "" {
		return fmt.Errorf("cached module interface has no name")
	}
	if _, exists := a.modules[iface.Name]; exists {
		return fmt.Errorf("module interface %q is already declared", iface.Name)
	}
	module := &symbols.Module{Name: iface.Name, Scope: symbols.NewScope(a.universe), TrustedStandardLibrary: iface.TrustedStandardLibrary}
	a.modules[iface.Name] = module
	a.importsByModule[iface.Name] = make(map[string]bool)
	a.aliasesByModule[iface.Name] = make(map[string]*aliasInfo)
	for _, encoded := range iface.Symbols {
		encoded.Public = true
		symbol, err := decodeInterfaceSymbol(encoded, iface.Name)
		if err != nil {
			return fmt.Errorf("decode %s.%s: %w", iface.Name, encoded.Name, err)
		}
		if err := module.Scope.Define(symbol); err != nil {
			return err
		}
	}
	methods := append([]InterfaceSymbol(nil), iface.Methods...)
	for i := range methods {
		methods[i].Public = true
	}
	methods = append(methods, iface.WitnessMethods...)
	for _, encoded := range methods {
		method, err := decodeInterfaceSymbol(encoded, iface.Name)
		if err != nil {
			return fmt.Errorf("decode method %s.%s: %w", iface.Name, encoded.Name, err)
		}
		if encoded.StructuralMethod {
			a.structuralMethods[encoded.MethodLookupName] = append(a.structuralMethods[encoded.MethodLookupName], method)
			continue
		}
		ownerModule, ownerName := encoded.MethodOwnerModule, encoded.MethodOwnerName
		if ownerModule == "" || ownerName == "" {
			return fmt.Errorf("method %q has no nominal owner", method.Name)
		}
		key := ownerModule + ":" + ownerName
		if a.methods[key] == nil {
			a.methods[key] = make(map[string]*symbols.Symbol)
		}
		a.methods[key][encoded.MethodLookupName] = method
	}
	for _, encoded := range iface.TraitDefaults {
		template, err := decodeInterfaceSymbol(encoded.Template, iface.Name)
		if err != nil {
			return fmt.Errorf("decode trait default %s.%s[%d]: %w", encoded.TraitModule, encoded.TraitName, encoded.Slot, err)
		}
		template.TraitDefault = true
		key := encoded.TraitModule + ":" + encoded.TraitName
		if len(a.traitDefaults[key]) <= encoded.Slot {
			grown := make([]*symbols.Symbol, encoded.Slot+1)
			copy(grown, a.traitDefaults[key])
			a.traitDefaults[key] = grown
		}
		a.traitDefaults[key][encoded.Slot] = template
	}
	return nil
}

func decodeInterfaceSymbol(encoded InterfaceSymbol, module string) (*symbols.Symbol, error) {
	kind, err := decodeSymbolKind(encoded.Kind)
	if err != nil {
		return nil, err
	}
	symbol := &symbols.Symbol{
		Name: encoded.Name, Kind: kind, Public: encoded.Public, Mutable: encoded.Mutable,
		Comptime: encoded.Comptime, InlineComptime: encoded.InlineComptime,
		ComptimeInteger: encoded.ComptimeInteger, DefinitionModule: module,
		Method: encoded.Method, StaticMethod: encoded.StaticMethod,
		MethodReceiver: decodeReceiver(encoded.MethodReceiver),
	}
	symbol.Type, err = decodeInterfaceType(encoded.TypeInfo)
	if err != nil {
		return nil, err
	}
	if kind == symbols.SymbolKindType {
		symbol.TypeInfo, symbol.Type = symbol.Type, nil
	}
	symbol.MethodOwnerType, err = decodeInterfaceType(encoded.MethodOwner)
	if err != nil {
		return nil, err
	}
	for _, parameter := range encoded.GenericTypes {
		constraint, decodeErr := decodeInterfaceType(parameter.Constraint)
		if decodeErr != nil {
			return nil, decodeErr
		}
		symbol.GenericParameters = append(symbol.GenericParameters, types.TypeParameter{Owner: parameter.Owner, Name: parameter.Name, Index: parameter.Index, Constraint: constraint})
	}
	symbol.Template = len(symbol.GenericParameters) != 0
	if kind == symbols.SymbolKindFunction {
		signature := &symbols.FunctionSignature{RequiredParameters: encoded.RequiredParameters, Variadic: encoded.Variadic, TypedVariadic: encoded.TypedVariadic}
		for _, parameter := range encoded.ParameterTypes {
			decoded, decodeErr := decodeInterfaceType(parameter)
			if decodeErr != nil {
				return nil, decodeErr
			}
			signature.Parameters = append(signature.Parameters, decoded)
		}
		signature.ReturnType, err = decodeInterfaceType(encoded.ReturnTypeInfo)
		if err != nil {
			return nil, err
		}
		signature.VariadicElement, err = decodeInterfaceType(encoded.VariadicElement)
		if err != nil {
			return nil, err
		}
		symbol.Signature = signature
	}
	symbol.Attributes, err = decodeInterfaceAttributes(encoded.Attributes)
	return symbol, err
}

func decodeSymbolKind(kind string) (symbols.SymbolKind, error) {
	switch kind {
	case "variable":
		return symbols.SymbolKindVariable, nil
	case "function":
		return symbols.SymbolKindFunction, nil
	case "type":
		return symbols.SymbolKindType, nil
	default:
		return 0, fmt.Errorf("unsupported symbol kind %q", kind)
	}
}

func decodeInterfaceAttributes(encoded []InterfaceAttribute) (attributes.Attributes, error) {
	var result attributes.Attributes
	for _, item := range encoded {
		switch attributes.AttributeType(item.Kind) {
		case attributes.AttributeTypeNoReturn:
			result = append(result, attributes.AttributeNoReturn{})
		case attributes.AttributeTypeInline:
			result = append(result, attributes.AttributeInline{})
		case attributes.AttributeTypeNoInline:
			result = append(result, attributes.AttributeNoInline{})
		case attributes.AttributeTypePacked:
			result = append(result, attributes.AttributePacked{})
		case attributes.AttributeTypeForeign:
			result = append(result, attributes.FunctionAttributeForeign{From: item.From, ABI: decodeInterfaceABI(item.ABI)})
		case attributes.AttributeTypeExport:
			result = append(result, attributes.FunctionAttributeExport{As: item.From, ABI: decodeInterfaceABI(item.ABI)})
		default:
			return nil, fmt.Errorf("unsupported attribute %q", item.Kind)
		}
	}
	return result, nil
}

func decodeInterfaceABI(value string) attributes.ForeignABI {
	if value == "qk" {
		return attributes.ForeignABIQK
	}
	return attributes.ForeignABIC
}

func decodeReceiver(value string) types.TraitReceiverKind {
	switch value {
	case "pointer":
		return types.TraitReceiverPointer
	case "mutable_pointer":
		return types.TraitReceiverMutablePointer
	default:
		return types.TraitReceiverValue
	}
}

func decodeInterfaceTypes(encoded []*InterfaceType) ([]types.Type, error) {
	result := make([]types.Type, len(encoded))
	for i, item := range encoded {
		decoded, err := decodeInterfaceType(item)
		if err != nil {
			return nil, err
		}
		result[i] = decoded
	}
	return result, nil
}

func decodeInterfaceFields(encoded []InterfaceField) ([]shared.Pair[string, types.Type], error) {
	result := make([]shared.Pair[string, types.Type], len(encoded))
	for i, field := range encoded {
		decoded, err := decodeInterfaceType(field.Type)
		if err != nil {
			return nil, err
		}
		result[i] = shared.Pair[string, types.Type]{L: field.Name, R: decoded}
	}
	return result, nil
}

func decodeInterfaceType(encoded *InterfaceType) (types.Type, error) {
	if encoded == nil {
		return nil, nil
	}
	base, err := decodeInterfaceType(encoded.Base)
	if err != nil {
		return nil, err
	}
	switch encoded.Kind {
	case "primitive":
		return types.PrimitiveType(encoded.Name), nil
	case "self":
		return types.SelfType{}, nil
	case "parameter":
		constraint, err := decodeInterfaceType(encoded.Constraint)
		return types.TypeParameter{Owner: encoded.Owner, Name: encoded.Name, Index: encoded.Index, Constraint: constraint}, err
	case "defined":
		underlying, err := decodeInterfaceType(encoded.Underlying)
		if err != nil {
			return nil, err
		}
		arguments, err := decodeInterfaceTypes(encoded.TypeArguments)
		if err != nil {
			return nil, err
		}
		return types.DefinedType{Module: encoded.Module, Name: encoded.Name, GenericName: encoded.GenericName, Underlying: underlying, TypeArguments: arguments}, nil
	case "alias_ref":
		return &types.AliasRef{Module: encoded.Module, Name: encoded.Name}, nil
	case "pointer":
		return types.PointerType{Base: base, Mutable: encoded.Mutable}, nil
	case "slice":
		return types.SliceType{Base: base, Mutable: encoded.Mutable}, nil
	case "array":
		return types.ArrayType{Base: base, Length: encoded.Length}, nil
	case "sequence":
		return types.SequenceType{Base: base, Length: encoded.Length}, nil
	case "struct":
		fields, err := decodeInterfaceFields(encoded.Fields)
		if err != nil {
			return nil, err
		}
		result := types.StructType{Fields: fields, Packed: encoded.Packed}
		if encoded.TaggedUnion != nil {
			tag, err := decodeInterfaceType(encoded.TaggedUnion.Tag)
			if err != nil {
				return nil, err
			}
			info := &types.TaggedUnionInfo{Tag: tag, Auto: encoded.TaggedUnion.Auto}
			for _, variant := range encoded.TaggedUnion.Variants {
				variantFields, err := decodeInterfaceFields(variant.Fields)
				if err != nil {
					return nil, err
				}
				info.Variants = append(info.Variants, types.TaggedUnionVariant{Name: variant.Name, TagValue: variant.TagValue, Fields: variantFields})
			}
			result.TaggedUnion = info
		}
		return result, nil
	case "union":
		fields, err := decodeInterfaceFields(encoded.Fields)
		return types.UnionType{Module: encoded.Module, Name: encoded.Name, Fields: fields}, err
	case "enum":
		return types.EnumType{Module: encoded.Module, Name: encoded.Name, IdentityName: encoded.IdentityName, Variants: encoded.Variants, Values: encoded.Values}, nil
	case "flags":
		underlying, err := decodeInterfaceType(encoded.Underlying)
		if err != nil {
			return nil, err
		}
		primitive, ok := underlying.(types.PrimitiveType)
		if !ok {
			return nil, fmt.Errorf("flags underlying type is not primitive")
		}
		return types.FlagsType{Underlying: primitive, Variants: encoded.Variants, Values: encoded.Values}, nil
	case "trait":
		trait := types.TraitType{Module: encoded.Module, Name: encoded.Name, Any: encoded.Any}
		for _, method := range encoded.Methods {
			parameters, err := decodeInterfaceTypes(method.Parameters)
			if err != nil {
				return nil, err
			}
			result, err := decodeInterfaceType(method.ReturnType)
			if err != nil {
				return nil, err
			}
			decoded := types.TraitMethod{Name: method.Name, Receiver: decodeReceiver(method.Receiver), Parameters: parameters, ReturnType: result, HasDefault: method.HasDefault}
			for _, parameter := range method.GenericParameters {
				constraint, err := decodeInterfaceType(parameter.Constraint)
				if err != nil {
					return nil, err
				}
				decoded.GenericParameters = append(decoded.GenericParameters, types.TypeParameter{Owner: parameter.Owner, Name: parameter.Name, Index: parameter.Index, Constraint: constraint})
			}
			trait.Methods = append(trait.Methods, decoded)
		}
		return trait, nil
	case "trait_pointer":
		trait, ok := types.Underlying(base).(types.TraitType)
		if !ok {
			return nil, fmt.Errorf("trait pointer base is not a trait")
		}
		return types.TraitPointerType{Trait: trait, Mutable: encoded.Mutable}, nil
	case "function":
		parameters, err := decodeInterfaceTypes(encoded.Parameters)
		if err != nil {
			return nil, err
		}
		result, err := decodeInterfaceType(encoded.ReturnType)
		if err != nil {
			return nil, err
		}
		return types.FunctionType{Parameters: parameters, ReturnType: result, TypedVariadic: encoded.TypedVariadic, VariadicElement: base}, nil
	case "multiple_return":
		values, err := decodeInterfaceTypes(encoded.Types)
		return types.MultipleReturnType{Types: values}, err
	case "opaque":
		return types.OpaqueType{}, nil
	case "no_initializer":
		return types.NoInitializerType{}, nil
	case "untyped_integer":
		return types.UntypedInt{}, nil
	case "untyped_float":
		return types.UntypedFloat{}, nil
	case "unresolved_enum":
		return types.UnresolvedEnum{}, nil
	case "error":
		return types.ErrorType{}, nil
	default:
		return nil, fmt.Errorf("unsupported cached type kind %q", encoded.Kind)
	}
}
