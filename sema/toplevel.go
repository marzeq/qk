package sema

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func (a *Analyser) collectTopLevel(root *parser.RootNode) {
	for _, node := range root.Body {
		switch n := node.(type) {

		case *parser.FunctionDefNode:
			a.collectFunctionSignature(n)

		case *parser.TypeAliasNode:
			a.collectTypeAlias(n)

		case *parser.DeclarationNode:
			a.collectGlobalVariable(n)

		case *parser.ImportNode:
			a.collectImport(n)
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
		Name:      n.Name,
		Kind:      symbols.SymbolKindFunction,
		Signature: sig,
	}

	if a.defineSymbol(sym, n) {
		n.Symbol = sym
	}
}

func (a *Analyser) collectTypeAlias(n *parser.TypeAliasNode) {
	sym := &symbols.Symbol{
		Name: n.Name,
		Kind: symbols.SymbolKindType,
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
		Name: n.Name,
		Kind: symbols.SymbolKindVariable,
		Type: varType,
	}

	if a.defineSymbol(sym, n) {
		n.Symbol = sym
	}
}

func (a *Analyser) collectImport(n *parser.ImportNode) {
	for _, name := range n.Modules {
		mod, ok := a.modules[name]
		if !ok {
			a.errorf(n, "unknown module %q", name)
			continue
		}

		sym := &symbols.Symbol{
			Name:   name,
			Kind:   symbols.SymbolKindModule,
			Module: mod,
		}

		a.defineSymbol(sym, n)
	}
}
