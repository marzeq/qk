package sema

import (
	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
)

func (a *Analyser) libraryFunctionDeferred(fn *parser.FunctionDefNode) bool {
	for _, arg := range fn.Args {
		if arg.CompileTime {
			return false
		}
	}
	return a.demandLibraryBodies && a.currentTrustedStandardLibrary &&
		fn.Symbol != nil && fn.Body != nil && !fn.IsParameterized() &&
		!fn.Symbol.Referenced && fn.Attributes.Get(attributes.AttributeTypeExport) == nil
}

func (a *Analyser) functionVisited(fn *parser.FunctionDefNode) bool {
	return fn != nil && a.visitedFunctions[fn]
}

func (a *Analyser) VisitedLibraryBodyCount(roots ...*parser.RootNode) int {
	count := 0
	for _, root := range roots {
		module := a.modules[a.modulePaths[root]]
		if module == nil || !module.TrustedStandardLibrary {
			continue
		}
		for _, node := range root.Body {
			fn, ok := node.(*parser.FunctionDefNode)
			if ok && fn.Body != nil && a.visitedFunctions[fn] {
				count++
			}
		}
	}
	return count
}

func (a *Analyser) visitFunctionOnce(fn *parser.FunctionDefNode) bool {
	if fn.Symbol == nil || a.visitedFunctions[fn] {
		return false
	}
	a.visitedFunctions[fn] = true
	a.visitFunction(fn)
	return true
}

// VisitReferencedLibraryBodies resolves newly referenced standard-library
// functions and their transitive source references. Declarations are already
// installed, so callers can run this after ordinary source-body resolution.
func (a *Analyser) VisitReferencedLibraryBodies(roots ...*parser.RootNode) []*parser.FunctionDefNode {
	if !a.demandLibraryBodies {
		return nil
	}
	previousScope, previousModule := a.current, a.currentMod
	previousState, previousTrusted := a.currentState, a.currentTrustedStandardLibrary
	defer func() {
		a.current, a.currentMod = previousScope, previousModule
		a.currentState, a.currentTrustedStandardLibrary = previousState, previousTrusted
	}()
	var visited []*parser.FunctionDefNode
	for changed := true; changed; {
		changed = false
		for _, root := range roots {
			a.selectModule(a.modulePaths[root])
			if !a.currentTrustedStandardLibrary {
				continue
			}
			for _, raw := range root.Body {
				fn, ok := raw.(*parser.FunctionDefNode)
				if !ok || fn.Symbol == nil || fn.ParameterizedInstance || a.visitedFunctions[fn] || a.libraryFunctionDeferred(fn) {
					continue
				}
				if a.visitFunctionOnce(fn) {
					visited = append(visited, fn)
					changed = true
				}
			}
		}
	}
	return visited
}
