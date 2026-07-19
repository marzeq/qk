package sema

import (
	"strings"

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
		switch n := node.(type) {
		case *parser.FunctionDefNode:
			a.visitFunction(n)
		case *parser.DeclarationNode:
			if n.Value != nil {
				a.visitExpression(n.Value)
			}
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
	requiredParameters := len(n.Args)

	for i, arg := range n.Args {
		paramTypes[i] = a.resolveTypeNode(arg.Type)
		if arg.Default != nil && requiredParameters == len(n.Args) {
			requiredParameters = i
		}
	}
	if n.TypedVariadic && requiredParameters == len(n.Args) {
		requiredParameters--
	}

	var retType types.Type
	if n.RetTypeNode != nil {
		retType = a.resolveTypeNode(n.RetTypeNode)
	}

	sig := &symbols.FunctionSignature{
		Parameters:         paramTypes,
		RequiredParameters: requiredParameters,
		ReturnType:         retType,
		Variadic:           n.HasVariadic,
		TypedVariadic:      n.TypedVariadic,
	}
	if n.TypedVariadic {
		sig.VariadicElement = types.Underlying(paramTypes[len(paramTypes)-1]).(types.SliceType).Base
	}

	sym := &symbols.Symbol{
		Name:             n.Name,
		Kind:             symbols.SymbolKindFunction,
		Signature:        sig,
		Public:           n.Pub,
		Attributes:       n.Attributes,
		DefinitionModule: a.currentMod,
	}

	if a.defineSymbol(sym, n) {
		n.Symbol = sym
	}
}

func (a *Analyser) collectMethodSignature(n *parser.FunctionDefNode) {
	var ownerType types.Type
	ownerModule := a.currentMod
	if info, ok := a.aliases[n.MethodOwner]; ok {
		if info.node.Transparent {
			a.errorf(n, "cannot attach method to type alias %q", n.MethodOwner)
			return
		}
		if n.Pub && !info.node.Pub {
			a.errorf(n, "public method %q requires public owner type %q", n.Name, n.MethodOwner)
			return
		}
		ownerType = a.resolveAlias(info, n, false)
	} else if builtin, ok := a.universe.Resolve(n.MethodOwner); ok && builtin.Kind == symbols.SymbolKindType {
		if !a.currentTrustedStandardLibrary {
			a.errorf(n, "methods on builtin type %q may only be defined by the trusted standard library", n.MethodOwner)
			return
		}
		switch builtinType := builtin.TypeInfo.(type) {
		case types.PrimitiveType:
			if builtinType == types.PrimitiveVoid {
				a.errorf(n, "cannot attach method to builtin type %q", n.MethodOwner)
				return
			}
			ownerType = builtinType
		case types.SliceType, types.PointerType:
			ownerType = builtin.TypeInfo
		default:
			a.errorf(n, "cannot attach method to builtin type %q", n.MethodOwner)
			return
		}
		ownerModule = "builtin"
	} else {
		a.errorf(n, "cannot attach method to unknown or imported type %q", n.MethodOwner)
		return
	}
	if fieldExists(types.Underlying(ownerType), n.Name) {
		a.errorf(n, "cannot define method %q because type %q already has a field with that name", n.Name, n.MethodOwner)
		return
	}
	key := ownerModule + ":" + n.MethodOwner
	if a.methods[key] == nil {
		a.methods[key] = make(map[string]*symbols.Symbol)
	}
	if _, exists := a.methods[key][n.Name]; exists {
		a.errorf(n, "method %q already defined on type %q", n.Name, n.MethodOwner)
		return
	}
	paramTypes := make([]types.Type, len(n.Args))
	requiredParameters := len(n.Args)
	for i, arg := range n.Args {
		paramTypes[i] = a.resolveTypeNode(arg.Type)
		if arg.Default != nil && requiredParameters == len(n.Args) {
			requiredParameters = i
		}
	}
	if n.TypedVariadic && requiredParameters == len(n.Args) {
		requiredParameters--
	}
	var ret types.Type
	if n.RetTypeNode != nil {
		ret = a.resolveTypeNode(n.RetTypeNode)
	}
	sym := &symbols.Symbol{Name: n.MethodOwner + "." + n.Name, Kind: symbols.SymbolKindFunction,
		Signature: &symbols.FunctionSignature{Parameters: paramTypes, RequiredParameters: requiredParameters, ReturnType: ret, Variadic: n.HasVariadic, TypedVariadic: n.TypedVariadic},
		Public:    n.Pub, Attributes: n.Attributes, Method: true, StaticMethod: n.Receiver == parser.MethodReceiverNone,
		DefinitionModule: a.currentMod}
	a.methods[key][n.Name] = sym
	if n.TypedVariadic {
		sym.Signature.VariadicElement = types.Underlying(paramTypes[len(paramTypes)-1]).(types.SliceType).Base
	}
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

		a.currentImports[name] = true
		alias := strings.Split(name, ".")[0]
		if i < len(n.Aliases) && n.Aliases[i] != "" {
			alias = n.Aliases[i]
		} else if strings.Contains(name, ".") {
			prefix := strings.Split(name, ".")[0]
			mod = &symbols.Module{Name: prefix, Scope: symbols.NewScope(a.universe)}
		}
		sym := &symbols.Symbol{
			Name:   alias,
			Kind:   symbols.SymbolKindModule,
			Module: mod,
		}

		if existing, ok := a.current.Resolve(alias); ok && existing.Kind == symbols.SymbolKindModule && existing.Module.Name == mod.Name {
			continue
		}
		a.defineSymbol(sym, n)
	}
}
