package sema

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
)

func (a *Analyser) defineSymbol(sym *symbols.Symbol, node parser.Node) bool {
	if _, exists := a.current.Symbols[sym.Name]; exists {
		a.errorf(node, "symbol %q already defined in this scope", sym.Name)
		return false
	}

	a.current.Symbols[sym.Name] = sym
	return true
}
