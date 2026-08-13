package sema

import (
	"sort"
	"strconv"
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
	aliasNames := make([]string, 0, len(a.aliases))
	for name := range a.aliases {
		aliasNames = append(aliasNames, name)
	}
	sort.Strings(aliasNames)
	for _, name := range aliasNames {
		info := a.aliases[name]
		if info.state == aliasUnseen {
			a.resolveAlias(info, info.node, false)
		}
	}

	// Resolving a generic trait use can materialize hidden default-method
	// templates in this root. Keep walking until those appended definitions
	// have received their scopes and parameter symbols too.
	for i := 0; i < len(root.Body); i++ {
		node := root.Body[i]
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
	if n.HasMethodOwner() {
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
	paramTypes := a.resolveFunctionParameterTypes(n.Args)
	requiredParameters := len(n.Args)

	for i, arg := range n.Args {
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
	if n.MethodOwnerType != nil {
		a.collectPatternMethodSignature(n)
		return
	}
	if len(n.MethodOwnerGenericParameters) != 0 {
		a.errorf(n, "generic method owners use the form (Type<T>).%s<T>(...)", n.Name)
		return
	}
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
	} else if ownerSymbol, ok := a.current.Resolve(n.MethodOwner); ok && ownerSymbol.Kind == symbols.SymbolKindType && ownerSymbol.Template {
		a.errorf(n, "generic method owner %q requires a parenthesized type pattern", n.MethodOwner)
		return
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
	if types.HasError(ownerType) {
		return
	}

	previousBindings := a.typeParameterBindings
	methodParameterNodes := n.GenericParameters
	methodParameters := a.makeGenericParameters(n.MethodOwner+"."+n.Name, methodParameterNodes)
	genericParameters := methodParameters
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
	paramTypes := a.resolveFunctionParameterTypes(n.Args)
	requiredParameters := len(n.Args)
	for i, arg := range n.Args {
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

func (a *Analyser) collectPatternMethodSignature(n *parser.FunctionDefNode) {
	genericParameters := a.makeGenericParameters(n.Name, n.GenericParameters)
	previousBindings := a.typeParameterBindings
	if len(genericParameters) != 0 {
		a.typeParameterBindings = make(map[string]types.Type, len(genericParameters))
		for _, parameter := range genericParameters {
			a.typeParameterBindings[parameter.Name] = parameter
		}
		defer func() { a.typeParameterBindings = previousBindings }()
	}

	ownerType := a.resolveTypeNode(n.MethodOwnerType)
	if types.HasError(ownerType) {
		return
	}
	ownerParameters := methodOwnerTypeParameters(ownerType)
	if len(ownerParameters) > len(genericParameters) {
		a.errorf(n, "method owner type %v uses undeclared generic parameters", ownerType)
		return
	}
	for i, ownerParameter := range ownerParameters {
		if !ownerParameter.Equals(genericParameters[i]) {
			a.errorf(n, "generic parameters used by method owner %v must be declared first and in owner order", ownerType)
			return
		}
	}
	ownerModule, ownerName, structural, valid := a.classifyPatternMethodOwner(n, ownerType)
	if !valid {
		return
	}
	if fieldExists(types.Underlying(ownerType), n.Name) {
		a.errorf(n, "cannot define method %q because owner type %v already has a field with that name", n.Name, ownerType)
		return
	}

	paramTypes := a.resolveFunctionParameterTypes(n.Args)
	requiredParameters := len(n.Args)
	for i, arg := range n.Args {
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
	displayOwner := ownerType.String()
	sym := &symbols.Symbol{Name: displayOwner + "." + n.Name, Kind: symbols.SymbolKindFunction,
		Signature: &symbols.FunctionSignature{Parameters: paramTypes, RequiredParameters: requiredParameters, ReturnType: ret, Variadic: n.HasVariadic, TypedVariadic: n.TypedVariadic},
		Public:    n.Pub, Attributes: n.Attributes, Method: true, StaticMethod: n.Receiver == parser.MethodReceiverNone,
		MethodReceiver: receiver, MethodOwnerType: ownerType, DefinitionModule: a.currentMod,
		GenericParameters: genericParameters, Template: len(genericParameters) != 0}
	if n.TypedVariadic {
		sym.Signature.VariadicElement = types.Underlying(paramTypes[len(paramTypes)-1]).(types.SliceType).Base
	}

	if structural {
		for _, existing := range a.structuralMethods[n.Name] {
			if structuralOwnerShape(existing.MethodOwnerType) == structuralOwnerShape(ownerType) {
				a.errorf(n, "method %q is already defined for structural owner %v", n.Name, ownerType)
				return
			}
		}
		a.structuralMethods[n.Name] = append(a.structuralMethods[n.Name], sym)
	} else {
		key := ownerModule + ":" + ownerName
		if a.methods[key] == nil {
			a.methods[key] = make(map[string]*symbols.Symbol)
		}
		if _, exists := a.methods[key][n.Name]; exists {
			a.errorf(n, "method %q already defined on type %v", n.Name, ownerType)
			return
		}
		a.methods[key][n.Name] = sym
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

func (a *Analyser) resolveFunctionParameterTypes(args []*parser.FunctionNodeArg) []types.Type {
	result := make([]types.Type, len(args))
	var previousNode parser.TypeNode
	var previousType types.Type
	for i, arg := range args {
		if arg.Type == nil {
			previousNode = nil
			previousType = nil
			continue
		}
		if previousNode != nil && arg.Type.GetLoc() == previousNode.GetLoc() {
			result[i] = previousType
			continue
		}
		result[i] = a.resolveTypeNode(arg.Type)
		previousNode = arg.Type
		previousType = result[i]
	}
	return result
}

func (a *Analyser) classifyPatternMethodOwner(n *parser.FunctionDefNode, ownerType types.Type) (string, string, bool, bool) {
	switch owner := ownerType.(type) {
	case types.DefinedType:
		name := owner.Name
		if owner.GenericName != "" {
			name = owner.GenericName
		}
		if owner.Module == "" {
			if !a.currentTrustedStandardLibrary {
				a.errorf(n, "methods on builtin type %v may only be defined by the trusted standard library", ownerType)
				return "", "", false, false
			}
			return "builtin", name, false, true
		}
		if owner.Module != a.currentMod {
			if !a.currentTrustedStandardLibrary {
				a.errorf(n, "cannot attach method to type %v owned by another module", ownerType)
				return "", "", false, false
			}
			return owner.Module, name, false, true
		}
		if n.Pub {
			if symbol, ok := a.current.Resolve(name); ok && !symbol.Public {
				a.errorf(n, "public method %q requires public owner type %q", n.Name, name)
				return "", "", false, false
			}
		}
		return owner.Module, name, false, true
	case *types.AliasRef:
		if owner.Module != a.currentMod {
			if !a.currentTrustedStandardLibrary {
				a.errorf(n, "cannot attach method to type %v owned by another module", ownerType)
				return "", "", false, false
			}
			return owner.Module, owner.Name, false, true
		}
		return owner.Module, owner.Name, false, true
	case types.PrimitiveType:
		if owner == types.PrimitiveVoid {
			a.errorf(n, "cannot attach method to void")
			return "", "", false, false
		}
		if !a.currentTrustedStandardLibrary {
			a.errorf(n, "methods on builtin type %v may only be defined by the trusted standard library", ownerType)
			return "", "", false, false
		}
		return "builtin", owner.String(), false, true
	default:
		if !a.currentTrustedStandardLibrary {
			a.errorf(n, "methods on structural builtin type %v may only be defined by the trusted standard library", ownerType)
			return "", "", true, false
		}
		return "builtin", structuralOwnerShape(ownerType), true, true
	}
}

func methodOwnerTypeParameters(t types.Type) []types.TypeParameter {
	result := []types.TypeParameter{}
	seen := make(map[string]bool)
	var visit func(types.Type)
	visit = func(t types.Type) {
		switch t := t.(type) {
		case types.TypeParameter:
			if !seen[t.Key()] {
				seen[t.Key()] = true
				result = append(result, t)
			}
		case types.DefinedType:
			for _, argument := range t.TypeArguments {
				visit(argument)
			}
		case types.PointerType:
			visit(t.Base)
		case types.SliceType:
			visit(t.Base)
		case types.ArrayType:
			visit(t.Base)
		case types.FunctionType:
			for _, parameter := range t.Parameters {
				visit(parameter)
			}
			visit(t.ReturnType)
		case types.MultipleReturnType:
			for _, item := range t.Types {
				visit(item)
			}
		}
	}
	visit(t)
	return result
}

func structuralOwnerShape(t types.Type) string {
	switch t := t.(type) {
	case types.TypeParameter:
		return "T"
	case types.PointerType:
		if t.Mutable {
			return "*mut " + structuralOwnerShape(t.Base)
		}
		return "*" + structuralOwnerShape(t.Base)
	case types.SliceType:
		if t.Mutable {
			return "[]mut " + structuralOwnerShape(t.Base)
		}
		return "[]" + structuralOwnerShape(t.Base)
	case types.ArrayType:
		return "[" + strconv.Itoa(t.Length) + "]" + structuralOwnerShape(t.Base)
	case types.FunctionType:
		parameters := make([]string, len(t.Parameters))
		for i, parameter := range t.Parameters {
			parameters[i] = structuralOwnerShape(parameter)
		}
		return "(" + strings.Join(parameters, ",") + "):" + structuralOwnerShape(t.ReturnType)
	default:
		return t.String()
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
		node: n, root: a.currentRoot, module: a.currentMod, parameters: genericParameters,
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
		target := name
		if i < len(n.ResolvedModules) && n.ResolvedModules[i] != "" {
			target = n.ResolvedModules[i]
		}
		mod, ok := a.modules[target]
		if !ok {
			a.errorf(n, "unknown module %q", name)
			continue
		}

		a.currentImports[target] = true
		a.currentImportAliases[name] = target
		visibleParts := strings.Split(name, ".")
		targetParts := strings.Split(target, ".")
		for visibleCount := 1; visibleCount < len(visibleParts); visibleCount++ {
			targetCount := len(targetParts) - (len(visibleParts) - visibleCount)
			if targetCount > 0 {
				a.currentImportAliases[strings.Join(visibleParts[:visibleCount], ".")] = strings.Join(targetParts[:targetCount], ".")
			}
		}
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
