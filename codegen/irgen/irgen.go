package irgen

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strings"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/tokeniser"
	"github.com/marzeq/qk/types"
)

type Env struct {
	Parent *Env

	Variables map[*symbols.Symbol]ir.SlotID
}

func NewEnv(parent *Env) *Env {
	return &Env{
		Parent:    parent,
		Variables: make(map[*symbols.Symbol]ir.SlotID),
	}
}

func (e *Env) Lookup(sym *symbols.Symbol) (ir.SlotID, bool) {
	for scope := e; scope != nil; scope = scope.Parent {
		if slot, ok := scope.Variables[sym]; ok {
			return slot, true
		}
	}
	return 0, false
}

type Generator struct {
	Module                 *ir.Module
	ModuleName             string
	MainModule             string
	DependencyInitializers []string

	currentFunction *ir.Function
	currentBlock    *ir.Block
	currentEnv      *Env
	globals         map[*symbols.Symbol]string
	loopTargets     []loopTargets
	deferScopes     [][]parser.Node
	dynamicGlobals  []dynamicGlobalInitializer
}

type dynamicGlobalInitializer struct {
	name string
	expr parser.ExpressionNode
}

type loopTargets struct {
	breakTarget    ir.BlockID
	continueTarget ir.BlockID
	cleanupDepth   int
}

func (g *Generator) Emit(instruction ir.Instr) {
	if g.currentBlock == nil {
		panic("cannot emit instruction without a current block")
	}
	if g.currentBlockHasTerminator() {
		panic("cannot emit instruction after block terminator")
	}
	g.currentBlock.Instr = append(g.currentBlock.Instr, instruction)
}

func (g *Generator) Generate(root *parser.RootNode) *ir.Module {
	return g.GenerateRoots([]*parser.RootNode{root})
}

func (g *Generator) GenerateRoots(roots []*parser.RootNode) *ir.Module {
	g.Module = &ir.Module{}
	g.globals = make(map[*symbols.Symbol]string)
	g.dynamicGlobals = nil

	for _, root := range roots {
		for _, node := range root.Body {
			if node, ok := node.(*parser.DeclarationNode); ok {
				g.generateGlobalDeclaration(node)
			}
		}
	}

	for _, root := range roots {
		for _, rawNode := range root.Body {
			node, ok := rawNode.(*parser.FunctionDefNode)
			if !ok {
				continue
			}
			if node.Body == nil {
				var externFrom string
				for _, attr := range node.Attributes {
					switch attr := attr.(type) {
					case attributes.FunctionAttributeForeign:
						externFrom = attr.From
					}
				}
				if externFrom == "" {
					panic("function without body must have foreign attribute")
				}
				sig := g.buildFunctionSignature(node)
				name := node.Name
				g.Module.AddExtern(ir.ExternDecl{Name: name, Signature: sig, From: externFrom})
				g.generateDefaultWrappers(node, name)
				continue
			}
			g.GenerateFunction(node)
		}
	}

	if len(g.dynamicGlobals) > 0 || len(g.DependencyInitializers) > 0 {
		g.generateModuleInitializer()
	}

	return g.Module
}

func (g *Generator) generateGlobalDeclaration(node *parser.DeclarationNode) {
	if node.Symbol == nil {
		panic("global declaration symbol is nil")
	}
	if foreign, ok := node.Symbol.Attributes.Get(attributes.AttributeTypeForeign).(attributes.FunctionAttributeForeign); ok {
		name := foreign.From
		if name == "" {
			name = node.Name
		}
		g.Module.AddExternGlobal(ir.ExternGlobal{
			Name:    name,
			Type:    node.Symbol.Type,
			Mutable: node.Mutable,
		})
		g.globals[node.Symbol] = name
		return
	}

	linkage := ir.LinkageInternal
	visibility := ir.VisibilityDefault
	if node.Symbol.Public {
		linkage = ir.LinkageExternal
		visibility = ir.VisibilityHidden
	}
	name := g.mangleGlobalName(g.ModuleName, node.Name)
	value, constant := g.tryGenerateGlobalInitializer(node.Value)
	if !constant {
		value = ir.ZeroConstOperand(node.Symbol.Type)
		g.dynamicGlobals = append(g.dynamicGlobals, dynamicGlobalInitializer{
			name: name,
			expr: node.Value,
		})
	}
	g.Module.AddGlobal(ir.Global{
		Name:       name,
		Type:       node.Symbol.Type,
		Mutable:    node.Mutable || !constant,
		Linkage:    linkage,
		Visibility: visibility,
		Value:      value,
	})
	g.globals[node.Symbol] = g.Module.Globals[len(g.Module.Globals)-1].Name
}

func (g *Generator) tryGenerateGlobalInitializer(expr parser.ExpressionNode) (ir.Operand, bool) {
	switch node := expr.(type) {
	case *parser.IntegerLiteralNode:
		if types.IsFloat(node.GetType()) {
			return ir.FloatConstOperand(node.Value, node.GetType()), true
		}
		return ir.IntConstOperand(node.Value, node.GetType()), true
	case *parser.FloatLiteralNode:
		return ir.FloatConstOperand(node.Value, node.GetType()), true
	case *parser.BoolLiteralNode:
		return ir.BoolConstOperand(node.Value == string(tokeniser.KeywordTrue)), true
	case *parser.CharLiteralNode:
		return ir.IntConstOperand(fmt.Sprint(int(node.Value)), node.GetType()), true
	case *parser.CStringLiteralNode:
		return ir.CStringConstOperand(node.Value), true
	case *parser.NilLiteralNode:
		return ir.NullConstOperand(node.GetType()), true
	case *parser.EnumLiteralNode:
		return ir.IntConstOperand(node.Value, node.GetType()), true
	case *parser.FieldAccessNode:
		if node.IsEnumValue {
			return ir.IntConstOperand(node.EnumValue, node.GetType()), true
		}
		return ir.Operand{}, false
	case *parser.StructLiteralNode:
		structType, ok := types.Underlying(node.GetType()).(types.StructType)
		if !ok {
			return ir.Operand{}, false
		}
		fields := make(map[string]parser.ExpressionNode, len(node.Fields))
		for _, field := range node.Fields {
			fields[field.L] = field.R
		}
		values := make([]ir.Operand, len(structType.Fields))
		for i, field := range structType.Fields {
			value, exists := fields[field.L]
			if !exists {
				return ir.Operand{}, false
			}
			fieldValue, constant := g.tryGenerateGlobalInitializer(value)
			if !constant {
				return ir.Operand{}, false
			}
			values[i] = fieldValue
		}
		return ir.StructConstOperand(structType, values), true
	default:
		return ir.Operand{}, false
	}
}

func (g *Generator) generateModuleInitializer() {
	name := g.mangleFunctionName(g.ModuleName, "__module_init")
	guardName := g.mangleGlobalName(g.ModuleName, "__module_initialized")
	g.Module.AddGlobal(ir.Global{
		Name: guardName, Type: types.PrimitiveBool, Mutable: true,
		Linkage: ir.LinkageInternal, Value: ir.BoolConstOperand(false),
	})

	fn := ir.NewFunction(name, ir.LinkageExternal, nil)
	fn.Visibility = ir.VisibilityHidden
	fn.Signature = ir.FunctionSignature{ReturnType: types.PrimitiveVoid}
	g.Module.AddFunction(fn)
	g.Module.Initializer = name

	g.currentFunction = fn
	g.currentEnv = NewEnv(nil)
	g.deferScopes = nil
	entry := fn.NewBlock("entry")
	initialize := fn.NewBlock("initialize")
	done := fn.NewBlock("done")
	fn.Entry = entry.ID
	g.currentBlock = entry
	initialized := fn.NewValueOfType(types.PrimitiveBool)
	g.Emit(ir.LoadGlobal{Dest: initialized, Name: guardName, Type: types.PrimitiveBool})
	g.Emit(ir.Branch{Cond: ir.ValueOperand(initialized, types.PrimitiveBool), Then: done.ID, Else: initialize.ID})

	g.currentBlock = initialize
	g.Emit(ir.StoreGlobal{Name: guardName, Value: ir.BoolConstOperand(true)})
	for _, dependency := range g.DependencyInitializers {
		signature := ir.FunctionSignature{ReturnType: types.PrimitiveVoid}
		g.Module.AddExtern(ir.ExternDecl{Name: dependency, Signature: signature, Visibility: ir.VisibilityHidden})
		g.Emit(ir.Call{Name: dependency, Signature: signature})
	}
	for _, global := range g.dynamicGlobals {
		value := g.GenerateExpr(global.expr)
		g.Emit(ir.StoreGlobal{Name: global.name, Value: value})
	}
	g.Emit(ir.Jump{Target: done.ID})

	g.currentBlock = done
	g.Emit(ir.Return{})
}

func (g *Generator) GenerateFunction(fn *parser.FunctionDefNode) {
	name := fn.Symbol.Name
	linkage := ir.LinkageInternal
	visibility := ir.VisibilityDefault
	if export, ok := fn.Attributes.Get(attributes.AttributeTypeExport).(attributes.FunctionAttributeExport); ok {
		name = export.As
		linkage = ir.LinkageExternal
	} else {
		if fn.Symbol.Public || fn.Symbol.Method {
			linkage = ir.LinkageExternal
			visibility = ir.VisibilityHidden
		}
		if !g.isProgramEntryFunction(fn.Symbol.Name) {
			name = g.mangleFunctionName(g.ModuleName, fn.Symbol.Name)
		}
	}
	irFn := ir.NewFunction(name, linkage, fn.Attributes)
	irFn.Visibility = visibility
	irFn.Signature = g.buildFunctionSignature(fn)
	g.Module.AddFunction(irFn)

	g.currentFunction = irFn
	g.currentBlock = irFn.NewBlock("entry")
	irFn.Entry = g.currentBlock.ID
	g.currentEnv = NewEnv(nil)
	g.deferScopes = nil

	g.emitFunctionParams(fn)

	switch body := fn.Body.(type) {
	case *parser.BlockNode:
		g.GenerateNode(body)
		if !g.currentBlockHasTerminator() {
			if g.isVoidFunction(fn) {
				g.Emit(ir.Return{})
			} else {
				panic("non-void function may fall through without return")
			}
		}
	case parser.ExpressionNode:
		ret := g.GenerateExpr(body)
		if g.currentBlockHasTerminator() {
			break
		}
		if g.isVoidFunction(fn) {
			g.Emit(ir.Return{})
		} else {
			g.Emit(ir.Return{HasValue: true, Value: ret})
		}
	default:
		panic(fmt.Sprintf("todo: function body %T", body))
	}

	g.currentEnv = nil
	g.currentBlock = nil
	g.currentFunction = nil
	g.deferScopes = nil

	g.generateDefaultWrappers(fn, name)
}

func (g *Generator) generateDefaultWrappers(fn *parser.FunctionDefNode, targetName string) {
	if fn.Symbol.Signature.TypedVariadic {
		return
	}
	required := fn.Symbol.Signature.RequiredParameters
	total := len(fn.Args)
	linkage := ir.LinkageInternal
	visibility := ir.VisibilityDefault
	if fn.Attributes.Get(attributes.AttributeTypeExport) != nil {
		linkage = ir.LinkageExternal
	} else if fn.Symbol.Public {
		linkage = ir.LinkageExternal
		visibility = ir.VisibilityHidden
	}
	for arity := required; arity < total; arity++ {
		wrapper := ir.NewFunction(g.defaultWrapperName(targetName, arity), linkage, nil)
		wrapper.Visibility = visibility
		wrapper.Signature = ir.FunctionSignature{
			ParamTypes: append([]types.Type(nil), fn.Symbol.Signature.Parameters[:arity]...),
			ReturnType: fn.Symbol.Signature.ReturnType,
		}
		g.Module.AddFunction(wrapper)

		g.currentFunction = wrapper
		g.currentBlock = wrapper.NewBlock("entry")
		wrapper.Entry = g.currentBlock.ID
		g.currentEnv = NewEnv(nil)
		g.deferScopes = nil

		args := make([]ir.Operand, 0, total)
		for i := 0; i < arity; i++ {
			arg := fn.Args[i]
			slot := wrapper.NewSlot(arg.Symbol.Type, fmt.Sprintf("arg%d", i))
			wrapper.AddParameter(arg.Name, arg.Symbol.Type, slot)
			g.currentEnv.Variables[arg.Symbol] = slot
			g.Emit(ir.Alloca{Slot: slot})
			incoming := wrapper.NewValueOfType(arg.Symbol.Type)
			g.Emit(ir.Store{Slot: slot, Value: ir.ValueOperand(incoming, arg.Symbol.Type)})
			args = append(args, g.generateIdentifierExpr(&parser.IdentifierNode{Symbol: arg.Symbol, Type: arg.Symbol.Type}))
		}
		for i := arity; i < total; i++ {
			value := g.GenerateExpr(fn.Args[i].Default)
			args = append(args, value)

			// Later defaults can refer to the value produced by this default.
			slot := wrapper.NewSlot(fn.Args[i].Symbol.Type, fmt.Sprintf("default%d", i))
			g.currentEnv.Variables[fn.Args[i].Symbol] = slot
			g.Emit(ir.Alloca{Slot: slot})
			g.Emit(ir.Store{Slot: slot, Value: value})
		}

		callSig := g.buildFunctionSignature(fn)
		if callSig.ReturnType == nil || callSig.ReturnType.Equals(types.PrimitiveVoid) {
			g.Emit(ir.Call{Name: targetName, Args: args, Signature: callSig})
			g.Emit(ir.Return{})
		} else {
			dst := wrapper.NewValueOfType(callSig.ReturnType)
			g.Emit(ir.Call{Dest: dst, Name: targetName, Args: args, Signature: callSig})
			g.Emit(ir.Return{HasValue: true, Value: ir.ValueOperand(dst, callSig.ReturnType)})
		}

		g.currentEnv = nil
		g.currentBlock = nil
		g.currentFunction = nil
		g.deferScopes = nil
	}
}

