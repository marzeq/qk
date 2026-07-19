package sema

import (
	"strings"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
)

func (a *Analyser) resolveIdentifier(n *parser.IdentifierNode) (*symbols.Symbol, bool) {
	if n.Module == "" {
		sym, ok := a.current.Resolve(n.Name)
		if !ok {
			a.errorf(n, "undefined identifier %q", n.Name)
			return nil, false
		}
		n.Symbol = sym
		return sym, true
	}

	var mod *symbols.Module
	if modSym, ok := a.current.Resolve(n.Module); ok && modSym.Kind == symbols.SymbolKindModule {
		mod = modSym.Module
	} else if a.modulePathAccessible(n.Module, true) {
		mod = a.modules[n.Module]
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

	if !sym.Public && n.Module != a.currentMod {
		a.errorf(n, "symbol %q is not public in module %q", sym.Name, n.Module)
		return nil, false
	}

	n.Symbol = sym
	n.ResolvedModuleName = mod.Name
	return sym, true
}

func (a *Analyser) resolveFunctionCall(n *parser.FunctionCallNode) {
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
	if mod := a.modules[candidate]; mod != nil && a.modulePathAccessible(candidate, false) {
		n.ModulePath = candidate
		return true
	}
	if !a.modulePathAccessible(path, true) {
		return false
	}
	mod := a.modules[path]
	if mod == nil {
		return false
	}
	sym, ok := mod.Scope.Resolve(n.Field.Name)
	if !ok {
		a.errorf(n, "undefined symbol %q in module %q", n.Field.Name, path)
		return true
	}
	if !sym.Public && path != a.currentMod {
		a.errorf(n, "symbol %q is not public in module %q", sym.Name, path)
		return true
	}
	n.ResolvedIdentifier = &parser.IdentifierNode{Name: n.Field.Name, Module: path, ResolvedModuleName: path, Loc: n.Field.Loc, Symbol: sym}
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
