package sema

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
)

func (a *Analyser) resolveIdentifier(n *parser.IdentifierNode) {
	sym, ok := a.current.Resolve(n.Name)
	if !ok {
		a.errorf(n, "undefined identifier %q", n.Name)
		return
	}
	n.Symbol = sym
}

func (a *Analyser) resolveModuleAccess(n *parser.ModuleAccessNode) (*symbols.Symbol, bool) {
	if n.ModName == "" {
		sym, ok := a.current.Resolve(n.Ident.Name)
		if !ok {
			a.errorf(n, "undefined identifier %q", n.Ident.Name)
			return nil, false
		}
		n.Symbol = sym
		return sym, true
	}

	modSym, ok := a.current.Resolve(n.ModName)
	if !ok || modSym.Kind != symbols.SymbolKindModule {
		a.errorf(n, "unknown module %q", n.ModName)
		return nil, false
	}

	sym, ok := modSym.Module.Scope.Resolve(n.Ident.Name)
	if !ok {
		a.errorf(n, "undefined symbol %q in module %q", n.Ident.Name, n.ModName)
		return nil, false
	}

	n.Symbol = sym
	return sym, true
}

func (a *Analyser) resolveFunctionCall(n *parser.FunctionCallNode) {
	sym, ok := a.resolveModuleAccess(n.Name)
	if !ok {
		return
	}

	if sym.Kind != symbols.SymbolKindFunction {
		a.errorf(n, "%q is not a function", sym.Name)
		return
	}

	n.Symbol = sym

	for _, arg := range n.Args {
		a.visitExpression(arg)
	}
}

func (a *Analyser) visitStructLiteral(n *parser.StructLiteralNode) {
	sym, ok := a.resolveModuleAccess(n.Name)
	if !ok {
		return
	}

	if sym.Kind != symbols.SymbolKindType {
		a.errorf(n, "%q is not a type", sym.Name)
		return
	}

	n.Symbol = sym

	for _, field := range n.Fields {
		a.visitExpression(field.R)
	}
}