func (g *Generator) defaultWrapperName(targetName string, arity int) string {
	return fmt.Sprintf("%s__default_%d", targetName, arity)
}

func (g *Generator) emitFunctionParams(fn *parser.FunctionDefNode) {
	for i, arg := range fn.Args {
		if arg.Symbol == nil {
			panic("function arg symbol is nil")
		}

		slot := g.currentFunction.NewSlot(arg.Symbol.Type, fmt.Sprintf("arg%d", i))
		g.currentFunction.AddParameter(arg.Name, arg.Symbol.Type, slot)
		g.currentEnv.Variables[arg.Symbol] = slot

		g.Emit(ir.Alloca{Slot: slot})

		incoming := g.currentFunction.NewValueOfType(arg.Symbol.Type)
		g.Emit(ir.Store{Slot: slot, Value: ir.ValueOperand(incoming, arg.Symbol.Type)})
	}
}

func (g *Generator) buildFunctionSignature(fn *parser.FunctionDefNode) ir.FunctionSignature {
	sig := ir.FunctionSignature{}

	if fn != nil && fn.Symbol != nil && fn.Symbol.Signature != nil {
		sig.ParamTypes = append(sig.ParamTypes, fn.Symbol.Signature.Parameters...)
		sig.ReturnType = fn.Symbol.Signature.ReturnType
		sig.Variadic = fn.Symbol.Signature.Variadic
		sig.Attributes = fn.Symbol.Attributes
		return sig
	}

	for _, arg := range fn.Args {
		if arg.Symbol != nil {
			sig.ParamTypes = append(sig.ParamTypes, arg.Symbol.Type)
		}
	}
	if fn == nil || fn.Symbol == nil || fn.Symbol.Signature == nil || fn.Symbol.Signature.ReturnType == nil {
		sig.ReturnType = types.PrimitiveVoid
	}
	return sig
}

func (g *Generator) isVoidFunction(fn *parser.FunctionDefNode) bool {
	if fn == nil || fn.Symbol == nil || fn.Symbol.Signature == nil || fn.Symbol.Signature.ReturnType == nil {
		return true
	}
	return fn.Symbol.Signature.ReturnType.Equals(types.PrimitiveVoid)
}

func (g *Generator) currentBlockHasTerminator() bool {
	if g.currentBlock == nil || len(g.currentBlock.Instr) == 0 {
		return false
	}
	last := g.currentBlock.Instr[len(g.currentBlock.Instr)-1]
	switch last.(type) {
	case ir.Return, ir.Jump, ir.Branch, ir.Unreachable:
		return true
	default:
		return false
	}
}

func (g *Generator) GenerateNode(node parser.Node) {
	switch n := node.(type) {
	case *parser.BlockNode:
		g.generateBlock(n)
	case *parser.DeclarationNode:
		g.generateDeclaration(n)
	case *parser.AssignmentNode:
		g.generateAssignment(n)
	case *parser.ControlKeywordNode:
		g.generateControlKeyword(n)
	case *parser.DeferNode:
		g.registerDefer(n)
	case *parser.IfNode:
		g.generateIf(n)
	case *parser.ForNode:
		g.generateFor(n)
	case *parser.RangeForNode:
		g.generateRangeFor(n)
	case *parser.ForEachNode:
		g.generateForEach(n)
	case *parser.FunctionCallNode:
		g.generateFunctionCallExpr(n)
	case parser.ExpressionNode:
		g.GenerateExpr(n)
	default:
		panic(fmt.Sprintf("todo: generate node %T", n))
	}
}

func (g *Generator) generateBlock(node *parser.BlockNode) {
	prev := g.currentEnv
	g.currentEnv = NewEnv(prev)
	g.deferScopes = append(g.deferScopes, nil)
	defer func() {
		g.deferScopes = g.deferScopes[:len(g.deferScopes)-1]
		g.currentEnv = prev
	}()

	for _, child := range node.Body {
		if g.currentBlockHasTerminator() {
			break
		}
		g.GenerateNode(child)
	}
	if !g.currentBlockHasTerminator() {
		g.emitCurrentScopeDefers()
	}
}

func (g *Generator) registerDefer(node *parser.DeferNode) {
	if len(g.deferScopes) == 0 {
		panic("defer outside a lexical scope")
	}
	last := len(g.deferScopes) - 1
	g.deferScopes[last] = append(g.deferScopes[last], node.Action)
}

func (g *Generator) emitDeferredAction(action parser.Node) {
	if expr, ok := action.(parser.ExpressionNode); ok {
		g.GenerateExpr(expr)
		return
	}
	g.GenerateNode(action)
}

func (g *Generator) emitCurrentScopeDefers() {
	if len(g.deferScopes) == 0 {
		return
	}
	g.emitScopeDefers(len(g.deferScopes) - 1)
}

func (g *Generator) emitDefersUntil(depth int) {
	for i := len(g.deferScopes) - 1; i >= depth; i-- {
		g.emitScopeDefers(i)
		if g.currentBlockHasTerminator() {
			return
		}
	}
}

func (g *Generator) emitScopeDefers(scope int) {
	actions := g.deferScopes[scope]
	defer func() { g.deferScopes[scope] = actions }()
	for i, action := range slices.Backward(actions) {
		// If this action transfers control, only the earlier actions remain pending.
		g.deferScopes[scope] = actions[:i]
		g.emitDeferredAction(action)
		if g.currentBlockHasTerminator() {
			return
		}
	}
}

func (g *Generator) generateDeclaration(node *parser.DeclarationNode) {
	if node.Symbol == nil {
		panic("declaration symbol is nil")
	}

	slot := g.currentFunction.NewSlot(node.Symbol.Type, node.Name)
	g.currentEnv.Variables[node.Symbol] = slot

	g.Emit(ir.Alloca{Slot: slot})

	if lit, ok := node.Value.(*parser.StructLiteralNode); ok {
		g.generateStructLiteralIntoSlot(slot, lit)
		return
	}

	value := g.GenerateExpr(node.Value)
	g.Emit(ir.Store{Slot: slot, Value: value})
}

func (g *Generator) generateAssignment(node *parser.AssignmentNode) {
	if node.Compound {
		g.generateCompoundAssignment(node)
		return
	}

	switch n := node.Assignee.(type) {
	case *parser.IdentifierNode:
		slot, ok := g.currentEnv.Lookup(n.GetSymbol())
		if !ok {
			global := g.globalNameForIdentifier(n)
			rhs := g.GenerateExpr(node.Value)
			g.Emit(ir.StoreGlobal{Name: global, Value: rhs})
			return
		}

		if lit, ok := node.Value.(*parser.StructLiteralNode); ok {
			g.generateStructLiteralIntoSlot(slot, lit)
			return
		}

		rhs := g.GenerateExpr(node.Value)
		g.Emit(ir.Store{Slot: slot, Value: rhs})

	case *parser.UnaryOpNode:
		switch n.Op {
		case parser.UnaryOpDereference:
			ptr := g.GenerateExpr(n.Operand)
			value := g.GenerateExpr(node.Value)

			g.Emit(ir.StorePtr{
				Ptr:   ptr,
				Value: value,
			})
		default:
			panic(fmt.Sprintf("todo: assignment unary op %T", n))
		}

	case *parser.FieldAccessNode:
		base := g.generateFieldSubjectAddress(n.Subject)
		fieldPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: n.GetType()})
		g.Emit(ir.FieldAddress{
			Dest:  fieldPtrID,
			Base:  base,
			Field: n.Field.Name,
		})
		value := g.GenerateExpr(node.Value)
		g.Emit(ir.StorePtr{
			Ptr:   ir.ValueOperand(fieldPtrID, types.PointerType{Base: n.GetType()}),
			Value: value,
		})

	case *parser.IndexExprNode:
		elementPtr := g.generateIndexAddress(n)
		value := g.GenerateExpr(node.Value)
		g.Emit(ir.StorePtr{
			Ptr:   elementPtr,
			Value: value,
		})

	default:
		panic(fmt.Sprintf("todo: assignment assignee %T", n))
	}
}

func (g *Generator) generateCompoundAssignment(node *parser.AssignmentNode) {
	binary, ok := node.Value.(*parser.BinaryOpNode)
	if !ok {
		panic("compound assignment value is not a binary operation")
	}

	address := g.generateAddressOfExpr(node.Assignee)
	currentID := g.currentFunction.NewValueOfType(node.Assignee.GetType())
	g.Emit(ir.LoadPtr{Dest: currentID, Ptr: address})
	left := ir.ValueOperand(currentID, node.Assignee.GetType())
	right := g.GenerateExpr(binary.Operand2)
	result := g.emitBinaryOperation(binary.Op, left, right, node.Assignee.GetType())
	g.Emit(ir.StorePtr{Ptr: address, Value: result})
}

func (g *Generator) generateControlKeyword(node *parser.ControlKeywordNode) {
	switch node.Keyword {
	case tokeniser.KeywordReturn:
		var value ir.Operand
		if node.ReturnValue != nil {
			value = g.GenerateExpr(node.ReturnValue)
		}
		g.emitDefersUntil(0)
		if g.currentBlockHasTerminator() {
			return
		}
		if node.ReturnValue == nil {
			g.Emit(ir.Return{})
			return
		}
		g.Emit(ir.Return{HasValue: true, Value: value})
	case tokeniser.KeywordBreak:
		targets := g.currentLoopTargets()
		g.emitDefersUntil(targets.cleanupDepth)
		if g.currentBlockHasTerminator() {
			return
		}
		g.Emit(ir.Jump{Target: targets.breakTarget})
	case tokeniser.KeywordContinue:
		targets := g.currentLoopTargets()
		g.emitDefersUntil(targets.cleanupDepth)
		if g.currentBlockHasTerminator() {
			return
		}
		g.Emit(ir.Jump{Target: targets.continueTarget})
	default:
		panic(fmt.Sprintf("unsupported control keyword %q", node.Keyword))
	}
}

func (g *Generator) currentLoopTargets() loopTargets {
	if len(g.loopTargets) == 0 {
		panic("loop control keyword outside a loop")
	}
	return g.loopTargets[len(g.loopTargets)-1]
}

func (g *Generator) pushLoopTargets(breakTarget, continueTarget ir.BlockID) func() {
	g.loopTargets = append(g.loopTargets, loopTargets{
		breakTarget: breakTarget, continueTarget: continueTarget, cleanupDepth: len(g.deferScopes),
	})
	return func() { g.loopTargets = g.loopTargets[:len(g.loopTargets)-1] }
}

