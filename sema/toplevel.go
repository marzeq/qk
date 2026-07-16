package sema

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func (a *Analyser) collectTopLevel(root *parser.RootNode) {
	for _, node := range root.Body {
		if n, ok := node.(*parser.ImportNode); ok {
			a.collectImport(n)
		}
	}

	for _, node := range root.Body {
		if n, ok := node.(*parser.TypeAliasNode); ok {
			a.collectTypeAlias(n)
		}
	}

	for _, node := range root.Body {
		switch n := node.(type) {
		case *parser.FunctionDefNode:
			a.collectFunctionSignature(n)
		case *parser.DeclarationNode:
			a.collectGlobalVariable(n)
		}
	}
}

func (a *Analyser) resolveBodies(root *parser.RootNode) {
	for _, info := range a.aliases {
		if info.state == aliasUnseen {
			a.resolveAlias(info, info.node)
		}
	}

	for _, node := range root.Body {
		if fn, ok := node.(*parser.FunctionDefNode); ok {
			a.visitFunction(fn)
		}
	}
}

func (a *Analyser) collectFunctionSignature(n *parser.FunctionDefNode) {
	paramTypes := make([]types.Type, len(n.Args))

	for i, arg := range n.Args {
		paramTypes[i] = a.resolveTypeNode(arg.Type)
	}

	var retType types.Type
	if n.RetTypeNode != nil {
		retType = a.resolveTypeNode(n.RetTypeNode)
	}

	sig := &symbols.FunctionSignature{
		Parameters: paramTypes,
		ReturnType: retType,
		Variadic:   n.HasVariadic,
	}

	sym := &symbols.Symbol{
		Name:       n.Name,
		Kind:       symbols.SymbolKindFunction,
		Signature:  sig,
		Public:     n.Pub,
		Attributes: n.Attributes,
	}

	if a.defineSymbol(sym, n) {
		n.Symbol = sym
	}
}

func (a *Analyser) collectTypeAlias(n *parser.TypeAliasNode) {
	if enum, ok := n.Type.(*parser.EnumTypeNode); ok {
		enum.Name = n.Name
		enum.Module = a.currentMod
	}
	sym := &symbols.Symbol{
		Name:   n.Name,
		Kind:   symbols.SymbolKindType,
		Public: n.Pub,
	}

	if !a.defineSymbol(sym, n) {
		return
	}

	n.Symbol = sym

	a.aliases[n.Name] = &aliasInfo{
		node:  n,
		state: aliasUnseen,
	}
}

func (a *Analyser) collectGlobalVariable(n *parser.DeclarationNode) {
	var varType types.Type

	if n.TypeNode != nil {
		varType = a.resolveTypeNode(n.TypeNode)
	}

	sym := &symbols.Symbol{
		Name:       n.Name,
		Kind:       symbols.SymbolKindVariable,
		Type:       varType,
		Mutable:    n.Mutable,
		Public:     n.Pub,
		Attributes: n.Attributes,
	}

	if a.defineSymbol(sym, n) {
		n.Symbol = sym
	}
}

func (a *Analyser) collectImport(n *parser.ImportNode) {
	for i, name := range n.Modules {
		mod, ok := a.modules[name]
		if !ok {
			a.errorf(n, "unknown module %q", name)
			continue
		}

		alias := name
		if i < len(n.Aliases) && n.Aliases[i] != "" {
			alias = n.Aliases[i]
		}
		sym := &symbols.Symbol{
			Name:   alias,
			Kind:   symbols.SymbolKindModule,
			Module: mod,
		}

		a.defineSymbol(sym, n)
	}
}
