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
	currentImports                map[string]bool
	importsByModule               map[string]map[string]bool
}

func NewAnalyser() *Analyser {
	u := symbols.NewScope(nil)

	a := &Analyser{
		universe:        u,
		modules:         make(map[string]*symbols.Module),
		aliases:         make(map[string]*aliasInfo),
		methods:         make(map[string]map[string]*symbols.Symbol),
		concreteTypes:   make(map[string]types.Type),
		importsByModule: make(map[string]map[string]bool),
	}

	a.predefineBuiltins()
	for _, name := range []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64", "isz", "usz", "char", "bool", "str", "cstr"} {
		if symbol, ok := a.universe.Resolve(name); ok {
			a.concreteTypes["builtin:"+name] = symbol.TypeInfo
		}
	}
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
	a.currentImports = a.importsByModule[name]
	if a.currentImports == nil {
		a.currentImports = make(map[string]bool)
		a.importsByModule[name] = a.currentImports
	}

	a.collectTopLevel(root)
	a.resolveBodies(root)
}
