package sema

import "github.com/marzeq/qk/parser"

func (a *Analyser) collectTopLevel(root *parser.RootNode) {
	for _, node := range root.Body {
		switch n := node.(type) {

		case *parser.FunctionDefNode:
			a.collectFunctionSignature(n)

		case *parser.TypeAliasNode:
			a.collectTypeAlias(n)

		case *parser.DeclarationNode:
			a.collectGlobalVariable(n)

		case *parser.ImportNode:
			a.collectImport(n)
		}
	}
}

func (a *Analyser) resolveBodies(root *parser.RootNode) {
	for _, info := range a.aliases {
		if info.state == aliasUnseen {
			a.resolveAlias(info, info.node)
		}
	}

	for _, node := range root.Body {
		a.visit(node)
	}
}
