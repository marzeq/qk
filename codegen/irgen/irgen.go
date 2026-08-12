package irgen

import (
	"fmt"
	"hash/fnv"
	"math/big"
	"slices"
	"strconv"
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
	DemandedTraitSlots     map[string]map[int]bool

	currentFunction             *ir.Function
	currentBlock                *ir.Block
	currentEnv                  *Env
	globals                     map[*symbols.Symbol]string
	loopTargets                 []loopTargets
	deferScopes                 [][]parser.Node
	dynamicGlobals              []dynamicGlobalInitializer
	initializingGlobal          bool
	specializedVariadicElements map[*symbols.Symbol][]ir.Operand
	specializedConstants        map[*symbols.Symbol]symbols.SpecializationConstant
	lambdaFunctions             []*parser.FunctionDefNode
	queuedLambdas               map[*symbols.Symbol]bool
	templateMode                bool
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
	g.lambdaFunctions = nil
	g.queuedLambdas = make(map[*symbols.Symbol]bool)

	for _, root := range roots {
		for _, node := range root.Body {
			if node, ok := node.(*parser.DeclarationNode); ok {
				if len(node.GenericParameters) != 0 || node.Symbol.InlineComptime {
					continue
				}
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
			if node.IsGeneric() {
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

	for i := 0; i < len(g.lambdaFunctions); i++ {
		g.GenerateFunction(g.lambdaFunctions[i])
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
	var value ir.Operand
	var constant bool
	if node.Comptime {
		value, constant = g.tryGenerateComptimeInitializer(node.Value)
	} else {
		value, constant = g.tryGenerateGlobalInitializer(node.Value)
	}
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

func (g *Generator) tryGenerateComptimeInitializer(expr parser.ExpressionNode) (ir.Operand, bool) {
	if value, ok := g.tryGenerateGlobalInitializer(expr); ok {
		return value, true
	}
	switch node := expr.(type) {
	case *parser.SizeOfNode:
		return ir.SizeofConstOperand(node.OperandType), true
	case *parser.CastNode:
		value, ok := g.tryGenerateComptimeInitializer(node.Operand)
		if !ok {
			return ir.Operand{}, false
		}
		value.Type = node.GetType()
		return value, true
	case *parser.UnaryOpNode:
		value, ok := g.tryGenerateComptimeInitializer(node.Operand)
		if !ok {
			return ir.Operand{}, false
		}
		switch node.Op {
		case parser.UnaryOpNegate:
			zero := ir.IntConstOperand("0", node.GetType())
			return ir.BinaryConstOperand("sub", zero, value, node.GetType()), true
		case parser.UnaryOpBitwiseNot:
			allBits := ir.IntConstOperand("-1", node.GetType())
			return ir.BinaryConstOperand("xor", value, allBits, node.GetType()), true
		case parser.UnaryOpLogicalNot:
			one := ir.BoolConstOperand(true)
			return ir.BinaryConstOperand("xor", value, one, node.GetType()), true
		}
		return ir.Operand{}, false
	case *parser.BinaryOpNode:
		left, leftOK := g.tryGenerateComptimeInitializer(node.Operand1)
		right, rightOK := g.tryGenerateComptimeInitializer(node.Operand2)
		if !leftOK || !rightOK {
			return ir.Operand{}, false
		}
		operator := ""
		switch node.Op {
		case parser.BinaryOpAdd:
			operator = "add"
		case parser.BinaryOpSubtract:
			operator = "sub"
		case parser.BinaryOpMultiply:
			operator = "mul"
		case parser.BinaryOpDivide:
			if types.IsSigned(node.GetType()) {
				operator = "sdiv"
			} else {
				operator = "udiv"
			}
		case parser.BinaryOpModulo:
			if types.IsSigned(node.GetType()) {
				operator = "srem"
			} else {
				operator = "urem"
			}
		case parser.BinaryOpBitwiseAnd:
			operator = "and"
		case parser.BinaryOpBitwiseOr:
			operator = "or"
		case parser.BinaryOpBitwiseXor:
			operator = "xor"
		case parser.BinaryOpShiftLeft:
			operator = "shl"
		case parser.BinaryOpShiftRight:
			if types.IsSigned(node.GetType()) {
				operator = "ashr"
			} else {
				operator = "lshr"
			}
		}
		if operator == "" {
			return ir.Operand{}, false
		}
		return ir.BinaryConstOperand(operator, left, right, node.GetType()), true
	}
	return ir.Operand{}, false
}

func (g *Generator) tryGenerateGlobalInitializer(expr parser.ExpressionNode) (ir.Operand, bool) {
	switch node := expr.(type) {
	case *parser.NoInitializerNode:
		return ir.ZeroConstOperand(node.GetType()), true
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
		if _, traitPointer := types.Underlying(node.GetType()).(types.TraitPointerType); traitPointer {
			return ir.ZeroConstOperand(node.GetType()), true
		}
		return ir.NullConstOperand(node.GetType()), true
	case *parser.EnumLiteralNode:
		return ir.IntConstOperand(node.Value, node.GetType()), true
	case *parser.FieldAccessNode:
		if node.IsEnumValue {
			return ir.IntConstOperand(node.EnumValue, node.GetType()), true
		}
		if node.IsFlagValue {
			return ir.IntConstOperand(node.FlagValue, node.FlagType), true
		}
		return ir.Operand{}, false
	case *parser.StructLiteralNode:
		if node.NoInitRemaining && len(node.Fields) == 0 {
			return ir.ZeroConstOperand(node.GetType()), true
		}
		if flagType, ok := types.Underlying(node.GetType()).(types.FlagsType); ok {
			value := new(big.Int)
			for _, member := range node.FlagMembers {
				raw, _ := flagType.VariantValue(member)
				part, _ := new(big.Int).SetString(raw, 10)
				value.Or(value, part)
			}
			return ir.IntConstOperand(value.String(), node.GetType()), true
		}
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
				if node.NoInitRemaining {
					values[i] = ir.ZeroConstOperand(field.R)
					continue
				}
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
		g.initializingGlobal = true
		value := g.GenerateExpr(global.expr)
		g.initializingGlobal = false
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
		if fn.Symbol.Public || fn.Symbol.Method || g.isProgramEntryFunction(fn.Symbol.Name) {
			linkage = ir.LinkageExternal
			visibility = ir.VisibilityHidden
		}
		name = g.mangleFunctionName(g.ModuleName, fn.Symbol.Name)
	}
	irFn := ir.NewFunction(name, linkage, fn.Attributes)
	irFn.Visibility = visibility
	irFn.Signature = g.buildFunctionSignature(fn)
	g.Module.AddFunction(irFn)
	if g.isProgramEntryFunction(fn.Symbol.Name) {
		g.Module.Entry = name
	}

	g.currentFunction = irFn
	g.currentBlock = irFn.NewBlock("entry")
	irFn.Entry = g.currentBlock.ID
	g.currentEnv = NewEnv(nil)
	g.deferScopes = nil

	g.emitFunctionParams(fn)
	g.generateFunctionBody(fn)

	g.currentEnv = nil
	g.currentBlock = nil
	g.currentFunction = nil
	g.deferScopes = nil

	g.generateDefaultWrappers(fn, name)
	if g.templateMode {
		return
	}
	g.generateTypedVariadicWrappers(fn, name)
	if !fn.Symbol.Signature.TypedVariadic {
		g.generateConstantWrapper(fn, name)
	}
}

// GenerateGenericTemplate lowers an attributed generic body into symbolic QK
// IR. Type parameters deliberately remain in the result and are substituted
// before target lowering.
func (g *Generator) GenerateGenericTemplate(fn *parser.FunctionDefNode) []*ir.Function {
	if fn == nil || !fn.IsGeneric() {
		return nil
	}
	if g.Module == nil {
		g.Module = &ir.Module{}
	}
	if g.globals == nil {
		g.globals = make(map[*symbols.Symbol]string)
	}
	if g.queuedLambdas == nil {
		g.queuedLambdas = make(map[*symbols.Symbol]bool)
	}
	before := len(g.Module.Functions)
	g.templateMode = true
	g.GenerateFunction(fn)
	g.templateMode = false
	return append([]*ir.Function(nil), g.Module.Functions[before:]...)
}

func (g *Generator) generateFunctionBody(fn *parser.FunctionDefNode) {
	if fn.ExpressionBody {
		ret := g.GenerateExpr(fn.Body.(parser.ExpressionNode))
		if !g.currentBlockHasTerminator() {
			if g.isVoidFunction(fn) {
				g.Emit(ir.Return{})
			} else {
				g.Emit(ir.Return{HasValue: true, Value: ret})
			}
		}
	} else {
		g.GenerateNode(fn.Body)
		if !g.currentBlockHasTerminator() {
			if g.isVoidFunction(fn) {
				g.Emit(ir.Return{})
			} else {
				panic("non-void function may fall through without return")
			}
		}
	}
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
		}
		for i := 0; i < arity; i++ {
			arg := fn.Args[i]
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

func (g *Generator) generateTypedVariadicWrappers(fn *parser.FunctionDefNode, targetName string) {
	if !fn.Symbol.Signature.TypedVariadic || len(fn.Symbol.Signature.TypedVariadicArities) == 0 {
		return
	}

	arities := make([]int, 0, len(fn.Symbol.Signature.TypedVariadicArities))
	for arity := range fn.Symbol.Signature.TypedVariadicArities {
		arities = append(arities, arity)
	}
	slices.Sort(arities)

	fixedCount := len(fn.Symbol.Signature.Parameters) - 1
	constants := selectedSpecializationConstants(fn.Symbol.Signature)
	sliceType := fn.Symbol.Signature.Parameters[fixedCount]
	for _, arity := range arities {
		linkage := ir.LinkageInternal
		visibility := ir.VisibilityDefault
		if fn.Attributes.Get(attributes.AttributeTypeExport) != nil || fn.Symbol.Public || fn.Symbol.Method {
			linkage = ir.LinkageExternal
			visibility = ir.VisibilityHidden
		}

		paramTypes := make([]types.Type, 0, fixedCount+arity)
		for i, parameter := range fn.Symbol.Signature.Parameters[:fixedCount] {
			if _, specialized := constants[i]; !specialized {
				paramTypes = append(paramTypes, parameter)
			}
		}
		for range arity {
			paramTypes = append(paramTypes, fn.Symbol.Signature.VariadicElement)
		}
		wrapper := ir.NewFunction(g.specializedFunctionName(g.typedVariadicWrapperName(targetName, arity), constants), linkage, nil)
		wrapper.Visibility = visibility
		wrapper.Signature = ir.FunctionSignature{
			ParamTypes: paramTypes,
			ReturnType: fn.Symbol.Signature.ReturnType,
		}
		g.Module.AddFunction(wrapper)

		g.currentFunction = wrapper
		g.currentBlock = wrapper.NewBlock("entry")
		wrapper.Entry = g.currentBlock.ID
		g.currentEnv = NewEnv(nil)
		g.deferScopes = nil

		variadicElements := make([]ir.Operand, 0, arity)
		variadicSlots := make([]ir.SlotID, 0, arity)
		for i := 0; i < fixedCount; i++ {
			if _, specialized := constants[i]; specialized {
				continue
			}
			paramType := fn.Symbol.Signature.Parameters[i]
			slot := wrapper.NewSlot(paramType, fmt.Sprintf("arg%d", i))
			g.currentEnv.Variables[fn.Args[i].Symbol] = slot
			wrapper.AddParameter(fn.Args[i].Name, paramType, slot)
			g.Emit(ir.Alloca{Slot: slot})
			value := wrapper.NewValueOfType(paramType)
			g.Emit(ir.Store{Slot: slot, Value: ir.ValueOperand(value, paramType)})
		}
		for i := 0; i < arity; i++ {
			paramType := fn.Symbol.Signature.VariadicElement
			slot := wrapper.NewSlot(paramType, fmt.Sprintf("variadic%d", i))
			wrapper.AddParameter(fmt.Sprintf("variadic%d", i), paramType, slot)
			g.Emit(ir.Alloca{Slot: slot})
			value := wrapper.NewValueOfType(paramType)
			g.Emit(ir.Store{Slot: slot, Value: ir.ValueOperand(value, paramType)})
			variadicSlots = append(variadicSlots, slot)
		}
		for _, slot := range variadicSlots {
			loaded := wrapper.NewValueOfType(fn.Symbol.Signature.VariadicElement)
			g.Emit(ir.Load{Dest: loaded, Slot: slot})
			variadicElements = append(variadicElements, ir.ValueOperand(loaded, fn.Symbol.Signature.VariadicElement))
		}

		packed := g.generatePackedVariadicSlice(variadicElements, sliceType)
		variadicSlot := wrapper.NewSlot(sliceType, fn.Args[fixedCount].Name)
		g.currentEnv.Variables[fn.Args[fixedCount].Symbol] = variadicSlot
		g.Emit(ir.Alloca{Slot: variadicSlot})
		g.Emit(ir.Store{Slot: variadicSlot, Value: packed})
		previousSpecialized := g.specializedVariadicElements
		previousConstants := g.specializedConstants
		g.specializedVariadicElements = map[*symbols.Symbol][]ir.Operand{
			fn.Args[fixedCount].Symbol: variadicElements,
		}
		g.specializedConstants = make(map[*symbols.Symbol]symbols.SpecializationConstant, len(constants))
		for index, constant := range constants {
			symbol := fn.Args[index].Symbol
			g.specializedConstants[symbol] = constant
			slot := wrapper.NewSlot(symbol.Type, fmt.Sprintf("constant%d", index))
			g.currentEnv.Variables[symbol] = slot
			g.Emit(ir.Alloca{Slot: slot})
			g.Emit(ir.Store{Slot: slot, Value: g.generateSpecializationConstant(constant)})
		}
		g.generateFunctionBody(fn)
		g.specializedVariadicElements = previousSpecialized
		g.specializedConstants = previousConstants

		g.currentEnv = nil
		g.currentBlock = nil
		g.currentFunction = nil
		g.deferScopes = nil
	}
}

func (g *Generator) generateConstantWrapper(fn *parser.FunctionDefNode, targetName string) {
	constants := selectedSpecializationConstants(fn.Symbol.Signature)
	if len(constants) == 0 {
		return
	}
	linkage := ir.LinkageInternal
	visibility := ir.VisibilityDefault
	if fn.Attributes.Get(attributes.AttributeTypeExport) != nil || fn.Symbol.Public || fn.Symbol.Method {
		linkage = ir.LinkageExternal
		visibility = ir.VisibilityHidden
	}
	paramTypes := make([]types.Type, 0, len(fn.Symbol.Signature.Parameters)-len(constants))
	for i, parameter := range fn.Symbol.Signature.Parameters {
		if _, specialized := constants[i]; !specialized {
			paramTypes = append(paramTypes, parameter)
		}
	}
	wrapper := ir.NewFunction(g.specializedFunctionName(targetName, constants), linkage, nil)
	wrapper.Visibility = visibility
	wrapper.Signature = ir.FunctionSignature{ParamTypes: paramTypes, ReturnType: fn.Symbol.Signature.ReturnType}
	g.Module.AddFunction(wrapper)

	g.currentFunction = wrapper
	g.currentBlock = wrapper.NewBlock("entry")
	wrapper.Entry = g.currentBlock.ID
	g.currentEnv = NewEnv(nil)
	g.deferScopes = nil
	for i, argument := range fn.Args {
		if _, specialized := constants[i]; specialized {
			continue
		}
		slot := wrapper.NewSlot(argument.Symbol.Type, fmt.Sprintf("arg%d", i))
		wrapper.AddParameter(argument.Name, argument.Symbol.Type, slot)
		g.currentEnv.Variables[argument.Symbol] = slot
		g.Emit(ir.Alloca{Slot: slot})
		incoming := wrapper.NewValueOfType(argument.Symbol.Type)
		g.Emit(ir.Store{Slot: slot, Value: ir.ValueOperand(incoming, argument.Symbol.Type)})
	}
	previousConstants := g.specializedConstants
	g.specializedConstants = make(map[*symbols.Symbol]symbols.SpecializationConstant, len(constants))
	for index, constant := range constants {
		symbol := fn.Args[index].Symbol
		g.specializedConstants[symbol] = constant
		slot := wrapper.NewSlot(symbol.Type, fmt.Sprintf("constant%d", index))
		g.currentEnv.Variables[symbol] = slot
		g.Emit(ir.Alloca{Slot: slot})
		g.Emit(ir.Store{Slot: slot, Value: g.generateSpecializationConstant(constant)})
	}
	g.generateFunctionBody(fn)
	g.specializedConstants = previousConstants

	g.currentEnv = nil
	g.currentBlock = nil
	g.currentFunction = nil
	g.deferScopes = nil
}

func selectedSpecializationConstants(signature *symbols.FunctionSignature) map[int]symbols.SpecializationConstant {
	selected := make(map[int]symbols.SpecializationConstant)
	if signature == nil {
		return selected
	}
	type candidate struct {
		index    int
		priority int
		constant symbols.SpecializationConstant
	}
	var candidates []candidate
	for i := 0; i < len(signature.Parameters); i++ {
		values := signature.ConstantArguments[i]
		if len(values) != 1 {
			continue
		}
		for _, constant := range values {
			priority := 2
			switch constant.Kind {
			case "str", "cstr":
				priority = 0
			case "bool":
				priority = 1
			case "float":
				priority = 3
			}
			candidates = append(candidates, candidate{index: i, priority: priority, constant: constant})
		}
	}
	slices.SortFunc(candidates, func(left, right candidate) int {
		if left.priority != right.priority {
			return left.priority - right.priority
		}
		return left.index - right.index
	})
	for _, candidate := range candidates[:min(2, len(candidates))] {
		selected[candidate.index] = candidate.constant
	}
	return selected
}

func (g *Generator) specializedFunctionName(targetName string, constants map[int]symbols.SpecializationConstant) string {
	if len(constants) == 0 {
		return targetName
	}
	hash := fnv.New64a()
	indices := make([]int, 0, len(constants))
	for index := range constants {
		indices = append(indices, index)
	}
	slices.Sort(indices)
	for _, index := range indices {
		fmt.Fprintf(hash, "%d:%s;", index, constants[index].Key())
	}
	return fmt.Sprintf("%s__const_%016x", targetName, hash.Sum64())
}

func (g *Generator) generatePackedVariadicSlice(elements []ir.Operand, targetType types.Type) ir.Operand {
	sliceType := types.Underlying(targetType).(types.SliceType)
	tmpSlot := g.currentFunction.NewSlot(targetType, "")
	g.Emit(ir.Alloca{Slot: tmpSlot})
	slicePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: targetType})
	g.Emit(ir.AddressOf{Dest: slicePtrID, Slot: tmpSlot})
	slicePtr := ir.ValueOperand(slicePtrID, types.PointerType{Base: targetType})

	var elemPtr ir.Operand
	if len(elements) == 0 {
		elemPtr = ir.NullConstOperand(types.PointerType{Base: sliceType.Base})
	} else {
		bufferType := types.StructType{Fields: make([]shared.Pair[string, types.Type], len(elements))}
		for i := range elements {
			bufferType.Fields[i] = shared.Pair[string, types.Type]{L: fmt.Sprintf("%d", i), R: sliceType.Base}
		}
		bufferSlot := g.currentFunction.NewSlot(bufferType, "")
		g.Emit(ir.Alloca{Slot: bufferSlot})
		bufferPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: bufferType})
		g.Emit(ir.AddressOf{Dest: bufferPtrID, Slot: bufferSlot})
		bufferPtr := ir.ValueOperand(bufferPtrID, types.PointerType{Base: bufferType})
		for i, element := range elements {
			fieldPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType.Base})
			g.Emit(ir.FieldAddress{Dest: fieldPtrID, Base: bufferPtr, Field: fmt.Sprintf("%d", i)})
			g.Emit(ir.StorePtr{
				Ptr:   ir.ValueOperand(fieldPtrID, types.PointerType{Base: sliceType.Base}),
				Value: element,
			})
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
	g.Emit(ir.StorePtr{
		Ptr:   ir.ValueOperand(lenPtrID, types.PointerType{Base: types.PrimitiveUsz}),
		Value: ir.IntConstOperand(strconv.Itoa(len(elements)), types.PrimitiveUsz),
	})
	loaded := g.currentFunction.NewValueOfType(targetType)
	g.Emit(ir.Load{Dest: loaded, Slot: tmpSlot})
	return ir.ValueOperand(loaded, targetType)
}

func (g *Generator) defaultWrapperName(targetName string, arity int) string {
	return fmt.Sprintf("%s__default_%d", targetName, arity)
}

func (g *Generator) typedVariadicWrapperName(targetName string, arity int) string {
	return fmt.Sprintf("%s__variadic_%d", targetName, arity)
}

func hasTypedVariadicSpecialization(signature *symbols.FunctionSignature, arity int) bool {
	return signature != nil && signature.TypedVariadicArities[arity]
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
	case *parser.MultiDeclarationNode:
		g.generateMultiDeclaration(n)
	case *parser.AssignmentNode:
		g.generateAssignment(n)
	case *parser.ControlKeywordNode:
		g.generateControlKeyword(n)
	case *parser.DeferNode:
		g.registerDefer(n)
	case *parser.IfNode:
		g.generateIf(n)
	case *parser.MatchNode:
		g.generateMatch(n)
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
	if node.Name == "_" {
		g.GenerateExpr(node.Value)
		return
	}
	if node.Symbol == nil {
		panic("declaration symbol is nil")
	}
	if node.Symbol.InlineComptime {
		return
	}

	slot := g.currentFunction.NewSlot(node.Symbol.Type, node.Name)
	g.currentEnv.Variables[node.Symbol] = slot

	g.Emit(ir.Alloca{Slot: slot})
	if _, noInit := node.Value.(*parser.NoInitializerNode); noInit {
		return
	}

	if lit, ok := node.Value.(*parser.StructLiteralNode); ok {
		g.generateStructLiteralIntoSlot(slot, lit)
		return
	}

	value := g.GenerateExpr(node.Value)
	g.Emit(ir.Store{Slot: slot, Value: value})
}

func (g *Generator) generateMultiDeclaration(node *parser.MultiDeclarationNode) {
	bundle := g.GenerateExpr(node.Value)
	result := bundle.Type.(types.MultipleReturnType)
	for i, sym := range node.Symbols {
		if sym == nil {
			continue
		}
		valueID := g.currentFunction.NewValueOfType(result.Types[i])
		g.Emit(ir.ExtractValue{Dest: valueID, Aggregate: bundle, Index: i})
		slot := g.currentFunction.NewSlot(sym.Type, node.Names[i])
		g.currentEnv.Variables[sym] = slot
		g.Emit(ir.Alloca{Slot: slot})
		g.Emit(ir.Store{Slot: slot, Value: ir.ValueOperand(valueID, result.Types[i])})
	}
}

func (g *Generator) generateAssignment(node *parser.AssignmentNode) {
	if node.Compound {
		g.generateCompoundAssignment(node)
		return
	}
	if len(node.Assignees) > 0 {
		bundle := g.GenerateExpr(node.Value)
		result := bundle.Type.(types.MultipleReturnType)
		for i, target := range node.Assignees {
			if id, ok := target.(*parser.IdentifierNode); ok && id.Name == "_" {
				continue
			}
			valueID := g.currentFunction.NewValueOfType(result.Types[i])
			g.Emit(ir.ExtractValue{Dest: valueID, Aggregate: bundle, Index: i})
			value := ir.ValueOperand(valueID, result.Types[i])
			if !result.Types[i].Equals(target.GetType()) {
				castID := g.currentFunction.NewValueOfType(target.GetType())
				g.Emit(ir.Cast{Dest: castID, From: value, To: target.GetType()})
				value = ir.ValueOperand(castID, target.GetType())
			}
			address := g.generateAddressOfExpr(target)
			g.Emit(ir.StorePtr{Ptr: address, Value: value})
		}
		return
	}
	if id, ok := node.Assignee.(*parser.IdentifierNode); ok && id.Name == "_" {
		g.GenerateExpr(node.Value)
		return
	}
	if field, ok := node.Assignee.(*parser.FieldAccessNode); ok && field.IsFlagTest {
		g.generateFlagMemberAssignment(field, node.Value)
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

func (g *Generator) generateFlagMemberAssignment(field *parser.FieldAccessNode, rhs parser.ExpressionNode) {
	address := g.generateAddressOfExpr(field.Subject)
	currentID := g.currentFunction.NewValueOfType(field.FlagType)
	g.Emit(ir.LoadPtr{Dest: currentID, Ptr: address})
	current := ir.ValueOperand(currentID, field.FlagType)
	mask := ir.IntConstOperand(field.FlagValue, field.FlagType)
	condition := g.GenerateExpr(rhs)
	setBlock := g.currentFunction.NewBlock("flags.set")
	clearBlock := g.currentFunction.NewBlock("flags.clear")
	mergeBlock := g.currentFunction.NewBlock("flags.assign.merge")
	g.Emit(ir.Branch{Cond: condition, Then: setBlock.ID, Else: clearBlock.ID})
	g.currentBlock = setBlock
	set := g.emitBinaryOperation(parser.BinaryOpBitwiseOr, current, mask, field.FlagType)
	g.Emit(ir.StorePtr{Ptr: address, Value: set})
	g.Emit(ir.Jump{Target: mergeBlock.ID})
	g.currentBlock = clearBlock
	notID := g.currentFunction.NewValueOfType(field.FlagType)
	g.Emit(ir.BitwiseNot{Dest: notID, Operand: mask})
	clear := g.emitBinaryOperation(parser.BinaryOpBitwiseAnd, current, ir.ValueOperand(notID, field.FlagType), field.FlagType)
	g.Emit(ir.StorePtr{Ptr: address, Value: clear})
	g.Emit(ir.Jump{Target: mergeBlock.ID})
	g.currentBlock = mergeBlock
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
		if len(node.ReturnValues) > 1 {
			multi := g.currentFunction.Signature.ReturnType.(types.MultipleReturnType)
			value = ir.ZeroConstOperand(multi)
			for i, expr := range node.ReturnValues {
				item := g.GenerateExpr(expr)
				dest := g.currentFunction.NewValueOfType(multi)
				g.Emit(ir.InsertValue{Dest: dest, Aggregate: value, Value: item, Index: i})
				value = ir.ValueOperand(dest, multi)
			}
		} else if node.ReturnValue != nil {
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
	boundSlot := g.currentFunction.NewSlot(node.Symbol.Type, "for.range.bound")
	g.Emit(ir.Alloca{Slot: boundSlot})
	start := g.GenerateExpr(node.Start)
	if node.Reversed {
		g.Emit(ir.Store{Slot: boundSlot, Value: start})
		g.Emit(ir.Store{Slot: iteratorSlot, Value: g.GenerateExpr(node.End)})
	} else {
		g.Emit(ir.Store{Slot: iteratorSlot, Value: start})
		g.Emit(ir.Store{Slot: boundSlot, Value: g.GenerateExpr(node.End)})
	}

	conditionBlock := g.currentFunction.NewBlock("for.range.condition")
	bodyBlock := g.currentFunction.NewBlock("for.range.body")
	postBlock := g.currentFunction.NewBlock("for.range.post")
	endBlock := g.currentFunction.NewBlock("for.range.end")
	g.Emit(ir.Jump{Target: conditionBlock.ID})

	g.currentBlock = conditionBlock
	iterator := g.loadSlot(iteratorSlot, node.Symbol.Type)
	bound := g.loadSlot(boundSlot, node.Symbol.Type)
	condition := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	if node.Reversed && node.Inclusive {
		g.Emit(ir.CmpGe{Dest: condition, Left: iterator, Right: bound})
	} else if node.Reversed {
		g.Emit(ir.CmpGt{Dest: condition, Left: iterator, Right: bound})
	} else if node.Inclusive {
		g.Emit(ir.CmpLe{Dest: condition, Left: iterator, Right: bound})
	} else {
		g.Emit(ir.CmpLt{Dest: condition, Left: iterator, Right: bound})
	}
	g.Emit(ir.Branch{Cond: ir.ValueOperand(condition, types.PrimitiveBool), Then: bodyBlock.ID, Else: endBlock.ID})

	g.currentBlock = bodyBlock
	if node.Reversed && !node.Inclusive {
		current := g.loadSlot(iteratorSlot, node.Symbol.Type)
		previous := g.currentFunction.NewValueOfType(node.Symbol.Type)
		g.Emit(ir.Sub{Dest: previous, Left: current, Right: ir.IntConstOperand("1", node.Symbol.Type)})
		g.Emit(ir.Store{Slot: iteratorSlot, Value: ir.ValueOperand(previous, node.Symbol.Type)})
	}
	popLoop := g.pushLoopTargets(endBlock.ID, postBlock.ID)
	g.generateBlock(node.Body)
	popLoop()
	if !g.currentBlockHasTerminator() {
		g.Emit(ir.Jump{Target: postBlock.ID})
	}

	g.currentBlock = postBlock
	if node.Reversed && !node.Inclusive {
		g.Emit(ir.Jump{Target: conditionBlock.ID})
		g.currentBlock = endBlock
		return
	}
	current := g.loadSlot(iteratorSlot, node.Symbol.Type)
	if node.Reversed {
		atStart := g.currentFunction.NewValueOfType(types.PrimitiveBool)
		start := g.loadSlot(boundSlot, node.Symbol.Type)
		g.Emit(ir.CmpEq{Dest: atStart, Left: current, Right: start})
		decrementBlock := g.currentFunction.NewBlock("for.range.decrement")
		g.Emit(ir.Branch{Cond: ir.ValueOperand(atStart, types.PrimitiveBool), Then: endBlock.ID, Else: decrementBlock.ID})
		g.currentBlock = decrementBlock
		previous := g.currentFunction.NewValueOfType(node.Symbol.Type)
		g.Emit(ir.Sub{Dest: previous, Left: current, Right: ir.IntConstOperand("1", node.Symbol.Type)})
		g.Emit(ir.Store{Slot: iteratorSlot, Value: ir.ValueOperand(previous, node.Symbol.Type)})
	} else {
		next := g.currentFunction.NewValueOfType(node.Symbol.Type)
		g.Emit(ir.Add{Dest: next, Left: current, Right: ir.IntConstOperand("1", node.Symbol.Type)})
		g.Emit(ir.Store{Slot: iteratorSlot, Value: ir.ValueOperand(next, node.Symbol.Type)})
	}
	g.Emit(ir.Jump{Target: conditionBlock.ID})
	g.currentBlock = endBlock
}

func (g *Generator) generateForEach(node *parser.ForEachNode) {
	iterableType := types.Underlying(node.Iterable.GetType())
	switch iterableType.(type) {
	case types.SliceType, types.ArrayType:
	default:
		panic("for-each iterable is not an array or slice")
	}

	prevEnv := g.currentEnv
	g.currentEnv = NewEnv(prevEnv)
	defer func() { g.currentEnv = prevEnv }()

	iterableSlot := g.currentFunction.NewSlot(node.Iterable.GetType(), "for.each.iterable")
	g.Emit(ir.Alloca{Slot: iterableSlot})
	g.Emit(ir.Store{Slot: iterableSlot, Value: g.GenerateExpr(node.Iterable)})
	indexSlot := g.currentFunction.NewSlot(types.PrimitiveUsz, "for.each.index")
	g.Emit(ir.Alloca{Slot: indexSlot})
	if node.Reversed {
		var length ir.Operand
		switch t := iterableType.(type) {
		case types.SliceType:
			length = g.sliceLength(iterableSlot, t)
		case types.ArrayType:
			length = ir.IntConstOperand(strconv.Itoa(t.Length), types.PrimitiveUsz)
		}
		g.Emit(ir.Store{Slot: indexSlot, Value: length})
	} else {
		g.Emit(ir.Store{Slot: indexSlot, Value: ir.IntConstOperand("0", types.PrimitiveUsz)})
	}
	var elementSlot ir.SlotID
	destructureSlots := make([]ir.SlotID, len(node.Destructure))
	if len(node.Destructure) != 0 {
		for i := range node.Destructure {
			binding := &node.Destructure[i]
			if binding.Symbol == nil {
				continue
			}
			slot := g.currentFunction.NewSlot(binding.Symbol.Type, binding.Name)
			destructureSlots[i] = slot
			g.currentEnv.Variables[binding.Symbol] = slot
			g.Emit(ir.Alloca{Slot: slot})
		}
	} else if node.Symbol != nil {
		elementSlot = g.currentFunction.NewSlot(node.Symbol.Type, node.Name)
		g.currentEnv.Variables[node.Symbol] = elementSlot
		g.Emit(ir.Alloca{Slot: elementSlot})
	}
	var visibleIndexSlot ir.SlotID
	if node.IndexSymbol != nil {
		visibleIndexSlot = g.currentFunction.NewSlot(types.PrimitiveUsz, node.IndexName)
		g.currentEnv.Variables[node.IndexSymbol] = visibleIndexSlot
		g.Emit(ir.Alloca{Slot: visibleIndexSlot})
	}

	conditionBlock := g.currentFunction.NewBlock("for.each.condition")
	bodyBlock := g.currentFunction.NewBlock("for.each.body")
	postBlock := g.currentFunction.NewBlock("for.each.post")
	endBlock := g.currentFunction.NewBlock("for.each.end")
	g.Emit(ir.Jump{Target: conditionBlock.ID})

	g.currentBlock = conditionBlock
	index := g.loadSlot(indexSlot, types.PrimitiveUsz)
	condition := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	if node.Reversed {
		g.Emit(ir.CmpGt{Dest: condition, Left: index, Right: ir.IntConstOperand("0", types.PrimitiveUsz)})
	} else {
		var length ir.Operand
		switch t := iterableType.(type) {
		case types.SliceType:
			length = g.sliceLength(iterableSlot, t)
		case types.ArrayType:
			length = ir.IntConstOperand(strconv.Itoa(t.Length), types.PrimitiveUsz)
		}
		g.Emit(ir.CmpLt{Dest: condition, Left: index, Right: length})
	}
	g.Emit(ir.Branch{Cond: ir.ValueOperand(condition, types.PrimitiveBool), Then: bodyBlock.ID, Else: endBlock.ID})

	g.currentBlock = bodyBlock
	elementIndex := index
	if node.Reversed {
		previousIndex := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
		g.Emit(ir.Sub{Dest: previousIndex, Left: index, Right: ir.IntConstOperand("1", types.PrimitiveUsz)})
		elementIndex = ir.ValueOperand(previousIndex, types.PrimitiveUsz)
	}
	if len(node.Destructure) != 0 {
		elementPointer := g.forEachElementPointer(iterableSlot, iterableType, elementIndex, false)
		g.storeForEachDestructure(elementPointer, node.Destructure, destructureSlots)
	} else if node.Symbol != nil {
		if node.ElementKind == parser.ForEachElementValue {
			g.storeForEachElement(iterableSlot, iterableType, elementIndex, elementSlot)
		} else {
			elementPointer := g.forEachElementPointer(
				iterableSlot,
				iterableType,
				elementIndex,
				node.ElementKind == parser.ForEachElementMutablePointer,
			)
			g.Emit(ir.Store{Slot: elementSlot, Value: elementPointer})
		}
	}
	if node.IndexSymbol != nil {
		g.Emit(ir.Store{Slot: visibleIndexSlot, Value: elementIndex})
	}
	popLoop := g.pushLoopTargets(endBlock.ID, postBlock.ID)
	g.generateBlock(node.Body)
	popLoop()
	if !g.currentBlockHasTerminator() {
		g.Emit(ir.Jump{Target: postBlock.ID})
	}

	g.currentBlock = postBlock
	currentIndex := g.loadSlot(indexSlot, types.PrimitiveUsz)
	nextIndex := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
	if node.Reversed {
		g.Emit(ir.Sub{Dest: nextIndex, Left: currentIndex, Right: ir.IntConstOperand("1", types.PrimitiveUsz)})
	} else {
		g.Emit(ir.Add{Dest: nextIndex, Left: currentIndex, Right: ir.IntConstOperand("1", types.PrimitiveUsz)})
	}
	g.Emit(ir.Store{Slot: indexSlot, Value: ir.ValueOperand(nextIndex, types.PrimitiveUsz)})
	g.Emit(ir.Jump{Target: conditionBlock.ID})
	g.currentBlock = endBlock
}

func (g *Generator) storeForEachDestructure(
	elementPointer ir.Operand,
	bindings []parser.ForEachBinding,
	slots []ir.SlotID,
) {
	elementType := types.Underlying(types.Underlying(elementPointer.Type).(types.PointerType).Base)
	switch element := elementType.(type) {
	case types.ArrayType:
		for i := range bindings {
			if bindings[i].Symbol == nil {
				continue
			}
			pointerType := types.PointerType{Base: element.Base}
			componentPointer := g.currentFunction.NewValueOfType(pointerType)
			g.Emit(ir.ElementAddress{
				Dest:        componentPointer,
				Base:        elementPointer,
				Index:       ir.IntConstOperand(strconv.Itoa(i), types.PrimitiveUsz),
				Element:     element.Base,
				ArrayObject: true,
			})
			value := g.currentFunction.NewValueOfType(element.Base)
			g.Emit(ir.LoadPtr{Dest: value, Ptr: ir.ValueOperand(componentPointer, pointerType)})
			g.Emit(ir.Store{Slot: slots[i], Value: ir.ValueOperand(value, element.Base)})
		}
	case types.StructType:
		for i, field := range element.Fields {
			if bindings[i].Symbol == nil {
				continue
			}
			pointerType := types.PointerType{Base: field.R}
			componentPointer := g.currentFunction.NewValueOfType(pointerType)
			g.Emit(ir.FieldAddress{Dest: componentPointer, Base: elementPointer, Field: field.L})
			value := g.currentFunction.NewValueOfType(field.R)
			g.Emit(ir.LoadPtr{Dest: value, Ptr: ir.ValueOperand(componentPointer, pointerType)})
			g.Emit(ir.Store{Slot: slots[i], Value: ir.ValueOperand(value, field.R)})
		}
	default:
		panic("validated for-each destructuring element is not an array or struct")
	}
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

func (g *Generator) storeForEachElement(iterableSlot ir.SlotID, iterableType types.Type, index ir.Operand, elementSlot ir.SlotID) {
	elementPointer := g.forEachElementPointer(iterableSlot, iterableType, index, false)
	pointerType := types.Underlying(elementPointer.Type).(types.PointerType)
	elementID := g.currentFunction.NewValueOfType(pointerType.Base)
	g.Emit(ir.LoadPtr{Dest: elementID, Ptr: elementPointer})
	g.Emit(ir.Store{Slot: elementSlot, Value: ir.ValueOperand(elementID, pointerType.Base)})
}

func iterableTypeIsArray(iterableType types.Type) bool {
	_, ok := iterableType.(types.ArrayType)
	return ok
}

func (g *Generator) forEachElementPointer(
	iterableSlot ir.SlotID,
	iterableType types.Type,
	index ir.Operand,
	mutable bool,
) ir.Operand {
	var base ir.Operand
	var elementType types.Type
	switch t := iterableType.(type) {
	case types.SliceType:
		elementType = t.Base
		slicePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: t})
		g.Emit(ir.AddressOf{Dest: slicePtrID, Slot: iterableSlot})
		slicePtr := ir.ValueOperand(slicePtrID, types.PointerType{Base: t})
		basePtrPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PointerType{Base: t.Base}})
		g.Emit(ir.FieldAddress{Dest: basePtrPtrID, Base: slicePtr, Field: "0"})
		basePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: t.Base})
		g.Emit(ir.LoadPtr{Dest: basePtrID, Ptr: ir.ValueOperand(basePtrPtrID, types.PointerType{Base: types.PointerType{Base: t.Base}})})
		base = ir.ValueOperand(basePtrID, types.PointerType{Base: t.Base})
	case types.ArrayType:
		elementType = t.Base
		arrayPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: t})
		g.Emit(ir.AddressOf{Dest: arrayPtrID, Slot: iterableSlot})
		base = ir.ValueOperand(arrayPtrID, types.PointerType{Base: t})
	}
	elementPointerType := types.PointerType{Base: elementType, Mutable: mutable}
	elementPtrID := g.currentFunction.NewValueOfType(elementPointerType)
	g.Emit(ir.ElementAddress{
		Dest:        elementPtrID,
		Base:        base,
		Index:       index,
		Element:     elementType,
		ArrayObject: iterableTypeIsArray(iterableType),
	})
	return ir.ValueOperand(elementPtrID, elementPointerType)
}

