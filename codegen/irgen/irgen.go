package irgen

import (
	"fmt"
	"strings"

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
	Module     *ir.Module
	ModuleName string
	MainModule string

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
		if fn.ExternFrom != "" {
			sig := g.buildFunctionSignature(fn)
			name := fn.Name
			g.Module.AddExtern(ir.ExternDecl{Name: name, Signature: sig, From: fn.ExternFrom})
			continue
		}
		g.GenerateFunction(fn)
	}

	return g.Module
}

func (g *Generator) GenerateFunction(fn *parser.FunctionDefNode) {
	name := fn.Name
	if !fn.Extern && !g.isProgramEntryFunction(fn.Name) {
		name = g.mangleFunctionName(g.ModuleName, fn.Name)
	}
	irFn := ir.NewFunction(name, fn.Extern)
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
	case *parser.IndexAssignmentNode:
		g.generateIndexAssignment(n)
	case *parser.ControlKeywordNode:
		g.generateControlKeyword(n)
	case *parser.IfNode:
		g.generateIf(n)
	case *parser.FunctionCallNode:
		g.generateFunctionCallExpr(n)
	case parser.ExpressionNode:
		g.GenerateExpr(n)
	default:
		panic(fmt.Sprintf("todo: generate node type %T", node))
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
	ident, ok := node.Subject.(*parser.IdentifierNode)
	if !ok {
		panic("assignment assignee is not an identifier")
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

func (g *Generator) generateIndexAssignment(node *parser.IndexAssignmentNode) {
	index := g.GenerateExpr(node.Index)
	switch subjectType := node.Subject.GetType().(type) {
	case types.SliceType:
		basePtr := g.generateAddressOfExpr(node.Subject)
		dataPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PointerType{Base: subjectType.Base}})
		g.Emit(ir.FieldAddress{Dest: dataPtrID, Base: basePtr, Field: "0"})
		dataPtr := g.currentFunction.NewValueOfType(types.PointerType{Base: subjectType.Base})
		g.Emit(ir.LoadPtr{Dest: dataPtr, Ptr: ir.ValueOperand(dataPtrID, types.PointerType{Base: types.PointerType{Base: subjectType.Base}})})
		elemPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: subjectType.Base})
		g.Emit(ir.IndexAddress{Dest: elemPtrID, Base: ir.ValueOperand(dataPtr, types.PointerType{Base: subjectType.Base}), Index: index})
		value := g.GenerateExpr(node.Value)
		g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(elemPtrID, types.PointerType{Base: subjectType.Base}), Value: value})
	case types.PointerType:
		subjectPtr := g.GenerateExpr(node.Subject)
		elemPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: subjectType.Base})
		g.Emit(ir.IndexAddress{Dest: elemPtrID, Base: subjectPtr, Index: index})
		value := g.GenerateExpr(node.Value)
		g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(elemPtrID, types.PointerType{Base: subjectType.Base}), Value: value})
	default:
		panic("index assignment requires slice or pointer")
	}
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
		panic(fmt.Sprintf("todo: control keyword %s", node.Keyword))
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
	case *parser.IfExprNode:
		return g.generateIfExpr(n)
	case *parser.BinaryOpNode:
		return g.generateBinaryExpr(n)
	case *parser.UnaryOpNode:
		return g.generateUnaryExpr(n)
	case *parser.StructLiteralNode:
		return g.generateStructLiteralExpr(n)
	case *parser.SliceLiteralNode:
		return g.generateSliceLiteralExpr(n)
	case *parser.FunctionCallNode:
		return g.generateFunctionCallExpr(n)
	case *parser.FieldAccessNode:
		return g.generateFieldAccessExpr(n)
	case *parser.IndexExprNode:
		return g.generateIndexExpr(n)
	case *parser.CastNode:
		return g.generateCastExpr(n)
	case *parser.StringLiteralNode:
		return g.generateStringLiteralExpr(n)
	default:
		panic(fmt.Sprintf("todo: generate expr type %T", expr))
	}
}

