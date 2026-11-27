package sema

import (
	"strings"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func (a *Analyser) resolveIdentifier(n *parser.IdentifierNode) (*symbols.Symbol, bool) {
	if n.Module == "" {
		if parameter, ok := a.typeParameterBindings[n.Name]; ok {
			if len(n.TypeArguments) != 0 {
				a.errorf(n, "type parameter %q does not accept type arguments", n.Name)
				return nil, false
			}
			sym := symbols.NewType(n.Name, parameter)
			n.Symbol = sym
			return sym, true
		}
		sym, ok := a.current.Resolve(n.Name)
		if !ok {
			a.errorf(n, "undefined identifier %q", n.Name)
			return nil, false
		}
		resolved, ok := a.resolveParameterizedIdentifier(n, sym)
		if ok {
			resolved.Referenced = true
			a.rejectLambdaCapture(n, resolved)
		}
		return resolved, ok
	}

	modulePath := a.currentState.importAliases[n.Module]
	if modulePath == "" {
		modulePath = n.Module
	}
	var mod *symbols.Module
	if modSym, ok := a.current.Resolve(n.Module); ok && modSym.Kind == symbols.SymbolKindModule {
		mod = modSym.Module
	} else if a.modulePathAccessible(modulePath, true) {
		mod = a.modules[modulePath]
	}
	if mod == nil {
		a.errorf(n, "unknown module %q", n.Module)
		return nil, false
	}

	sym, ok := mod.Scope.Resolve(n.Name)
	if !ok {
		a.errorf(n, "undefined symbol %q in module %q", n.Name, n.Module)
		return nil, false
	}

	if !sym.Public && modulePath != a.currentMod {
		a.errorf(n, "symbol %q is not public in module %q", sym.Name, n.Module)
		return nil, false
	}

	n.ResolvedModuleName = mod.Name
	resolved, ok := a.resolveParameterizedIdentifier(n, sym)
	if ok {
		resolved.Referenced = true
	}
	return resolved, ok
}

func (a *Analyser) rejectLambdaCapture(n *parser.IdentifierNode, sym *symbols.Symbol) {
	if len(a.lambdaOwnedSymbols) == 0 || sym == nil || sym.Kind != symbols.SymbolKindVariable {
		return
	}
	owned := a.lambdaOwnedSymbols[len(a.lambdaOwnedSymbols)-1]
	if owned[sym] {
		return
	}
	if module := a.modules[a.currentMod]; module != nil {
		if global, ok := module.Scope.Symbols[sym.Name]; ok && global == sym {
			return
		}
	}
	a.errorf(n, "lambda cannot capture local variable %q", sym.Name)
}

func (a *Analyser) resolveParameterizedIdentifier(n *parser.IdentifierNode, sym *symbols.Symbol) (*symbols.Symbol, bool) {
	if !sym.Template {
		if len(n.TypeArguments) != 0 {
			a.errorf(n, "non-parameterized binding %q does not accept type arguments", sym.Name)
			return nil, false
		}
		n.Symbol = sym
		return sym, true
	}
	if len(n.TypeArguments) == 0 {
		n.Symbol = sym
		return sym, true
	}
	arguments := a.resolveParameterizedArguments(n.TypeArguments)
	switch sym.Kind {
	case symbols.SymbolKindFunction:
		n.ResolvedTypeArgs = arguments
		n.Symbol = sym
		return sym, true
	case symbols.SymbolKindVariable:
		sym = a.specializeParameterizedValue(sym, arguments, n)
	case symbols.SymbolKindType:
		info := a.parameterizedAliases[sym]
		sym = a.specializeParameterizedAlias(info, arguments, n, false)
	}
	if sym == nil {
		return nil, false
	}
	n.Symbol = sym
	return sym, true
}

func (a *Analyser) resolveFunctionCall(n *parser.FunctionCallNode) {
	if n.CompileTimeApplication != nil {
		return
	}
	if a.resolveTaggedUnionConstructor(n) {
		for _, arg := range n.Args {
			a.visitExpression(arg)
		}
		return
	}
	name := n.Name
	if name != nil {
		if _, ok := a.resolveIdentifier(name); !ok {
			return
		}
	} else {
		a.visitExpression(n.Callee)
		switch callee := n.Callee.(type) {
		case *parser.IdentifierNode:
			name = callee
		case *parser.FieldAccessNode:
			name = callee.ResolvedIdentifier
		}
	}
	if name != nil && name.Symbol != nil {
		sym := name.Symbol
		if a.resolveCompileTimeApplication(n, sym, name) {
			return
		}
		if sym.Kind != symbols.SymbolKindFunction && sym.Kind != symbols.SymbolKindVariable {
			a.errorf(n, "cannot call %q: expected a function or function pointer", name.String())
			return
		}
		if sym.Kind == symbols.SymbolKindFunction {
			n.Symbol = sym
			if n.Name != nil && sym.Template {
				arguments, remaining, explicit, valid := a.explicitCallTypeArguments(sym.ParameterizedParameters, sym.Signature, n, false)
				if !valid {
					return
				}
				if explicit {
					n.Args = remaining
					n.ArgumentTypes = nil
					name.ResolvedTypeArgs = arguments
					n.Symbol = dependentParameterizedFunctionSymbol(sym, arguments)
					name.Symbol = n.Symbol
				}
			}
			n.Name = name
		} else {
			n.Name = nil
		}
	}

	for _, arg := range n.Args {
		a.visitExpression(arg)
	}
}

func (a *Analyser) resolveCompileTimeApplication(
	call *parser.FunctionCallNode,
	template *symbols.Symbol,
	name *parser.IdentifierNode,
) bool {
	if !template.Template || template.Kind != symbols.SymbolKindType && template.Kind != symbols.SymbolKindVariable {
		return false
	}
	parameters := template.ParameterizedParameters
	if len(call.Args) != len(parameters) {
		a.errorf(call, "parameterized binding expects %d type arguments, got %d", len(parameters), len(call.Args))
		return true
	}
	typeArguments := make([]parser.TypeNode, len(call.Args))
	for i, argument := range call.Args {
		if valueType := parameters[i].ValueType; valueType != nil {
			a.visitExpression(argument)
			value, valid := retainedArgumentValue(argument, valueType)
			if !valid {
				a.errorf(argument, "argument for $%s must be a compile-time value of type %v", parameters[i].Name, valueType)
				return true
			}
			typeArguments[i] = &parser.ResolvedTypeNode{Value: value, Loc: argument.GetLoc()}
			continue
		}
		typeNode, valid := callArgumentAsTypeNode(call, i)
		if !valid {
			a.errorf(argument, "compile-time binding %q requires type arguments", template.Name)
			return true
		}
		typeArguments[i] = typeNode
	}
	application := &parser.IdentifierNode{
		Name: name.Name, Module: name.Module, TypeArguments: typeArguments, Loc: call.Loc,
	}
	resolved, valid := a.resolveParameterizedIdentifier(application, template)
	if !valid {
		return true
	}
	application.Symbol = resolved
	call.CompileTimeApplication = application
	call.Symbol = resolved
	if resolved.Kind == symbols.SymbolKindType {
		call.SetType(types.MetaType{})
	} else {
		call.SetType(resolved.Type)
	}
	call.Args = nil
	call.ArgumentTypes = nil
	return true
}

func (a *Analyser) explicitCallTypeArguments(
	parameters []types.TypeParameter,
	signature *symbols.FunctionSignature,
	call *parser.FunctionCallNode,
	receiverOmitted bool,
) ([]types.Type, []parser.ExpressionNode, bool, bool) {
	arguments := call.Args
	if len(parameters) == 0 || signature == nil {
		return nil, arguments, false, true
	}
	required := signature.RequiredParameters
	maximum := len(signature.Parameters)
	if receiverOmitted {
		required--
		maximum--
	}
	validRuntimeArity := func(count int) bool {
		if signature.Variadic || signature.TypedVariadic {
			return count >= required
		}
		return count >= required && count <= maximum
	}
	explicitCount := 0
	limit := min(len(parameters), len(arguments))
	for count := limit; count > 0; count-- {
		if !validRuntimeArity(len(arguments) - count) {
			continue
		}
		allTypes := true
		for index := range count {
			if _, ok := callArgumentAsTypeNode(call, index); !ok {
				allTypes = false
				break
			}
		}
		if allTypes {
			explicitCount = count
			break
		}
	}
	if explicitCount == 0 {
		return nil, arguments, false, true
	}

	result := make([]types.Type, len(parameters))
	for i, parameter := range parameters {
		result[i] = parameter
		if i < explicitCount {
			node, _ := callArgumentAsTypeNode(call, i)
			result[i] = a.resolveTypeNode(node)
		}
	}
	return result, arguments[explicitCount:], true, true
}

func callArgumentAsTypeNode(call *parser.FunctionCallNode, index int) (parser.TypeNode, bool) {
	if node, ok := expressionAsTypeNode(call.Args[index]); ok {
		return node, true
	}
	if index < len(call.ArgumentTypes) {
		node := call.ArgumentTypes[index]
		return node, node != nil
	}
	return nil, false
}

func expressionAsTypeNode(expression parser.ExpressionNode) (parser.TypeNode, bool) {
	switch n := expression.(type) {
	case *parser.TypeValueExprNode:
		if n.ValueType != nil {
			return &parser.ResolvedTypeNode{Value: n.ValueType, Loc: n.Loc}, true
		}
		return n.Value, n.Value != nil
	case *parser.TypePointerExprNode:
		base, ok := expressionAsTypeNode(n.Base)
		if !ok {
			return nil, false
		}
		return &parser.PointerTypeNode{BaseType: base, Mutable: n.Mutable, Loc: n.Loc}, true
	case *parser.IdentifierNode:
		return &parser.NamedTypeNode{ModName: n.Module, Name: n.Name, TypeArguments: n.TypeArguments, Loc: n.Loc}, true
	case *parser.FieldAccessNode:
		parts := []string{n.Field.Name}
		subject := n.Subject
		for {
			switch current := subject.(type) {
			case *parser.IdentifierNode:
				parts = append([]string{current.Name}, parts...)
				if len(parts) < 2 {
					return nil, false
				}
				return &parser.NamedTypeNode{ModName: strings.Join(parts[:len(parts)-1], "."), Name: parts[len(parts)-1], TypeArguments: n.Field.TypeArguments, Loc: n.Loc}, true
			case *parser.FieldAccessNode:
				parts = append([]string{current.Field.Name}, parts...)
				subject = current.Subject
			default:
				return nil, false
			}
		}
	case *parser.FunctionCallNode:
		if identifier := resolvedTypeIdentifier(n); identifier != nil {
			return &parser.ResolvedTypeNode{Value: identifier.Symbol.TypeInfo, Loc: n.Loc}, true
		}
		var target *parser.NamedTypeNode
		if n.Name != nil {
			target = &parser.NamedTypeNode{ModName: n.Name.Module, Name: n.Name.Name}
		} else {
			var ok bool
			target, ok = typeApplicationName(n.Callee)
			if !ok {
				return nil, false
			}
		}
		arguments := make([]parser.TypeNode, len(n.Args))
		for i := range n.Args {
			if constant := constantTypeArgument(n.Args[i]); constant != nil {
				arguments[i] = constant
				continue
			}
			converted, ok := callArgumentAsTypeNode(n, i)
			if !ok {
				return nil, false
			}
			arguments[i] = converted
		}
		target.TypeArguments = arguments
		target.Loc = n.Loc
		return target, true
	default:
		return nil, false
	}
}

func constantTypeArgument(expression parser.ExpressionNode) *parser.ConstantTypeNode {
	switch expression.(type) {
	case *parser.CompileTimeExpressionNode, *parser.IntegerLiteralNode, *parser.FloatLiteralNode,
		*parser.StringLiteralNode, *parser.CStringLiteralNode, *parser.BoolLiteralNode:
		return &parser.ConstantTypeNode{Value: expression, Loc: expression.GetLoc()}
	default:
		return nil
	}
}

func (a *Analyser) resolveTaggedUnionConstructor(n *parser.FunctionCallNode) bool {
	member, ok := n.Callee.(*parser.FieldAccessNode)
	if !ok {
		return false
	}
	var symbol *symbols.Symbol
	switch owner := member.Subject.(type) {
	case *parser.IdentifierNode:
		resolved, ok := a.resolveIdentifier(owner)
		if !ok {
			return false
		}
		symbol = resolved
	case *parser.FieldAccessNode:
		a.visitExpression(owner)
		if owner.ResolvedIdentifier != nil {
			symbol = owner.ResolvedIdentifier.Symbol
		}
	case *parser.FunctionCallNode:
		a.visitExpression(owner)
		if owner.CompileTimeApplication != nil {
			symbol = owner.CompileTimeApplication.Symbol
		}
	}
	if symbol == nil || symbol.Kind != symbols.SymbolKindType {
		return false
	}
	if symbol.Template {
		parameterized := a.parameterizedAliases[symbol]
		if parameterized == nil {
			return false
		}
		arguments := make([]types.Type, len(parameterized.parameters))
		for i := range parameterized.parameters {
			arguments[i] = parameterized.parameters[i]
		}
		specialization := a.specializeParameterizedAlias(parameterized, arguments, n, false)
		if specialization == nil {
			return true
		}
		n.TaggedUnionTemplate = symbol
		symbol = specialization
	}
	info, tagged := types.TaggedUnion(symbol.TypeInfo)
	if !tagged {
		return false
	}
	_, index, exists := info.Variant(member.Field.Name)
	if !exists {
		return false
	}
	n.TaggedUnionType = symbol.TypeInfo
	n.TaggedUnionVariant = index
	n.SetType(symbol.TypeInfo)
	return true
}

func (a *Analyser) resolveModuleField(n *parser.FieldAccessNode) bool {
	path := ""
	switch subject := n.Subject.(type) {
	case *parser.IdentifierNode:
		if subject.Symbol != nil && subject.Symbol.Kind == symbols.SymbolKindModule {
			path = subject.Symbol.Module.Name
		}
	case *parser.FieldAccessNode:
		path = subject.ModulePath
	}
	if path == "" {
		return false
	}

	candidate := path + "." + n.Field.Name
	resolvedCandidate := a.currentState.importAliases[candidate]
	if resolvedCandidate == "" {
		resolvedCandidate = candidate
	}
	if mod := a.modules[resolvedCandidate]; mod != nil && a.modulePathAccessible(resolvedCandidate, false) {
		n.ModulePath = candidate
		return true
	}
	for imported := range a.currentState.importAliases {
		if strings.HasPrefix(imported, candidate+".") {
			n.ModulePath = candidate
			return true
		}
	}
	resolvedPath := a.currentState.importAliases[path]
	if resolvedPath == "" {
		resolvedPath = path
	}
	if !a.modulePathAccessible(resolvedPath, true) {
		return false
	}
	mod := a.modules[resolvedPath]
	if mod == nil {
		return false
	}
	sym, ok := mod.Scope.Resolve(n.Field.Name)
	if !ok {
		a.errorf(n, "undefined symbol %q in module %q", n.Field.Name, path)
		return true
	}
	if !sym.Public && resolvedPath != a.currentMod {
		a.errorf(n, "symbol %q is not public in module %q", sym.Name, path)
		return true
	}
	resolved := &parser.IdentifierNode{
		Name: n.Field.Name, Module: resolvedPath, ResolvedModuleName: resolvedPath, Loc: n.Field.Loc,
		TypeArguments: n.Field.TypeArguments,
	}
	if _, ok := a.resolveParameterizedIdentifier(resolved, sym); !ok {
		return true
	}
	if resolved.Symbol != nil {
		resolved.Symbol.Referenced = true
	}
	n.ResolvedIdentifier = resolved
	return true
}

func (a *Analyser) modulePathAccessible(path string, exact bool) bool {
	if a.currentState.imports[path] {
		return true
	}
	if exact {
		return false
	}
	for imported := range a.currentState.imports {
		if strings.HasPrefix(imported, path+".") {
			return true
		}
	}
	return false
}

func (a *Analyser) visitStructLiteral(n *parser.StructLiteralNode) {
	if n.Name != nil {
		sym, ok := a.resolveIdentifier(n.Name)
		if !ok {
			return
		}

		if sym.Kind != symbols.SymbolKindType {
			a.errorf(n, "%q is not a type", sym.Name)
			return
		}

		n.Symbol = sym
	}

	for _, field := range n.Fields {
		a.visitExpression(field.R)
	}
}
