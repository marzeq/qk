package sema

import (
	"strings"

	"github.com/marzeq/qk/attributes"
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
			a.precollectTypeAlias(n)
		}
	}

	for _, node := range root.Body {
		if n, ok := node.(*parser.TypeAliasNode); ok && len(n.GenericParameters) != 0 {
			a.finishGenericTypeAlias(n)
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
			if len(n.GenericParameters) != 0 {
				continue
			}
			if n.Value != nil {
				a.visitExpression(n.Value)
			}
		}
	}
}

func (a *Analyser) collectFunctionSignature(n *parser.FunctionDefNode) {
	if n.IsGeneric() {
		if n.Attributes.Get(attributes.AttributeTypeForeign) != nil {
			a.errorf(n, "generic functions cannot be foreign declarations")
		}
		if n.Attributes.Get(attributes.AttributeTypeExport) != nil {
			a.errorf(n, "generic functions cannot be exported")
		}
	}
	if n.MethodOwner != "" {
		a.collectMethodSignature(n)
		return
	}
	a.collectPlainFunctionSignature(n)
}

func (a *Analyser) collectPlainFunctionSignature(n *parser.FunctionDefNode) {
	genericParameters := a.makeGenericParameters(n.Name, n.GenericParameters)
	previousBindings := a.typeParameterBindings
	if len(genericParameters) != 0 {
		a.typeParameterBindings = make(map[string]types.Type, len(genericParameters))
		for _, parameter := range genericParameters {
			a.typeParameterBindings[parameter.Name] = parameter
		}
		defer func() { a.typeParameterBindings = previousBindings }()
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
		Name:              n.Name,
		Kind:              symbols.SymbolKindFunction,
		Signature:         sig,
		Public:            n.Pub,
		Attributes:        n.Attributes,
		DefinitionModule:  a.currentMod,
		GenericParameters: genericParameters,
		Template:          len(genericParameters) != 0,
	}

	if a.defineSymbol(sym, n) {
		n.Symbol = sym
		a.functionDefinitions[sym] = &functionDefinitionInfo{node: n, module: a.currentMod}
		if sym.Template {
			a.genericFunctions[sym] = &genericFunctionInfo{
				node: n, root: a.currentRoot, module: a.currentMod,
				specializations: make(map[string]*parser.FunctionDefNode),
			}
		}
	}
}

