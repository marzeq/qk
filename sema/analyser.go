package sema

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

type moduleState struct {
	root          *parser.RootNode
	aliases       map[string]*aliasInfo
	imports       map[string]bool
	importAliases map[string]string
}

type Analyser struct {
	universe                      *symbols.Scope
	current                       *symbols.Scope
	modules                       map[string]*symbols.Module
	moduleStates                  map[string]*moduleState
	currentState                  *moduleState
	methods                       map[string]map[string]*symbols.Symbol
	structuralMethods             map[string][]*symbols.Symbol
	concreteTypes                 map[string]types.Type
	nominalTraits                 map[string]types.TraitType
	errors                        []error
	currentMod                    string
	currentTrustedStandardLibrary bool
	typeParameterBindings         map[string]types.Type
	resolvingTraitMethodTypes     bool
	modulePaths                   map[*parser.RootNode]string
	functionDefinitions           map[*symbols.Symbol]*functionDefinitionInfo
	parameterizedFunctions        map[*symbols.Symbol]*parameterizedFunctionInfo
	parameterizedValues           map[*symbols.Symbol]*parameterizedValueInfo
	parameterizedAliases          map[*symbols.Symbol]*parameterizedAliasInfo
	traitDefaults                 map[string][]*symbols.Symbol
	definitionTypeBindings        map[*symbols.Symbol]map[string]types.Type
	lambdaCounter                 int
	lambdaOwnedSymbols            []map[*symbols.Symbol]bool
	staging                       bool
	deferredStageSelector         func(*parser.FunctionDefNode, *parser.RootNode, map[string]types.Type) error
	deferredTypeSelector          func(*parser.TypeAliasNode, *parser.RootNode, map[string]types.Type) (parser.TypeNode, error)
}

func (a *Analyser) SetStaging(staging bool) { a.staging = staging }

func (a *Analyser) SetDeferredStageSelector(selector func(*parser.FunctionDefNode, *parser.RootNode, map[string]types.Type) error) {
	a.deferredStageSelector = selector
}

func (a *Analyser) SetDeferredTypeSelector(selector func(*parser.TypeAliasNode, *parser.RootNode, map[string]types.Type) (parser.TypeNode, error)) {
	a.deferredTypeSelector = selector
}

func NewAnalyser() *Analyser {
	u := symbols.NewScope(nil)

	a := &Analyser{
		universe:               u,
		modules:                make(map[string]*symbols.Module),
		moduleStates:           make(map[string]*moduleState),
		methods:                make(map[string]map[string]*symbols.Symbol),
		structuralMethods:      make(map[string][]*symbols.Symbol),
		concreteTypes:          make(map[string]types.Type),
		nominalTraits:          make(map[string]types.TraitType),
		modulePaths:            make(map[*parser.RootNode]string),
		functionDefinitions:    make(map[*symbols.Symbol]*functionDefinitionInfo),
		parameterizedFunctions: make(map[*symbols.Symbol]*parameterizedFunctionInfo),
		parameterizedValues:    make(map[*symbols.Symbol]*parameterizedValueInfo),
		parameterizedAliases:   make(map[*symbols.Symbol]*parameterizedAliasInfo),
		traitDefaults:          make(map[string][]*symbols.Symbol),
		definitionTypeBindings: make(map[*symbols.Symbol]map[string]types.Type),
	}

	a.predefineBuiltins()
	for _, name := range []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64", "isz", "usz", "bool", "str", "cstr"} {
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

func (a *Analyser) AnalyseModule(root *parser.RootNode, path string, trustedStandardLibrary bool) {
	a.DeclareModule(root, path, trustedStandardLibrary)
	a.AnalyseModuleBody(root, path)
}

func (a *Analyser) selectModule(path string) {
	a.currentMod = path
	if mod := a.modules[path]; mod != nil {
		a.current = mod.Scope
		a.currentTrustedStandardLibrary = mod.TrustedStandardLibrary
	}
	a.currentState = a.moduleStates[path]
}

// DeclareModule installs module-level declarations before body analysis so
// imports and structural methods are available in dependency order.
func (a *Analyser) DeclareModule(root *parser.RootNode, path string, trustedStandardLibrary bool) {
	a.modulePaths[root] = path
	// Aliases are module-local; method tables remain available so later modules
	// can resolve methods exported by their imports.
	if a.moduleStates[path] == nil {
		a.moduleStates[path] = &moduleState{
			aliases:       make(map[string]*aliasInfo),
			imports:       make(map[string]bool),
			importAliases: make(map[string]string),
		}
	}
	a.moduleStates[path].root = root
	mod := a.modules[path]
	if mod == nil {
		mod = &symbols.Module{
			Name:                   path,
			Scope:                  symbols.NewScope(a.universe),
			TrustedStandardLibrary: trustedStandardLibrary,
		}
		a.modules[path] = mod
	}

	a.selectModule(path)

	a.collectTopLevel(root)
	if path == "builtin" {
		for _, name := range []string{"Unit", "Error", "Option", "Result"} {
			if symbol, ok := mod.Scope.Symbols[name]; ok {
				_ = a.universe.Define(symbol)
			}
		}
	}
}

// AnalyseModuleBody resolves one module against declarations installed from
// its dependencies. Dependencies must have been declared first.
func (a *Analyser) AnalyseModuleBody(root *parser.RootNode, path string) {
	mod := a.modules[path]
	if mod == nil {
		return
	}
	a.selectModule(path)
	a.resolveBodies(root)
}