func (g *Generator) generateFor(node *parser.ForNode) {
	prevEnv := g.currentEnv
	g.currentEnv = NewEnv(prevEnv)
	defer func() {
		g.currentEnv = prevEnv
	}()

	var condition parser.ExpressionNode
	var post parser.Node
	switch len(node.ExprsOrStmts) {
	case 0:
		// infinite loop
	case 1:
		var ok bool
		condition, ok = node.ExprsOrStmts[0].(parser.ExpressionNode)
		if !ok {
			panic("for loop condition must be an expression")
		}
	case 3:
		g.GenerateNode(node.ExprsOrStmts[0])
		var ok bool
		condition, ok = node.ExprsOrStmts[1].(parser.ExpressionNode)
		if !ok {
			panic("for loop condition must be an expression")
		}
		post = node.ExprsOrStmts[2]
	default:
		panic("for loop must have a condition or initializer, condition, and post expression")
	}

	var conditionBlock *ir.Block
	if condition != nil {
		conditionBlock = g.currentFunction.NewBlock("for.condition")
	}

	bodyBlock := g.currentFunction.NewBlock("for.body")
	endBlock := g.currentFunction.NewBlock("for.end")

	var postBlock *ir.Block
	if post != nil {
		postBlock = g.currentFunction.NewBlock("for.post")
	}

	if !g.currentBlockHasTerminator() {
		if conditionBlock != nil {
			g.Emit(ir.Jump{Target: conditionBlock.ID})
		} else {
			g.Emit(ir.Jump{Target: bodyBlock.ID})
		}
	}

	if conditionBlock != nil {
		g.currentBlock = conditionBlock

		conditionOperand := g.GenerateExpr(condition)
		conditionValue := g.currentFunction.NewValueOfType(types.PrimitiveBool)

		g.Emit(ir.CmpNe{
			Dest:  conditionValue,
			Left:  conditionOperand,
			Right: ir.BoolConstOperand(false),
		})
		g.Emit(ir.Branch{
			Cond: ir.ValueOperand(conditionValue, types.PrimitiveBool),
			Then: bodyBlock.ID,
			Else: endBlock.ID,
		})
	}

	g.currentBlock = bodyBlock
	continueTarget := bodyBlock.ID
	if postBlock != nil {
		continueTarget = postBlock.ID
	} else if conditionBlock != nil {
		continueTarget = conditionBlock.ID
	}
	popLoop := g.pushLoopTargets(endBlock.ID, continueTarget)
	g.generateBlock(node.Body)
	popLoop()
	if !g.currentBlockHasTerminator() {
		if postBlock != nil {
			g.Emit(ir.Jump{Target: postBlock.ID})
		} else if conditionBlock != nil {
			g.Emit(ir.Jump{Target: conditionBlock.ID})
		} else {
			g.Emit(ir.Jump{Target: bodyBlock.ID})
		}
	}

	if postBlock != nil {
		g.currentBlock = postBlock
		g.GenerateNode(post)
		if !g.currentBlockHasTerminator() {
			if conditionBlock != nil {
				g.Emit(ir.Jump{Target: conditionBlock.ID})
			} else {
				g.Emit(ir.Jump{Target: bodyBlock.ID})
			}
		}
	}

	g.currentBlock = endBlock
}

func (g *Generator) generateRangeFor(node *parser.RangeForNode) {
	if node.Symbol == nil {
		panic("range loop symbol is nil")
	}

	prevEnv := g.currentEnv
	g.currentEnv = NewEnv(prevEnv)
	defer func() { g.currentEnv = prevEnv }()

	iteratorSlot := g.currentFunction.NewSlot(node.Symbol.Type, node.Name)
	g.currentEnv.Variables[node.Symbol] = iteratorSlot
	g.Emit(ir.Alloca{Slot: iteratorSlot})
	g.Emit(ir.Store{Slot: iteratorSlot, Value: g.GenerateExpr(node.Start)})

	endSlot := g.currentFunction.NewSlot(node.Symbol.Type, "for.range.end")
	g.Emit(ir.Alloca{Slot: endSlot})
	g.Emit(ir.Store{Slot: endSlot, Value: g.GenerateExpr(node.End)})

	conditionBlock := g.currentFunction.NewBlock("for.range.condition")
	bodyBlock := g.currentFunction.NewBlock("for.range.body")
	postBlock := g.currentFunction.NewBlock("for.range.post")
	endBlock := g.currentFunction.NewBlock("for.range.end")
	g.Emit(ir.Jump{Target: conditionBlock.ID})

	g.currentBlock = conditionBlock
	iterator := g.loadSlot(iteratorSlot, node.Symbol.Type)
	end := g.loadSlot(endSlot, node.Symbol.Type)
	condition := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	if node.Inclusive {
		g.Emit(ir.CmpLe{Dest: condition, Left: iterator, Right: end})
	} else {
		g.Emit(ir.CmpLt{Dest: condition, Left: iterator, Right: end})
	}
	g.Emit(ir.Branch{Cond: ir.ValueOperand(condition, types.PrimitiveBool), Then: bodyBlock.ID, Else: endBlock.ID})

	g.currentBlock = bodyBlock
	popLoop := g.pushLoopTargets(endBlock.ID, postBlock.ID)
	g.generateBlock(node.Body)
	popLoop()
	if !g.currentBlockHasTerminator() {
		g.Emit(ir.Jump{Target: postBlock.ID})
	}

	g.currentBlock = postBlock
	current := g.loadSlot(iteratorSlot, node.Symbol.Type)
	next := g.currentFunction.NewValueOfType(node.Symbol.Type)
	g.Emit(ir.Add{Dest: next, Left: current, Right: ir.IntConstOperand("1", node.Symbol.Type)})
	g.Emit(ir.Store{Slot: iteratorSlot, Value: ir.ValueOperand(next, node.Symbol.Type)})
	g.Emit(ir.Jump{Target: conditionBlock.ID})
	g.currentBlock = endBlock
}

func (g *Generator) generateForEach(node *parser.ForEachNode) {
	if node.Symbol == nil {
		panic("for-each loop symbol is nil")
	}
	sliceType, ok := types.Underlying(node.Iterable.GetType()).(types.SliceType)
	if !ok {
		panic("for-each iterable is not a slice")
	}

	prevEnv := g.currentEnv
	g.currentEnv = NewEnv(prevEnv)
	defer func() { g.currentEnv = prevEnv }()

	sliceSlot := g.currentFunction.NewSlot(sliceType, "for.each.slice")
	g.Emit(ir.Alloca{Slot: sliceSlot})
	g.Emit(ir.Store{Slot: sliceSlot, Value: g.GenerateExpr(node.Iterable)})
	indexSlot := g.currentFunction.NewSlot(types.PrimitiveUsz, "for.each.index")
	g.Emit(ir.Alloca{Slot: indexSlot})
	g.Emit(ir.Store{Slot: indexSlot, Value: ir.IntConstOperand("0", types.PrimitiveUsz)})
	elementSlot := g.currentFunction.NewSlot(node.Symbol.Type, node.Name)
	g.currentEnv.Variables[node.Symbol] = elementSlot
	g.Emit(ir.Alloca{Slot: elementSlot})

	conditionBlock := g.currentFunction.NewBlock("for.each.condition")
	bodyBlock := g.currentFunction.NewBlock("for.each.body")
	postBlock := g.currentFunction.NewBlock("for.each.post")
	endBlock := g.currentFunction.NewBlock("for.each.end")
	g.Emit(ir.Jump{Target: conditionBlock.ID})

	g.currentBlock = conditionBlock
	index := g.loadSlot(indexSlot, types.PrimitiveUsz)
	length := g.sliceLength(sliceSlot, sliceType)
	condition := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	g.Emit(ir.CmpLt{Dest: condition, Left: index, Right: length})
	g.Emit(ir.Branch{Cond: ir.ValueOperand(condition, types.PrimitiveBool), Then: bodyBlock.ID, Else: endBlock.ID})

	g.currentBlock = bodyBlock
	g.storeForEachElement(sliceSlot, sliceType, index, elementSlot)
	popLoop := g.pushLoopTargets(endBlock.ID, postBlock.ID)
	g.generateBlock(node.Body)
	popLoop()
	if !g.currentBlockHasTerminator() {
		g.Emit(ir.Jump{Target: postBlock.ID})
	}

	g.currentBlock = postBlock
	currentIndex := g.loadSlot(indexSlot, types.PrimitiveUsz)
	nextIndex := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
	g.Emit(ir.Add{Dest: nextIndex, Left: currentIndex, Right: ir.IntConstOperand("1", types.PrimitiveUsz)})
	g.Emit(ir.Store{Slot: indexSlot, Value: ir.ValueOperand(nextIndex, types.PrimitiveUsz)})
	g.Emit(ir.Jump{Target: conditionBlock.ID})
	g.currentBlock = endBlock
}

func (g *Generator) loadSlot(slot ir.SlotID, ty types.Type) ir.Operand {
	dst := g.currentFunction.NewValueOfType(ty)
	g.Emit(ir.Load{Dest: dst, Slot: slot})
	return ir.ValueOperand(dst, ty)
}

func (g *Generator) sliceLength(slot ir.SlotID, sliceType types.SliceType) ir.Operand {
	slicePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType})
	g.Emit(ir.AddressOf{Dest: slicePtrID, Slot: slot})
	slicePtr := ir.ValueOperand(slicePtrID, types.PointerType{Base: sliceType})
	lengthPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveUsz})
	g.Emit(ir.FieldAddress{Dest: lengthPtrID, Base: slicePtr, Field: "1"})
	lengthID := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
	g.Emit(ir.LoadPtr{Dest: lengthID, Ptr: ir.ValueOperand(lengthPtrID, types.PointerType{Base: types.PrimitiveUsz})})
	return ir.ValueOperand(lengthID, types.PrimitiveUsz)
}

func (g *Generator) storeForEachElement(sliceSlot ir.SlotID, sliceType types.SliceType, index ir.Operand, elementSlot ir.SlotID) {
	slicePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType})
	g.Emit(ir.AddressOf{Dest: slicePtrID, Slot: sliceSlot})
	slicePtr := ir.ValueOperand(slicePtrID, types.PointerType{Base: sliceType})
	basePtrPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PointerType{Base: sliceType.Base}})
	g.Emit(ir.FieldAddress{Dest: basePtrPtrID, Base: slicePtr, Field: "0"})
	basePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType.Base})
	g.Emit(ir.LoadPtr{Dest: basePtrID, Ptr: ir.ValueOperand(basePtrPtrID, types.PointerType{Base: types.PointerType{Base: sliceType.Base}})})
	elementPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType.Base})
	g.Emit(ir.ElementAddress{
		Dest:    elementPtrID,
		Base:    ir.ValueOperand(basePtrID, types.PointerType{Base: sliceType.Base}),
		Index:   index,
		Element: sliceType.Base,
	})
	elementID := g.currentFunction.NewValueOfType(sliceType.Base)
	g.Emit(ir.LoadPtr{Dest: elementID, Ptr: ir.ValueOperand(elementPtrID, types.PointerType{Base: sliceType.Base})})
	g.Emit(ir.Store{Slot: elementSlot, Value: ir.ValueOperand(elementID, sliceType.Base)})
}

func (g *Generator) GenerateExpr(expr parser.ExpressionNode) ir.Operand {
	switch n := expr.(type) {
	case *parser.IntegerLiteralNode:
		if types.IsFloat(n.GetType()) {
			return ir.FloatConstOperand(n.Value, n.GetType())
		}
		return ir.IntConstOperand(n.Value, n.GetType())
	case *parser.FloatLiteralNode:
		return ir.FloatConstOperand(n.Value, n.GetType())
	case *parser.BoolLiteralNode:
		return ir.BoolConstOperand(n.Value == string(tokeniser.KeywordTrue))
	case *parser.EnumLiteralNode:
		return ir.IntConstOperand(n.Value, n.GetType())
	case *parser.IdentifierNode:
		return g.generateIdentifierExpr(n)
	case *parser.IfExprNode:
		return g.generateIfExpr(n)
	case *parser.GivenExprNode:
		return g.generateGivenExpr(n)
	case *parser.BinaryOpNode:
		return g.generateBinaryExpr(n)
	case *parser.TypeTestNode:
		return g.generateTypeTestExpr(n)
	case *parser.ImplementsTestNode:
		return g.generateImplementsTestExpr(n)
	case *parser.UnaryOpNode:
		return g.generateUnaryExpr(n)
	case *parser.StructLiteralNode:
		return g.generateStructLiteralExpr(n)
	case *parser.SliceLiteralNode:
		return g.generateSliceLiteralExpr(n)
	case *parser.IndexExprNode:
		return g.generateIndexExpr(n)
	case *parser.FunctionCallNode:
		return g.generateFunctionCallExpr(n)
	case *parser.FieldAccessNode:
		return g.generateFieldAccessExpr(n)
	case *parser.CastNode:
		return g.generateCastExpr(n)
	case *parser.SizeOfNode:
		return g.generateSizeOfExpr(n)
	case *parser.SizeOfExprNode:
		return g.generateSizeOfExprExpr(n)
	case *parser.AlignOfNode:
		return g.generateAlignOfExpr(n)
	case *parser.OffsetOfNode:
		return g.generateOffsetOfExpr(n)
	case *parser.StringLiteralNode:
		return g.generateStringLiteralExpr(n)
	case *parser.CStringLiteralNode:
		return g.generateCStringLiteralExpr(n)
	case *parser.CharLiteralNode:
		return ir.IntConstOperand(fmt.Sprint(int(n.Value)), n.GetType())
	case *parser.NilLiteralNode:
		return g.generateNilLiteralExpr(n)
	default:
		fmt.Printf("todo: generate expr %T\n", n)
		panic("todo")
	}
}