func (a *Analyser) collectMethodSignature(n *parser.FunctionDefNode) {
	var ownerType types.Type
	ownerModule := a.currentMod
	var ownerParameters []types.TypeParameter
	if info, ok := a.aliases[n.MethodOwner]; ok {
		if len(n.MethodOwnerGenericParameters) != 0 {
			a.errorf(n, "non-generic method owner %q does not accept type parameters", n.MethodOwner)
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
		ownerType = a.resolveAlias(info, n, false)
	} else if ownerSymbol, ok := a.current.Resolve(n.MethodOwner); ok && ownerSymbol.Kind == symbols.SymbolKindType && ownerSymbol.Template {
		info := a.genericAliases[ownerSymbol]
		if info == nil {
			a.errorf(n, "unsupported generic method owner %q", n.MethodOwner)
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
		if len(n.MethodOwnerGenericParameters) != len(info.parameters) {
			a.errorf(n, "generic method owner %q must declare its %d type parameters before '.'", n.MethodOwner, len(info.parameters))
			return
		}
		ownerParameters = make([]types.TypeParameter, len(info.parameters))
		for i, parameter := range n.MethodOwnerGenericParameters {
			if parameter.Constraint != nil {
				a.errorf(parameter, "method owner type parameter %q cannot specify a constraint", parameter.Name)
				return
			}
			ownerParameters[i] = info.parameters[i]
			ownerParameters[i].Name = parameter.Name
		}
		arguments := make([]types.Type, len(ownerParameters))
		for i, parameter := range ownerParameters {
			arguments[i] = parameter
		}
		specialization := a.specializeGenericAlias(info, arguments, n, false)
		if specialization == nil {
			return
		}
		ownerType = specialization.TypeInfo
	} else if builtin, ok := a.universe.Resolve(n.MethodOwner); ok && builtin.Kind == symbols.SymbolKindType {
		if len(n.MethodOwnerGenericParameters) != 0 {
			a.errorf(n, "builtin method owner %q does not accept type parameters", n.MethodOwner)
			return
		}
		if !a.currentTrustedStandardLibrary {
			a.errorf(n, "methods on builtin type %q may only be defined by the trusted standard library", n.MethodOwner)
			return
		}
		switch builtinType := types.Underlying(builtin.TypeInfo).(type) {
		case types.PrimitiveType:
			if builtinType == types.PrimitiveVoid {
				a.errorf(n, "cannot attach method to builtin type %q", n.MethodOwner)
				return
			}
			ownerType = builtin.TypeInfo
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

	previousBindings := a.typeParameterBindings
	if len(ownerParameters) != 0 {
		a.typeParameterBindings = make(map[string]types.Type, len(ownerParameters))
		for _, parameter := range ownerParameters {
			a.typeParameterBindings[parameter.Name] = parameter
		}
	}
	methodParameterNodes := n.GenericParameters
	for _, parameter := range methodParameterNodes {
		if _, exists := a.typeParameterBindings[parameter.Name]; exists {
			a.errorf(parameter, "generic method type parameter %q conflicts with an owner type parameter", parameter.Name)
			a.typeParameterBindings = previousBindings
			return
		}
	}
	methodParameters := a.makeGenericParameters(n.MethodOwner+"."+n.Name, methodParameterNodes)
	genericParameters := append(ownerParameters, methodParameters...)
	if len(genericParameters) != 0 {
		if a.typeParameterBindings == nil {
			a.typeParameterBindings = make(map[string]types.Type, len(genericParameters))
		}
		for _, parameter := range methodParameters {
			a.typeParameterBindings[parameter.Name] = parameter
		}
		defer func() { a.typeParameterBindings = previousBindings }()
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
	receiver := types.TraitReceiverValue
	switch n.Receiver {
	case parser.MethodReceiverPointer:
		receiver = types.TraitReceiverPointer
	case parser.MethodReceiverMutablePointer:
		receiver = types.TraitReceiverMutablePointer
	}
	sym := &symbols.Symbol{Name: n.MethodOwner + "." + n.Name, Kind: symbols.SymbolKindFunction,
		Signature: &symbols.FunctionSignature{Parameters: paramTypes, RequiredParameters: requiredParameters, ReturnType: ret, Variadic: n.HasVariadic, TypedVariadic: n.TypedVariadic},
		Public:    n.Pub, Attributes: n.Attributes, Method: true, StaticMethod: n.Receiver == parser.MethodReceiverNone,
		MethodReceiver: receiver, DefinitionModule: a.currentMod, GenericParameters: genericParameters, Template: len(genericParameters) != 0}
	a.methods[key][n.Name] = sym
	if n.TypedVariadic {
		sym.Signature.VariadicElement = types.Underlying(paramTypes[len(paramTypes)-1]).(types.SliceType).Base
	}
	n.Symbol = sym
	a.functionDefinitions[sym] = &functionDefinitionInfo{node: n, module: a.currentMod}
	if sym.Template {
		a.genericFunctions[sym] = &genericFunctionInfo{
			node: n, root: a.currentRoot, module: a.currentMod,
			specializations: make(map[string]*parser.FunctionDefNode),
		}
	}
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

func (a *Analyser) precollectTypeAlias(n *parser.TypeAliasNode) {
	sym := &symbols.Symbol{
		Name: n.Name, Kind: symbols.SymbolKindType, Public: n.Pub,
		Template: len(n.GenericParameters) != 0, DefinitionModule: a.currentMod,
	}

	if !a.defineSymbol(sym, n) {
		return
	}

	n.Symbol = sym
	if sym.Template {
		return
	}

	a.aliases[n.Name] = &aliasInfo{
		node:  n,
		state: aliasUnseen,
	}
}

func (a *Analyser) finishGenericTypeAlias(n *parser.TypeAliasNode) {
	if n.Symbol == nil {
		return
	}
	genericParameters := a.makeGenericParameters(n.Name, n.GenericParameters)
	n.Symbol.GenericParameters = genericParameters
	a.genericAliases[n.Symbol] = &genericAliasInfo{
		node: n, module: a.currentMod, parameters: genericParameters,
		specializations: make(map[string]*genericAliasSpecialization),
	}
}

func untypedComptimeInteger(n *parser.DeclarationNode) (string, bool) {
	if !n.Comptime || n.TypeNode != nil {
		return "", false
	}
	literal, ok := n.Value.(*parser.IntegerLiteralNode)
	if !ok {
		return "", false
	}
	return literal.Value, true
}

func (a *Analyser) collectGlobalVariable(n *parser.DeclarationNode) {
	if len(n.GenericParameters) != 0 && !n.Comptime {
		a.errorf(n, "generic value bindings require a comptime initializer")
	}
	if len(n.GenericParameters) != 0 && n.Attributes.Get(attributes.AttributeTypeForeign) != nil {
		a.errorf(n, "generic values cannot be foreign declarations")
	}
	genericParameters := a.makeGenericParameters(n.Name, n.GenericParameters)
	previousBindings := a.typeParameterBindings
	if len(genericParameters) != 0 {
		a.typeParameterBindings = make(map[string]types.Type, len(genericParameters))
		for _, parameter := range genericParameters {
			a.typeParameterBindings[parameter.Name] = parameter
		}
		defer func() { a.typeParameterBindings = previousBindings }()
	}
	var varType types.Type

	if n.TypeNode != nil {
		varType = a.resolveTypeNode(n.TypeNode)
	}

	sym := &symbols.Symbol{
		Name:              n.Name,
		Kind:              symbols.SymbolKindVariable,
		Type:              varType,
		Mutable:           n.Mutable,
		Public:            n.Pub,
		Comptime:          n.Comptime,
		Attributes:        n.Attributes,
		GenericParameters: genericParameters,
		Template:          len(genericParameters) != 0,
	}
	if value, ok := untypedComptimeInteger(n); ok && len(genericParameters) == 0 {
		sym.InlineComptime = true
		sym.ComptimeInteger = value
	}

	if a.defineSymbol(sym, n) {
		n.Symbol = sym
		if sym.Template {
			a.genericValues[sym] = &genericValueInfo{
				node: n, root: a.currentRoot, module: a.currentMod,
				trusted: a.currentTrustedStandardLibrary, specializations: make(map[string]*parser.DeclarationNode),
			}
		}
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