func (g *Generator) GenerateExpr(expr parser.ExpressionNode) ir.Operand {
	switch n := expr.(type) {
	case *parser.LambdaNode:
		if n.Function == nil || n.Function.Symbol == nil {
			panic("unresolved lambda reached IR generation")
		}
		if !g.queuedLambdas[n.Function.Symbol] {
			g.queuedLambdas[n.Function.Symbol] = true
			g.lambdaFunctions = append(g.lambdaFunctions, n.Function)
		}
		name := g.mangleFunctionName(g.ModuleName, n.Function.Symbol.Name)
		return ir.FunctionConstOperand(name, n.GetType())
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
	case *parser.BlockNode:
		return g.generateBlockExpr(n)
	case *parser.IfNode:
		return g.generateIfExpr(n)
	case *parser.MatchNode:
		return g.generateMatch(n)
	case *parser.BinaryOpNode:
		return g.generateBinaryExpr(n)
	case *parser.UnaryOpNode:
		return g.generateUnaryExpr(n)
	case *parser.StructLiteralNode:
		return g.generateStructLiteralExpr(n)
	case *parser.SliceLiteralNode:
		return g.generateSliceLiteralExpr(n)
	case *parser.IndexExprNode:
		return g.generateIndexExpr(n)
	case *parser.SliceExprNode:
		return g.generateSliceExpr(n)
	case *parser.FunctionCallNode:
		if n.TaggedUnionType != nil {
			return g.generateTaggedUnionConstructor(n)
		}
		return g.generateFunctionCallExpr(n)
	case *parser.InlineAsmNode:
		return g.generateInlineAsmExpr(n)
	case *parser.FieldAccessNode:
		if n.TaggedUnionType != nil {
			return g.generateTaggedUnionConstructor(&parser.FunctionCallNode{
				Loc: n.Loc, TaggedUnionType: n.TaggedUnionType, TaggedUnionVariant: n.TaggedUnionVariant,
			})
		}
		return g.generateFieldAccessExpr(n)
	case *parser.CastNode:
		return g.generateCastExpr(n)
	case *parser.ReprNode:
		value := g.GenerateExpr(n.Operand)
		value.Type = n.GetType()
		return value
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
	case *parser.EmbedNode:
		return g.generateStringLiteralExpr(&parser.StringLiteralNode{Value: n.Contents, Type: n.GetType()})
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

func (g *Generator) generateInlineAsmExpr(node *parser.InlineAsmNode) ir.Operand {
	args := make([]ir.Operand, len(node.Inputs))
	constraints := make([]string, 0, len(node.Outputs)+len(node.Inputs))
	for _, output := range node.Outputs {
		constraints = append(constraints, output.Constraint)
	}
	for i, input := range node.Inputs {
		args[i] = g.GenerateExpr(input.Value)
		constraints = append(constraints, input.Constraint)
	}
	instr := ir.InlineAsm{
		Template:    node.Template,
		Constraints: constraints,
		Clobbers:    append([]string(nil), node.Clobbers...),
		Args:        args,
		ResultType:  node.GetType(),
		SideEffect:  node.Volatile || len(node.Outputs) == 0 || len(node.Clobbers) != 0,
	}
	if node.GetType().Equals(types.PrimitiveVoid) {
		g.Emit(instr)
		return ir.NullConstOperand(types.PrimitiveVoid)
	}
	instr.Dest = g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(instr)
	return ir.ValueOperand(instr.Dest, node.GetType())
}

func (g *Generator) generateBlockExpr(node *parser.BlockNode) ir.Operand {
	prev := g.currentEnv
	g.currentEnv = NewEnv(prev)
	g.deferScopes = append(g.deferScopes, nil)
	defer func() {
		g.deferScopes = g.deferScopes[:len(g.deferScopes)-1]
		g.currentEnv = prev
	}()

	resultExpr, hasResult := parser.BlockResult(node)
	childCount := len(node.Body)
	if hasResult {
		childCount--
	}
	for _, child := range node.Body[:childCount] {
		if g.currentBlockHasTerminator() {
			break
		}
		g.GenerateNode(child)
	}
	if g.currentBlockHasTerminator() {
		return ir.ZeroConstOperand(node.GetType())
	}
	if !hasResult {
		panic("falling-through block expression has no result")
	}
	result := g.GenerateExpr(resultExpr)
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
	arrayObject := false
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

	case types.ArrayType:
		base = g.generateAddressOfExpr(node.Subject)
		arrayObject = true

	case types.PointerType:
		base = g.GenerateExpr(node.Subject)

	default:
		panic(fmt.Sprintf("cannot generate index expression for %T", subjectType))
	}

	elementPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: node.GetType()})
	g.Emit(ir.ElementAddress{
		Dest:        elementPtrID,
		Base:        base,
		Index:       index,
		Element:     node.GetType(),
		ArrayObject: arrayObject,
	})
	return ir.ValueOperand(elementPtrID, types.PointerType{Base: node.GetType()})
}

