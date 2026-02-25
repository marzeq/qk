package sema

import (
	"fmt"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

type Analyser struct {
	universe *symbols.Scope
	current  *symbols.Scope
	modules  map[string]*symbols.Module
	errors   []error
}

func NewAnalyser() *Analyser {
	u := symbols.NewScope(nil)

	a := &Analyser{
		universe: u,
		modules:  make(map[string]*symbols.Module),
	}

	a.predefineBuiltins()
	return a
}

func (a *Analyser) Errors() []error {
	return a.errors
}

func (a *Analyser) errorf(node parser.Node, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	a.errors = append(a.errors, fmt.Errorf("%s: %s", node.GetLoc(), msg))
}

func (a *Analyser) predefineBuiltins() {
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_I8), types.PRIMITIVE_I8))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_I16), types.PRIMITIVE_I16))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_I32), types.PRIMITIVE_I32))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_I64), types.PRIMITIVE_I64))

	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_U8), types.PRIMITIVE_U8))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_U16), types.PRIMITIVE_U16))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_U32), types.PRIMITIVE_U32))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_U64), types.PRIMITIVE_U64))

	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_F32), types.PRIMITIVE_F32))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_F64), types.PRIMITIVE_F64))

	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_ISZ), types.PRIMITIVE_ISZ))
	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_USZ), types.PRIMITIVE_USZ))

	a.universe.Define(symbols.NewType(string(types.PRIMITIVE_VOID), types.PRIMITIVE_VOID))
}

func (a *Analyser) AnalyseModule(root *parser.RootNode, name string) {
	modScope := symbols.NewScope(a.universe)

	mod := &symbols.Module{
		Name:  name,
		Scope: modScope,
	}
	a.modules[name] = mod

	a.current = modScope

	a.collectTopLevel(root)
	a.resolveBodies(root)
}

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

func (a *Analyser) collectFunctionSignature(n *parser.FunctionDefNode) {
	paramTypes := make([]types.Type, len(n.Args))

	for i, arg := range n.Args {
		paramTypes[i] = a.resolveTypeNode(arg.Type)
	}

	retType := a.resolveTypeNode(n.RetType.Type)

	sig := &symbols.FunctionSignature{
		Parameters: paramTypes,
		ReturnType: retType,
		Variadic:   n.HasVariadic,
	}

	sym := &symbols.Symbol{
		Name:      n.Name,
		Kind:      symbols.SymbolKindFunction,
		Signature: sig,
	}

	if err := a.current.Define(sym); err != nil {
		a.errors = append(a.errors, err)
	}

	n.Symbol = sym
}

func (a *Analyser) collectImport(n *parser.ImportNode) {
	for _, name := range n.Modules {
		mod, ok := a.modules[name]
		if !ok {
			a.errorf(n, "unknown module %q", name)
			continue
		}

		sym := &symbols.Symbol{
			Name:   name,
			Kind:   symbols.SymbolKindModule,
			Module: mod,
		}

		if err := a.current.Define(sym); err != nil {
			a.errors = append(a.errors, err)
		}
	}
}

func (a *Analyser) resolveBodies(root *parser.RootNode) {
	for _, node := range root.Body {
		a.visit(node)
	}
}

func (a *Analyser) resolveTypeNode(n parser.TypeNode) types.Type {
	switch t := n.(type) {

	case *parser.NamedTypeNode:
		if t.ModName == "" {
			sym, ok := a.current.Resolve(t.Name)
			if !ok || sym.Kind != symbols.SymbolKindType {
				a.errorf(t, "unknown type %q", t.Name)
				return types.ErrorType
			}
			return sym.TypeInfo
		}

		modSym, ok := a.current.Resolve(t.ModName)
		if !ok || modSym.Kind != symbols.SymbolKindModule {
			a.errorf(t, "unknown module %q", t.ModName)
			return types.ErrorType
		}

		sym, ok := modSym.Module.Scope.Resolve(t.Name)
		if !ok || sym.Kind != symbols.SymbolKindType {
			a.errorf(t, "unknown type %q in module %q", t.Name, t.ModName)
			return types.ErrorType
		}

		return sym.TypeInfo

	case *parser.PointerTypeNode:
		return &types.PointerType{
			Base: a.resolveTypeNode(t.BaseType),
		}

	case *parser.ArrayTypeNode:
		return &types.ArrayType{
			Base: a.resolveTypeNode(t.ElementType),
			Size: t.Size,
		}
	}

	a.errorf(n, "unsupported type node")
	return types.ErrorType
}