func (g *Generator) generateGivenExpr(node *parser.GivenExprNode) ir.Operand {
	prev := g.currentEnv
	g.currentEnv = NewEnv(prev)
	g.deferScopes = append(g.deferScopes, nil)
	defer func() {
		g.deferScopes = g.deferScopes[:len(g.deferScopes)-1]
		g.currentEnv = prev
	}()

	for _, child := range node.Block.Body {
		if g.currentBlockHasTerminator() {
			break
		}
		g.GenerateNode(child)
	}
	if g.currentBlockHasTerminator() {
		// Keep generating a well-formed value in an unreachable block. This lets a
		// given expression contain return, break, or continue without its enclosing
		// expression trying to append instructions after the terminator.
		g.currentBlock = g.currentFunction.NewBlock("given.unreachable")
	}
	result := g.GenerateExpr(node.FinalExpr)
	if !g.currentBlockHasTerminator() {
		g.emitCurrentScopeDefers()
	}
	return result
}

func (g *Generator) generateIndexExpr(node *parser.IndexExprNode) ir.Operand {
	elementPtr := g.generateIndexAddress(node)
	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.LoadPtr{Dest: dst, Ptr: elementPtr})
	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) generateIndexAddress(node *parser.IndexExprNode) ir.Operand {
	index := g.GenerateExpr(node.Index)
	var base ir.Operand
	switch subjectType := types.Underlying(node.Subject.GetType()).(type) {
	case types.SliceType:
		slicePtr := g.generateAddressOfExpr(node.Subject)
		dataPtrPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PointerType{Base: subjectType.Base}})
		g.Emit(ir.FieldAddress{
			Dest:  dataPtrPtrID,
			Base:  slicePtr,
			Field: "0",
		})

		dataPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: subjectType.Base})
		g.Emit(ir.LoadPtr{
			Dest: dataPtrID,
			Ptr:  ir.ValueOperand(dataPtrPtrID, types.PointerType{Base: types.PointerType{Base: subjectType.Base}}),
		})
		base = ir.ValueOperand(dataPtrID, types.PointerType{Base: subjectType.Base})

	case types.PointerType:
		base = g.GenerateExpr(node.Subject)

	default:
		panic(fmt.Sprintf("cannot generate index expression for %T", subjectType))
	}

	elementPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: node.GetType()})
	g.Emit(ir.ElementAddress{
		Dest:    elementPtrID,
		Base:    base,
		Index:   index,
		Element: node.GetType(),
	})
	return ir.ValueOperand(elementPtrID, types.PointerType{Base: node.GetType()})
}

func (g *Generator) generateStringLiteralExpr(node *parser.StringLiteralNode) ir.Operand {
	sliceType, ok := types.Underlying(node.GetType()).(types.SliceType)
	if !ok {
		panic("string literal must have slice type")
	}

	tmpSlot := g.currentFunction.NewSlot(sliceType, "")
	g.Emit(ir.Alloca{Slot: tmpSlot})

	stringPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveChar})
	g.Emit(ir.StringConst{Dest: stringPtrID, Value: node.Value})
	stringPtr := ir.ValueOperand(stringPtrID, types.PointerType{Base: types.PrimitiveChar})

	slicePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType})
	g.Emit(ir.AddressOf{Dest: slicePtrID, Slot: tmpSlot})
	slicePtr := ir.ValueOperand(slicePtrID, types.PointerType{Base: sliceType})

	basePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveChar})
	g.Emit(ir.FieldAddress{Dest: basePtrID, Base: slicePtr, Field: "0"})
	g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(basePtrID, types.PointerType{Base: types.PrimitiveChar}), Value: stringPtr})

	lenPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveUsz})
	g.Emit(ir.FieldAddress{Dest: lenPtrID, Base: slicePtr, Field: "1"})
	g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(lenPtrID, types.PointerType{Base: types.PrimitiveUsz}), Value: ir.IntConstOperand(fmt.Sprintf("%d", sliceType.Size), types.PrimitiveUsz)})

	loaded := g.currentFunction.NewValueOfType(sliceType)
	g.Emit(ir.Load{Dest: loaded, Slot: tmpSlot})
	return ir.ValueOperand(loaded, sliceType)
}

func (g *Generator) generateCStringLiteralExpr(node *parser.CStringLiteralNode) ir.Operand {
	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.StringConst{Dest: dst, Value: node.Value, NullTerminated: true})
	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) generateNilLiteralExpr(node *parser.NilLiteralNode) ir.Operand {
	return ir.NullConstOperand(node.GetType())
}

func (g *Generator) generateCastExpr(node *parser.CastNode) ir.Operand {
	targetType := node.GetType()
	if node.TraitRecast {
		return g.generateTraitRecast(node)
	}
	if node.TraitConversion {
		return g.generateTraitConversion(node)
	}
	if node.TraitUnwrap {
		return g.generateTraitUnwrap(node)
	}

	if str, ok := node.Operand.(*parser.StringLiteralNode); ok {
		if _, ok := types.Underlying(targetType).(types.PointerType); ok {
			dst := g.currentFunction.NewValueOfType(targetType)
			g.Emit(ir.StringConst{Dest: dst, Value: str.Value})
			return ir.ValueOperand(dst, targetType)
		}
	}

	from := g.GenerateExpr(node.Operand)
	if from.Type.Equals(targetType) {
		return from
	}
	if types.Underlying(from.Type).Equals(types.Underlying(targetType)) {
		from.Type = targetType
		return from
	}
	if fromSlice, ok := types.Underlying(from.Type).(types.SliceType); ok {
		if toSlice, ok := types.Underlying(targetType).(types.SliceType); ok && fromSlice.Base.Equals(toSlice.Base) {
			// Fixed-size and dynamic slices have the same runtime representation.
			from.Type = targetType
			return from
		}
	}

	dst := g.currentFunction.NewValueOfType(targetType)
	g.Emit(ir.Cast{Dest: dst, From: from, To: targetType})
	return ir.ValueOperand(dst, targetType)
}

func traitRuntimeName(t types.Type) string {
	switch t := t.(type) {
	case types.DefinedType:
		return t.Module + ":" + t.Name
	case *types.AliasRef:
		return t.Module + ":" + t.Name
	default:
		return t.String()
	}
}

func runtimeTypeID(t types.Type) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(traitRuntimeName(t)))
	return h.Sum64()
}

func traitVTableType(trait types.TraitType) types.StructType {
	fields := []shared.Pair[string, types.Type]{{L: "type_id", R: types.PrimitiveU64}}
	for _, method := range trait.Methods {
		params := append([]types.Type{traitErasedReceiverType(method)}, method.Parameters...)
		fields = append(fields, shared.Pair[string, types.Type]{L: method.Name, R: types.PointerType{Base: types.FunctionType{Parameters: params, ReturnType: method.ReturnType}}})
	}
	return types.StructType{Fields: fields}
}

func traitErasedReceiverType(method types.TraitMethod) types.PointerType {
	return types.PointerType{Base: types.PrimitiveVoid, Mutable: method.Receiver == types.TraitReceiverMutablePointer}
}

func (g *Generator) ensureTraitVTable(node *parser.CastNode, trait types.TraitType) string {
	key := traitRuntimeName(node.ConcreteType) + "__" + trait.String()
	name := "__qk_vtable_" + sanitizeName(key)
	for _, global := range g.Module.Globals {
		if global.Name == name {
			return name
		}
	}
	vt := traitVTableType(trait)
	values := []ir.Operand{ir.IntConstOperand(fmt.Sprintf("%d", runtimeTypeID(node.ConcreteType)), types.PrimitiveU64)}
	concreteModule := g.ModuleName
	if d, ok := node.ConcreteType.(types.DefinedType); ok {
		concreteModule = d.Module
	}
	for i, method := range node.TraitMethods {
		req := trait.Methods[i]
		methodModule := concreteModule
		if method.DefinitionModule != "" {
			methodModule = method.DefinitionModule
		}
		params := append([]types.Type{traitErasedReceiverType(req)}, req.Parameters...)
		fnType := types.PointerType{Base: types.FunctionType{Parameters: params, ReturnType: req.ReturnType}}
		fnName := g.mangleFunctionName(methodModule, method.Name)
		if req.Receiver == types.TraitReceiverValue {
			fnName = g.generateValueReceiverTraitThunk(name, i, node.ConcreteType, fnName, method, req)
		}
		values = append(values, ir.FunctionConstOperand(fnName, fnType))
		if methodModule != g.ModuleName && req.Receiver != types.TraitReceiverValue {
			g.addExternForCall(fnName, ir.FunctionSignature{ParamTypes: method.Signature.Parameters, ReturnType: method.Signature.ReturnType}, "", true)
		}
	}
	g.Module.AddGlobal(ir.Global{Name: name, Type: vt, Linkage: ir.LinkageInternal, Value: ir.StructConstOperand(vt, values)})
	return name
}

func (g *Generator) generateValueReceiverTraitThunk(vtableName string, slot int, concrete types.Type, targetName string, method *symbols.Symbol, requirement types.TraitMethod) string {
	name := fmt.Sprintf("%s__value_%d", vtableName, slot)
	for _, fn := range g.Module.Functions {
		if fn.Name == name {
			return name
		}
	}
	externalTarget := method.DefinitionModule != "" && method.DefinitionModule != g.ModuleName
	if d, ok := concrete.(types.DefinedType); ok && method.DefinitionModule == "" {
		externalTarget = d.Module != g.ModuleName
	}
	if externalTarget {
		g.addExternForCall(targetName, ir.FunctionSignature{ParamTypes: method.Signature.Parameters, ReturnType: method.Signature.ReturnType}, "", true)
	}
	fn := ir.NewFunction(name, ir.LinkageInternal, nil)
	params := append([]types.Type{traitErasedReceiverType(requirement)}, requirement.Parameters...)
	fn.Signature = ir.FunctionSignature{ParamTypes: params, ReturnType: requirement.ReturnType}
	g.Module.AddFunction(fn)

	previousFunction, previousBlock, previousEnv := g.currentFunction, g.currentBlock, g.currentEnv
	previousDefers := g.deferScopes
	g.currentFunction = fn
	g.currentBlock = fn.NewBlock("entry")
	fn.Entry = g.currentBlock.ID
	g.currentEnv = NewEnv(nil)
	g.deferScopes = nil

	incoming := make([]ir.Operand, len(params))
	for i, param := range params {
		fn.AddParameter(fmt.Sprintf("arg%d", i), param, 0)
		id := fn.NewValueOfType(param)
		incoming[i] = ir.ValueOperand(id, param)
	}
	concretePointer := types.PointerType{Base: concrete}
	castID := fn.NewValueOfType(concretePointer)
	g.Emit(ir.Cast{Dest: castID, From: incoming[0], To: concretePointer})
	valueID := fn.NewValueOfType(concrete)
	g.Emit(ir.LoadPtr{Dest: valueID, Ptr: ir.ValueOperand(castID, concretePointer)})
	args := append([]ir.Operand{ir.ValueOperand(valueID, concrete)}, incoming[1:]...)
	callSig := ir.FunctionSignature{ParamTypes: method.Signature.Parameters, ReturnType: method.Signature.ReturnType}
	if requirement.ReturnType.Equals(types.PrimitiveVoid) {
		g.Emit(ir.Call{Name: targetName, Args: args, Signature: callSig})
		g.Emit(ir.Return{})
	} else {
		result := fn.NewValueOfType(requirement.ReturnType)
		g.Emit(ir.Call{Dest: result, Name: targetName, Args: args, Signature: callSig})
		g.Emit(ir.Return{HasValue: true, Value: ir.ValueOperand(result, requirement.ReturnType)})
	}

	g.currentFunction, g.currentBlock, g.currentEnv = previousFunction, previousBlock, previousEnv
	g.deferScopes = previousDefers
	return name
}

func (g *Generator) generateTraitConversion(node *parser.CastNode) ir.Operand {
	target := node.GetType().(types.TraitPointerType)
	data := g.GenerateExpr(node.Operand)
	voidPtr := types.PointerType{Base: types.PrimitiveVoid, Mutable: target.Mutable}
	if !data.Type.Equals(voidPtr) {
		dst := g.currentFunction.NewValueOfType(voidPtr)
		g.Emit(ir.Cast{Dest: dst, From: data, To: voidPtr})
		data = ir.ValueOperand(dst, voidPtr)
	}
	vtName := g.ensureTraitVTable(node, target.Trait)
	vtType := traitVTableType(target.Trait)
	vtPtrType := types.PointerType{Base: vtType}
	vtID := g.currentFunction.NewValueOfType(vtPtrType)
	g.Emit(ir.AddressOfGlobal{Dest: vtID, Name: vtName, Type: vtType})
	agg := ir.ZeroConstOperand(target)
	first := g.currentFunction.NewValueOfType(target)
	g.Emit(ir.InsertValue{Dest: first, Aggregate: agg, Value: data, Index: 0})
	second := g.currentFunction.NewValueOfType(target)
	g.Emit(ir.InsertValue{Dest: second, Aggregate: ir.ValueOperand(first, target), Value: ir.ValueOperand(vtID, vtPtrType), Index: 1})
	return ir.ValueOperand(second, target)
}

