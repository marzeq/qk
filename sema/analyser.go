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
	aliasesByModule               map[string]map[string]*aliasInfo
	methods                       map[string]map[string]*symbols.Symbol
	concreteTypes                 map[string]types.Type
	errors                        []error
	currentMod                    string
	currentTrustedStandardLibrary bool
	currentImports                map[string]bool
	importsByModule               map[string]map[string]bool
	typeParameterBindings         map[string]types.Type
	resolvingTraitMethodTypes     bool
	currentRoot                   *parser.RootNode
	functionDefinitions           map[*symbols.Symbol]*functionDefinitionInfo
	genericFunctions              map[*symbols.Symbol]*genericFunctionInfo
	genericValues                 map[*symbols.Symbol]*genericValueInfo
	genericAliases                map[*symbols.Symbol]*genericAliasInfo
}

func NewAnalyser() *Analyser {
	u := symbols.NewScope(nil)

	a := &Analyser{
		universe:            u,
		modules:             make(map[string]*symbols.Module),
		aliases:             make(map[string]*aliasInfo),
		aliasesByModule:     make(map[string]map[string]*aliasInfo),
		methods:             make(map[string]map[string]*symbols.Symbol),
		concreteTypes:       make(map[string]types.Type),
		importsByModule:     make(map[string]map[string]bool),
		functionDefinitions: make(map[*symbols.Symbol]*functionDefinitionInfo),
		genericFunctions:    make(map[*symbols.Symbol]*genericFunctionInfo),
		genericValues:       make(map[*symbols.Symbol]*genericValueInfo),
		genericAliases:      make(map[*symbols.Symbol]*genericAliasInfo),
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
	a.AnalyseModuleRoots([]*parser.RootNode{root}, name, trustedStandardLibrary)
}

// AnalyseModuleRoots analyses all source roots that contribute to one module.
// Top-level declarations are collected across every root before any body is
// resolved, so the module scope does not depend on source discovery order.
func (a *Analyser) AnalyseModuleRoots(roots []*parser.RootNode, name string, trustedStandardLibrary bool) {
	// Aliases are module-local; method tables remain available so later modules
	// can resolve methods exported by their imports.
	if a.aliasesByModule[name] == nil {
		a.aliasesByModule[name] = make(map[string]*aliasInfo)
	}
	a.aliases = a.aliasesByModule[name]
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

	a.collectTopLevels(roots)
	a.resolveModuleBodies(roots)
}