func (a *Analyser) collectTypeAlias(n *parser.TypeAliasNode) {
	aliased := a.resolveTypeNode(n.Type)

	sym := &symbols.Symbol{
		Name:     n.Name,
		Kind:     symbols.SymbolKindType,
		TypeInfo: aliased,
	}

	if err := a.current.Define(sym); err != nil {
		a.errors = append(a.errors, err)
	}

	n.Symbol = sym
}

func (a *Analyser) collectGlobalVariable(n *parser.DeclarationNode) {
	var varType types.Type

	if n.Type != nil {
		varType = a.resolveTypeNode(n.Type)
	}

	sym := &symbols.Symbol{
		Name: n.Name,
		Kind: symbols.SymbolKindVariable,
		Type: varType,
	}

	if err := a.current.Define(sym); err != nil {
		a.errors = append(a.errors, err)
	}

	n.Symbol = sym
}

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
			Name: arg.Name,
			Kind: symbols.SymbolKindVariable,
			Type: n.Symbol.Signature.Parameters[i],
		}

		if err := a.current.Define(paramSym); err != nil {
			a.errors = append(a.errors, err)
		}
	}

	a.visit(n.Body)
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

	if n.Type != nil {
		varType = a.resolveTypeNode(n.Type)
	}

	sym := &symbols.Symbol{
		Name: n.Name,
		Kind: symbols.SymbolKindVariable,
		Type: varType,
	}

	if err := a.current.Define(sym); err != nil {
		a.errors = append(a.errors, err)
	}

	n.Symbol = sym

	if n.Value != nil {
		a.visitExpression(n.Value)
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
		a.resolveTypeNode(e.ToType)
		a.visitExpression(e.Operand)

	case *parser.SizeOfNode:
		a.resolveTypeNode(e.Operand)

	case *parser.GivenExprNode:
		a.visitBlock(e.Block)
		a.visitExpression(e.FinalExpr)
	}
}

func (a *Analyser) resolveIdentifier(n *parser.IdentifierNode) {
	sym, ok := a.current.Resolve(n.Name)
	if !ok {
		a.errorf(n, "undefined identifier %q", n.Name)
		return
	}
	n.Symbol = sym
}

func (a *Analyser) resolveFunctionCall(n *parser.FunctionCallNode) {
	sym, ok := a.resolveModuleAccess(n.Name)
	if !ok {
		return
	}

	if sym.Kind != symbols.SymbolKindFunction {
		a.errorf(n, "%q is not a function", sym.Name)
		return
	}

	n.Symbol = sym

	for _, arg := range n.Args {
		a.visitExpression(arg)
	}
}

func (a *Analyser) resolveModuleAccess(n *parser.ModuleAccessNode) (*symbols.Symbol, bool) {
	if n.ModName == "" {
		sym, ok := a.current.Resolve(n.Ident.Name)
		if !ok {
			a.errorf(n, "undefined identifier %q", n.Ident.Name)
			return nil, false
		}
		n.Symbol = sym
		return sym, true
	}

	modSym, ok := a.current.Resolve(n.ModName)
	if !ok || modSym.Kind != symbols.SymbolKindModule {
		a.errorf(n, "unknown module %q", n.ModName)
		return nil, false
	}

	sym, ok := modSym.Module.Scope.Resolve(n.Ident.Name)
	if !ok {
		a.errorf(n, "undefined symbol %q in module %q", n.Ident.Name, n.ModName)
		return nil, false
	}

	n.Symbol = sym
	return sym, true
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

func (a *Analyser) visitStructLiteral(n *parser.StructLiteralNode) {
	sym, ok := a.resolveModuleAccess(n.Name)
	if !ok {
		return
	}

	if sym.Kind != symbols.SymbolKindType {
		a.errorf(n, "%q is not a type", sym.Name)
		return
	}

	n.Symbol = sym

	for _, field := range n.Fields {
		a.visitExpression(field.R)
	}
}
