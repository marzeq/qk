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
		resolved, ok := a.resolveGenericIdentifier(n, sym)
		if ok {
			resolved.Referenced = true
			a.rejectLambdaCapture(n, resolved)
		}
		return resolved, ok
	}

	modulePath := a.currentImportAliases[n.Module]
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
	resolved, ok := a.resolveGenericIdentifier(n, sym)
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

func (a *Analyser) resolveGenericIdentifier(n *parser.IdentifierNode, sym *symbols.Symbol) (*symbols.Symbol, bool) {
	if !sym.Template {
		if len(n.TypeArguments) != 0 {
			a.errorf(n, "non-generic binding %q does not accept type arguments", sym.Name)
			return nil, false
		}
		n.Symbol = sym
		return sym, true
	}
	if len(n.TypeArguments) == 0 {
		n.Symbol = sym
		return sym, true
	}
	arguments := a.resolveGenericArguments(n.TypeArguments)
	switch sym.Kind {
	case symbols.SymbolKindFunction:
		n.ResolvedTypeArgs = arguments
		n.Symbol = sym
		return sym, true
	case symbols.SymbolKindVariable:
		sym = a.specializeGenericValue(sym, arguments, n)
	case symbols.SymbolKindType:
		info := a.genericAliases[sym]
		sym = a.specializeGenericAlias(info, arguments, n, false)
	}
	if sym == nil {
		return nil, false
	}
	n.Symbol = sym
	return sym, true
}

func (a *Analyser) resolveFunctionCall(n *parser.FunctionCallNode) {
	if a.resolveTaggedUnionConstructor(n) {
		for _, arg := range n.Args {
			a.visitExpression(arg)
		}
		return
	}
	if n.Name != nil {
		sym, ok := a.resolveIdentifier(n.Name)
		if !ok {
			return
		}
		if sym.Kind == symbols.SymbolKindFunction {
			n.Symbol = sym
		} else {
			n.Name = nil
		}
	} else {
		a.visitExpression(n.Callee)
		if field, ok := n.Callee.(*parser.FieldAccessNode); ok && field.ResolvedIdentifier != nil {
			sym := field.ResolvedIdentifier.Symbol
			if sym.Kind == symbols.SymbolKindFunction {
				n.Symbol = sym
				n.Name = field.ResolvedIdentifier
			}
		}
	}

	for _, arg := range n.Args {
		a.visitExpression(arg)
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
	}
	if symbol == nil || symbol.Kind != symbols.SymbolKindType {
		return false
	}
	if symbol.Template {
		generic := a.genericAliases[symbol]
		if generic == nil {
			return false
		}
		arguments := make([]types.Type, len(generic.parameters))
		for i := range generic.parameters {
			arguments[i] = generic.parameters[i]
		}
		specialization := a.specializeGenericAlias(generic, arguments, n, false)
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
	resolvedCandidate := a.currentImportAliases[candidate]
	if resolvedCandidate == "" {
		resolvedCandidate = candidate
	}
	if mod := a.modules[resolvedCandidate]; mod != nil && a.modulePathAccessible(resolvedCandidate, false) {
		n.ModulePath = candidate
		return true
	}
	for imported := range a.currentImportAliases {
		if strings.HasPrefix(imported, candidate+".") {
			n.ModulePath = candidate
			return true
		}
	}
	resolvedPath := a.currentImportAliases[path]
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
	if _, ok := a.resolveGenericIdentifier(resolved, sym); !ok {
		return true
	}
	n.ResolvedIdentifier = resolved
	return true
}

func (a *Analyser) modulePathAccessible(path string, exact bool) bool {
	if a.currentImports[path] {
		return true
	}
	if exact {
		return false
	}
	for imported := range a.currentImports {
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
