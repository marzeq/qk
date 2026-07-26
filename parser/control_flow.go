package parser

import (
	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/tokeniser"
)

// BlockResult returns the expression yielded when the block reaches its end.
func BlockResult(block *BlockNode) (ExpressionNode, bool) {
	if block == nil || !block.Expression || len(block.Body) == 0 {
		return nil, false
	}
	expr, ok := block.Body[len(block.Body)-1].(ExpressionNode)
	if !ok {
		return nil, false
	}
	if nested, ok := expr.(*BlockNode); ok && !nested.Expression {
		return nil, false
	}
	if conditional, ok := expr.(*IfNode); ok && !conditional.Expression {
		return nil, false
	}
	return expr, true
}

// NodeFallsThrough reports whether execution can continue past a node.
func NodeFallsThrough(node Node) bool {
	switch n := node.(type) {
	case *ControlKeywordNode:
		return n.Keyword != tokeniser.KeywordReturn &&
			n.Keyword != tokeniser.KeywordBreak &&
			n.Keyword != tokeniser.KeywordContinue
	case *BlockNode:
		for _, child := range n.Body {
			if !NodeFallsThrough(child) {
				return false
			}
		}
		return true
	case *IfNode:
		if n.ElseBranch == nil || NodeFallsThrough(n.IfBranch.Node) {
			return true
		}
		for _, branch := range n.ElseIfBranches {
			if NodeFallsThrough(branch.Node) {
				return true
			}
		}
		return NodeFallsThrough(n.ElseBranch)
	case *FunctionCallNode:
		return n.Symbol == nil || n.Symbol.Attributes.Get(attributes.AttributeTypeNoReturn) == nil
	default:
		return true
	}
}
