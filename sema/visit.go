package sema

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func (a *Analyser) visit(node parser.Node) {
	switch n := node.(type) {

	case *parser.FunctionDefNode:
		a.visitFunction(n)

	case *parser.BlockNode:
		a.visitBlock(n)

	case *parser.DeclarationNode:
		a.visitLocalDeclaration(n)

	case *parser.AssignmentNode:
		a.visitAssignment(n)

	case *parser.ArrayAssignmentNode:
		a.visitArrayAssignment(n)

	case *parser.IfNode:
		a.visitIf(n)

	case *parser.ForNode:
		a.visitFor(n)

	case *parser.ControlKeywordNode:
		a.visitControlKeyword(n)

	case parser.ExpressionNode:
		a.visitExpression(n)

	default:
		a.errorf(n, "unsupported node type %T", n)
	}
}

func (a *Analyser) visitFunction(n *parser.FunctionDefNode) {
	if n.Symbol == nil {
		return
	}

	prev := a.current
	a.current = symbols.NewScope(prev)

	for i, arg := range n.Args {
		paramSym := &symbols.Symbol{
			Name:    arg.Name,
			Kind:    symbols.SymbolKindVariable,
			Type:    n.Symbol.Signature.Parameters[i],
			Mutable: arg.Mutable,
		}
		a.defineSymbol(paramSym, n)
	}

	if n.Body != nil {
		a.visit(n.Body)
	}
	a.current = prev
}

func (a *Analyser) visitBlock(n *parser.BlockNode) {
	prev := a.current
	a.current = symbols.NewScope(prev)

	for _, stmt := range n.Body {
		a.visit(stmt)
	}

	a.current = prev
}

func (a *Analyser) visitLocalDeclaration(n *parser.DeclarationNode) {
	var varType types.Type

	if n.TypeNode != nil {
		varType = a.resolveTypeNode(n.TypeNode)
	}

	sym := &symbols.Symbol{
		Name:    n.Name,
		Kind:    symbols.SymbolKindVariable,
		Type:    varType,
		Mutable: n.Mutable,
	}

	a.defineSymbol(sym, n)
	n.Symbol = sym

	if n.Value != nil {
		a.visitExpression(n.Value)
	}
}

func (a *Analyser) visitExpression(expr parser.ExpressionNode) {
	switch e := expr.(type) {

	case *parser.IdentifierNode:
		a.resolveIdentifier(e)

	case *parser.ModuleAccessNode:
		a.resolveModuleAccess(e)

	case *parser.BinaryOpNode:
		a.visitExpression(e.Operand1)
		a.visitExpression(e.Operand2)

	case *parser.UnaryOpNode:
		a.visitExpression(e.Operand)

	case *parser.FunctionCallNode:
		a.resolveFunctionCall(e)

	case *parser.IndexExprNode:
		a.visitExpression(e.Subject)
		a.visitExpression(e.Index)

	case *parser.IfExprNode:
		a.visitExpression(e.IfBranch.Condition)
		a.visitExpression(e.IfBranch.Node)

		for _, br := range e.ElseIfBranches {
			a.visitExpression(br.Condition)
			a.visitExpression(br.Node)
		}

		if e.ElseBranch != nil {
			a.visitExpression(e.ElseBranch)
		}

	case *parser.StructLiteralNode:
		a.visitStructLiteral(e)

	case *parser.ArrayLiteralNode:
		for _, el := range e.Elements {
			a.visitExpression(el)
		}

	case *parser.CastNode:
		a.visitExpression(e.Operand)

	case *parser.SizeOfNode:
		a.resolveTypeNode(e.Operand)

	case *parser.GivenExprNode:
		a.visitBlock(e.Block)
		a.visitExpression(e.FinalExpr)
	}
}

func (a *Analyser) visitAssignment(n *parser.AssignmentNode) {
	a.visit(n.Assignee)
	a.visitExpression(n.Value)
}

func (a *Analyser) visitArrayAssignment(n *parser.ArrayAssignmentNode) {
	a.visitExpression(n.Assignee)
	a.visitExpression(n.Index)
	a.visitExpression(n.Value)
}

func (a *Analyser) visitIf(n *parser.IfNode) {
	a.visitExpression(n.IfBranch.Condition)
	a.visitBlock(n.IfBranch.Node)

	for _, br := range n.ElseIfBranches {
		a.visitExpression(br.Condition)
		a.visitBlock(br.Node)
	}

	if n.ElseBranch != nil {
		a.visitBlock(n.ElseBranch)
	}
}

func (a *Analyser) visitFor(n *parser.ForNode) {
	prev := a.current
	a.current = symbols.NewScope(prev)

	for _, node := range n.ExprsOrStmts {
		a.visit(node)
	}

	a.visitBlock(n.Body)
	a.current = prev
}

func (a *Analyser) visitControlKeyword(n *parser.ControlKeywordNode) {
	if n.ReturnValue != nil {
		a.visitExpression(n.ReturnValue)
	}
}