func (g *Generator) generateTraitRecast(node *parser.CastNode) ir.Operand {
	sourceValue := g.GenerateExpr(node.Operand)
	source := sourceValue.Type.(types.TraitPointerType)
	target := node.GetType().(types.TraitPointerType)
	sourceVT := traitVTableType(source.Trait)
	sourceVTPtr := types.PointerType{Base: sourceVT}
	vtID := g.currentFunction.NewValueOfType(sourceVTPtr)
	g.Emit(ir.ExtractValue{Dest: vtID, Aggregate: sourceValue, Index: 1})
	typeIDPtr := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveU64})
	g.Emit(ir.FieldAddress{Dest: typeIDPtr, Base: ir.ValueOperand(vtID, sourceVTPtr), Field: "type_id"})
	actualID := g.currentFunction.NewValueOfType(types.PrimitiveU64)
	g.Emit(ir.LoadPtr{Dest: actualID, Ptr: ir.ValueOperand(typeIDPtr, types.PointerType{Base: types.PrimitiveU64})})

	sourceDataType := types.PointerType{Base: types.PrimitiveVoid, Mutable: source.Mutable}
	sourceDataID := g.currentFunction.NewValueOfType(sourceDataType)
	g.Emit(ir.ExtractValue{Dest: sourceDataID, Aggregate: sourceValue, Index: 0})
	data := ir.ValueOperand(sourceDataID, sourceDataType)
	targetDataType := types.PointerType{Base: types.PrimitiveVoid, Mutable: target.Mutable}
	if !data.Type.Equals(targetDataType) {
		castID := g.currentFunction.NewValueOfType(targetDataType)
		g.Emit(ir.Cast{Dest: castID, From: data, To: targetDataType})
		data = ir.ValueOperand(castID, targetDataType)
	}

	resultSlot := g.currentFunction.NewSlot(target, "trait.recast")
	g.Emit(ir.Alloca{Slot: resultSlot})
	end := g.currentFunction.NewBlock("trait.recast.end")
	for _, candidate := range node.TraitCandidates {
		matches := g.currentFunction.NewValueOfType(types.PrimitiveBool)
		g.Emit(ir.CmpEq{Dest: matches, Left: ir.ValueOperand(actualID, types.PrimitiveU64), Right: ir.IntConstOperand(fmt.Sprintf("%d", runtimeTypeID(candidate.ConcreteType)), types.PrimitiveU64)})
		success := g.currentFunction.NewBlock("trait.recast.match")
		next := g.currentFunction.NewBlock("trait.recast.next")
		g.Emit(ir.Branch{Cond: ir.ValueOperand(matches, types.PrimitiveBool), Then: success.ID, Else: next.ID})

		g.currentBlock = success
		candidateCast := &parser.CastNode{ConcreteType: candidate.ConcreteType, TraitMethods: candidate.Methods}
		vtName := g.ensureTraitVTable(candidateCast, target.Trait)
		targetVT := traitVTableType(target.Trait)
		targetVTPtr := types.PointerType{Base: targetVT}
		targetVTID := g.currentFunction.NewValueOfType(targetVTPtr)
		g.Emit(ir.AddressOfGlobal{Dest: targetVTID, Name: vtName, Type: targetVT})
		first := g.currentFunction.NewValueOfType(target)
		g.Emit(ir.InsertValue{Dest: first, Aggregate: ir.ZeroConstOperand(target), Value: data, Index: 0})
		second := g.currentFunction.NewValueOfType(target)
		g.Emit(ir.InsertValue{Dest: second, Aggregate: ir.ValueOperand(first, target), Value: ir.ValueOperand(targetVTID, targetVTPtr), Index: 1})
		g.Emit(ir.Store{Slot: resultSlot, Value: ir.ValueOperand(second, target)})
		g.Emit(ir.Jump{Target: end.ID})
		g.currentBlock = next
	}

	messageText := "trait cast failed: value does not implement " + target.Trait.String()
	messageNode := &parser.StringLiteralNode{Value: messageText, Type: types.SliceType{Base: types.PrimitiveChar, Size: len(messageText)}, Loc: node.Loc}
	message := g.generateStringLiteralExpr(messageNode)
	runtimeStr := types.SliceType{Base: types.PrimitiveChar, Size: -1}
	message.Type = runtimeStr
	panicSig := ir.FunctionSignature{ParamTypes: []types.Type{runtimeStr}, ReturnType: types.PrimitiveVoid}
	g.addExternForCall("__qk_panic", panicSig, "", true)
	g.Emit(ir.Call{Name: "__qk_panic", Args: []ir.Operand{message}, Signature: panicSig})
	g.Emit(ir.Unreachable{})

	g.currentBlock = end
	result := g.currentFunction.NewValueOfType(target)
	g.Emit(ir.Load{Dest: result, Slot: resultSlot})
	return ir.ValueOperand(result, target)
}

func (g *Generator) generateTraitUnwrap(node *parser.CastNode) ir.Operand {
	traitValue := g.GenerateExpr(node.Operand)
	traitPtr := traitValue.Type.(types.TraitPointerType)
	vtType := traitVTableType(traitPtr.Trait)
	vtPtrType := types.PointerType{Base: vtType}
	vtID := g.currentFunction.NewValueOfType(vtPtrType)
	g.Emit(ir.ExtractValue{Dest: vtID, Aggregate: traitValue, Index: 1})
	typeIDPtr := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveU64})
	g.Emit(ir.FieldAddress{Dest: typeIDPtr, Base: ir.ValueOperand(vtID, vtPtrType), Field: "type_id"})
	actualID := g.currentFunction.NewValueOfType(types.PrimitiveU64)
	g.Emit(ir.LoadPtr{Dest: actualID, Ptr: ir.ValueOperand(typeIDPtr, types.PointerType{Base: types.PrimitiveU64})})
	matches := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	g.Emit(ir.CmpEq{Dest: matches, Left: ir.ValueOperand(actualID, types.PrimitiveU64), Right: ir.IntConstOperand(fmt.Sprintf("%d", runtimeTypeID(node.ConcreteType)), types.PrimitiveU64)})
	success := g.currentFunction.NewBlock("trait.unwrap.success")
	failure := g.currentFunction.NewBlock("trait.unwrap.failure")
	g.Emit(ir.Branch{Cond: ir.ValueOperand(matches, types.PrimitiveBool), Then: success.ID, Else: failure.ID})
	g.currentBlock = failure
	messageText := "trait unwrap failed: expected " + traitRuntimeName(node.ConcreteType)
	messageNode := &parser.StringLiteralNode{Value: messageText, Type: types.SliceType{Base: types.PrimitiveChar, Size: len(messageText)}, Loc: node.Loc}
	message := g.generateStringLiteralExpr(messageNode)
	runtimeStr := types.SliceType{Base: types.PrimitiveChar, Size: -1}
	message.Type = runtimeStr
	panicSig := ir.FunctionSignature{ParamTypes: []types.Type{runtimeStr}, ReturnType: types.PrimitiveVoid}
	g.addExternForCall("__qk_panic", panicSig, "", true)
	g.Emit(ir.Call{Name: "__qk_panic", Args: []ir.Operand{message}, Signature: panicSig})
	g.Emit(ir.Unreachable{})
	g.currentBlock = success
	dataType := types.PointerType{Base: types.PrimitiveVoid, Mutable: traitPtr.Mutable}
	dataID := g.currentFunction.NewValueOfType(dataType)
	g.Emit(ir.ExtractValue{Dest: dataID, Aggregate: traitValue, Index: 0})
	targetPointer, byPointer := types.Underlying(node.GetType()).(types.PointerType)
	if !byPointer {
		targetPointer = types.PointerType{Base: node.GetType()}
	}
	castID := g.currentFunction.NewValueOfType(targetPointer)
	g.Emit(ir.Cast{Dest: castID, From: ir.ValueOperand(dataID, dataType), To: targetPointer})
	if byPointer {
		return ir.ValueOperand(castID, node.GetType())
	}
	valueID := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.LoadPtr{Dest: valueID, Ptr: ir.ValueOperand(castID, targetPointer)})
	return ir.ValueOperand(valueID, node.GetType())
}

func (g *Generator) generateTypeTestExpr(node *parser.TypeTestNode) ir.Operand {
	traitValue := g.GenerateExpr(node.Operand)
	traitPtr := traitValue.Type.(types.TraitPointerType)
	vtType := traitVTableType(traitPtr.Trait)
	vtPtrType := types.PointerType{Base: vtType}
	vtID := g.currentFunction.NewValueOfType(vtPtrType)
	g.Emit(ir.ExtractValue{Dest: vtID, Aggregate: traitValue, Index: 1})
	typeIDPtr := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveU64})
	g.Emit(ir.FieldAddress{Dest: typeIDPtr, Base: ir.ValueOperand(vtID, vtPtrType), Field: "type_id"})
	actualID := g.currentFunction.NewValueOfType(types.PrimitiveU64)
	g.Emit(ir.LoadPtr{Dest: actualID, Ptr: ir.ValueOperand(typeIDPtr, types.PointerType{Base: types.PrimitiveU64})})
	matches := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	g.Emit(ir.CmpEq{
		Dest:  matches,
		Left:  ir.ValueOperand(actualID, types.PrimitiveU64),
		Right: ir.IntConstOperand(fmt.Sprintf("%d", runtimeTypeID(node.TargetType)), types.PrimitiveU64),
	})
	return ir.ValueOperand(matches, types.PrimitiveBool)
}

func (g *Generator) generateImplementsTestExpr(node *parser.ImplementsTestNode) ir.Operand {
	if node.CompileTime {
		return ir.BoolConstOperand(node.CompileResult)
	}
	traitValue := g.GenerateExpr(node.Operand)
	if node.Always {
		return ir.BoolConstOperand(true)
	}
	traitPtr := traitValue.Type.(types.TraitPointerType)
	vtType := traitVTableType(traitPtr.Trait)
	vtPtrType := types.PointerType{Base: vtType}
	vtID := g.currentFunction.NewValueOfType(vtPtrType)
	g.Emit(ir.ExtractValue{Dest: vtID, Aggregate: traitValue, Index: 1})
	typeIDPtr := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveU64})
	g.Emit(ir.FieldAddress{Dest: typeIDPtr, Base: ir.ValueOperand(vtID, vtPtrType), Field: "type_id"})
	actualID := g.currentFunction.NewValueOfType(types.PrimitiveU64)
	g.Emit(ir.LoadPtr{Dest: actualID, Ptr: ir.ValueOperand(typeIDPtr, types.PointerType{Base: types.PrimitiveU64})})
	result := ir.BoolConstOperand(false)
	for _, candidate := range node.Candidates {
		matches := g.currentFunction.NewValueOfType(types.PrimitiveBool)
		g.Emit(ir.CmpEq{Dest: matches, Left: ir.ValueOperand(actualID, types.PrimitiveU64), Right: ir.IntConstOperand(fmt.Sprintf("%d", runtimeTypeID(candidate)), types.PrimitiveU64)})
		match := ir.ValueOperand(matches, types.PrimitiveBool)
		if result.Kind == ir.OperandBoolConst && !result.BoolValue {
			result = match
			continue
		}
		combined := g.currentFunction.NewValueOfType(types.PrimitiveBool)
		g.Emit(ir.LogicalOr{Dest: combined, Left: result, Right: match})
		result = ir.ValueOperand(combined, types.PrimitiveBool)
	}
	return result
}

func (g *Generator) generateSizeOfExpr(node *parser.SizeOfNode) ir.Operand {
	dst := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
	g.Emit(ir.Sizeof{Dest: dst, Type: node.OperandType})
	return ir.ValueOperand(dst, types.PrimitiveUsz)
}

func (g *Generator) generateSizeOfExprExpr(node *parser.SizeOfExprNode) ir.Operand {
	dst := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
	g.Emit(ir.Sizeof{Dest: dst, Type: node.Operand.GetType()})
	return ir.ValueOperand(dst, types.PrimitiveUsz)
}

func (g *Generator) generateAlignOfExpr(node *parser.AlignOfNode) ir.Operand {
	dst := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
	g.Emit(ir.Alignof{Dest: dst, Type: node.OperandType})
	return ir.ValueOperand(dst, types.PrimitiveUsz)
}

