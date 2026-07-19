package sema

import (
	"fmt"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/types"
)

func (a *Analyser) DebugCheck(root *parser.RootNode) {
	w := &debugWalker{}
	w.walkNode(root)

	if len(w.errors) > 0 {
		for _, err := range w.errors {
			fmt.Println("DEBUG SEMANTIC ERROR:", err)
		}
		panic("semantic invariants violated")
	}
}

type debugWalker struct {
	errors []string
}

func (w *debugWalker) walkNode(node parser.Node) {
	switch n := node.(type) {

	case *parser.RootNode:
		for _, stmt := range n.Body {
			w.walkNode(stmt)
		}

	case *parser.FunctionDefNode:
		if n.Symbol == nil || n.Symbol.Signature == nil {
			w.errors = append(w.errors, "function symbol has nil type")
		}
		for _, arg := range n.Args {
			if arg.Default != nil {
				w.walkExpr(arg.Default)
			}
		}
		if n.Body != nil {
			w.walkNode(n.Body)
		}

	case *parser.BlockNode:
		for _, stmt := range n.Body {
			w.walkNode(stmt)
		}

	case *parser.DeferNode:
		w.walkNode(n.Action)

	case *parser.DeclarationNode:
		if n.Symbol == nil || n.Symbol.Type == nil {
			w.errors = append(w.errors, "declaration symbol has nil type")
		} else {
			w.checkType(n.Symbol.Type)
		}

		if n.Value != nil {
			w.walkExpr(n.Value)
		}

	case *parser.AssignmentNode:
		w.walkExpr(n.Assignee)
		w.walkExpr(n.Value)

	case *parser.IfNode:
		w.walkExpr(n.IfBranch.Condition)
		w.walkNode(n.IfBranch.Node)
		for _, elif := range n.ElseIfBranches {
			w.walkExpr(elif.Condition)
			w.walkNode(elif.Node)
		}
		if n.ElseBranch != nil {
			w.walkNode(n.ElseBranch)
		}

	case *parser.ForNode:
		w.walkNode(n.Body)

	case *parser.ControlKeywordNode:
		if n.ReturnValue != nil {
			w.walkExpr(n.ReturnValue)
		}

	case parser.ExpressionNode:
		w.walkExpr(n)
	}
}

func (w *debugWalker) walkExpr(expr parser.ExpressionNode) {
	if expr.GetType() == nil {
		w.errors = append(w.errors, fmt.Sprintf("expression %T at %v has nil type", expr, expr.GetLoc()))
		return
	}

	w.checkType(expr.GetType())

	switch n := expr.(type) {

	case *parser.IdentifierNode:

	case *parser.IntegerLiteralNode,
		*parser.FloatLiteralNode,
		*parser.BoolLiteralNode,
		*parser.StringLiteralNode,
		*parser.CStringLiteralNode,
		*parser.CharLiteralNode,
		*parser.NilLiteralNode,
		*parser.EnumLiteralNode:

	case *parser.CastNode:
		w.walkExpr(n.Operand)
		w.checkType(n.Type)

	case *parser.TypeTestNode:
		w.walkExpr(n.Operand)
		w.checkType(n.TargetType)

	case *parser.ImplementsTestNode:
		if !n.CompileTime {
			w.walkExpr(n.Operand)
		}
		if n.ConcreteType != nil {
			w.checkType(n.ConcreteType)
		}
		w.checkType(n.TargetType)

	case *parser.UnaryOpNode:
		w.walkExpr(n.Operand)

	case *parser.BinaryOpNode:
		w.walkExpr(n.Operand1)
		w.walkExpr(n.Operand2)

	case *parser.FunctionCallNode:
		// Instance method calls are resolved directly to their function symbol;
		// the member-shaped callee is syntax for receiver dispatch, not a bound
		// method expression with its own type.
		if n.Symbol == nil {
			w.walkExpr(n.Callee)
		}
		for _, arg := range n.Args {
			w.walkExpr(arg)
		}

	case *parser.IndexExprNode:
		w.walkExpr(n.Subject)
		w.walkExpr(n.Index)

	case *parser.FieldAccessNode:
		if n.MethodSymbol == nil {
			w.walkExpr(n.Subject)
		}

	case *parser.SliceLiteralNode:
		if n.RepeatValue != nil {
			w.walkExpr(n.RepeatValue)
			w.walkExpr(n.RepeatAmount)
			break
		}
		for _, el := range n.Elements {
			w.walkExpr(el)
		}

	case *parser.IfExprNode:
		w.walkExpr(n.IfBranch.Condition)
		w.walkExpr(n.IfBranch.Node)
		for _, elif := range n.ElseIfBranches {
			w.walkExpr(elif.Condition)
			w.walkExpr(elif.Node)
		}
		if n.ElseBranch != nil {
			w.walkExpr(n.ElseBranch)
		}

	case *parser.SizeOfNode:
		w.checkType(n.Type)

	case *parser.SizeOfExprNode:
		w.walkExpr(n.Operand)
		w.checkType(n.Operand.GetType())
		w.checkType(n.Type)

	case *parser.AlignOfNode:
		if n.Expression != nil {
			w.walkExpr(n.Expression)
		}
		w.checkType(n.OperandType)
		w.checkType(n.Type)

	case *parser.OffsetOfNode:
		w.checkType(n.OperandType)
		w.checkType(n.Type)

	case *parser.StructLiteralNode:
		for _, field := range n.Fields {
			w.walkExpr(field.R)
		}
	case *parser.GivenExprNode:
		w.walkNode(n.Block)
		w.walkExpr(n.FinalExpr)

	default:
		w.errors = append(w.errors, fmt.Sprintf("unhandled expression type %T", expr))
	}
}
func (w *debugWalker) checkType(t types.Type) {
	switch tt := t.(type) {

	case types.UntypedInt, types.UntypedFloat, types.UnresolvedEnum:
		w.errors = append(w.errors, "untyped type remains after validation")

	case types.ErrorType:
		w.errors = append(w.errors, "ErrorType remains after validation")

	case types.SliceType:
		w.checkType(tt.Base)

	case types.PointerType:
		w.checkType(tt.Base)

	case types.StructType:
		for _, field := range tt.Fields {
			w.checkType(field.R)
		}

	case types.EnumType:

	case types.UnionType:
		for _, field := range tt.Fields {
			w.checkType(field.R)
		}

	case types.FunctionType:
		for _, p := range tt.Parameters {
			w.checkType(p)
		}
		w.checkType(tt.ReturnType)

	default:
		// PrimitiveType is fine
	}
}