func (g *Generator) generateSliceExpr(node *parser.SliceExprNode) ir.Operand {
	subjectType := types.Underlying(node.Subject.GetType())
	resultType := types.Underlying(node.GetType()).(types.SliceType)
	var data ir.Operand
	var length ir.Operand
	checkEndAgainstLength := false

	switch subjectType := subjectType.(type) {
	case types.SliceType:
		subject := g.GenerateExpr(node.Subject)
		dataID := g.currentFunction.NewValueOfType(types.PointerType{Base: subjectType.Base})
		g.Emit(ir.ExtractValue{Dest: dataID, Aggregate: subject, Index: 0})
		lengthID := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
		g.Emit(ir.ExtractValue{Dest: lengthID, Aggregate: subject, Index: 1})
		data = ir.ValueOperand(dataID, types.PointerType{Base: subjectType.Base})
		length = ir.ValueOperand(lengthID, types.PrimitiveUsz)
		checkEndAgainstLength = true
	case types.ArrayType:
		arrayPtr := g.generateAddressOfExpr(node.Subject)
		dataID := g.currentFunction.NewValueOfType(types.PointerType{Base: subjectType.Base})
		g.Emit(ir.ElementAddress{
			Dest: dataID, Base: arrayPtr, Index: ir.IntConstOperand("0", types.PrimitiveUsz), Element: subjectType.Base, ArrayObject: true,
		})
		data = ir.ValueOperand(dataID, types.PointerType{Base: subjectType.Base})
		length = ir.IntConstOperand(strconv.Itoa(subjectType.Length), types.PrimitiveUsz)
		checkEndAgainstLength = true
	case types.PointerType:
		data = g.GenerateExpr(node.Subject)
	default:
		panic(fmt.Sprintf("cannot generate slice expression for %T", subjectType))
	}

	start := ir.IntConstOperand("0", types.PrimitiveUsz)
	if node.Start != nil {
		start = g.GenerateExpr(node.Start)
	}
	var end ir.Operand
	if node.End == nil {
		end = length
	} else {
		end = g.GenerateExpr(node.End)
	}

	startAfterEnd := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	g.Emit(ir.CmpGt{Dest: startAfterEnd, Left: start, Right: end})
	var invalid ir.Operand = ir.ValueOperand(startAfterEnd, types.PrimitiveBool)
	if checkEndAgainstLength {
		endAfterLength := g.currentFunction.NewValueOfType(types.PrimitiveBool)
		g.Emit(ir.CmpGt{Dest: endAfterLength, Left: end, Right: length})
		invalidID := g.currentFunction.NewValueOfType(types.PrimitiveBool)
		g.Emit(ir.LogicalOr{
			Dest:  invalidID,
			Left:  invalid,
			Right: ir.ValueOperand(endAfterLength, types.PrimitiveBool),
		})
		invalid = ir.ValueOperand(invalidID, types.PrimitiveBool)
	}

	panicBlock := g.currentFunction.NewBlock("slice.bounds.panic")
	validBlock := g.currentFunction.NewBlock("slice.bounds.valid")
	g.Emit(ir.Branch{
		Cond: invalid,
		Then: panicBlock.ID,
		Else: validBlock.ID,
	})

	g.currentBlock = panicBlock
	g.emitRuntimePanic("slice bounds out of range")

	g.currentBlock = validBlock
	slicedDataID := g.currentFunction.NewValueOfType(types.PointerType{Base: resultType.Base})
	g.Emit(ir.ElementAddress{
		Dest:    slicedDataID,
		Base:    data,
		Index:   start,
		Element: resultType.Base,
	})
	slicedLengthID := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
	g.Emit(ir.Sub{Dest: slicedLengthID, Left: end, Right: start})

	first := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.InsertValue{
		Dest:      first,
		Aggregate: ir.ZeroConstOperand(node.GetType()),
		Value:     ir.ValueOperand(slicedDataID, types.PointerType{Base: resultType.Base}),
		Index:     0,
	})
	second := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.InsertValue{
		Dest:      second,
		Aggregate: ir.ValueOperand(first, node.GetType()),
		Value:     ir.ValueOperand(slicedLengthID, types.PrimitiveUsz),
		Index:     1,
	})
	return ir.ValueOperand(second, node.GetType())
}

