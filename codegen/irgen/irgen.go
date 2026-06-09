package irgen

import (
	"fmt"

	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/parser"
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
	Module *ir.Module

	currentFunction *ir.Function
	currentBlock    *ir.Block
	currentEnv      *Env
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
	g.Module = &ir.Module{}

	for _, node := range root.Body {
		fn, ok := node.(*parser.FunctionDefNode)
		if !ok {
			continue
		}
		if fn.Extern {
			continue
		}
		g.GenerateFunction(fn)
	}

	return g.Module
}

func (g *Generator) GenerateFunction(fn *parser.FunctionDefNode) {
	irFn := ir.NewFunction(fn.Name)
	irFn.Signature = g.buildFunctionSignature(fn)
	g.Module.AddFunction(irFn)

	g.currentFunction = irFn
	g.currentBlock = irFn.NewBlock("entry")
	irFn.Entry = g.currentBlock.ID
	g.currentEnv = NewEnv(nil)

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
		if g.isVoidFunction(fn) {
			g.Emit(ir.Return{})
		} else {
			g.Emit(ir.Return{HasValue: true, Value: ret})
		}
	case nil:
		g.Emit(ir.Return{})
	default:
		panic(fmt.Sprintf("todo: function body %T", body))
	}

	g.currentEnv = nil
	g.currentBlock = nil
	g.currentFunction = nil
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
	case ir.Return, ir.Jump, ir.Branch:
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
	case *parser.IfNode:
		g.generateIf(n)
	default:
		panic("todo")
	}
}

func (g *Generator) generateBlock(node *parser.BlockNode) {
	prev := g.currentEnv
	g.currentEnv = NewEnv(prev)
	defer func() {
		g.currentEnv = prev
	}()

	for _, child := range node.Body {
		if g.currentBlockHasTerminator() {
			break
		}
		g.GenerateNode(child)
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
	ident, ok := node.Assignee.(*parser.IdentifierNode)
	if !ok {
		panic("todo")
	}
	if ident.Symbol == nil {
		panic("assignment identifier symbol is nil")
	}

	slot, ok := g.currentEnv.Lookup(ident.Symbol)
	if !ok {
		panic("assignment slot not found")
	}

	if lit, ok := node.Value.(*parser.StructLiteralNode); ok {
		g.generateStructLiteralIntoSlot(slot, lit)
		return
	}

	rhs := g.GenerateExpr(node.Value)
	g.Emit(ir.Store{Slot: slot, Value: rhs})
}

func (g *Generator) generateControlKeyword(node *parser.ControlKeywordNode) {
	switch node.Keyword {
	case tokeniser.KeywordReturn:
		if node.ReturnValue == nil {
			g.Emit(ir.Return{})
			return
		}
		value := g.GenerateExpr(node.ReturnValue)
		g.Emit(ir.Return{HasValue: true, Value: value})
	default:
		panic("todo")
	}
}

func (g *Generator) GenerateExpr(expr parser.ExpressionNode) ir.Operand {
	switch n := expr.(type) {
	case *parser.IntegerLiteralNode:
		return ir.IntConstOperand(n.Value, n.GetType())
	case *parser.BoolLiteralNode:
		return ir.BoolConstOperand(n.Value == string(tokeniser.KeywordTrue))
	case *parser.IdentifierNode:
		return g.generateIdentifierExpr(n)
	case *parser.BinaryOpNode:
		return g.generateBinaryExpr(n)
	case *parser.StructLiteralNode:
		return g.generateStructLiteralExpr(n)
	case *parser.FunctionCallNode:
		return g.generateFunctionCallExpr(n)
	case *parser.FieldAccessNode:
		return g.generateFieldAccessExpr(n)
	default:
		panic("todo")
	}
}

func (g *Generator) generateStructLiteralExpr(node *parser.StructLiteralNode) ir.Operand {
	tmpSlot := g.currentFunction.NewSlot(node.GetType(), "")
	g.Emit(ir.Alloca{Slot: tmpSlot})
	g.generateStructLiteralIntoSlot(tmpSlot, node)

	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.Load{Dest: dst, Slot: tmpSlot})
	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) generateStructLiteralIntoSlot(slot ir.SlotID, node *parser.StructLiteralNode) {

	basePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: node.GetType()})
	g.Emit(ir.AddressOf{Dest: basePtrID, Slot: slot})
	basePtr := ir.ValueOperand(basePtrID, types.PointerType{Base: node.GetType()})

	for _, field := range node.Fields {
		fieldTy := node.GetType()
		if st, ok := node.GetType().(types.StructType); ok {
			for _, f := range st.Fields {
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
	basePtr := g.generateAddressOfExpr(node.Subject)

	fieldPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: node.GetType()})
	g.Emit(ir.FieldAddress{Dest: fieldPtrID, Base: basePtr, Field: node.Field.Name})

	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.LoadPtr{Dest: dst, Ptr: ir.ValueOperand(fieldPtrID, types.PointerType{Base: node.GetType()})})
	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) generateAddressOfExpr(expr parser.ExpressionNode) ir.Operand {
	if ident, ok := expr.(*parser.IdentifierNode); ok {
		if ident.Symbol == nil {
			panic("identifier symbol is nil")
		}

		slot, ok := g.currentEnv.Lookup(ident.Symbol)
		if !ok {
			panic("identifier slot not found")
		}

		addr := g.currentFunction.NewValueOfType(types.PointerType{Base: ident.GetType()})
		g.Emit(ir.AddressOf{Dest: addr, Slot: slot})
		return ir.ValueOperand(addr, types.PointerType{Base: ident.GetType()})
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
	args := make([]ir.Operand, 0, len(node.Args))
	for _, arg := range node.Args {
		args = append(args, g.GenerateExpr(arg))
	}
	callSig := g.buildCallSignature(node)

	name := node.Name.String()
	if node.Symbol != nil {
		name = node.Symbol.Name
	}

	if node.GetType().Equals(types.PrimitiveVoid) {
		g.Emit(ir.Call{
			Name:      name,
			Args:      args,
			Signature: callSig,
		})
		return ir.NullConstOperand(types.PrimitiveVoid)
	}

	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.Call{Dest: dst, Name: name, Args: args, Signature: callSig})
	return ir.ValueOperand(dst, node.GetType())
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
	}

	return sig
}

func (g *Generator) generateIdentifierExpr(node *parser.IdentifierNode) ir.Operand {
	if node.Symbol == nil {
		panic("identifier symbol is nil")
	}

	slot, ok := g.currentEnv.Lookup(node.Symbol)
	if !ok {
		panic("identifier slot not found")
	}

	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.Load{Dest: dst, Slot: slot})

	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) generateBinaryExpr(node *parser.BinaryOpNode) ir.Operand {
	left := g.GenerateExpr(node.Operand1)
	right := g.GenerateExpr(node.Operand2)
	dst := g.currentFunction.NewValueOfType(node.GetType())

	switch node.Op {
	case parser.BinaryOpAdd:
		g.Emit(ir.Add{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpSubtract:
		g.Emit(ir.Sub{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpMultiply:
		g.Emit(ir.Mul{Dest: dst, Left: left, Right: right})
	case parser.BinaryOpDivide:
		g.Emit(ir.Div{Dest: dst, Left: left, Right: right})
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
