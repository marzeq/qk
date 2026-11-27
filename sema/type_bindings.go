package sema

import (
	"strings"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
)

// Calls to an existing type binding produce another type binding. Recognize
// them during collection so forward references in aggregates can resolve.
func (a *Analyser) promoteTypeApplications(root *parser.RootNode) {
	for index, node := range root.Body {
		declaration, ok := node.(*parser.DeclarationNode)
		if !ok || declaration.Mutable || declaration.TypeNode != nil || len(declaration.ParameterizedParameters) != 0 {
			continue
		}
		call, ok := declaration.Value.(*parser.FunctionCallNode)
		if !ok {
			continue
		}
		name, ok := typeApplicationName(call.Callee)
		if !ok || !a.isTypeBinding(name) {
			continue
		}
		typeNode, ok := expressionAsTypeNode(call)
		if !ok {
			continue
		}
		alias := &parser.TypeAliasNode{
			Name: declaration.Name, Type: typeNode, Transparent: true,
			Pub: declaration.Pub, Loc: declaration.Loc,
		}
		root.Body[index] = alias
		a.precollectTypeAlias(alias)
	}
}

func (a *Analyser) isTypeBinding(name *parser.NamedTypeNode) bool {
	if name.ModName == "" {
		symbol, ok := a.current.Resolve(name.Name)
		return ok && symbol.Kind == symbols.SymbolKindType
	}
	modulePath := a.currentState.importAliases[name.ModName]
	if modulePath == "" {
		return false
	}
	module := a.modules[modulePath]
	if module == nil {
		return false
	}
	symbol, ok := module.Scope.Resolve(name.Name)
	return ok && symbol.Kind == symbols.SymbolKindType && symbol.Public
}

func typeApplicationName(expression parser.ExpressionNode) (*parser.NamedTypeNode, bool) {
	switch node := expression.(type) {
	case *parser.IdentifierNode:
		return &parser.NamedTypeNode{Name: node.Name, Loc: node.Loc}, true
	case *parser.FieldAccessNode:
		parts := []string{node.Field.Name}
		for subject := node.Subject; subject != nil; {
			switch value := subject.(type) {
			case *parser.IdentifierNode:
				parts = append(parts, value.Name)
				subject = nil
			case *parser.FieldAccessNode:
				parts = append(parts, value.Field.Name)
				subject = value.Subject
			default:
				return nil, false
			}
		}
		for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
			parts[left], parts[right] = parts[right], parts[left]
		}
		return &parser.NamedTypeNode{ModName: strings.Join(parts[:len(parts)-1], "."), Name: parts[len(parts)-1], Loc: node.Loc}, true
	}
	return nil, false
}