func (g *Generator) emitRuntimePanic(messageText string) {
	messageNode := &parser.StringLiteralNode{
		Value: messageText,
		Type:  types.SliceType{Base: types.PrimitiveU8},
	}
	message := g.generateStringLiteralExpr(messageNode)
	runtimeStr := types.SliceType{Base: types.PrimitiveU8}
	message.Type = runtimeStr
	panicSig := ir.FunctionSignature{ParamTypes: []types.Type{runtimeStr}, ReturnType: types.PrimitiveVoid}
	g.addExternForCall("__qk_panic", panicSig, "", true)
	g.Emit(ir.Call{Name: "__qk_panic", Args: []ir.Operand{message}, Signature: panicSig})
	g.Emit(ir.Unreachable{})
}

func (g *Generator) generateStringLiteralExpr(node *parser.StringLiteralNode) ir.Operand {
	sliceType, ok := types.Underlying(node.GetType()).(types.SliceType)
	if !ok {
		panic("string literal must have slice type")
	}

	tmpSlot := g.currentFunction.NewSlot(sliceType, "")
	g.Emit(ir.Alloca{Slot: tmpSlot})

	stringPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveU8})
	g.Emit(ir.StringConst{Dest: stringPtrID, Value: node.Value})
	stringPtr := ir.ValueOperand(stringPtrID, types.PointerType{Base: types.PrimitiveU8})

	slicePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType})
	g.Emit(ir.AddressOf{Dest: slicePtrID, Slot: tmpSlot})
	slicePtr := ir.ValueOperand(slicePtrID, types.PointerType{Base: sliceType})

	basePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveU8})
	g.Emit(ir.FieldAddress{Dest: basePtrID, Base: slicePtr, Field: "0"})
	g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(basePtrID, types.PointerType{Base: types.PrimitiveU8}), Value: stringPtr})

	lenPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveUsz})
	g.Emit(ir.FieldAddress{Dest: lenPtrID, Base: slicePtr, Field: "1"})
	g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(lenPtrID, types.PointerType{Base: types.PrimitiveUsz}), Value: ir.IntConstOperand(fmt.Sprintf("%d", len(node.Value)), types.PrimitiveUsz)})

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
	if _, traitPointer := types.Underlying(node.GetType()).(types.TraitPointerType); traitPointer {
		return ir.ZeroConstOperand(node.GetType())
	}
	return ir.NullConstOperand(node.GetType())
}