func (g *Generator) generateStringLiteralExpr(node *parser.StringLiteralNode) ir.Operand {
	sliceType, ok := node.GetType().(types.SliceType)
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

func (g *Generator) generateCastExpr(node *parser.CastNode) ir.Operand {
	targetType := node.GetType()

	if str, ok := node.Operand.(*parser.StringLiteralNode); ok {
		if _, ok := targetType.(types.PointerType); ok {
			dst := g.currentFunction.NewValueOfType(targetType)
			g.Emit(ir.StringConst{Dest: dst, Value: str.Value})
			return ir.ValueOperand(dst, targetType)
		}
	}

	from := g.GenerateExpr(node.Operand)
	if from.Type.Equals(targetType) || sameIntegerWidthCast(from.Type, targetType) {
		return from
	}

	dst := g.currentFunction.NewValueOfType(targetType)
	g.Emit(ir.Cast{Dest: dst, From: from, To: targetType})
	return ir.ValueOperand(dst, targetType)
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
	sliceType, ok := node.GetType().(types.SliceType)
	if !ok {
		panic("slice literal must have slice type")
	}

	tmpSlot := g.currentFunction.NewSlot(sliceType, "")
	g.Emit(ir.Alloca{Slot: tmpSlot})

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

func (g *Generator) generateIndexExpr(node *parser.IndexExprNode) ir.Operand {
	index := g.GenerateExpr(node.Index)

	switch subjectType := node.Subject.GetType().(type) {
	case types.SliceType:
		basePtr := g.generateAddressOfExpr(node.Subject)

		dataPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PointerType{Base: subjectType.Base}})
		g.Emit(ir.FieldAddress{Dest: dataPtrID, Base: basePtr, Field: "0"})

		dataPtr := g.currentFunction.NewValueOfType(types.PointerType{Base: subjectType.Base})
		g.Emit(ir.LoadPtr{Dest: dataPtr, Ptr: ir.ValueOperand(dataPtrID, types.PointerType{Base: types.PointerType{Base: subjectType.Base}})})

		elemPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: subjectType.Base})
		g.Emit(ir.IndexAddress{Dest: elemPtrID, Base: ir.ValueOperand(dataPtr, types.PointerType{Base: subjectType.Base}), Index: index})

		dst := g.currentFunction.NewValueOfType(node.GetType())
		g.Emit(ir.LoadPtr{Dest: dst, Ptr: ir.ValueOperand(elemPtrID, types.PointerType{Base: node.GetType()})})
		return ir.ValueOperand(dst, node.GetType())

	case types.PointerType:
		elemPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: subjectType.Base})
		g.Emit(ir.IndexAddress{Dest: elemPtrID, Base: g.GenerateExpr(node.Subject), Index: index})

		dst := g.currentFunction.NewValueOfType(node.GetType())
		g.Emit(ir.LoadPtr{Dest: dst, Ptr: ir.ValueOperand(elemPtrID, types.PointerType{Base: node.GetType()})})
		return ir.ValueOperand(dst, node.GetType())

	default:
		panic("index expression requires slice or pointer")
	}
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

		if !node.Symbol.Extern && node.Symbol.ExternFrom == "" {
			callModule := g.ModuleName
			if node.Name != nil && node.Name.ModName != "" {
				callModule = node.Name.ModName
			}
			if g.isProgramEntryFunction(node.Symbol.Name) {
				name = node.Symbol.Name
			} else {
				name = g.mangleFunctionName(callModule, node.Symbol.Name)
			}
		}
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

func (g *Generator) mangleFunctionName(moduleName, fnName string) string {
	mod := sanitizeName(moduleName)
	fn := sanitizeName(fnName)
	if mod == "" {
		return "__qk_" + fn
	}
	return "__qk_" + mod + "_" + fn
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

	if types.IsNumeric(left.Type) && types.IsNumeric(right.Type) {
		common := types.PromoteNumeric(left.Type, right.Type)
		left = g.coerceOperand(left, common)
		right = g.coerceOperand(right, common)
	}

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
	default:
		panic(fmt.Sprintf("todo: binary operator %v", node.Op))
	}

	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) coerceOperand(op ir.Operand, target types.Type) ir.Operand {
	if op.Type.Equals(target) || sameIntegerWidthCast(op.Type, target) {
		return op
	}

	dst := g.currentFunction.NewValueOfType(target)
	g.Emit(ir.Cast{Dest: dst, From: op, To: target})
	return ir.ValueOperand(dst, target)
}

func sameIntegerWidthCast(from types.Type, to types.Type) bool {
	fromPrim, fromOK := from.(types.PrimitiveType)
	toPrim, toOK := to.(types.PrimitiveType)
	if !fromOK || !toOK {
		return false
	}
	if !types.IsInteger(fromPrim) || !types.IsInteger(toPrim) {
		return false
	}
	return types.IntegerRank(fromPrim) == types.IntegerRank(toPrim)
}

func (g *Generator) generateUnaryExpr(node *parser.UnaryOpNode) ir.Operand {
	dst := g.currentFunction.NewValueOfType(node.GetType())

	switch node.Op {
	case parser.UnaryOpNegate:
		operand := g.GenerateExpr(node.Operand)
		g.Emit(ir.Sub{Dest: dst, Left: ir.IntConstOperand("0", node.GetType()), Right: operand})
	case parser.UnaryOpReference:
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
		panic(fmt.Sprintf("todo: unary operator %v", node.Op))
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
