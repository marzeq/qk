package sema

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
	"math/big"
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
	case *parser.MultipleReturnTypeNode:
		result := make([]types.Type, len(t.Types))
		for i, item := range t.Types {
			result[i] = a.resolveTypeNodeAt(item, indirect)
		}
		return types.MultipleReturnType{Types: result}

	case *parser.NamedTypeNode:
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
		return types.TraitPointerType{Trait: trait, Mutable: t.Mutable}

	case *parser.OpaqueTypeNode:
		return types.OpaqueType{}

	case *parser.TraitTypeNode:
		methods := make([]types.TraitMethod, len(t.Methods))
		for i, method := range t.Methods {
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
			methods[i] = types.TraitMethod{Name: method.Name, Receiver: receiver, Parameters: params, ReturnType: a.resolveTypeNode(method.ReturnType)}
		}
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
			Base: a.resolveTypeNodeAt(t.ElementType, indirect),
			Size: t.Size,
		}

	case *parser.StructTypeNode:
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
		fields := make([]shared.Pair[string, types.Type], 0, len(t.Fields))
		for _, field := range t.Fields {
			fields = append(fields, shared.Pair[string, types.Type]{L: field.Name, R: a.resolveTypeNodeAt(field.Type, indirect)})
		}
		return types.UnionType{Module: t.Module, Name: t.Name, Fields: fields}
	}

	a.errorf(n, "unsupported type node")
	return types.ErrorType{}
}