func (g *Generator) generateCastExpr(node *parser.CastNode) ir.Operand {
	targetType := node.GetType()
	if node.Checked {
		targetType = node.CheckedType
	}
	if node.TraitRecast {
		return g.generateTraitRecast(node)
	}
	if node.TraitConversion {
		value := g.generateTraitConversion(node)
		if node.Checked {
			return g.packCheckedCast(value, ir.BoolConstOperand(true), node.GetType().(types.MultipleReturnType))
		}
		return value
	}
	if node.TraitUnwrap {
		return g.generateTraitUnwrap(node)
	}
	assertionMatches := node.AssertionMatches
	if node.GenericAssertion {
		assertionMatches = node.Operand.GetType().Equals(targetType)
	}
	if (node.GenericAssertion || node.StaticAssertion || node.StaticTraitView != nil) && !assertionMatches {
		return g.generateFailedStaticAssertion(node, targetType)
	}

	if str, ok := node.Operand.(*parser.StringLiteralNode); ok {
		if _, ok := types.Underlying(targetType).(types.PointerType); ok {
			dst := g.currentFunction.NewValueOfType(targetType)
			g.Emit(ir.StringConst{Dest: dst, Value: str.Value})
			return g.finishCertainCheckedCast(node, ir.ValueOperand(dst, targetType))
		}
	}
	if sourceArray, ok := types.Underlying(node.Operand.GetType()).(types.ArrayType); ok {
		if targetSlice, ok := types.Underlying(targetType).(types.SliceType); ok && sourceArray.Base.Equals(targetSlice.Base) {
			return g.finishCertainCheckedCast(node, g.generateArrayBorrow(node.Operand, targetType, sourceArray))
		}
	}
	if sourceSlice, ok := types.Underlying(node.Operand.GetType()).(types.SliceType); ok {
		if targetArray, ok := types.Underlying(targetType).(types.ArrayType); ok && sourceSlice.Base.Equals(targetArray.Base) {
			return g.generateSliceToArrayCast(node, targetType, sourceSlice, targetArray)
		}
	}

	from := g.GenerateExpr(node.Operand)
	if from.Type.Equals(targetType) {
		return g.finishCertainCheckedCast(node, from)
	}
	if types.Underlying(from.Type).Equals(types.Underlying(targetType)) {
		from.Type = targetType
		return g.finishCertainCheckedCast(node, from)
	}
	if fromSlice, ok := types.Underlying(from.Type).(types.SliceType); ok {
		if toSlice, ok := types.Underlying(targetType).(types.SliceType); ok && fromSlice.Base.Equals(toSlice.Base) {
			// Mutable and immutable slices have the same runtime representation.
			from.Type = targetType
			return g.finishCertainCheckedCast(node, from)
		}
	}

	dst := g.currentFunction.NewValueOfType(targetType)
	g.Emit(ir.Cast{Dest: dst, From: from, To: targetType})
	value := ir.ValueOperand(dst, targetType)
	if node.Checked {
		return g.packCheckedCast(value, ir.BoolConstOperand(true), node.GetType().(types.MultipleReturnType))
	}
	return value
}

func (g *Generator) generateArrayBorrow(operand parser.ExpressionNode, targetType types.Type, arrayType types.ArrayType) ir.Operand {
	arrayPtr := g.generateAddressOfExpr(operand)
	dataID := g.currentFunction.NewValueOfType(types.PointerType{Base: arrayType.Base})
	g.Emit(ir.ElementAddress{
		Dest: dataID, Base: arrayPtr, Index: ir.IntConstOperand("0", types.PrimitiveUsz), Element: arrayType.Base, ArrayObject: true,
	})
	data := ir.ValueOperand(dataID, types.PointerType{Base: arrayType.Base})

	resultSlot := g.currentFunction.NewSlot(targetType, "array.view")
	g.Emit(ir.Alloca{Slot: resultSlot})
	resultPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: targetType, Mutable: true})
	g.Emit(ir.AddressOf{Dest: resultPtrID, Slot: resultSlot})
	resultPtr := ir.ValueOperand(resultPtrID, types.PointerType{Base: targetType, Mutable: true})
	dataFieldID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PointerType{Base: arrayType.Base}, Mutable: true})
	g.Emit(ir.FieldAddress{Dest: dataFieldID, Base: resultPtr, Field: "0"})
	g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(dataFieldID, types.PointerType{Base: types.PointerType{Base: arrayType.Base}, Mutable: true}), Value: data})
	lengthFieldID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveUsz, Mutable: true})
	g.Emit(ir.FieldAddress{Dest: lengthFieldID, Base: resultPtr, Field: "1"})
	g.Emit(ir.StorePtr{
		Ptr:   ir.ValueOperand(lengthFieldID, types.PointerType{Base: types.PrimitiveUsz, Mutable: true}),
		Value: ir.IntConstOperand(strconv.Itoa(arrayType.Length), types.PrimitiveUsz),
	})
	resultID := g.currentFunction.NewValueOfType(targetType)
	g.Emit(ir.Load{Dest: resultID, Slot: resultSlot})
	return ir.ValueOperand(resultID, targetType)
}

func (g *Generator) generateSliceToArrayCast(node *parser.CastNode, targetType types.Type, sourceSlice types.SliceType, targetArray types.ArrayType) ir.Operand {
	source := g.GenerateExpr(node.Operand)
	dataID := g.currentFunction.NewValueOfType(types.PointerType{Base: sourceSlice.Base})
	g.Emit(ir.ExtractValue{Dest: dataID, Aggregate: source, Index: 0})
	data := ir.ValueOperand(dataID, types.PointerType{Base: sourceSlice.Base})
	lengthID := g.currentFunction.NewValueOfType(types.PrimitiveUsz)
	g.Emit(ir.ExtractValue{Dest: lengthID, Aggregate: source, Index: 1})
	length := ir.ValueOperand(lengthID, types.PrimitiveUsz)
	matchesID := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	g.Emit(ir.CmpEq{Dest: matchesID, Left: length, Right: ir.IntConstOperand(strconv.Itoa(targetArray.Length), types.PrimitiveUsz)})
	matches := ir.ValueOperand(matchesID, types.PrimitiveBool)

	arraySlot := g.currentFunction.NewSlot(targetType, "slice.array")
	g.Emit(ir.Alloca{Slot: arraySlot})
	if node.Checked {
		g.Emit(ir.Store{Slot: arraySlot, Value: ir.ZeroConstOperand(targetType)})
	}
	success := g.currentFunction.NewBlock("slice.array.success")
	failure := g.currentFunction.NewBlock("slice.array.failure")
	merge := g.currentFunction.NewBlock("slice.array.end")
	g.Emit(ir.Branch{Cond: matches, Then: success.ID, Else: failure.ID})

	g.currentBlock = failure
	if node.Checked {
		g.Emit(ir.Jump{Target: merge.ID})
	} else {
		g.emitRuntimePanic("slice-to-array cast length mismatch")
	}

	g.currentBlock = success
	arrayPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: targetType, Mutable: true})
	g.Emit(ir.AddressOf{Dest: arrayPtrID, Slot: arraySlot})
	arrayPtr := ir.ValueOperand(arrayPtrID, types.PointerType{Base: targetType, Mutable: true})
	for i := 0; i < targetArray.Length; i++ {
		index := ir.IntConstOperand(strconv.Itoa(i), types.PrimitiveUsz)
		sourcePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: sourceSlice.Base})
		g.Emit(ir.ElementAddress{Dest: sourcePtrID, Base: data, Index: index, Element: sourceSlice.Base})
		valueID := g.currentFunction.NewValueOfType(sourceSlice.Base)
		g.Emit(ir.LoadPtr{Dest: valueID, Ptr: ir.ValueOperand(sourcePtrID, types.PointerType{Base: sourceSlice.Base})})
		targetPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: targetArray.Base, Mutable: true})
		g.Emit(ir.ElementAddress{Dest: targetPtrID, Base: arrayPtr, Index: index, Element: targetArray.Base, ArrayObject: true})
		g.Emit(ir.StorePtr{
			Ptr:   ir.ValueOperand(targetPtrID, types.PointerType{Base: targetArray.Base, Mutable: true}),
			Value: ir.ValueOperand(valueID, sourceSlice.Base),
		})
	}
	g.Emit(ir.Jump{Target: merge.ID})

	g.currentBlock = merge
	arrayID := g.currentFunction.NewValueOfType(targetType)
	g.Emit(ir.Load{Dest: arrayID, Slot: arraySlot})
	array := ir.ValueOperand(arrayID, targetType)
	if node.Checked {
		return g.packCheckedCast(array, matches, node.GetType().(types.MultipleReturnType))
	}
	return array
}

func (g *Generator) generateFailedStaticAssertion(node *parser.CastNode, targetType types.Type) ir.Operand {
	// Assertions evaluate their operand even when specialization makes the
	// outcome statically known.
	g.GenerateExpr(node.Operand)
	zero := ir.ZeroConstOperand(targetType)
	if node.Checked {
		return g.packCheckedCast(zero, ir.BoolConstOperand(false), node.GetType().(types.MultipleReturnType))
	}

	expected := traitRuntimeName(targetType)
	if node.StaticTraitView != nil {
		expected = node.StaticTraitView.String()
	}
	messageText := "type assertion failed: " + traitRuntimeName(node.Operand.GetType()) + " is not " + expected
	messageNode := &parser.StringLiteralNode{Value: messageText, Type: types.SliceType{Base: types.PrimitiveU8}, Loc: node.Loc}
	message := g.generateStringLiteralExpr(messageNode)
	runtimeStr := types.SliceType{Base: types.PrimitiveU8}
	message.Type = runtimeStr
	panicSig := ir.FunctionSignature{ParamTypes: []types.Type{runtimeStr}, ReturnType: types.PrimitiveVoid}
	g.addExternForCall("__qk_panic", panicSig, "", true)
	g.Emit(ir.Call{Name: "__qk_panic", Args: []ir.Operand{message}, Signature: panicSig})
	g.Emit(ir.Unreachable{})
	g.currentBlock = g.currentFunction.NewBlock("assert.unreachable")
	return zero
}

func (g *Generator) finishCertainCheckedCast(node *parser.CastNode, value ir.Operand) ir.Operand {
	if !node.Checked {
		return value
	}
	return g.packCheckedCast(value, ir.BoolConstOperand(true), node.GetType().(types.MultipleReturnType))
}

func (g *Generator) packCheckedCast(value, ok ir.Operand, result types.MultipleReturnType) ir.Operand {
	first := g.currentFunction.NewValueOfType(result)
	g.Emit(ir.InsertValue{Dest: first, Aggregate: ir.ZeroConstOperand(result), Value: value, Index: 0})
	second := g.currentFunction.NewValueOfType(result)
	g.Emit(ir.InsertValue{Dest: second, Aggregate: ir.ValueOperand(first, result), Value: ok, Index: 1})
	return ir.ValueOperand(second, result)
}

func traitRuntimeName(t types.Type) string {
	switch t := t.(type) {
	case types.DefinedType:
		return t.Module + "." + t.Name
	case *types.AliasRef:
		return t.Module + "." + t.Name
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
	name := fmt.Sprintf("__qk_vtable_%s_%x", sanitizeName(key), stringHash(key))
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
		if g.DemandedTraitSlots != nil && !g.DemandedTraitSlots[trait.String()][i] {
			params := append([]types.Type{traitErasedReceiverType(req)}, req.Parameters...)
			fnType := types.PointerType{Base: types.FunctionType{Parameters: params, ReturnType: req.ReturnType}}
			values = append(values, ir.NullConstOperand(fnType))
			continue
		}
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
		functionOperand := ir.FunctionConstOperand(fnName, fnType)
		if method.TemplateSymbol != nil {
			functionOperand.Generic = &ir.GenericReference{
				Module: method.TemplateSymbol.DefinitionModule, Name: method.TemplateSymbol.Name,
				TypeArguments: append([]types.Type(nil), method.TypeArguments...),
			}
		}
		values = append(values, functionOperand)
		if methodModule != g.ModuleName && req.Receiver != types.TraitReceiverValue {
			g.addExternForCall(fnName, ir.FunctionSignature{ParamTypes: method.Signature.Parameters, ReturnType: method.Signature.ReturnType}, "", true)
		}
	}
	g.Module.AddGlobal(ir.Global{Name: name, Type: vt, Linkage: ir.LinkageInternal, Value: ir.StructConstOperand(vt, values)})
	return name
}

