package sema

import (
	"fmt"

	"github.com/marzeq/qk/attributes"
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
	case *parser.MultiDeclarationNode:
		a.visitMultiDeclaration(n)

	case *parser.AssignmentNode:
		a.visitAssignment(n)

	case *parser.IfNode:
		a.visitIf(n)

	case *parser.MatchNode:
		a.visitMatch(n)

	case *parser.ForNode:
		a.visitFor(n)

	case *parser.RangeForNode:
		a.visitRangeFor(n)

	case *parser.ForEachNode:
		a.visitForEach(n)

	case *parser.ControlKeywordNode:
		a.visitControlKeyword(n)

	case *parser.DeferNode:
		a.visitDefer(n)

	case parser.ExpressionNode:
		a.visitExpression(n)

	default:
		a.errorf(n, "unsupported node type %T", n)
	}
}

func (a *Analyser) visitDefer(n *parser.DeferNode) {
	switch action := n.Action.(type) {
	case *parser.BlockNode:
		a.visitBlock(action)
	case parser.ExpressionNode:
		a.visitExpression(action)
	default:
		a.errorf(n, "unsupported deferred action %T", action)
	}
}

func (a *Analyser) visitFunction(n *parser.FunctionDefNode) {
	if n.Symbol == nil {
		return
	}

	previousBindings := a.typeParameterBindings
	if n.Symbol.Template {
		a.typeParameterBindings = make(map[string]types.Type, len(n.Symbol.GenericParameters))
		for _, parameter := range n.Symbol.GenericParameters {
			a.typeParameterBindings[parameter.Name] = parameter
		}
		defer func() { a.typeParameterBindings = previousBindings }()
	}

	prev := a.current
	a.current = symbols.NewScope(prev)

	for i, arg := range n.Args {
		if arg.Default != nil {
			a.visitExpression(arg.Default)
		}
		var genericOrigin types.Type
		if n.Symbol.Template {
			genericOrigin = n.Symbol.Signature.Parameters[i]
		} else if n.Symbol.TemplateSymbol != nil {
			genericOrigin = n.Symbol.TemplateSymbol.Signature.Parameters[i]
		}
		if !types.HasTypeParameter(genericOrigin) {
			genericOrigin = nil
		}
		paramSym := &symbols.Symbol{
			Name:            arg.Name,
			Kind:            symbols.SymbolKindVariable,
			Type:            n.Symbol.Signature.Parameters[i],
			GenericOrigin:   genericOrigin,
			StaticTraitView: staticTraitViewForGenericType(genericOrigin),
			Mutable:         arg.Mutable,
		}
		a.defineSymbol(paramSym, arg)
		arg.Symbol = paramSym
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
	if len(n.GenericParameters) != 0 {
		a.errorf(n, "generic bindings may only be declared at module scope")
		return
	}
	if n.Attributes.Get(attributes.AttributeTypeForeign) != nil {
		a.errorf(n, "foreign variables must be declared at module scope")
		return
	}
	if n.Name == "_" {
		if n.Value != nil {
			a.visitExpression(n.Value)
		}
		return
	}

	var varType types.Type

	if n.TypeNode != nil {
		varType = a.resolveTypeNode(n.TypeNode)
	}

	sym := &symbols.Symbol{
		Name:     n.Name,
		Kind:     symbols.SymbolKindVariable,
		Type:     varType,
		Mutable:  n.Mutable,
		Comptime: n.Comptime,
	}
	if value, ok := untypedComptimeInteger(n); ok {
		sym.InlineComptime = true
		sym.ComptimeInteger = value
	}

	if n.Value != nil {
		a.visitExpression(n.Value)
	}
	a.current.Symbols[sym.Name] = sym
	n.Symbol = sym
}

func (a *Analyser) visitMultiDeclaration(n *parser.MultiDeclarationNode) {
	a.visitExpression(n.Value)
	n.Symbols = make([]*symbols.Symbol, len(n.Names))
	seen := map[string]bool{}
	for i, name := range n.Names {
		if name == "_" {
			continue
		}
		if seen[name] {
			a.errorf(n, "symbol '%v' appears more than once in declaration", name)
			continue
		}
		seen[name] = true
		sym := symbols.NewVariable(name, nil)
		sym.Mutable = n.Mutable
		n.Symbols[i] = sym
		a.current.Symbols[name] = sym
	}
}

func (a *Analyser) visitExpression(expr parser.ExpressionNode) {
	switch e := expr.(type) {

	case *parser.IdentifierNode:
		a.resolveIdentifier(e)

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

	case *parser.SliceExprNode:
		a.visitExpression(e.Subject)
		if e.Start != nil {
			a.visitExpression(e.Start)
		}
		if e.End != nil {
			a.visitExpression(e.End)
		}

	case *parser.FieldAccessNode:
		a.visitExpression(e.Subject)
		a.resolveModuleField(e)

	case *parser.BlockNode:
		a.visitBlock(e)

	case *parser.IfNode:
		a.visitExpression(e.IfBranch.Condition)
		a.visitBlock(e.IfBranch.Node)

		for _, br := range e.ElseIfBranches {
			a.visitExpression(br.Condition)
			a.visitBlock(br.Node)
		}

		if e.ElseBranch != nil {
			a.visitBlock(e.ElseBranch)
		}

	case *parser.MatchNode:
		a.visitMatch(e)

	case *parser.StructLiteralNode:
		a.visitStructLiteral(e)

	case *parser.SliceLiteralNode:
		if e.RepeatValue != nil {
			a.visitExpression(e.RepeatValue)
			a.visitExpression(e.RepeatAmount)
			break
		}
		for _, el := range e.Elements {
			a.visitExpression(el)
		}

	case *parser.CastNode:
		a.visitExpression(e.Operand)

	case *parser.ReprNode:
		a.visitExpression(e.Operand)

	case *parser.SizeOfNode:
		a.resolveTypeNode(e.Operand)

	case *parser.SizeOfExprNode:
		a.visitExpression(e.Operand)

	case *parser.AlignOfNode:
		if e.Operand != nil && e.Expression != nil {
			identifier := e.Expression.(*parser.IdentifierNode)
			var symbol *symbols.Symbol
			var ok bool
			if identifier.Module == "" {
				symbol, ok = a.current.Resolve(identifier.Name)
			} else if module, found := a.current.Resolve(identifier.Module); found && module.Kind == symbols.SymbolKindModule {
				symbol, ok = module.Module.Scope.Resolve(identifier.Name)
			}
			if ok && symbol.Kind != symbols.SymbolKindType {
				a.visitExpression(e.Expression)
				e.Operand = nil
				break
			}
			e.Expression = nil
		}
		if e.Operand != nil {
			a.resolveTypeNode(e.Operand)
		} else if e.Expression != nil {
			a.visitExpression(e.Expression)
		}

	case *parser.OffsetOfNode:
		a.resolveTypeNode(e.Operand)

	case *parser.IntegerLiteralNode,
		*parser.FloatLiteralNode,
		*parser.StringLiteralNode,
		*parser.CStringLiteralNode,
		*parser.BoolLiteralNode,
		*parser.CharLiteralNode,
		*parser.NilLiteralNode,
		*parser.EnumLiteralNode,
		*parser.NoInitializerNode:

	default:
		panic(fmt.Sprintf("unsupported expression node type %T", e))
	}
}

func (a *Analyser) visitAssignment(n *parser.AssignmentNode) {
	if len(n.Assignees) > 0 {
		for _, target := range n.Assignees {
			if id, ok := target.(*parser.IdentifierNode); !ok || id.Name != "_" {
				a.visitExpression(target)
			}
		}
	} else {
		if id, ok := n.Assignee.(*parser.IdentifierNode); !ok || id.Name != "_" {
			a.visitExpression(n.Assignee)
		}
	}
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

func (a *Analyser) visitMatch(n *parser.MatchNode) {
	a.visitExpression(n.Subject)
	previous := a.current
	a.current = symbols.NewScope(previous)
	defer func() { a.current = previous }()
	if n.BindingName != "" && n.BindingName != "_" {
		binding := symbols.NewVariable(n.BindingName, nil)
		if a.defineSymbol(binding, n) {
			n.Binding = binding
		}
	}
	for armIndex := range n.Arms {
		arm := &n.Arms[armIndex]
		armScope := symbols.NewScope(a.current)
		a.current = armScope
		for _, binding := range matchPatternBindings(arm.Pattern) {
			if binding.Name == "_" {
				continue
			}
			symbol := symbols.NewVariable(binding.Name, nil)
			if a.defineSymbol(symbol, arm.Pattern) {
				binding.Symbol = symbol
			}
		}
		if arm.Guard != nil {
			a.visitExpression(arm.Guard)
		}
		a.visitExpression(arm.Body)
		a.current = armScope.Parent
	}
}

func matchPatternBindings(pattern *parser.MatchPatternNode) []*parser.MatchBinding {
	if pattern == nil {
		return nil
	}
	if pattern.Kind == parser.MatchPatternAlternative {
		var result []*parser.MatchBinding
		for _, alternative := range pattern.Alternatives {
			result = append(result, matchPatternBindings(alternative)...)
		}
		return result
	}
	result := make([]*parser.MatchBinding, len(pattern.Bindings))
	for i := range pattern.Bindings {
		result[i] = &pattern.Bindings[i]
	}
	return result
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

func (a *Analyser) visitRangeFor(n *parser.RangeForNode) {
	prev := a.current
	a.current = symbols.NewScope(prev)
	defer func() {
		a.current = prev
	}()

	a.visitExpression(n.Start)
	a.visitExpression(n.End)

	sym := &symbols.Symbol{
		Name: n.Name,
		Kind: symbols.SymbolKindVariable,
		Type: types.PrimitiveUsz,
	}
	if a.defineSymbol(sym, n) {
		n.Symbol = sym
	}

	a.visitBlock(n.Body)
}

func (a *Analyser) visitForEach(n *parser.ForEachNode) {
	prev := a.current
	a.current = symbols.NewScope(prev)
	defer func() {
		a.current = prev
	}()

	a.visitExpression(n.Iterable)
	var elementType types.Type = types.ErrorType{}
	switch iterable := types.Underlying(n.Iterable.GetType()).(type) {
	case types.SliceType:
		elementType = iterable.Base
	case types.ArrayType:
		elementType = iterable.Base
	}
	elementType = forEachElementType(n, elementType)

	if n.Name != "_" {
		sym := &symbols.Symbol{
			Name: n.Name,
			Kind: symbols.SymbolKindVariable,
			Type: elementType,
		}
		if a.defineSymbol(sym, n) {
			n.Symbol = sym
		}
	}

	if n.IndexName != "" && n.IndexName != "_" {
		indexSym := &symbols.Symbol{
			Name: n.IndexName,
			Kind: symbols.SymbolKindVariable,
			Type: types.PrimitiveUsz,
		}
		if a.defineSymbol(indexSym, n) {
			n.IndexSymbol = indexSym
		}
	}

	a.visitBlock(n.Body)
}

func forEachElementType(n *parser.ForEachNode, elementType types.Type) types.Type {
	switch n.ElementKind {
	case parser.ForEachElementPointer:
		return types.PointerType{Base: elementType}
	case parser.ForEachElementMutablePointer:
		return types.PointerType{Base: elementType, Mutable: true}
	default:
		return elementType
	}
}

func (a *Analyser) visitControlKeyword(n *parser.ControlKeywordNode) {
	for _, value := range n.ReturnValues {
		a.visitExpression(value)
	}
}
