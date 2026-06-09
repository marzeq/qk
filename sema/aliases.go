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
	node  *parser.TypeAliasNode
	state aliasState
}

func (a *Analyser) resolveAlias(info *aliasInfo, node parser.Node) types.Type {
	switch info.state {
	case aliasResolving:
		a.errorf(node, "circular type alias detected")
		return types.ErrorType{}

	case aliasResolved:
		return info.node.Symbol.TypeInfo
	}

	info.state = aliasResolving

	resolved := a.resolveTypeNode(info.node.Type)

	info.node.Symbol.TypeInfo = resolved
	info.state = aliasResolved

	return resolved
}