func stringHash(value string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(value))
	return h.Sum64()
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
	targetType := node.GetType()
	if node.Checked {
		targetType = node.CheckedType
	}
	target := targetType.(types.TraitPointerType)
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
	targetType := node.GetType()
	if node.Checked {
		targetType = node.CheckedType
	}
	target := targetType.(types.TraitPointerType)
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
	okSlot := g.currentFunction.NewSlot(types.PrimitiveBool, "trait.recast.ok")
	if node.Checked {
		g.Emit(ir.Alloca{Slot: okSlot})
		g.Emit(ir.Store{Slot: okSlot, Value: ir.BoolConstOperand(false)})
	}
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
		if node.Checked {
			g.Emit(ir.Store{Slot: okSlot, Value: ir.BoolConstOperand(true)})
		}
		g.Emit(ir.Jump{Target: end.ID})
		g.currentBlock = next
	}

	if node.Checked {
		g.Emit(ir.Store{Slot: resultSlot, Value: ir.ZeroConstOperand(target)})
		g.Emit(ir.Jump{Target: end.ID})
	} else {
		messageText := "trait cast failed: value does not implement " + target.Trait.String()
		messageNode := &parser.StringLiteralNode{Value: messageText, Type: types.SliceType{Base: types.PrimitiveU8}, Loc: node.Loc}
		message := g.generateStringLiteralExpr(messageNode)
		runtimeStr := types.SliceType{Base: types.PrimitiveU8}
		message.Type = runtimeStr
		panicSig := ir.FunctionSignature{ParamTypes: []types.Type{runtimeStr}, ReturnType: types.PrimitiveVoid}
		g.addExternForCall("__qk_panic", panicSig, "", true)
		g.Emit(ir.Call{Name: "__qk_panic", Args: []ir.Operand{message}, Signature: panicSig})
		g.Emit(ir.Unreachable{})
	}

	g.currentBlock = end
	result := g.currentFunction.NewValueOfType(target)
	g.Emit(ir.Load{Dest: result, Slot: resultSlot})
	value := ir.ValueOperand(result, target)
	if node.Checked {
		okID := g.currentFunction.NewValueOfType(types.PrimitiveBool)
		g.Emit(ir.Load{Dest: okID, Slot: okSlot})
		return g.packCheckedCast(value, ir.ValueOperand(okID, types.PrimitiveBool), node.GetType().(types.MultipleReturnType))
	}
	return value
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
	end := g.currentFunction.NewBlock("trait.unwrap.end")
	targetType := node.GetType()
	if node.Checked {
		targetType = node.CheckedType
	}
	resultSlot := g.currentFunction.NewSlot(targetType, "trait.unwrap")
	okSlot := g.currentFunction.NewSlot(types.PrimitiveBool, "trait.unwrap.ok")
	if node.Checked {
		g.Emit(ir.Alloca{Slot: resultSlot})
		g.Emit(ir.Alloca{Slot: okSlot})
		g.Emit(ir.Store{Slot: okSlot, Value: ir.BoolConstOperand(false)})
	}
	g.Emit(ir.Branch{Cond: ir.ValueOperand(matches, types.PrimitiveBool), Then: success.ID, Else: failure.ID})
	g.currentBlock = failure
	if node.Checked {
		g.Emit(ir.Store{Slot: resultSlot, Value: ir.ZeroConstOperand(targetType)})
		g.Emit(ir.Jump{Target: end.ID})
	} else {
		messageText := "trait unwrap failed: expected " + traitRuntimeName(node.ConcreteType)
		messageNode := &parser.StringLiteralNode{Value: messageText, Type: types.SliceType{Base: types.PrimitiveU8}, Loc: node.Loc}
		message := g.generateStringLiteralExpr(messageNode)
		runtimeStr := types.SliceType{Base: types.PrimitiveU8}
		message.Type = runtimeStr
		panicSig := ir.FunctionSignature{ParamTypes: []types.Type{runtimeStr}, ReturnType: types.PrimitiveVoid}
		g.addExternForCall("__qk_panic", panicSig, "", true)
		g.Emit(ir.Call{Name: "__qk_panic", Args: []ir.Operand{message}, Signature: panicSig})
		g.Emit(ir.Unreachable{})
	}
	g.currentBlock = success
	dataType := types.PointerType{Base: types.PrimitiveVoid, Mutable: traitPtr.Mutable}
	dataID := g.currentFunction.NewValueOfType(dataType)
	g.Emit(ir.ExtractValue{Dest: dataID, Aggregate: traitValue, Index: 0})
	targetPointer, byPointer := types.Underlying(targetType).(types.PointerType)
	if !byPointer {
		targetPointer = types.PointerType{Base: targetType}
	}
	castID := g.currentFunction.NewValueOfType(targetPointer)
	g.Emit(ir.Cast{Dest: castID, From: ir.ValueOperand(dataID, dataType), To: targetPointer})
	if byPointer {
		value := ir.ValueOperand(castID, targetType)
		if node.Checked {
			g.Emit(ir.Store{Slot: resultSlot, Value: value})
			g.Emit(ir.Store{Slot: okSlot, Value: ir.BoolConstOperand(true)})
			g.Emit(ir.Jump{Target: end.ID})
		} else {
			return value
		}
	} else {
		valueID := g.currentFunction.NewValueOfType(targetType)
		g.Emit(ir.LoadPtr{Dest: valueID, Ptr: ir.ValueOperand(castID, targetPointer)})
		value := ir.ValueOperand(valueID, targetType)
		if node.Checked {
			g.Emit(ir.Store{Slot: resultSlot, Value: value})
			g.Emit(ir.Store{Slot: okSlot, Value: ir.BoolConstOperand(true)})
			g.Emit(ir.Jump{Target: end.ID})
		} else {
			return value
		}
	}
	g.currentBlock = end
	valueID := g.currentFunction.NewValueOfType(targetType)
	g.Emit(ir.Load{Dest: valueID, Slot: resultSlot})
	okID := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	g.Emit(ir.Load{Dest: okID, Slot: okSlot})
	return g.packCheckedCast(ir.ValueOperand(valueID, targetType), ir.ValueOperand(okID, types.PrimitiveBool), node.GetType().(types.MultipleReturnType))
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
	if flagType, ok := types.Underlying(node.GetType()).(types.FlagsType); ok {
		value := new(big.Int)
		for _, member := range node.FlagMembers {
			raw, _ := flagType.VariantValue(member)
			part, _ := new(big.Int).SetString(raw, 10)
			value.Or(value, part)
		}
		return ir.IntConstOperand(value.String(), node.GetType())
	}
	tmpSlot := g.currentFunction.NewSlot(node.GetType(), "")
	g.Emit(ir.Alloca{Slot: tmpSlot})
	g.generateStructLiteralIntoSlot(tmpSlot, node)

	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.Load{Dest: dst, Slot: tmpSlot})
	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) generateTaggedUnionConstructor(node *parser.FunctionCallNode) ir.Operand {
	info, ok := types.TaggedUnion(node.TaggedUnionType)
	if !ok || node.TaggedUnionVariant < 0 || node.TaggedUnionVariant >= len(info.Variants) {
		panic("invalid tagged union constructor")
	}
	variant := info.Variants[node.TaggedUnionVariant]
	slot := g.currentFunction.NewSlot(node.TaggedUnionType, "tagged.union")
	g.Emit(ir.Alloca{Slot: slot})
	g.Emit(ir.Store{Slot: slot, Value: ir.ZeroConstOperand(node.TaggedUnionType)})
	baseID := g.currentFunction.NewValueOfType(types.PointerType{Base: node.TaggedUnionType})
	g.Emit(ir.AddressOf{Dest: baseID, Slot: slot})
	base := ir.ValueOperand(baseID, types.PointerType{Base: node.TaggedUnionType})

	tagPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: info.Tag})
	g.Emit(ir.FieldAddress{Dest: tagPtrID, Base: base, Field: "$tag"})
	g.Emit(ir.StorePtr{
		Ptr:   ir.ValueOperand(tagPtrID, types.PointerType{Base: info.Tag}),
		Value: ir.IntConstOperand(variant.TagValue, info.Tag),
	})

	if len(variant.Fields) != 0 {
		storage := types.Underlying(node.TaggedUnionType).(types.StructType)
		payloadUnion := storage.Fields[1].R
		payloadPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: payloadUnion})
		g.Emit(ir.FieldAddress{Dest: payloadPtrID, Base: base, Field: "$payload"})
		payloadPtr := ir.ValueOperand(payloadPtrID, types.PointerType{Base: payloadUnion})
		variantType := types.StructType{Fields: variant.Fields}
		variantPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: variantType})
		g.Emit(ir.FieldAddress{Dest: variantPtrID, Base: payloadPtr, Field: variant.Name})
		variantPtr := ir.ValueOperand(variantPtrID, types.PointerType{Base: variantType})
		for i, field := range variant.Fields {
			fieldPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: field.R})
			g.Emit(ir.FieldAddress{Dest: fieldPtrID, Base: variantPtr, Field: field.L})
			g.Emit(ir.StorePtr{
				Ptr:   ir.ValueOperand(fieldPtrID, types.PointerType{Base: field.R}),
				Value: g.GenerateExpr(node.Args[i]),
			})
		}
	}

	resultID := g.currentFunction.NewValueOfType(node.TaggedUnionType)
	g.Emit(ir.Load{Dest: resultID, Slot: slot})
	return ir.ValueOperand(resultID, node.TaggedUnionType)
}

func (g *Generator) generateMatch(node *parser.MatchNode) ir.Operand {
	previousEnv := g.currentEnv
	g.currentEnv = NewEnv(previousEnv)
	defer func() { g.currentEnv = previousEnv }()

	subjectSlot := g.currentFunction.NewSlot(node.Subject.GetType(), "match.subject")
	g.Emit(ir.Alloca{Slot: subjectSlot})
	g.Emit(ir.Store{Slot: subjectSlot, Value: g.GenerateExpr(node.Subject)})
	if node.Binding != nil {
		g.currentEnv.Variables[node.Binding] = subjectSlot
	}

	var resultSlot ir.SlotID
	fallsThrough := node.Expression && parser.NodeFallsThrough(node)
	if fallsThrough {
		resultSlot = g.currentFunction.NewSlot(node.GetType(), "match.result")
		g.Emit(ir.Alloca{Slot: resultSlot})
	}
	mergeBlock := g.currentFunction.NewBlock("match.merge")

	for index := range node.Arms {
		arm := &node.Arms[index]
		setupBlock := g.currentFunction.NewBlock(fmt.Sprintf("match.arm.%d.setup", index))
		bodyBlock := g.currentFunction.NewBlock(fmt.Sprintf("match.arm.%d.body", index))
		nextBlock := g.currentFunction.NewBlock(fmt.Sprintf("match.arm.%d.next", index))
		condition := g.generateMatchPatternCondition(arm.Pattern, subjectSlot, node.Subject.GetType())
		g.Emit(ir.Branch{Cond: condition, Then: setupBlock.ID, Else: nextBlock.ID})

		g.currentBlock = setupBlock
		armEnv := NewEnv(g.currentEnv)
		g.currentEnv = armEnv
		g.bindMatchPattern(arm.Pattern, subjectSlot, node.Subject.GetType())
		if arm.Guard != nil {
			guard := g.GenerateExpr(arm.Guard)
			g.Emit(ir.Branch{Cond: guard, Then: bodyBlock.ID, Else: nextBlock.ID})
		} else {
			g.Emit(ir.Jump{Target: bodyBlock.ID})
		}

		g.currentBlock = bodyBlock
		if node.Expression {
			value := g.GenerateExpr(arm.Body)
			if !g.currentBlockHasTerminator() {
				if fallsThrough {
					g.Emit(ir.Store{Slot: resultSlot, Value: value})
				}
				g.Emit(ir.Jump{Target: mergeBlock.ID})
			}
		} else {
			g.GenerateNode(arm.Body)
			if !g.currentBlockHasTerminator() {
				g.Emit(ir.Jump{Target: mergeBlock.ID})
			}
		}
		g.currentEnv = armEnv.Parent
		g.currentBlock = nextBlock
	}

	g.Emit(ir.Unreachable{})
	g.currentBlock = mergeBlock
	if node.Expression {
		if !fallsThrough {
			g.Emit(ir.Unreachable{})
			return ir.ZeroConstOperand(node.GetType())
		}
		resultID := g.currentFunction.NewValueOfType(node.GetType())
		g.Emit(ir.Load{Dest: resultID, Slot: resultSlot})
		return ir.ValueOperand(resultID, node.GetType())
	}
	if !parser.NodeFallsThrough(node) {
		g.Emit(ir.Unreachable{})
	}
	return ir.Operand{}
}

func (g *Generator) generateMatchPatternCondition(pattern *parser.MatchPatternNode, subjectSlot ir.SlotID, subjectType types.Type) ir.Operand {
	switch pattern.Kind {
	case parser.MatchPatternWildcard:
		return ir.BoolConstOperand(true)
	case parser.MatchPatternVariant:
		if info, tagged := types.TaggedUnion(subjectType); tagged {
			tag := g.loadTaggedUnionTag(subjectSlot, subjectType, info.Tag)
			return g.emitBinaryOperation(parser.BinaryOpEqual, tag, ir.IntConstOperand(pattern.TagValue, info.Tag), types.PrimitiveBool)
		}
		subject := g.loadMatchSubject(subjectSlot, subjectType)
		return g.emitBinaryOperation(parser.BinaryOpEqual, subject, ir.IntConstOperand(pattern.TagValue, subjectType), types.PrimitiveBool)
	case parser.MatchPatternLiteral:
		subject := g.loadMatchSubject(subjectSlot, subjectType)
		return g.emitBinaryOperation(parser.BinaryOpEqual, subject, g.GenerateExpr(pattern.Literal), types.PrimitiveBool)
	case parser.MatchPatternRange:
		subject := g.loadMatchSubject(subjectSlot, subjectType)
		start := g.GenerateExpr(pattern.Start)
		end := g.GenerateExpr(pattern.End)
		atLeast := g.emitBinaryOperation(parser.BinaryOpGreaterEqual, subject, start, types.PrimitiveBool)
		below := g.emitBinaryOperation(parser.BinaryOpLess, subject, end, types.PrimitiveBool)
		return g.emitBinaryOperation(parser.BinaryOpBitwiseAnd, atLeast, below, types.PrimitiveBool)
	case parser.MatchPatternAlternative:
		result := ir.BoolConstOperand(false)
		for _, alternative := range pattern.Alternatives {
			condition := g.generateMatchPatternCondition(alternative, subjectSlot, subjectType)
			result = g.emitBinaryOperation(parser.BinaryOpBitwiseOr, result, condition, types.PrimitiveBool)
		}
		return result
	default:
		panic("unsupported match pattern")
	}
}