func (g *Generator) generateOffsetOfExpr(node *parser.OffsetOfNode) ir.Operand {
	dst := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
	g.Emit(ir.Offsetof{Dest: dst, Type: node.OperandType, Field: node.Field})
	return ir.ValueOperand(dst, types.PrimitiveUsz)
}

func (g *Generator) generateStructLiteralExpr(node *parser.StructLiteralNode) ir.Operand {
	tmpSlot := g.currentFunction.NewSlot(node.GetType(), "")
	g.Emit(ir.Alloca{Slot: tmpSlot})
	g.generateStructLiteralIntoSlot(tmpSlot, node)

	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.Load{Dest: dst, Slot: tmpSlot})
	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) generateSliceLiteralExpr(node *parser.SliceLiteralNode) ir.Operand {
	sliceType, ok := types.Underlying(node.GetType()).(types.SliceType)
	if !ok {
		panic("slice literal must have slice type")
	}

	tmpSlot := g.currentFunction.NewSlot(sliceType, "")
	g.Emit(ir.Alloca{Slot: tmpSlot})
	if node.RepeatValue != nil {
		return g.generateRepeatedSliceLiteral(node, sliceType, tmpSlot)
	}

	slicePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType})
	g.Emit(ir.AddressOf{Dest: slicePtrID, Slot: tmpSlot})
	slicePtr := ir.ValueOperand(slicePtrID, types.PointerType{Base: sliceType})

	var elemPtr ir.Operand
	if len(node.Elements) == 0 {
		elemPtr = ir.NullConstOperand(types.PointerType{Base: sliceType.Base})
	} else {
		bufferType := types.StructType{
			Fields: make([]shared.Pair[string, types.Type], len(node.Elements)),
		}
		for i := range node.Elements {
			bufferType.Fields[i] = shared.Pair[string, types.Type]{
				L: fmt.Sprintf("%d", i),
				R: sliceType.Base,
			}
		}

		bufferSlot := g.currentFunction.NewSlot(bufferType, "")
		g.Emit(ir.Alloca{Slot: bufferSlot})

		bufferPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: bufferType})
		g.Emit(ir.AddressOf{Dest: bufferPtrID, Slot: bufferSlot})
		bufferPtr := ir.ValueOperand(bufferPtrID, types.PointerType{Base: bufferType})

		for i, element := range node.Elements {
			fieldPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType.Base})
			g.Emit(ir.FieldAddress{Dest: fieldPtrID, Base: bufferPtr, Field: fmt.Sprintf("%d", i)})
			value := g.GenerateExpr(element)
			g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(fieldPtrID, types.PointerType{Base: sliceType.Base}), Value: value})
		}

		elemPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType.Base})
		g.Emit(ir.FieldAddress{Dest: elemPtrID, Base: bufferPtr, Field: "0"})
		elemPtr = ir.ValueOperand(elemPtrID, types.PointerType{Base: sliceType.Base})
	}

	basePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType.Base})
	g.Emit(ir.FieldAddress{Dest: basePtrID, Base: slicePtr, Field: "0"})
	g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(basePtrID, types.PointerType{Base: sliceType.Base}), Value: elemPtr})

	lenPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveUsz})
	g.Emit(ir.FieldAddress{Dest: lenPtrID, Base: slicePtr, Field: "1"})
	g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(lenPtrID, types.PointerType{Base: types.PrimitiveUsz}), Value: ir.IntConstOperand(fmt.Sprintf("%d", len(node.Elements)), types.PrimitiveUsz)})

	loaded := g.currentFunction.NewValueOfType(sliceType)
	g.Emit(ir.Load{Dest: loaded, Slot: tmpSlot})
	return ir.ValueOperand(loaded, sliceType)
}

func (g *Generator) generateRepeatedSliceLiteral(node *parser.SliceLiteralNode, sliceType types.SliceType, tmpSlot ir.SlotID) ir.Operand {
	value := g.GenerateExpr(node.RepeatValue)
	count := g.GenerateExpr(node.RepeatAmount)

	bufferID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType.Base})
	g.Emit(ir.AllocaArray{Dest: bufferID, Element: sliceType.Base, Count: count})
	buffer := ir.ValueOperand(bufferID, types.PointerType{Base: sliceType.Base})

	indexSlot := g.currentFunction.NewSlot(types.PrimitiveUsz, "repeat.index")
	g.Emit(ir.Alloca{Slot: indexSlot})
	g.Emit(ir.Store{Slot: indexSlot, Value: ir.IntConstOperand("0", types.PrimitiveUsz)})

	conditionBlock := g.currentFunction.NewBlock("repeat.condition")
	bodyBlock := g.currentFunction.NewBlock("repeat.body")
	endBlock := g.currentFunction.NewBlock("repeat.end")
	g.Emit(ir.Jump{Target: conditionBlock.ID})

	g.currentBlock = conditionBlock
	indexID := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
	g.Emit(ir.Load{Dest: indexID, Slot: indexSlot})
	index := ir.ValueOperand(indexID, types.PrimitiveUsz)
	conditionID := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	g.Emit(ir.CmpLt{Dest: conditionID, Left: index, Right: count})
	g.Emit(ir.Branch{Cond: ir.ValueOperand(conditionID, types.PrimitiveBool), Then: bodyBlock.ID, Else: endBlock.ID})

	g.currentBlock = bodyBlock
	elementPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType.Base})
	g.Emit(ir.ElementAddress{Dest: elementPtrID, Base: buffer, Index: index, Element: sliceType.Base})
	g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(elementPtrID, types.PointerType{Base: sliceType.Base}), Value: value})
	nextID := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
	g.Emit(ir.Add{Dest: nextID, Left: index, Right: ir.IntConstOperand("1", types.PrimitiveUsz)})
	g.Emit(ir.Store{Slot: indexSlot, Value: ir.ValueOperand(nextID, types.PrimitiveUsz)})
	g.Emit(ir.Jump{Target: conditionBlock.ID})

	g.currentBlock = endBlock
	slicePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType})
	g.Emit(ir.AddressOf{Dest: slicePtrID, Slot: tmpSlot})
	slicePtr := ir.ValueOperand(slicePtrID, types.PointerType{Base: sliceType})
	basePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PointerType{Base: sliceType.Base}})
	g.Emit(ir.FieldAddress{Dest: basePtrID, Base: slicePtr, Field: "0"})
	g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(basePtrID, types.PointerType{Base: types.PointerType{Base: sliceType.Base}}), Value: buffer})
	lenPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveUsz})
	g.Emit(ir.FieldAddress{Dest: lenPtrID, Base: slicePtr, Field: "1"})
	g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(lenPtrID, types.PointerType{Base: types.PrimitiveUsz}), Value: count})

	loaded := g.currentFunction.NewValueOfType(sliceType)
	g.Emit(ir.Load{Dest: loaded, Slot: tmpSlot})
	return ir.ValueOperand(loaded, sliceType)
}

func (g *Generator) generateStructLiteralIntoSlot(slot ir.SlotID, node *parser.StructLiteralNode) {

	basePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: node.GetType()})
	g.Emit(ir.AddressOf{Dest: basePtrID, Slot: slot})
	basePtr := ir.ValueOperand(basePtrID, types.PointerType{Base: node.GetType()})

	for _, field := range node.Fields {
		fieldTy := node.GetType()
		switch composite := types.Underlying(node.GetType()).(type) {
		case types.StructType:
			for _, f := range composite.Fields {
				if f.L == field.L {
					fieldTy = f.R
					break
				}
				if f.L == "" {
					if embedded, ok := f.R.(types.UnionType); ok {
						for _, unionField := range embedded.Fields {
							if unionField.L == field.L {
								fieldTy = unionField.R
								break
							}
						}
					}
				}
			}
		case types.UnionType:
			for _, f := range composite.Fields {
				if f.L == field.L {
					fieldTy = f.R
					break
				}
			}
		}
		fieldPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: fieldTy})
		g.Emit(ir.FieldAddress{Dest: fieldPtrID, Base: basePtr, Field: field.L})

		value := g.GenerateExpr(field.R)
		g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(fieldPtrID, types.PointerType{Base: fieldTy}), Value: value})
	}
}

func (g *Generator) generateFieldAccessExpr(node *parser.FieldAccessNode) ir.Operand {
	if node.MethodSymbol != nil {
		ident := &parser.IdentifierNode{Name: node.MethodSymbol.Name, Symbol: node.MethodSymbol, Type: node.GetType(), Loc: node.Loc}
		if node.MethodModule != g.ModuleName {
			ident.Module, ident.ResolvedModuleName = node.MethodModule, node.MethodModule
		}
		return g.generateIdentifierExpr(ident)
	}
	if node.IsEnumValue {
		return ir.IntConstOperand(node.EnumValue, node.GetType())
	}
	basePtr := g.generateFieldSubjectAddress(node.Subject)

	fieldPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: node.GetType()})
	g.Emit(ir.FieldAddress{Dest: fieldPtrID, Base: basePtr, Field: node.Field.Name})

	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.LoadPtr{Dest: dst, Ptr: ir.ValueOperand(fieldPtrID, types.PointerType{Base: node.GetType()})})
	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) generateFieldSubjectAddress(subject parser.ExpressionNode) ir.Operand {
	if _, ok := types.Underlying(subject.GetType()).(types.PointerType); ok {
		return g.GenerateExpr(subject)
	}
	return g.generateAddressOfExpr(subject)
}

func (g *Generator) generateIfExpr(node *parser.IfExprNode) ir.Operand {
	resultType := node.GetType()
	tmpSlot := g.currentFunction.NewSlot(resultType, "ifexpr.tmp")
	g.Emit(ir.Alloca{Slot: tmpSlot})

	mergeBlock := g.currentFunction.NewBlock("ifexpr.merge")
	thenBlock := g.currentFunction.NewBlock("ifexpr.then")

	elseTarget := mergeBlock
	if len(node.ElseIfBranches) > 0 || node.ElseBranch != nil {
		elseTarget = g.currentFunction.NewBlock("ifexpr.else")
	}

	cond := g.GenerateExpr(node.IfBranch.Condition)
	condVal := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	g.Emit(ir.CmpNe{Dest: condVal, Left: cond, Right: ir.BoolConstOperand(false)})
	g.Emit(ir.Branch{Cond: ir.ValueOperand(condVal, types.PrimitiveBool), Then: thenBlock.ID, Else: elseTarget.ID})

	g.currentBlock = thenBlock
	thenVal := g.GenerateExpr(node.IfBranch.Node)
	g.Emit(ir.Store{Slot: tmpSlot, Value: thenVal})
	if !g.currentBlockHasTerminator() {
		g.Emit(ir.Jump{Target: mergeBlock.ID})
	}

	if elseTarget != mergeBlock {
		g.currentBlock = elseTarget

		for i, elif := range node.ElseIfBranches {
			thenB := g.currentFunction.NewBlock(fmt.Sprintf("ifexpr.elseif.then.%d", i))

			next := mergeBlock
			if i < len(node.ElseIfBranches)-1 || node.ElseBranch != nil {
				next = g.currentFunction.NewBlock(fmt.Sprintf("ifexpr.elseif.next.%d", i))
			}

			c := g.GenerateExpr(elif.Condition)
			cval := g.currentFunction.NewValueOfType(types.PrimitiveBool)
			g.Emit(ir.CmpNe{Dest: cval, Left: c, Right: ir.BoolConstOperand(false)})
			g.Emit(ir.Branch{Cond: ir.ValueOperand(cval, types.PrimitiveBool), Then: thenB.ID, Else: next.ID})

			g.currentBlock = thenB
			v := g.GenerateExpr(elif.Node)
			g.Emit(ir.Store{Slot: tmpSlot, Value: v})
			if !g.currentBlockHasTerminator() {
				g.Emit(ir.Jump{Target: mergeBlock.ID})
			}

			g.currentBlock = next
		}

		if node.ElseBranch != nil {
			v := g.GenerateExpr(node.ElseBranch)
			g.Emit(ir.Store{Slot: tmpSlot, Value: v})
			if !g.currentBlockHasTerminator() {
				g.Emit(ir.Jump{Target: mergeBlock.ID})
			}
		}
	}

	g.currentBlock = mergeBlock
	dst := g.currentFunction.NewValueOfType(resultType)
	g.Emit(ir.Load{Dest: dst, Slot: tmpSlot})
	return ir.ValueOperand(dst, resultType)
}

