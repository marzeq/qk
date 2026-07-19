package sema

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/types"
)

type aliasState int

const (
	aliasUnseen aliasState = iota
	aliasResolving
	aliasResolved
)

type aliasInfo struct {
	node     *parser.TypeAliasNode
	state    aliasState
	resolved types.Type
}

func (a *Analyser) resolveAlias(info *aliasInfo, node parser.Node, indirect bool) types.Type {
	switch info.state {
	case aliasResolving:
		if indirect {
			return &types.AliasRef{Module: a.currentMod, Name: info.node.Name, Target: &info.resolved}
		}
		a.errorf(node, "circular type definition detected")
		return types.ErrorType{}

	case aliasResolved:
		return info.node.Symbol.TypeInfo
	}

	info.state = aliasResolving

	resolved := a.resolveTypeNodeAt(info.node.Type, false)
	if trait, ok := resolved.(types.TraitType); ok && !info.node.Transparent {
		trait.Module = a.currentMod
		trait.Name = info.node.Name
		resolved = trait
	} else if !info.node.Transparent {
		resolved = types.DefinedType{Module: a.currentMod, Name: info.node.Name, Underlying: resolved}
		a.concreteTypes[a.currentMod+":"+info.node.Name] = resolved
	}

	info.node.Symbol.TypeInfo = resolved
	info.resolved = resolved
	info.state = aliasResolved

	return resolved
}
