package sema

import (
	"fmt"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
)

type Analyser struct {
	universe *symbols.Scope
	current  *symbols.Scope
	modules  map[string]*symbols.Module
	aliases  map[string]*aliasInfo
	errors   []error
}

func NewAnalyser() *Analyser {
	u := symbols.NewScope(nil)

	a := &Analyser{
		universe: u,
		modules:  make(map[string]*symbols.Module),
		aliases:  make(map[string]*aliasInfo),
	}

	a.predefineBuiltins()
	return a
}

func (a *Analyser) Errors() []error {
	return a.errors
}

func (a *Analyser) errorf(node parser.Node, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	a.errors = append(a.errors, fmt.Errorf("%s: %s", node.GetLoc(), msg))
}

func (a *Analyser) AnalyseModule(root *parser.RootNode, name string) {
	modScope := symbols.NewScope(a.universe)

	mod := &symbols.Module{
		Name:  name,
		Scope: modScope,
	}
	a.modules[name] = mod

	a.current = modScope

	a.collectTopLevel(root)
	a.resolveBodies(root)
}
