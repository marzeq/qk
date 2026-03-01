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
			Base: a.resolveTypeNode(t.BaseType),
		}

	case *parser.ArrayTypeNode:
		return types.ArrayType{
			Base: a.resolveTypeNode(t.ElementType),
			Size: t.Size,
		}

	case *parser.StructTypeNode:
		fields := []shared.Pair[string, types.Type]{}
		for _, f := range t.Fields {
			fields = append(fields, shared.Pair[string, types.Type]{
				L: f.Name,
				R: a.resolveTypeNode(f.Type),
			})
		}
		return types.StructType{
			Fields:  fields,
			Ordered: true,
		}
	}

	a.errorf(n, "unsupported type node")
	return types.ErrorType{}
}