func (g *Generator) loadMatchSubject(slot ir.SlotID, subjectType types.Type) ir.Operand {
	valueID := g.currentFunction.NewValueOfType(subjectType)
	g.Emit(ir.Load{Dest: valueID, Slot: slot})
	return ir.ValueOperand(valueID, subjectType)
}

func (g *Generator) loadTaggedUnionTag(slot ir.SlotID, subjectType, tagType types.Type) ir.Operand {
	baseID := g.currentFunction.NewValueOfType(types.PointerType{Base: subjectType})
	g.Emit(ir.AddressOf{Dest: baseID, Slot: slot})
	tagPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: tagType})
	g.Emit(ir.FieldAddress{
		Dest: tagPtrID, Base: ir.ValueOperand(baseID, types.PointerType{Base: subjectType}), Field: "$tag",
	})
	tagID := g.currentFunction.NewValueOfType(tagType)
	g.Emit(ir.LoadPtr{Dest: tagID, Ptr: ir.ValueOperand(tagPtrID, types.PointerType{Base: tagType})})
	return ir.ValueOperand(tagID, tagType)
}

func (g *Generator) bindMatchPattern(pattern *parser.MatchPatternNode, subjectSlot ir.SlotID, subjectType types.Type) {
	if pattern.Kind != parser.MatchPatternVariant || len(pattern.Bindings) == 0 {
		return
	}
	info, tagged := types.TaggedUnion(subjectType)
	if !tagged {
		return
	}
	variant, _, ok := info.Variant(pattern.Variant)
	if !ok || len(variant.Fields) == 0 {
		return
	}
	storage := types.Underlying(subjectType).(types.StructType)
	payloadUnion := storage.Fields[1].R
	baseID := g.currentFunction.NewValueOfType(types.PointerType{Base: subjectType})
	g.Emit(ir.AddressOf{Dest: baseID, Slot: subjectSlot})
	payloadPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: payloadUnion})
	g.Emit(ir.FieldAddress{
		Dest: payloadPtrID,
		Base: ir.ValueOperand(baseID, types.PointerType{Base: subjectType}), Field: "$payload",
	})
	variantType := types.StructType{Fields: variant.Fields}
	variantPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: variantType})
	g.Emit(ir.FieldAddress{
		Dest: variantPtrID,
		Base: ir.ValueOperand(payloadPtrID, types.PointerType{Base: payloadUnion}), Field: variant.Name,
	})
	variantPtr := ir.ValueOperand(variantPtrID, types.PointerType{Base: variantType})
	for _, binding := range pattern.Bindings {
		if binding.Symbol == nil {
			continue
		}
		fieldType := binding.Symbol.Type
		fieldPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: fieldType})
		g.Emit(ir.FieldAddress{Dest: fieldPtrID, Base: variantPtr, Field: binding.Field})
		valueID := g.currentFunction.NewValueOfType(fieldType)
		g.Emit(ir.LoadPtr{Dest: valueID, Ptr: ir.ValueOperand(fieldPtrID, types.PointerType{Base: fieldType})})
		slot := g.currentFunction.NewSlot(fieldType, binding.Name)
		g.currentEnv.Variables[binding.Symbol] = slot
		g.Emit(ir.Alloca{Slot: slot})
		g.Emit(ir.Store{Slot: slot, Value: ir.ValueOperand(valueID, fieldType)})
	}
}

func (g *Generator) generateSliceLiteralExpr(node *parser.SliceLiteralNode) ir.Operand {
	switch literalType := types.Underlying(node.GetType()).(type) {
	case types.ArrayType:
		return g.generateArrayLiteral(node, literalType)
	case types.SliceType:
		return g.generateViewLiteral(node, literalType)
	default:
		panic("sequence literal must have array or slice type")
	}
}

func (g *Generator) generateViewLiteral(node *parser.SliceLiteralNode, sliceType types.SliceType) ir.Operand {
	targetType := node.GetType()
	tmpSlot := g.currentFunction.NewSlot(targetType, "")
	g.Emit(ir.Alloca{Slot: tmpSlot})
	if node.RepeatValue != nil {
		return g.generateRepeatedSliceLiteral(node, targetType, sliceType, tmpSlot)
	}

	slicePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: targetType})
	g.Emit(ir.AddressOf{Dest: slicePtrID, Slot: tmpSlot})
	slicePtr := ir.ValueOperand(slicePtrID, types.PointerType{Base: targetType})

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

	loaded := g.currentFunction.NewValueOfType(targetType)
	g.Emit(ir.Load{Dest: loaded, Slot: tmpSlot})
	return ir.ValueOperand(loaded, targetType)
}

func (g *Generator) generateArrayLiteral(node *parser.SliceLiteralNode, arrayType types.ArrayType) ir.Operand {
	arraySlot := g.currentFunction.NewSlot(node.GetType(), "")
	g.Emit(ir.Alloca{Slot: arraySlot})
	arrayPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: node.GetType(), Mutable: true})
	g.Emit(ir.AddressOf{Dest: arrayPtrID, Slot: arraySlot})
	arrayPtr := ir.ValueOperand(arrayPtrID, types.PointerType{Base: node.GetType(), Mutable: true})

	storeElement := func(index ir.Operand, value ir.Operand) {
		elementPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: arrayType.Base, Mutable: true})
		g.Emit(ir.ElementAddress{Dest: elementPtrID, Base: arrayPtr, Index: index, Element: arrayType.Base, ArrayObject: true})
		g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(elementPtrID, types.PointerType{Base: arrayType.Base, Mutable: true}), Value: value})
	}

	if node.RepeatValue == nil {
		for i, element := range node.Elements {
			storeElement(ir.IntConstOperand(strconv.Itoa(i), types.PrimitiveUsz), g.GenerateExpr(element))
		}
	} else if _, noInit := node.RepeatValue.(*parser.NoInitializerNode); !noInit {
		value := g.GenerateExpr(node.RepeatValue)
		for i := 0; i < arrayType.Length; i++ {
			storeElement(ir.IntConstOperand(strconv.Itoa(i), types.PrimitiveUsz), value)
		}
	}

	loaded := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.Load{Dest: loaded, Slot: arraySlot})
	return ir.ValueOperand(loaded, node.GetType())
}

func (g *Generator) generateRepeatedSliceLiteral(node *parser.SliceLiteralNode, targetType types.Type, sliceType types.SliceType, tmpSlot ir.SlotID) ir.Operand {
	count := g.GenerateExpr(node.RepeatAmount)

	bufferID := g.currentFunction.NewValueOfType(types.PointerType{Base: sliceType.Base})
	g.Emit(ir.AllocaArray{Dest: bufferID, Element: sliceType.Base, Count: count})
	buffer := ir.ValueOperand(bufferID, types.PointerType{Base: sliceType.Base})

	if _, noInit := node.RepeatValue.(*parser.NoInitializerNode); !noInit {
		value := g.GenerateExpr(node.RepeatValue)
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
	}
	slicePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: targetType})
	g.Emit(ir.AddressOf{Dest: slicePtrID, Slot: tmpSlot})
	slicePtr := ir.ValueOperand(slicePtrID, types.PointerType{Base: targetType})
	basePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PointerType{Base: sliceType.Base}})
	g.Emit(ir.FieldAddress{Dest: basePtrID, Base: slicePtr, Field: "0"})
	g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(basePtrID, types.PointerType{Base: types.PointerType{Base: sliceType.Base}}), Value: buffer})
	lenPtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: types.PrimitiveUsz})
	g.Emit(ir.FieldAddress{Dest: lenPtrID, Base: slicePtr, Field: "1"})
	g.Emit(ir.StorePtr{Ptr: ir.ValueOperand(lenPtrID, types.PointerType{Base: types.PrimitiveUsz}), Value: count})

	loaded := g.currentFunction.NewValueOfType(targetType)
	g.Emit(ir.Load{Dest: loaded, Slot: tmpSlot})
	return ir.ValueOperand(loaded, targetType)
}

