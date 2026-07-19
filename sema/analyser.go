package sema

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

type Analyser struct {
	universe                      *symbols.Scope
	current                       *symbols.Scope
	modules                       map[string]*symbols.Module
	aliases                       map[string]*aliasInfo
	methods                       map[string]map[string]*symbols.Symbol
	concreteTypes                 map[string]types.Type
	errors                        []error
	currentMod                    string
	currentTrustedStandardLibrary bool
}

func NewAnalyser() *Analyser {
	u := symbols.NewScope(nil)

	a := &Analyser{
		universe:      u,
		modules:       make(map[string]*symbols.Module),
		aliases:       make(map[string]*aliasInfo),
		methods:       make(map[string]map[string]*symbols.Symbol),
		concreteTypes: make(map[string]types.Type),
	}

	a.predefineBuiltins()
	return a
}

func (a *Analyser) Errors() []error {
	return a.errors
}

func (a *Analyser) errorf(node parser.Node, format string, args ...any) {
	a.errors = append(a.errors, shared.NewError(node.GetLoc(), format, args...))
}

func (a *Analyser) AnalyseModule(root *parser.RootNode, name string, trustedStandardLibrary bool) {
	// Aliases are module-local; method tables remain available so later modules
	// can resolve methods exported by their imports.
	a.aliases = make(map[string]*aliasInfo)
	mod := a.modules[name]
	if mod == nil {
		mod = &symbols.Module{
			Name:                   name,
			Scope:                  symbols.NewScope(a.universe),
			TrustedStandardLibrary: trustedStandardLibrary,
		}
		a.modules[name] = mod
	}

	a.current = mod.Scope
	a.currentMod = name
	a.currentTrustedStandardLibrary = trustedStandardLibrary

	a.collectTopLevel(root)
	a.resolveBodies(root)
}
