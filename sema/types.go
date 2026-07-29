package sema

import (
	"math/big"
	"strconv"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func (a *Analyser) resolveCastTarget(node parser.TypeNode) (types.Type, *types.StaticTraitView) {
	if pointer, ok := node.(*parser.PointerTypeNode); ok {
		base := a.resolveTypeNodeAt(pointer.BaseType, true)
		if trait, ok := types.Underlying(base).(types.TraitType); ok {
			access := types.TraitReceiverPointer
			if pointer.Mutable {
				access = types.TraitReceiverMutablePointer
			}
			return nil, &types.StaticTraitView{Trait: trait, Access: access}
		}
		return types.PointerType{Base: base, Mutable: pointer.Mutable}, nil
	}

	target := a.resolveTypeNode(node)
	if trait, ok := types.Underlying(target).(types.TraitType); ok {
		return nil, &types.StaticTraitView{Trait: trait, Access: types.TraitReceiverValue}
	}
	return target, nil
}

func (a *Analyser) resolveTypeNode(n parser.TypeNode) types.Type {
	return a.resolveTypeNodeAt(n, false)
}

func (a *Analyser) resolveTypeNodeAt(n parser.TypeNode, indirect bool) types.Type {
	switch t := n.(type) {
	case *parser.ReprTypeNode:
		operand := a.resolveTypeNodeAt(t.Operand, indirect)
		repr, ok := types.TaggedUnionRepr(operand)
		if !ok {
			a.errorf(t, "reprof requires an explicitly tagged union type, got %v", operand)
			return types.ErrorType{}
		}
		return repr

	case *parser.MultipleReturnTypeNode:
		result := make([]types.Type, len(t.Types))
		for i, item := range t.Types {
			result[i] = a.resolveTypeNodeAt(item, indirect)
		}
		return types.MultipleReturnType{Types: result}

	case *parser.NamedTypeNode:
		if a.resolvingTraitMethodTypes && t.ModName == "" && t.Name == "Self" {
			if len(t.TypeArguments) != 0 {
				a.errorf(t, "Self does not accept type arguments")
				return types.ErrorType{}
			}
			return types.SelfType{}
		}
		if t.ModName == "" {
			if parameter, ok := a.typeParameterBindings[t.Name]; ok {
				if len(t.TypeArguments) != 0 {
					a.errorf(t, "type parameter %q does not accept type arguments", t.Name)
					return types.ErrorType{}
				}
				return parameter
			}
		}
		if info, ok := a.aliases[t.Name]; ok && t.ModName == "" {
			if len(t.TypeArguments) != 0 {
				a.errorf(t, "non-generic type %q does not accept type arguments", t.Name)
				return types.ErrorType{}
			}
			return a.resolveAlias(info, t, indirect)
		}

		if t.ModName == "" {
			sym, ok := a.current.Resolve(t.Name)
			if !ok || sym.Kind != symbols.SymbolKindType {
				a.errorf(t, "unknown type %q", t.Name)
				return types.ErrorType{}
			}
			if sym.Template {
				info := a.genericAliases[sym]
				if info == nil {
					a.errorf(t, "unsupported generic type %q", t.Name)
					return types.ErrorType{}
				}
				arguments := a.resolveGenericArguments(t.TypeArguments)
				specialization := a.specializeGenericAlias(info, arguments, t, indirect)
				if specialization == nil {
					return types.ErrorType{}
				}
				return specialization.TypeInfo
			}
			if len(t.TypeArguments) != 0 {
				a.errorf(t, "non-generic type %q does not accept type arguments", t.Name)
				return types.ErrorType{}
			}
			return sym.TypeInfo
		}

		var mod *symbols.Module
		if modSym, ok := a.current.Resolve(t.ModName); ok && modSym.Kind == symbols.SymbolKindModule {
			mod = modSym.Module
		} else if a.modulePathAccessible(t.ModName, true) {
			mod = a.modules[t.ModName]
		}
		if mod == nil {
			a.errorf(t, "unknown module %q", t.ModName)
			return types.ErrorType{}
		}

		sym, ok := mod.Scope.Resolve(t.Name)
		if !ok || sym.Kind != symbols.SymbolKindType {
			a.errorf(t, "unknown type %q in module %q", t.Name, t.ModName)
			return types.ErrorType{}
		}
		if sym.Template {
			info := a.genericAliases[sym]
			if info == nil {
				a.errorf(t, "unsupported generic type %q", t.Name)
				return types.ErrorType{}
			}
			arguments := a.resolveGenericArguments(t.TypeArguments)
			specialization := a.specializeGenericAlias(info, arguments, t, indirect)
			if specialization == nil {
				return types.ErrorType{}
			}
			return specialization.TypeInfo
		}
		if len(t.TypeArguments) != 0 {
			a.errorf(t, "non-generic type %q does not accept type arguments", t.Name)
			return types.ErrorType{}
		}

		return sym.TypeInfo

	case *parser.PointerTypeNode:
		base := a.resolveTypeNodeAt(t.BaseType, true)
		if trait, ok := types.Underlying(base).(types.TraitType); ok {
			mutStr := ""
			if t.Mutable {
				mutStr = "mut "
			}
			a.errorf(t, "you probably meant %sdyn %v, not *%s%v", mutStr, trait, mutStr, trait)
			return types.ErrorType{}
		}
		return types.PointerType{Base: base, Mutable: t.Mutable}

	case *parser.DynTypeNode:
		base := a.resolveTypeNodeAt(t.TraitType, true)
		trait, ok := types.Underlying(base).(types.TraitType)
		if !ok {
			a.errorf(t, "dyn requires a trait type, got %v", base)
			return types.ErrorType{}
		}
		if method, reason, incompatible := trait.DynamicIncompatibility(); incompatible {
			a.errorf(t, "trait %v cannot be used dynamically because method %q %s", trait, method, reason)
			return types.ErrorType{}
		}
		return types.TraitPointerType{Trait: trait, Mutable: t.Mutable}

	case *parser.OpaqueTypeNode:
		return types.OpaqueType{}

	case *parser.TraitTypeNode:
		methods := make([]types.TraitMethod, len(t.Methods))
		previousTraitContext := a.resolvingTraitMethodTypes
		a.resolvingTraitMethodTypes = true
		for i, method := range t.Methods {
			genericParameters := a.makeGenericParameters(
				"<trait-method:"+method.Name+"@"+method.Loc.String()+">",
				method.GenericParameters,
			)
			previousBindings := a.typeParameterBindings
			if len(genericParameters) != 0 {
				bindings := make(map[string]types.Type, len(previousBindings)+len(genericParameters))
				for name, binding := range previousBindings {
					bindings[name] = binding
				}
				for _, parameter := range genericParameters {
					bindings[parameter.Name] = parameter
				}
				a.typeParameterBindings = bindings
			}
			params := make([]types.Type, len(method.Args))
			for j, arg := range method.Args {
				params[j] = a.resolveTypeNode(arg.Type)
			}
			receiver := types.TraitReceiverValue
			switch method.Receiver {
			case parser.MethodReceiverPointer:
				receiver = types.TraitReceiverPointer
			case parser.MethodReceiverMutablePointer:
				receiver = types.TraitReceiverMutablePointer
			}
			methods[i] = types.TraitMethod{
				Name: method.Name, GenericParameters: genericParameters, Receiver: receiver,
				Parameters: params, ReturnType: a.resolveTypeNode(method.ReturnType),
			}
			a.typeParameterBindings = previousBindings
		}
		a.resolvingTraitMethodTypes = previousTraitContext
		return types.TraitType{Methods: methods}

	case *parser.FunctionTypeNode:
		params := make([]types.Type, len(t.Parameters))
		for i, param := range t.Parameters {
			params[i] = a.resolveTypeNodeAt(param, indirect)
		}
		fn := types.FunctionType{Parameters: params, ReturnType: a.resolveTypeNodeAt(t.ReturnType, indirect), TypedVariadic: t.TypedVariadic}
		if t.TypedVariadic {
			fn.VariadicElement = types.Underlying(params[len(params)-1]).(types.SliceType).Base
		}
		return fn

	case *parser.SliceTypeNode:
		return types.SliceType{
			Base:    a.resolveTypeNodeAt(t.ElementType, indirect),
			Mutable: t.Mutable,
		}

	case *parser.ArrayTypeNode:
		length, ok := staticIntegerValue(t.Length)
		if !ok || !length.IsInt64() || length.Sign() < 0 || length.BitLen() >= strconv.IntSize {
			a.errorf(t.Length, "array length must be a non-negative compile-time integer")
			return types.ErrorType{}
		}
		return types.ArrayType{Base: a.resolveTypeNodeAt(t.ElementType, indirect), Length: int(length.Int64())}

	case *parser.StructTypeNode:
		for _, attr := range t.Attributes {
			if attr.GetType() != attributes.AttributeTypePacked {
				a.errorf(t, "@%s attribute does not apply to structs", attr.GetType())
			}
		}
		fields := []shared.Pair[string, types.Type]{}
		fieldNames := make(map[string]struct{})
		for _, f := range t.Fields {
			resolved := a.resolveTypeNodeAt(f.Type, indirect)
			if f.Name != "" {
				if _, exists := fieldNames[f.Name]; exists {
					a.errorf(f.Type, "duplicate struct field %q", f.Name)
				}
				fieldNames[f.Name] = struct{}{}
			} else if embedded, ok := resolved.(types.UnionType); ok {
				for _, member := range embedded.Fields {
					if _, exists := fieldNames[member.L]; exists {
						a.errorf(f.Type, "embedded union field %q conflicts with another struct field", member.L)
					}
					fieldNames[member.L] = struct{}{}
				}
			}
			fields = append(fields, shared.Pair[string, types.Type]{
				L: f.Name,
				R: resolved,
			})
		}
		return types.StructType{
			Fields: fields,
			Packed: t.Attributes.Get(attributes.AttributeTypePacked) != nil,
		}

	case *parser.EnumTypeNode:
		return types.EnumType{
			Module: t.Module, Name: t.Name,
			Variants: append([]string(nil), t.Variants...),
			Values:   append([]string(nil), t.Values...),
		}

	case *parser.FlagsTypeNode:
		base := a.resolveTypeNodeAt(t.Underlying, indirect)
		primitive, ok := types.Underlying(base).(types.PrimitiveType)
		if !ok || !types.IsInteger(primitive) || primitive == types.PrimitiveIsz || primitive == types.PrimitiveUsz {
			a.errorf(t, "flags underlying type must be a fixed-width integer")
			return types.ErrorType{}
		}
		bits := map[types.PrimitiveType]uint{types.PrimitiveI8: 8, types.PrimitiveU8: 8, types.PrimitiveI16: 16, types.PrimitiveU16: 16, types.PrimitiveI32: 32, types.PrimitiveU32: 32, types.PrimitiveI64: 64, types.PrimitiveU64: 64}[primitive]
		limit := new(big.Int).Lsh(big.NewInt(1), bits)
		for i, raw := range t.Values {
			value, _ := new(big.Int).SetString(raw, 10)
			if value.Sign() < 0 || value.Cmp(limit) >= 0 {
				a.errorf(t, "flag %q value does not fit in %s", t.Variants[i], primitive)
				return types.ErrorType{}
			}
		}
		return types.FlagsType{Underlying: primitive, Variants: append([]string(nil), t.Variants...), Values: append([]string(nil), t.Values...)}

	case *parser.UnionTypeNode:
		if t.TagType != nil || t.AutoTag {
			var tagType types.Type
			if t.AutoTag {
				variants := make([]string, len(t.Variants))
				values := make([]string, len(t.Variants))
				for i, variant := range t.Variants {
					variants[i] = variant.Name
					values[i] = strconv.Itoa(i)
				}
				tagType = types.EnumType{
					IdentityName: a.currentMod + ":auto-tag:" + t.Loc.FilePath + ":" + strconv.Itoa(t.Loc.Offset),
					Variants:     variants,
					Values:       values,
				}
			} else {
				tagType = a.resolveTypeNodeAt(t.TagType, indirect)
			}
			tagEnum, ok := types.Underlying(tagType).(types.EnumType)
			if !ok {
				a.errorf(t, "tagged union tag type must be an enum")
				return types.ErrorType{}
			}
			tagValues := make(map[string]string, len(tagEnum.Values))
			for i, value := range tagEnum.Values {
				if previous, duplicate := tagValues[value]; duplicate {
					a.errorf(t.TagType, "tag enum members %q and %q have the same value %s", previous, tagEnum.Variants[i], value)
				}
				tagValues[value] = tagEnum.Variants[i]
			}
			info := &types.TaggedUnionInfo{Tag: tagType, Auto: t.AutoTag}
			payloadFields := make([]shared.Pair[string, types.Type], 0, len(t.Variants))
			seen := make(map[string]bool, len(t.Variants))
			for _, variantNode := range t.Variants {
				tagValue, exists := tagEnum.VariantValue(variantNode.Name)
				if !exists {
					a.errorf(variantNode, "tag enum %v has no member %q", tagType, variantNode.Name)
					continue
				}
				seen[variantNode.Name] = true
				variant := types.TaggedUnionVariant{Name: variantNode.Name, TagValue: tagValue}
				named, positional := false, false
				for index, field := range variantNode.Fields {
					name := field.Name
					if name == "" {
						positional = true
						name = strconv.Itoa(index)
					} else {
						named = true
					}
					variant.Fields = append(variant.Fields, shared.Pair[string, types.Type]{
						L: name, R: a.resolveTypeNodeAt(field.Type, indirect),
					})
				}
				if named && positional {
					a.errorf(variantNode, "tagged union variant %q cannot mix named and positional payload fields", variantNode.Name)
				}
				info.Variants = append(info.Variants, variant)
				if len(variant.Fields) != 0 {
					payloadFields = append(payloadFields, shared.Pair[string, types.Type]{
						L: variant.Name, R: types.StructType{Fields: variant.Fields},
					})
				}
			}
			for _, member := range tagEnum.Variants {
				if !seen[member] {
					a.errorf(t, "tagged union is missing variant %q from tag enum %v", member, tagType)
				}
			}
			return types.StructType{
				Fields: []shared.Pair[string, types.Type]{
					{L: "$tag", R: tagType},
					{L: "$payload", R: types.UnionType{Fields: payloadFields}},
				},
				TaggedUnion: info,
			}
		}
		fields := make([]shared.Pair[string, types.Type], 0, len(t.Fields))
		for _, field := range t.Fields {
			fields = append(fields, shared.Pair[string, types.Type]{L: field.Name, R: a.resolveTypeNodeAt(field.Type, indirect)})
		}
		return types.UnionType{Module: t.Module, Name: t.Name, Fields: fields}
	}

	a.errorf(n, "unsupported type node")
	return types.ErrorType{}
}