func (g *Generator) generateAddressOfExpr(expr parser.ExpressionNode) ir.Operand {
	switch node := expr.(type) {
	case *parser.IdentifierNode:
		ident := node
		if ident.Symbol == nil {
			panic("identifier symbol is nil")
		}

		slot, ok := g.currentEnv.Lookup(ident.Symbol)
		if !ok {
			global := g.globalNameForIdentifier(ident)
			addr := g.currentFunction.NewValueOfType(types.PointerType{Base: ident.GetType()})
			g.Emit(ir.AddressOfGlobal{Dest: addr, Name: global, Type: ident.GetType()})
			return ir.ValueOperand(addr, types.PointerType{Base: ident.GetType()})
		}

		addr := g.currentFunction.NewValueOfType(types.PointerType{Base: ident.GetType()})
		g.Emit(ir.AddressOf{Dest: addr, Slot: slot})
		return ir.ValueOperand(addr, types.PointerType{Base: ident.GetType()})
	case *parser.FieldAccessNode:
		base := g.generateFieldSubjectAddress(node.Subject)
		dest := g.currentFunction.NewValueOfType(types.PointerType{Base: node.GetType()})
		g.Emit(ir.FieldAddress{Dest: dest, Base: base, Field: node.Field.Name})
		return ir.ValueOperand(dest, types.PointerType{Base: node.GetType()})
	case *parser.IndexExprNode:
		return g.generateIndexAddress(node)
	case *parser.UnaryOpNode:
		if node.Op == parser.UnaryOpDereference {
			return g.GenerateExpr(node.Operand)
		}
	}

	value := g.GenerateExpr(expr)
	tmpSlot := g.currentFunction.NewSlot(expr.GetType(), "")
	g.Emit(ir.Alloca{Slot: tmpSlot})
	g.Emit(ir.Store{Slot: tmpSlot, Value: value})

	addr := g.currentFunction.NewValueOfType(types.PointerType{Base: expr.GetType()})
	g.Emit(ir.AddressOf{Dest: addr, Slot: tmpSlot})
	return ir.ValueOperand(addr, types.PointerType{Base: expr.GetType()})
}

func (g *Generator) generateFunctionCallExpr(node *parser.FunctionCallNode) ir.Operand {
	if node.TraitCall {
		return g.generateTraitCall(node)
	}
	var callee *ir.Operand
	if node.Symbol == nil {
		value := g.GenerateExpr(node.Callee)
		callee = &value
	}
	args := make([]ir.Operand, 0, len(node.Args))
	if node.TypedVariadic {
		for _, arg := range node.Args[:node.TypedVariadicStart] {
			args = append(args, g.GenerateExpr(arg))
		}
		if node.VariadicExpansion {
			args = append(args, g.GenerateExpr(node.Args[node.TypedVariadicStart]))
		} else {
			sliceType := node.TypedVariadicSlice
			packed := &parser.SliceLiteralNode{Elements: append([]parser.ExpressionNode(nil), node.Args[node.TypedVariadicStart:]...), Type: sliceType, Loc: node.Loc}
			args = append(args, g.generateSliceLiteralExpr(packed))
		}
	} else {
		for _, arg := range node.Args {
			args = append(args, g.GenerateExpr(arg))
		}
	}
	callSig := g.buildCallSignature(node)
	args = g.promoteVariadicArgs(args, callSig)

	name := ""
	if node.Name != nil {
		name = node.Name.String()
	}
	if node.Symbol != nil {
		name = node.Symbol.Name
		if node.Symbol.Name == "panic" {
			name = "__qk_panic"
		}

		foreignAttr := node.Symbol.Attributes.Get(attributes.AttributeTypeForeign)
		if foreign, ok := foreignAttr.(attributes.FunctionAttributeForeign); ok &&
			node.Name != nil && node.Name.Module != "" {
			g.addExternForCall(node.Symbol.Name, callSig, foreign.From, false)
		}

		if export, ok := node.Symbol.Attributes.Get(attributes.AttributeTypeExport).(attributes.FunctionAttributeExport); ok {
			name = export.As
		} else if foreignAttr == nil && node.Symbol.Name != "panic" {
			callModule := g.ModuleName
			if node.Name != nil && node.Name.Module != "" {
				callModule = node.Name.Module
				if node.Name.ResolvedModuleName != "" {
					callModule = node.Name.ResolvedModuleName
				}
			}
			if g.isProgramEntryFunction(node.Symbol.Name) {
				name = node.Symbol.Name
			} else {
				name = g.mangleFunctionName(callModule, node.Symbol.Name)
			}
		}

		if node.Name != nil && node.Name.Module != "" && foreignAttr == nil {
			hidden := node.Symbol.Attributes.Get(attributes.AttributeTypeExport) == nil
			g.addExternForCall(name, callSig, "", hidden)
		}

		if !node.Symbol.Signature.TypedVariadic && len(node.Args) < len(node.Symbol.Signature.Parameters) {
			name = g.defaultWrapperName(name, len(node.Args))
			callSig.ParamTypes = append([]types.Type(nil), node.Symbol.Signature.Parameters[:len(node.Args)]...)
			callSig.Variadic = false
			callSig.Attributes = nil
			if node.Name != nil && node.Name.Module != "" {
				hidden := node.Symbol.Attributes.Get(attributes.AttributeTypeExport) == nil
				g.addExternForCall(name, callSig, "", hidden)
			}
		}
	}
	if node.Symbol != nil && node.Symbol.Name == "panic" {
		g.addExternForCall("__qk_panic", callSig, "", true)
	}

	if node.GetType().Equals(types.PrimitiveVoid) {
		g.Emit(ir.Call{
			Name:      name,
			Callee:    callee,
			Args:      args,
			Signature: callSig,
		})
		if node.Symbol != nil && node.Symbol.Name == "panic" {
			g.Emit(ir.Unreachable{})
		}
		return ir.NullConstOperand(types.PrimitiveVoid)
	}

	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.Call{Dest: dst, Name: name, Callee: callee, Args: args, Signature: callSig})
	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) generateTraitCall(node *parser.FunctionCallNode) ir.Operand {
	receiver := g.GenerateExpr(node.Args[0])
	traitPtr := receiver.Type.(types.TraitPointerType)
	vtType := traitVTableType(traitPtr.Trait)
	vtPtrType := types.PointerType{Base: vtType}
	vtID := g.currentFunction.NewValueOfType(vtPtrType)
	g.Emit(ir.ExtractValue{Dest: vtID, Aggregate: receiver, Index: 1})
	requirement := traitPtr.Trait.Methods[node.TraitSlot]
	params := append([]types.Type{traitErasedReceiverType(requirement)}, requirement.Parameters...)
	fnType := types.PointerType{Base: types.FunctionType{Parameters: params, ReturnType: requirement.ReturnType}}
	fnPtrPtr := g.currentFunction.NewValueOfType(types.PointerType{Base: fnType})
	g.Emit(ir.FieldAddress{Dest: fnPtrPtr, Base: ir.ValueOperand(vtID, vtPtrType), Field: requirement.Name})
	fnID := g.currentFunction.NewValueOfType(fnType)
	g.Emit(ir.LoadPtr{Dest: fnID, Ptr: ir.ValueOperand(fnPtrPtr, types.PointerType{Base: fnType})})
	dataType := params[0]
	dataID := g.currentFunction.NewValueOfType(dataType)
	g.Emit(ir.ExtractValue{Dest: dataID, Aggregate: receiver, Index: 0})
	args := []ir.Operand{ir.ValueOperand(dataID, dataType)}
	for _, arg := range node.Args[1:] {
		args = append(args, g.GenerateExpr(arg))
	}
	sig := ir.FunctionSignature{ParamTypes: params, ReturnType: requirement.ReturnType}
	callee := ir.ValueOperand(fnID, fnType)
	if requirement.ReturnType.Equals(types.PrimitiveVoid) {
		g.Emit(ir.Call{Callee: &callee, Args: args, Signature: sig})
		return ir.NullConstOperand(types.PrimitiveVoid)
	}
	dst := g.currentFunction.NewValueOfType(requirement.ReturnType)
	g.Emit(ir.Call{Dest: dst, Callee: &callee, Args: args, Signature: sig})
	return ir.ValueOperand(dst, requirement.ReturnType)
}

func (g *Generator) addExternForCall(name string, signature ir.FunctionSignature, from string, hidden bool) {
	for _, extern := range g.Module.Externs {
		if extern.Name == name {
			return
		}
	}
	visibility := ir.VisibilityDefault
	if hidden {
		visibility = ir.VisibilityHidden
	}
	g.Module.AddExtern(ir.ExternDecl{Name: name, Signature: signature, From: from, Visibility: visibility})
}

func (g *Generator) promoteVariadicArgs(args []ir.Operand, sig ir.FunctionSignature) []ir.Operand {
	if !sig.Variadic {
		return args
	}

	for i := len(sig.ParamTypes); i < len(args); i++ {
		if !args[i].Type.Equals(types.PrimitiveF32) {
			continue
		}

		dst := g.currentFunction.NewValueOfType(types.PrimitiveF64)
		g.Emit(ir.Cast{Dest: dst, From: args[i], To: types.PrimitiveF64})
		args[i] = ir.ValueOperand(dst, types.PrimitiveF64)
	}

	return args
}

func (g *Generator) mangleFunctionName(moduleName, fnName string) string {
	mod := sanitizeName(moduleName)
	fn := sanitizeName(fnName)
	if mod == "" {
		return "__qk_" + fn
	}
	return "__qk_" + mod + "_" + fn
}

func (g *Generator) mangleGlobalName(moduleName, globalName string) string {
	mod := sanitizeName(moduleName)
	global := sanitizeName(globalName)
	if mod == "" {
		return "__qk_global_" + global
	}
	return "__qk_" + mod + "_global_" + global
}

func (g *Generator) isProgramEntryFunction(fnName string) bool {
	return g.ModuleName == g.MainModule && fnName == "main"
}

func sanitizeName(name string) string {
	if name == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(name))
	for i, r := range name {
		valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (r >= '0' && r <= '9')
		if !valid {
			b.WriteByte('_')
			continue
		}
		if i == 0 && r >= '0' && r <= '9' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (g *Generator) buildCallSignature(node *parser.FunctionCallNode) ir.FunctionSignature {
	sig := ir.FunctionSignature{ReturnType: node.GetType()}
	for _, arg := range node.Args {
		sig.ParamTypes = append(sig.ParamTypes, arg.GetType())
	}

	if node.Symbol != nil && node.Symbol.Signature != nil {
		sig.ParamTypes = append([]types.Type(nil), node.Symbol.Signature.Parameters...)
		sig.ReturnType = node.Symbol.Signature.ReturnType
		sig.Variadic = node.Symbol.Signature.Variadic
		sig.Attributes = node.Symbol.Attributes
	}
	if node.Symbol == nil {
		ptr := types.Underlying(node.Callee.GetType()).(types.PointerType)
		fn := types.Underlying(ptr.Base).(types.FunctionType)
		sig.ParamTypes = append([]types.Type(nil), fn.Parameters...)
		sig.ReturnType = fn.ReturnType
	}

	return sig
}

func (g *Generator) generateIdentifierExpr(node *parser.IdentifierNode) ir.Operand {
	if node.Symbol == nil {
		panic("identifier symbol is nil")
	}
	if node.Symbol.Kind == symbols.SymbolKindFunction {
		name, sig := g.functionValueName(node)
		if node.Module != "" {
			from := ""
			if foreign, ok := node.Symbol.Attributes.Get(attributes.AttributeTypeForeign).(attributes.FunctionAttributeForeign); ok {
				from = foreign.From
			}
			hidden := node.Symbol.Attributes.Get(attributes.AttributeTypeForeign) == nil &&
				node.Symbol.Attributes.Get(attributes.AttributeTypeExport) == nil
			g.addExternForCall(name, sig, from, hidden)
		}
		return ir.FunctionConstOperand(name, node.GetType())
	}
	if node.Module != "" {
		return g.generateModuleIdentifierExpr(node)
	}

	slot, ok := g.currentEnv.Lookup(node.Symbol)
	if !ok {
		global, exists := g.globals[node.Symbol]
		if !exists {
			panic("identifier slot not found")
		}
		dst := g.currentFunction.NewValueOfType(node.GetType())
		g.Emit(ir.LoadGlobal{Dest: dst, Name: global, Type: node.GetType()})
		return ir.ValueOperand(dst, node.GetType())
	}

	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.Load{Dest: dst, Slot: slot})

	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) functionValueName(node *parser.IdentifierNode) (string, ir.FunctionSignature) {
	sym := node.Symbol
	ret := sym.Signature.ReturnType
	if ret == nil {
		ret = types.PrimitiveVoid
	}
	sig := ir.FunctionSignature{ParamTypes: append([]types.Type(nil), sym.Signature.Parameters...), ReturnType: ret, Variadic: sym.Signature.Variadic, Attributes: sym.Attributes}
	name := sym.Name
	if export, ok := sym.Attributes.Get(attributes.AttributeTypeExport).(attributes.FunctionAttributeExport); ok {
		name = export.As
	} else if foreign := sym.Attributes.Get(attributes.AttributeTypeForeign); foreign == nil {
		module := g.ModuleName
		if node.Module != "" {
			module = node.Module
		}
		if node.ResolvedModuleName != "" {
			module = node.ResolvedModuleName
		}
		if !g.isProgramEntryFunction(sym.Name) {
			name = g.mangleFunctionName(module, sym.Name)
		}
	}
	return name, sig
}