func (g *Generator) generateStructLiteralIntoSlot(slot ir.SlotID, node *parser.StructLiteralNode) {
	if _, ok := types.Underlying(node.GetType()).(types.FlagsType); ok {
		g.Emit(ir.Store{Slot: slot, Value: g.generateStructLiteralExpr(node)})
		return
	}

	if !node.NoInitRemaining || g.initializingGlobal {
		g.Emit(ir.Store{Slot: slot, Value: ir.ZeroConstOperand(node.GetType())})
	}

	basePtrID := g.currentFunction.NewValueOfType(types.PointerType{Base: node.GetType()})
	g.Emit(ir.AddressOf{Dest: basePtrID, Slot: slot})
	basePtr := ir.ValueOperand(basePtrID, types.PointerType{Base: node.GetType()})

	for _, field := range node.Fields {
		if _, noInit := field.R.(*parser.NoInitializerNode); noInit {
			continue
		}
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
	if node.ResolvedIdentifier != nil {
		return g.generateIdentifierExpr(node.ResolvedIdentifier)
	}
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
	if node.IsFlagValue {
		return ir.IntConstOperand(node.FlagValue, node.FlagType)
	}
	if node.IsFlagTest {
		value := g.GenerateExpr(node.Subject)
		mask := ir.IntConstOperand(node.FlagValue, node.FlagType)
		masked := g.emitBinaryOperation(parser.BinaryOpBitwiseAnd, value, mask, node.FlagType)
		dst := g.currentFunction.NewValueOfType(types.PrimitiveBool)
		g.Emit(ir.CmpEq{Dest: dst, Left: masked, Right: mask})
		return ir.ValueOperand(dst, types.PrimitiveBool)
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

func (g *Generator) generateIfExpr(node *parser.IfNode) ir.Operand {
	resultType := node.GetType()
	fallsThrough := parser.NodeFallsThrough(node)
	var tmpSlot ir.SlotID
	if fallsThrough {
		tmpSlot = g.currentFunction.NewSlot(resultType, "ifexpr.tmp")
		g.Emit(ir.Alloca{Slot: tmpSlot})
	}

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
	if !g.currentBlockHasTerminator() {
		g.Emit(ir.Store{Slot: tmpSlot, Value: thenVal})
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
			if !g.currentBlockHasTerminator() {
				g.Emit(ir.Store{Slot: tmpSlot, Value: v})
				g.Emit(ir.Jump{Target: mergeBlock.ID})
			}

			g.currentBlock = next
		}

		if node.ElseBranch != nil {
			v := g.GenerateExpr(node.ElseBranch)
			if !g.currentBlockHasTerminator() {
				g.Emit(ir.Store{Slot: tmpSlot, Value: v})
				g.Emit(ir.Jump{Target: mergeBlock.ID})
			}
		}
	}

	g.currentBlock = mergeBlock
	if !fallsThrough {
		g.Emit(ir.Unreachable{})
		return ir.ZeroConstOperand(resultType)
	}
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
		if node.TaggedUnionType != nil {
			break
		}
		if node.ResolvedIdentifier != nil {
			return g.generateAddressOfExpr(node.ResolvedIdentifier)
		}
		if node.IsEnumValue || node.IsFlagValue || node.IsFlagTest || node.MethodSymbol != nil || node.ModulePath != "" {
			break
		}
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
	runtimeBuiltin := node.Symbol != nil && node.Symbol.DefinitionModule == "" &&
		(node.Symbol.Name == "panic" || node.Symbol.Name == "assert")
	var callee *ir.Operand
	if node.Symbol == nil {
		value := g.GenerateExpr(node.Callee)
		callee = &value
	}
	args := make([]ir.Operand, 0, len(node.Args))
	callTypedVariadicArity := len(node.Args) - node.TypedVariadicStart
	typedVariadicWrapper := node.TypedVariadic && !node.VariadicExpansion && node.Symbol != nil &&
		hasTypedVariadicSpecialization(node.Symbol.Signature, callTypedVariadicArity)
	constants := map[int]symbols.SpecializationConstant(nil)
	constantWrapper := false
	if node.Symbol != nil && !runtimeBuiltin {
		constants = selectedSpecializationConstants(node.Symbol.Signature)
		constantWrapper = len(constants) != 0 && g.callMatchesSpecialization(node, constants)
		if !node.Symbol.Signature.TypedVariadic && len(node.Args) != len(node.Symbol.Signature.Parameters) {
			constantWrapper = false
		}
	}
	typedVariadicWrapper = typedVariadicWrapper && (len(constants) == 0 || constantWrapper)
	flattenedTypedVariadic := false
	typedVariadicArity := len(node.Args) - node.TypedVariadicStart
	if node.TypedVariadic && node.VariadicExpansion && node.Symbol != nil &&
		len(node.Args) == node.TypedVariadicStart+1 {
		if argument, ok := node.Args[node.TypedVariadicStart].(*parser.IdentifierNode); ok {
			if elements, specialized := g.specializedVariadicElements[argument.Symbol]; specialized &&
				hasTypedVariadicSpecialization(node.Symbol.Signature, len(elements)) &&
				(len(constants) == 0 || constantWrapper) {
				typedVariadicWrapper = true
				flattenedTypedVariadic = true
				typedVariadicArity = len(elements)
				for i, arg := range node.Args[:node.TypedVariadicStart] {
					if _, specialized := constants[i]; !specialized {
						args = append(args, g.GenerateExpr(arg))
					}
				}
				args = append(args, elements...)
			}
		}
	}
	if typedVariadicWrapper && !flattenedTypedVariadic {
		for i, arg := range node.Args {
			if i < node.TypedVariadicStart {
				if _, specialized := constants[i]; specialized {
					continue
				}
			}
			args = append(args, g.GenerateExpr(arg))
		}
	} else if !typedVariadicWrapper && node.TypedVariadic {
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
		for i, arg := range node.Args {
			if constantWrapper {
				if _, specialized := constants[i]; specialized {
					continue
				}
			}
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
		if runtimeBuiltin {
			name = "__qk_" + node.Symbol.Name
		}

		foreignAttr := node.Symbol.Attributes.Get(attributes.AttributeTypeForeign)
		if foreign, ok := foreignAttr.(attributes.FunctionAttributeForeign); ok &&
			node.Name != nil && node.Name.Module != "" {
			g.addExternForCall(node.Symbol.Name, callSig, foreign.From, false)
		}

		if export, ok := node.Symbol.Attributes.Get(attributes.AttributeTypeExport).(attributes.FunctionAttributeExport); ok {
			name = export.As
		} else if foreignAttr == nil && !runtimeBuiltin {
			callModule := g.ModuleName
			if node.Symbol.DefinitionModule != "" {
				callModule = node.Symbol.DefinitionModule
			}
			if node.Name != nil && node.Name.Module != "" {
				callModule = node.Name.Module
				if node.Name.ResolvedModuleName != "" {
					callModule = node.Name.ResolvedModuleName
				}
			}
			name = g.mangleFunctionName(callModule, node.Symbol.Name)
			if callModule != g.ModuleName {
				hidden := node.Symbol.Attributes.Get(attributes.AttributeTypeExport) == nil
				g.addExternForCall(name, callSig, "", hidden)
			}
		}

		if !node.Symbol.Signature.TypedVariadic && len(node.Args) < len(node.Symbol.Signature.Parameters) {
			name = g.defaultWrapperName(name, len(node.Args))
			callSig.ParamTypes = append([]types.Type(nil), node.Symbol.Signature.Parameters[:len(node.Args)]...)
			callSig.Variadic = false
			callSig.Attributes = nil
			if node.Symbol.DefinitionModule != "" && node.Symbol.DefinitionModule != g.ModuleName ||
				node.Name != nil && node.Name.Module != "" {
				hidden := node.Symbol.Attributes.Get(attributes.AttributeTypeExport) == nil
				g.addExternForCall(name, callSig, "", hidden)
			}
		}
		if typedVariadicWrapper {
			name = g.typedVariadicWrapperName(name, typedVariadicArity)
			fixedTypes := make([]types.Type, 0, node.TypedVariadicStart)
			for i, parameter := range callSig.ParamTypes[:node.TypedVariadicStart] {
				if _, specialized := constants[i]; !specialized {
					fixedTypes = append(fixedTypes, parameter)
				}
			}
			callSig.ParamTypes = fixedTypes
			for range typedVariadicArity {
				callSig.ParamTypes = append(callSig.ParamTypes, node.Symbol.Signature.VariadicElement)
			}
			name = g.specializedFunctionName(name, constants)
			callSig.Variadic = false
			callSig.Attributes = nil
			if node.Symbol.DefinitionModule != "" && node.Symbol.DefinitionModule != g.ModuleName ||
				node.Name != nil && node.Name.Module != "" {
				hidden := node.Symbol.Attributes.Get(attributes.AttributeTypeExport) == nil
				g.addExternForCall(name, callSig, "", hidden)
			}
		} else if constantWrapper {
			name = g.specializedFunctionName(name, constants)
			paramTypes := make([]types.Type, 0, len(callSig.ParamTypes)-len(constants))
			for i, parameter := range callSig.ParamTypes {
				if _, specialized := constants[i]; !specialized {
					paramTypes = append(paramTypes, parameter)
				}
			}
			callSig.ParamTypes = paramTypes
			callSig.Variadic = false
			callSig.Attributes = nil
			if node.Symbol.DefinitionModule != "" && node.Symbol.DefinitionModule != g.ModuleName ||
				node.Name != nil && node.Name.Module != "" {
				hidden := node.Symbol.Attributes.Get(attributes.AttributeTypeExport) == nil
				g.addExternForCall(name, callSig, "", hidden)
			}
		}
	}
	if runtimeBuiltin {
		g.addExternForCall("__qk_"+node.Symbol.Name, callSig, "", true)
	}
	var genericReference *ir.GenericReference
	var requirementReference *ir.TraitRequirementReference
	genericSymbol := node.Symbol
	if genericSymbol == nil {
		if member, ok := node.Callee.(*parser.FieldAccessNode); ok {
			genericSymbol = member.MethodSymbol
		}
	}
	if genericSymbol != nil && genericSymbol.TemplateSymbol != nil {
		template := genericSymbol.TemplateSymbol
		genericReference = &ir.GenericReference{
			Module: template.DefinitionModule, Name: template.Name,
			TypeArguments: append([]types.Type(nil), genericSymbol.TypeArguments...),
		}
	} else if genericSymbol != nil && genericSymbol.Template {
		genericReference = &ir.GenericReference{Module: genericSymbol.DefinitionModule, Name: genericSymbol.Name}
		for _, parameter := range genericSymbol.GenericParameters {
			genericReference.TypeArguments = append(genericReference.TypeArguments, parameter)
		}
	}
	if genericSymbol != nil && genericSymbol.TraitRequirement && len(genericSymbol.Signature.Parameters) != 0 {
		requirementReference = &ir.TraitRequirementReference{
			Name: genericSymbol.Name, ReceiverType: genericSymbol.Signature.Parameters[0],
		}
	}

	if node.GetType().Equals(types.PrimitiveVoid) {
		g.Emit(ir.Call{
			Name:        name,
			Callee:      callee,
			Args:        args,
			Signature:   callSig,
			Generic:     genericReference,
			Requirement: requirementReference,
		})
		if node.Symbol != nil && node.Symbol.Name == "panic" {
			g.Emit(ir.Unreachable{})
		}
		return ir.NullConstOperand(types.PrimitiveVoid)
	}

	dst := g.currentFunction.NewValueOfType(node.GetType())
	g.Emit(ir.Call{Dest: dst, Name: name, Callee: callee, Args: args, Signature: callSig, Generic: genericReference, Requirement: requirementReference})
	return ir.ValueOperand(dst, node.GetType())
}

func (g *Generator) callMatchesSpecialization(node *parser.FunctionCallNode, constants map[int]symbols.SpecializationConstant) bool {
	for index, expected := range constants {
		if index >= len(node.Args) {
			return false
		}
		actual, ok := specializationConstantExpr(node.Args[index])
		if !ok {
			identifier, identifierOK := node.Args[index].(*parser.IdentifierNode)
			if !identifierOK || identifier.Symbol == nil {
				return false
			}
			actual, ok = g.specializedConstants[identifier.Symbol]
		}
		if !ok || actual.Key() != expected.Key() {
			return false
		}
	}
	return true
}

func specializationConstantExpr(expr parser.ExpressionNode) (symbols.SpecializationConstant, bool) {
	switch node := expr.(type) {
	case *parser.StringLiteralNode:
		return symbols.SpecializationConstant{Kind: "str", Value: node.Value, Type: node.GetType()}, true
	case *parser.CStringLiteralNode:
		return symbols.SpecializationConstant{Kind: "cstr", Value: node.Value, Type: node.GetType()}, true
	case *parser.BoolLiteralNode:
		return symbols.SpecializationConstant{Kind: "bool", Value: node.Value, Type: node.GetType()}, true
	case *parser.IntegerLiteralNode:
		return symbols.SpecializationConstant{Kind: "int", Value: node.Value, Type: node.GetType()}, true
	case *parser.FloatLiteralNode:
		return symbols.SpecializationConstant{Kind: "float", Value: node.Value, Type: node.GetType()}, true
	case *parser.CharLiteralNode:
		return symbols.SpecializationConstant{Kind: "char", Value: strconv.Itoa(int(node.Value)), Type: node.GetType()}, true
	default:
		return symbols.SpecializationConstant{}, false
	}
}

func (g *Generator) generateTraitCall(node *parser.FunctionCallNode) ir.Operand {
	receiver := g.GenerateExpr(node.Args[0])
	traitPtr := node.Symbol.Signature.Parameters[0].(types.TraitPointerType)
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
	return MangleFunctionName(moduleName, fnName)
}

func MangleFunctionName(moduleName, fnName string) string {
	mod := encodeModuleName(moduleName)
	fn := sanitizeName(fnName)
	if mod == "" {
		return "__qk_" + fn
	}
	return "__qk_" + mod + "_" + fn
}

func (g *Generator) mangleGlobalName(moduleName, globalName string) string {
	mod := encodeModuleName(moduleName)
	global := sanitizeName(globalName)
	if mod == "" {
		return "__qk_global_" + global
	}
	return "__qk_" + mod + "_global_" + global
}

func encodeModuleName(name string) string {
	if name == "" {
		return ""
	}
	if !strings.Contains(name, ".") {
		return sanitizeName(name)
	}
	var b strings.Builder
	b.WriteString("0_")
	for _, component := range strings.Split(name, ".") {
		fmt.Fprintf(&b, "%d_%s", len(component), component)
	}
	return b.String()
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
	if node.Symbol.InlineComptime {
		if types.HasUntyped(node.GetType()) {
			panic("untyped compile-time integer reached IR generation")
		}
		if types.IsFloat(node.GetType()) {
			return ir.FloatConstOperand(node.Symbol.ComptimeInteger, node.GetType())
		}
		return ir.IntConstOperand(node.Symbol.ComptimeInteger, node.GetType())
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

func (g *Generator) generateSpecializationConstant(constant symbols.SpecializationConstant) ir.Operand {
	switch constant.Kind {
	case "str":
		return g.generateStringLiteralExpr(&parser.StringLiteralNode{Value: constant.Value, Type: constant.Type})
	case "cstr":
		return g.generateCStringLiteralExpr(&parser.CStringLiteralNode{Value: constant.Value, Type: constant.Type})
	case "bool":
		return ir.BoolConstOperand(constant.Value == string(tokeniser.KeywordTrue))
	case "float":
		return ir.FloatConstOperand(constant.Value, constant.Type)
	case "int", "char":
		if types.IsFloat(constant.Type) {
			return ir.FloatConstOperand(constant.Value, constant.Type)
		}
		return ir.IntConstOperand(constant.Value, constant.Type)
	default:
		panic("unsupported specialization constant")
	}
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
		name = g.mangleFunctionName(module, sym.Name)
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
	if node.Op == parser.BinaryOpEqual || node.Op == parser.BinaryOpNotEqual {
		if isNilTraitComparison(node.Operand1, node.Operand2) {
			return g.generateTraitNilComparison(node, node.Operand2)
		}
		if isNilTraitComparison(node.Operand2, node.Operand1) {
			return g.generateTraitNilComparison(node, node.Operand1)
		}
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

func isNilTraitComparison(nilNode, traitNode parser.ExpressionNode) bool {
	if _, nilLiteral := nilNode.(*parser.NilLiteralNode); !nilLiteral {
		return false
	}
	_, traitPointer := types.Underlying(traitNode.GetType()).(types.TraitPointerType)
	return traitPointer
}

func (g *Generator) generateTraitNilComparison(node *parser.BinaryOpNode, traitNode parser.ExpressionNode) ir.Operand {
	traitType := types.Underlying(traitNode.GetType()).(types.TraitPointerType)
	traitValue := g.GenerateExpr(traitNode)
	dataType := types.PointerType{Base: types.PrimitiveVoid, Mutable: traitType.Mutable}
	dataID := g.currentFunction.NewValueOfType(dataType)
	g.Emit(ir.ExtractValue{Dest: dataID, Aggregate: traitValue, Index: 0})
	return g.emitBinaryOperation(
		node.Op,
		ir.ValueOperand(dataID, dataType),
		ir.NullConstOperand(dataType),
		node.GetType(),
	)
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
		if types.IsInteger(resultType) {
			g.emitIntegerZeroCheck(right, "integer division by zero")
		}
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
		if types.IsInteger(resultType) {
			g.emitIntegerZeroCheck(right, "integer modulo by zero")
		}
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

func (g *Generator) emitIntegerZeroCheck(divisor ir.Operand, message string) {
	isZero := g.currentFunction.NewValueOfType(types.PrimitiveBool)
	g.Emit(ir.CmpEq{Dest: isZero, Left: divisor, Right: ir.IntConstOperand("0", divisor.Type)})
	panicBlock := g.currentFunction.NewBlock("integer.zero.panic")
	validBlock := g.currentFunction.NewBlock("integer.zero.valid")
	g.Emit(ir.Branch{Cond: ir.ValueOperand(isZero, types.PrimitiveBool), Then: panicBlock.ID, Else: validBlock.ID})
	g.currentBlock = panicBlock
	g.emitRuntimePanic(message)
	g.currentBlock = validBlock
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
		if array, ok := types.Underlying(node.Operand.GetType()).(types.ArrayType); ok {
			return ir.IntConstOperand(strconv.Itoa(array.Length), node.GetType())
		}
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
