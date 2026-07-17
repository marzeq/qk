package sema

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func (a *Analyser) resolveTypeNode(n parser.TypeNode) types.Type {
	switch t := n.(type) {

	case *parser.NamedTypeNode:
		if info, ok := a.aliases[t.Name]; ok {
			return a.resolveAlias(info, t)
		}

		if t.ModName == "" {
			sym, ok := a.current.Resolve(t.Name)
			if !ok || sym.Kind != symbols.SymbolKindType {
				a.errorf(t, "unknown type %q", t.Name)
				return types.ErrorType{}
			}
			return sym.TypeInfo
		}

		modSym, ok := a.current.Resolve(t.ModName)
		if !ok || modSym.Kind != symbols.SymbolKindModule {
			a.errorf(t, "unknown module %q", t.ModName)
			return types.ErrorType{}
		}

		sym, ok := modSym.Module.Scope.Resolve(t.Name)
		if !ok || sym.Kind != symbols.SymbolKindType {
			a.errorf(t, "unknown type %q in module %q", t.Name, t.ModName)
			return types.ErrorType{}
		}

		return sym.TypeInfo

	case *parser.PointerTypeNode:
		return types.PointerType{
			Base:    a.resolveTypeNode(t.BaseType),
			Mutable: t.Mutable,
		}

	case *parser.FunctionTypeNode:
		params := make([]types.Type, len(t.Parameters))
		for i, param := range t.Parameters {
			params[i] = a.resolveTypeNode(param)
		}
		return types.FunctionType{Parameters: params, ReturnType: a.resolveTypeNode(t.ReturnType)}

	case *parser.SliceTypeNode:
		return types.SliceType{
			Base: a.resolveTypeNode(t.ElementType),
			Size: t.Size,
		}

	case *parser.StructTypeNode:
		fields := []shared.Pair[string, types.Type]{}
		fieldNames := make(map[string]struct{})
		for _, f := range t.Fields {
			resolved := a.resolveTypeNode(f.Type)
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

	case *parser.UnionTypeNode:
		fields := make([]shared.Pair[string, types.Type], 0, len(t.Fields))
		for _, field := range t.Fields {
			fields = append(fields, shared.Pair[string, types.Type]{L: field.Name, R: a.resolveTypeNode(field.Type)})
		}
		return types.UnionType{Module: t.Module, Name: t.Name, Fields: fields}
	}

	a.errorf(n, "unsupported type node")
	return types.ErrorType{}
}