func (g *Generator) generateModuleIdentifierExpr(node *parser.IdentifierNode) ir.Operand {
	if node.Symbol == nil || node.Symbol.Kind != symbols.SymbolKindVariable {
		panic("module access is not a variable")
	}

	name := g.globalNameForIdentifier(node)

	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.LoadGlobal{
		Dest: dst,
		Name: name,
		Type: node.GetType(),
	})
	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) globalNameForIdentifier(node *parser.IdentifierNode) string {
	if node.Symbol == nil {
		panic("identifier symbol is nil")
	}
	if node.Module == "" {
		if name, ok := g.globals[node.Symbol]; ok {
			return name
		}
		panic("identifier slot not found")
	}

	moduleName := node.Module
	if node.ResolvedModuleName != "" {
		moduleName = node.ResolvedModuleName
	}
	name := g.mangleGlobalName(moduleName, node.Symbol.Name)
	if foreign, ok := node.Symbol.Attributes.Get(attributes.AttributeTypeForeign).(attributes.FunctionAttributeForeign); ok {
		name = foreign.From
	}
	visibility := ir.VisibilityHidden
	if node.Symbol.Attributes.Get(attributes.AttributeTypeForeign) != nil {
		visibility = ir.VisibilityDefault
	}
	g.Module.AddExternGlobal(ir.ExternGlobal{Name: name, Type: node.GetType(), Mutable: node.Symbol.Mutable, Visibility: visibility})
	return name
}

func (g *Generator) generateBinaryExpr(node *parser.BinaryOpNode) ir.Operand {
	if node.Op == parser.BinaryOpLogicalAnd || node.Op == parser.BinaryOpLogicalOr {
		return g.generateShortCircuitExpr(node)
	}
	leftPtr, leftIsPtr := types.Underlying(node.Operand1.GetType()).(types.PointerType)
	_, rightIsPtr := types.Underlying(node.Operand2.GetType()).(types.PointerType)
	if node.Op == parser.BinaryOpAdd && (leftIsPtr || rightIsPtr) {
		return g.generatePointerOffset(node, false)
	}
	if node.Op == parser.BinaryOpSubtract && leftIsPtr && rightIsPtr {
		return g.generatePointerDifference(node, leftPtr.Base)
	}
	if node.Op == parser.BinaryOpSubtract && leftIsPtr {
		return g.generatePointerOffset(node, true)
	}

	left := g.GenerateExpr(node.Operand1)
	right := g.GenerateExpr(node.Operand2)
	return g.emitBinaryOperation(node.Op, left, right, node.GetType())
}

func (g *Generator) generatePointerOffset(node *parser.BinaryOpNode, subtract bool) ir.Operand {
	pointerNode, offsetNode := node.Operand1, node.Operand2
	if _, leftIsPtr := types.Underlying(pointerNode.GetType()).(types.PointerType); !leftIsPtr {
		pointerNode, offsetNode = offsetNode, pointerNode
	}

	pointer := g.GenerateExpr(pointerNode)
	offset := g.GenerateExpr(offsetNode)
	if subtract {
		negated := g.currentFunction.NewValueOfType(offset.Type)
		g.Emit(ir.Negate{Dest: negated, Operand: offset})
		offset = ir.ValueOperand(negated, offset.Type)
	}

	pointerType := types.Underlying(pointer.Type).(types.PointerType)
	dest := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.ElementAddress{Dest: dest, Base: pointer, Index: offset, Element: pointerType.Base})
	return ir.ValueOperand(dest, node.GetType())
}

func (g *Generator) generatePointerDifference(node *parser.BinaryOpNode, elementType types.Type) ir.Operand {
	left := g.GenerateExpr(node.Operand1)
	right := g.GenerateExpr(node.Operand2)

	leftIntID := g.currentFunction.NewValueOfType(types.PrimitiveIsz)
	g.Emit(ir.Cast{Dest: leftIntID, From: left, To: types.PrimitiveIsz})
	rightIntID := g.currentFunction.NewValueOfType(types.PrimitiveIsz)
	g.Emit(ir.Cast{Dest: rightIntID, From: right, To: types.PrimitiveIsz})

	bytesID := g.currentFunction.NewValueOfType(types.PrimitiveIsz)
	g.Emit(ir.Sub{
		Dest:  bytesID,
		Left:  ir.ValueOperand(leftIntID, types.PrimitiveIsz),
		Right: ir.ValueOperand(rightIntID, types.PrimitiveIsz),
	})

	sizeID := g.currentFunction.NewValueOfType(types.PrimitiveIsz)
	g.Emit(ir.Sizeof{Dest: sizeID, Type: elementType})
	elementsID := g.currentFunction.NewValueOfType(types.PrimitiveIsz)
	g.Emit(ir.Div{
		Dest:  elementsID,
		Left:  ir.ValueOperand(bytesID, types.PrimitiveIsz),
		Right: ir.ValueOperand(sizeID, types.PrimitiveIsz),
	})
	return ir.ValueOperand(elementsID, types.PrimitiveIsz)
}

func (g *Generator) emitBinaryOperation(op parser.BinaryOpKind, left, right ir.Operand, resultType types.Type) ir.Operand {
	dst := g.currentFunction.NewValueOfType(resultType)

	switch op {
	case parser.BinaryOpAdd:
		g.Emit(ir.Add{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpSubtract:
		g.Emit(ir.Sub{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpMultiply:
		g.Emit(ir.Mul{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpDivide:
		g.Emit(ir.Div{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpEqual:
		g.Emit(ir.CmpEq{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpNotEqual:
		g.Emit(ir.CmpNe{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpLess:
		g.Emit(ir.CmpLt{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpLessEqual:
		g.Emit(ir.CmpLe{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpGreater:
		g.Emit(ir.CmpGt{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpGreaterEqual:
		g.Emit(ir.CmpGe{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpModulo:
		g.Emit(ir.Mod{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpLogicalAnd:
		g.Emit(ir.LogicalAnd{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpLogicalOr:
		g.Emit(ir.LogicalOr{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpBitwiseAnd:
		g.Emit(ir.BitwiseAnd{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpBitwiseOr:
		g.Emit(ir.BitwiseOr{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpBitwiseXor:
		g.Emit(ir.BitwiseXor{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpShiftLeft:
		g.Emit(ir.ShiftLeft{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpShiftRight:
		g.Emit(ir.ShiftRight{Dest: dst, Left: left, Right: right})
	default:
		panic(fmt.Sprintf("todo: generate binary expr for op %s", op))
	}

	return ir.ValueOperand(dst, resultType)
}

func (g *Generator) generateShortCircuitExpr(node *parser.BinaryOpNode) ir.Operand {
	resultSlot := g.currentFunction.NewSlot(types.PrimitiveBool, "short.circuit.result")
	g.Emit(ir.Alloca{Slot: resultSlot})

	left := g.GenerateExpr(node.Operand1)
	rightBlock := g.currentFunction.NewBlock("short.circuit.right")
	shortBlock := g.currentFunction.NewBlock("short.circuit.skip")
	mergeBlock := g.currentFunction.NewBlock("short.circuit.merge")

	if node.Op == parser.BinaryOpLogicalAnd {
		g.Emit(ir.Branch{Cond: left, Then: rightBlock.ID, Else: shortBlock.ID})
	} else {
		g.Emit(ir.Branch{Cond: left, Then: shortBlock.ID, Else: rightBlock.ID})
	}

	g.currentBlock = shortBlock
	shortValue := node.Op == parser.BinaryOpLogicalOr
	g.Emit(ir.Store{Slot: resultSlot, Value: ir.BoolConstOperand(shortValue)})
	g.Emit(ir.Jump{Target: mergeBlock.ID})

	g.currentBlock = rightBlock
	right := g.GenerateExpr(node.Operand2)
	if !g.currentBlockHasTerminator() {
		g.Emit(ir.Store{Slot: resultSlot, Value: right})
		g.Emit(ir.Jump{Target: mergeBlock.ID})
	}

	g.currentBlock = mergeBlock
	result := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	g.Emit(ir.Load{Dest: result, Slot: resultSlot})
	return ir.ValueOperand(result, types.PrimitiveBool)
}

func (g *Generator) generateUnaryExpr(node *parser.UnaryOpNode) ir.Operand {
	dst := g.currentFunction.NewValueOfType(node.GetType())

	switch node.Op {
	case parser.UnaryOpNegate:
		operand := g.GenerateExpr(node.Operand)
		g.Emit(ir.Negate{Dest: dst, Operand: operand})
	case parser.UnaryOpLogicalNot:
		operand := g.GenerateExpr(node.Operand)
		g.Emit(ir.LogicalNot{Dest: dst, Operand: operand})
	case parser.UnaryOpBitwiseNot:
		operand := g.GenerateExpr(node.Operand)
		g.Emit(ir.BitwiseNot{Dest: dst, Operand: operand})
	case parser.UnaryOpReference:
		fallthrough
	case parser.UnaryOpMutableReference:
		return g.generateAddressOfExpr(node.Operand)
	case parser.UnaryOpDereference:
		operand := g.GenerateExpr(node.Operand)
		g.Emit(ir.LoadPtr{Dest: dst, Ptr: operand})
	case parser.UnaryOpSliceLen:
		operand := g.generateAddressOfExpr(node.Operand)
		lenPtr := g.currentFunction.NewValueOfType(types.PointerType{Base: node.GetType()})
		g.Emit(ir.FieldAddress{Dest: lenPtr, Base: operand, Field: "1"})
		g.Emit(ir.LoadPtr{Dest: dst, Ptr: ir.ValueOperand(lenPtr, types.PointerType{Base: node.GetType()})})
	default:
		panic("todo")
	}

	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) generateIf(node *parser.IfNode) {
	mergeBlock := g.currentFunction.NewBlock("if.merge")
	thenBlock := g.currentFunction.NewBlock("if.then")

	elseTarget := mergeBlock
	if len(node.ElseIfBranches) > 0 || node.ElseBranch != nil {
		elseTarget = g.currentFunction.NewBlock("if.else")
	}

	cond := g.GenerateExpr(node.IfBranch.Condition)
	condVal := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	g.Emit(ir.CmpNe{
		Dest:  condVal,
		Left:  cond,
		Right: ir.BoolConstOperand(false),
	})
	g.Emit(ir.Branch{
		Cond: ir.ValueOperand(condVal, types.PrimitiveBool),
		Then: thenBlock.ID,
		Else: elseTarget.ID,
	})

	g.currentBlock = thenBlock
	g.generateBlock(node.IfBranch.Node)
	if !g.currentBlockHasTerminator() {
		g.Emit(ir.Jump{Target: mergeBlock.ID})
	}

	g.generateElseChain(node, elseTarget, mergeBlock)

	g.currentBlock = mergeBlock
}

func (g *Generator) generateElseChain(node *parser.IfNode, startElse *ir.Block, mergeBlock *ir.Block) {
	if startElse == mergeBlock {
		return
	}

	g.currentBlock = startElse

	for i, elif := range node.ElseIfBranches {
		thenBlock := g.currentFunction.NewBlock(fmt.Sprintf("if.elseif.then.%d", i))

		next := mergeBlock
		if i < len(node.ElseIfBranches)-1 || node.ElseBranch != nil {
			next = g.currentFunction.NewBlock(fmt.Sprintf("if.elseif.next.%d", i))
		}

		cond := g.GenerateExpr(elif.Condition)
		condVal := g.currentFunction.NewValueOfType(types.PrimitiveBool)
		g.Emit(ir.CmpNe{
			Dest:  condVal,
			Left:  cond,
			Right: ir.BoolConstOperand(false),
		})
		g.Emit(ir.Branch{
			Cond: ir.ValueOperand(condVal, types.PrimitiveBool),
			Then: thenBlock.ID,
			Else: next.ID,
		})

		g.currentBlock = thenBlock
		g.generateBlock(elif.Node)
		if !g.currentBlockHasTerminator() {
			g.Emit(ir.Jump{Target: mergeBlock.ID})
		}

		g.currentBlock = next
	}

	if node.ElseBranch != nil {
		g.generateBlock(node.ElseBranch)
		if !g.currentBlockHasTerminator() {
			g.Emit(ir.Jump{Target: mergeBlock.ID})
		}
	}
}
