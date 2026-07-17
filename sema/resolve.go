package sema

import (
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

	modSym, ok := a.current.Resolve(n.Module)
	if !ok || modSym.Kind != symbols.SymbolKindModule {
		a.errorf(n, "unknown module %q", n.Module)
		return nil, false
	}

	sym, ok := modSym.Module.Scope.Resolve(n.Name)
	if !ok {
		a.errorf(n, "undefined symbol %q in module %q", n.Name, n.Module)
		return nil, false
	}

	if !sym.Public && n.Module != a.currentMod {
		a.errorf(n, "symbol %q is not public in module %q", sym.Name, n.Module)
		return nil, false
	}

	n.Symbol = sym
	n.ResolvedModuleName = modSym.Module.Name
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
	}

	for _, arg := range n.Args {
		a.visitExpression(arg)
	}
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
