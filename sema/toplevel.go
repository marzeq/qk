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
			a.resolveAlias(info, info.node, false)
		}
	}

	for _, node := range root.Body {
		if fn, ok := node.(*parser.FunctionDefNode); ok {
			a.visitFunction(fn)
		}
	}
}

func (a *Analyser) collectFunctionSignature(n *parser.FunctionDefNode) {
	if n.MethodOwner != "" {
		a.collectMethodSignature(n)
		return
	}
	a.collectPlainFunctionSignature(n)
}

func (a *Analyser) collectPlainFunctionSignature(n *parser.FunctionDefNode) {
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

func (a *Analyser) collectMethodSignature(n *parser.FunctionDefNode) {
	info, ok := a.aliases[n.MethodOwner]
	if !ok {
		a.errorf(n, "cannot attach method to unknown or imported type %q", n.MethodOwner)
		return
	}
	if info.node.Transparent {
		a.errorf(n, "cannot attach method to type alias %q", n.MethodOwner)
		return
	}
	if n.Pub && !info.node.Pub {
		a.errorf(n, "public method %q requires public owner type %q", n.Name, n.MethodOwner)
		return
	}
	ownerType := a.resolveAlias(info, n, false)
	if fieldExists(types.Underlying(ownerType), n.Name) {
		a.errorf(n, "cannot define method %q because type %q already has a field with that name", n.Name, n.MethodOwner)
		return
	}
	key := a.currentMod + ":" + n.MethodOwner
	if a.methods[key] == nil {
		a.methods[key] = make(map[string]*symbols.Symbol)
	}
	if _, exists := a.methods[key][n.Name]; exists {
		a.errorf(n, "method %q already defined on type %q", n.Name, n.MethodOwner)
		return
	}
	paramTypes := make([]types.Type, len(n.Args))
	for i, arg := range n.Args {
		paramTypes[i] = a.resolveTypeNode(arg.Type)
	}
	var ret types.Type
	if n.RetTypeNode != nil {
		ret = a.resolveTypeNode(n.RetTypeNode)
	}
	sym := &symbols.Symbol{Name: n.MethodOwner + "." + n.Name, Kind: symbols.SymbolKindFunction,
		Signature: &symbols.FunctionSignature{Parameters: paramTypes, ReturnType: ret, Variadic: n.HasVariadic},
		Public:    n.Pub, Attributes: n.Attributes, StaticMethod: n.Receiver == parser.MethodReceiverNone}
	a.methods[key][n.Name] = sym
	n.Symbol = sym
}

func fieldExists(t types.Type, name string) bool {
	switch t := t.(type) {
	case types.StructType:
		for _, field := range t.Fields {
			if field.L == name {
				return true
			}
			if field.L == "" && fieldExists(types.Underlying(field.R), name) {
				return true
			}
		}
	case types.UnionType:
		for _, field := range t.Fields {
			if field.L == name {
				return true
			}
		}
	}
	return false
}

func (a *Analyser) collectTypeAlias(n *parser.TypeAliasNode) {
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
