package llvm

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

type Emitter struct {
	Variables    map[ir.SlotID]*symbols.Symbol
	SlotTypes    map[ir.SlotID]types.Type
	MainModule   string
	ModuleName   string
	TargetTriple string
	Executable   bool
	currentFn    *ir.Function
	externMap    map[string]string // qk name -> actual symbol name for @foreign functions
	stringMap    map[string]string // literal value -> global name
	stringDefs   []string
	abiTemp      int
}

func (e *Emitter) EmitModule(out *strings.Builder, m *ir.Module) {

	if e.ModuleName != "" {
		fmt.Fprintf(out, "; module %s\n\n", e.ModuleName)
	}

	e.externMap = make(map[string]string)
	e.stringMap = make(map[string]string)
	e.stringDefs = e.collectStringDefs(m)
	e.abiTemp = 0
	for _, def := range e.stringDefs {
		out.WriteString(def)
		out.WriteString("\n")
	}
	for _, global := range m.Globals {
		e.GlobalEmit(out, global)
		out.WriteString("\n")
	}
	for _, global := range m.ExternGlobals {
		e.ExternGlobalEmit(out, global)
		out.WriteString("\n")
	}
	if len(e.stringDefs) > 0 || len(m.Globals) > 0 || len(m.ExternGlobals) > 0 {
		out.WriteString("\n")
	}
	for i, ex := range m.Externs {
		if i > 0 {
			out.WriteString("\n")
		}
		llvmName := ex.Name
		if ex.From != "" {
			llvmName = ex.From
			fmt.Fprintf(out, "; extern from %s\n", ex.From)
		}
		e.externMap[ex.Name] = llvmName

		fmt.Fprintf(out, "declare %s @%s(", e.foreignABIReturnType(ex.Signature.ReturnType), llvmName)
		for j, p := range ex.Signature.ParamTypes {
			for k, abiType := range e.foreignABIParamTypes(p) {
				if j > 0 || k > 0 {
					out.WriteString(", ")
				}
				out.WriteString(abiType)
			}
		}
		if ex.Signature.Variadic {
			if len(ex.Signature.ParamTypes) > 0 {
				out.WriteString(", ")
			}
			out.WriteString("...")
		}
		out.WriteString(")")
		for _, attr := range ex.Signature.Attributes {
			at := attr.GetType()
			switch at {
			case attributes.AttributeTypeNoReturn:
				out.WriteString(" noreturn")
			case attributes.AttributeTypeForeign:
			default:
				panic(fmt.Sprintf("unsupported attribute type for extern function: %s", at))
			}
		}
		out.WriteString("\n")
	}

	for i, fn := range m.Functions {
		if i > 0 {
			out.WriteString("\n")
		}
		e.EmitFunction(out, fn)
		if !strings.HasSuffix(out.String(), "\n") {
			out.WriteString("\n")
		}
	}
}

func (e *Emitter) ExternGlobalEmit(out *strings.Builder, global ir.ExternGlobal) {
	kind := "constant"
	if global.Mutable {
		kind = "global"
	}
	fmt.Fprintf(out, "@%s = external %s %s", global.Name, kind, e.TypeEmit(global.Type))
}

func (e *Emitter) GlobalEmit(out *strings.Builder, global ir.Global) {
	kind := "constant"
	if global.Mutable {
		kind = "global"
	}
	linkage := "internal "
	if global.Public {
		linkage = ""
	}
	fmt.Fprintf(out, "@%s = %s%s %s %s", global.Name, linkage, kind, e.TypeEmit(global.Type), e.OperandEmit(global.Value))
}

func (e *Emitter) EmitFunction(out *strings.Builder, fn *ir.Function) {
	e.currentFn = fn
	returnType := fn.Signature.ReturnType
	if e.isLLVMMainFunction(fn) {
		returnType = types.PrimitiveI32
	}
	linkage := "internal"
	if fn.Extern || e.isLLVMMainFunction(fn) {
		linkage = "external"
	}

	fmt.Fprintf(out, "define %s %s @%s(", linkage, e.TypeEmit(returnType), fn.Name)
	paramTypes := fn.Signature.ParamTypes
	if len(paramTypes) == 0 && len(fn.Parameters) > 0 {
		paramTypes = make([]types.Type, len(fn.Parameters))
		for i, param := range fn.Parameters {
			paramTypes[i] = param.Type
		}
	}
	for i, paramType := range paramTypes {
		if i > 0 {
			out.WriteString(", ")
		}
		paramName := fmt.Sprintf("arg%d", i)
		if i < len(fn.Parameters) && fn.Parameters[i].Name != "" {
			paramName = fn.Parameters[i].Name
		}
		fmt.Fprintf(out, "%s %%%s", e.TypeEmit(paramType), paramName)
	}
	out.WriteString(") ")

	for _, attr := range fn.Attributes {
		at := attr.GetType()
		switch at {
		case attributes.AttributeTypeNoInline:
			out.WriteString("noinline ")
		case attributes.AttributeTypeInline:
			out.WriteString("alwaysinline ")
		case attributes.AttributeTypeNoReturn:
			out.WriteString("noreturn ")
		case attributes.AttributeTypeExport:
		default:
			panic(fmt.Sprintf("unsupported attribute type for function: %s", at))
		}
	}

	out.WriteString("{\n")

	for _, slot := range fn.Slots {
		fmt.Fprintf(out, "  ; slot %s %s %s\n", e.SlotIDEmit(slot.ID), e.TypeEmit(slot.Type), slot.Name)
	}
	if len(fn.Slots) > 0 {
		out.WriteString("\n")
	}

	for _, block := range fn.Blocks {
		e.EmitBlock(out, block)
	}

	out.WriteString("}\n")
}

func (e *Emitter) EmitBlock(out *strings.Builder, block *ir.Block) {
	label := e.blockLabel(block.ID, block.Name)
	fmt.Fprintf(out, "%s:\n", label)

	for _, instr := range block.Instr {
		e.InstrEmit(out, instr)
		out.WriteString("\n")
	}
}

func (e *Emitter) slotType(slot ir.SlotID) types.Type {
	fn := e.currentFn
	if fn == nil {
		panic("no current function set")
	}
	if ty := e.SlotTypes[slot]; ty != nil {
		return ty
	}
	for _, declared := range fn.Slots {
		if declared.ID == slot {
			return declared.Type
		}
	}
	if symbol, ok := e.Variables[slot]; ok && symbol != nil {
		return symbol.Type
	}
	panic("unknown slot type")
}

func (e *Emitter) pointerBaseType(ty types.Type) types.Type {
	if ptr, ok := ty.(types.PointerType); ok {
		return ptr.Base
	}
	panic("expected pointer type")
}

func (e *Emitter) structFieldIndex(ty types.Type, field string) int {
	if _, ok := ty.(types.SliceType); ok {
		switch field {
		case "0":
			return 0
		case "1":
			return 1
		default:
			panic("slice field index not found")
		}
	}

	st, ok := ty.(types.StructType)
	if !ok {
		panic("expected struct type")
	}
	for i, entry := range st.Fields {
		if entry.L == field {
			return i
		}
	}
	panic("field not found")
}

func (e *Emitter) TypeEmit(ty types.Type) string {
	switch ty := ty.(type) {
	case types.PrimitiveType:
		switch ty {
		case types.PrimitiveBool:
			return "i1"
		case types.PrimitiveVoid:
			return "void"
		case types.PrimitiveIsz:
			return "i64"
		case types.PrimitiveUsz:
			return "i64"
		case types.PrimitiveF32:
			return "float"
		case types.PrimitiveF64:
			return "double"
		case types.PrimitiveChar:
			return "i8"
		case types.PrimitiveI8:
			return "i8"
		case types.PrimitiveI16:
			return "i16"
		case types.PrimitiveI32:
			return "i32"
		case types.PrimitiveI64:
			return "i64"
		case types.PrimitiveU8:
			return "i8"
		case types.PrimitiveU16:
			return "i16"
		case types.PrimitiveU32:
			return "i32"
		case types.PrimitiveU64:
			return "i64"
		default:
			return ty.String()
		}
	case types.PointerType:
		return "ptr"
	case types.StructType:
		var sb strings.Builder
		sb.WriteString("{ ")
		for i, field := range ty.Fields {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(e.TypeEmit(field.R))
		}
		sb.WriteString(" }")
		return sb.String()
	case types.SliceType:
		return "{ ptr, i64 }"
	case types.FunctionType:
		var sb strings.Builder
		sb.WriteString(e.TypeEmit(ty.ReturnType))
		sb.WriteString(" (")
		for i, param := range ty.Parameters {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(e.TypeEmit(param))
		}
		sb.WriteString(")")
		return sb.String()
	default:
		panic(fmt.Sprintf("unsupported type: %T", ty))
	}
}

func (e *Emitter) OperandEmit(op ir.Operand) string {
	switch op.Kind {
	case ir.OperandValue:
		if name, ok := e.parameterNameForValue(op.Value); ok {
			return "%" + name
		}
		return e.ValueIDEmit(op.Value)
	case ir.OperandIntConst:
		return op.IntValue
	case ir.OperandFloatConst:
		return llvmFloatLiteral(op.FloatValue, op.Type)
	case ir.OperandBoolConst:
		if op.BoolValue {
			return "1"
		}
		return "0"
	case ir.OperandNullConst:
		return "null"
	case ir.OperandStructConst:
		var fields strings.Builder
		fields.WriteString("{ ")
		for i, field := range op.Fields {
			if i > 0 {
				fields.WriteString(", ")
			}
			fmt.Fprintf(&fields, "%s %s", e.TypeEmit(field.Type), e.OperandEmit(field))
		}
		fields.WriteString(" }")
		return fields.String()
	default:
		panic("unreachable")
	}
}

func llvmFloatLiteral(value string, ty types.Type) string {
	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		panic(fmt.Sprintf("invalid float literal %q: %v", value, err))
	}

	if ty.Equals(types.PrimitiveF32) {
		floatValue = float64(float32(floatValue))
	}

	return fmt.Sprintf("0x%016X", math.Float64bits(floatValue))
}

func (e *Emitter) parameterNameForValue(id ir.ValueID) (string, bool) {
	if e.currentFn == nil {
		return "", false
	}
	index := int(id) - 1
	if index < 0 || index >= len(e.currentFn.Parameters) {
		return "", false
	}

	param := e.currentFn.Parameters[index]
	if param.Name != "" {
		return param.Name, true
	}
	return fmt.Sprintf("arg%d", index), true
}

func (e *Emitter) ValueIDEmit(id ir.ValueID) string {
	return fmt.Sprintf("%%v%d", id)
}

func (e *Emitter) SlotIDEmit(id ir.SlotID) string {
	return fmt.Sprintf("%%s%d", id)
}

func (e *Emitter) BlockIDEmit(id ir.BlockID) string {
	return "%" + e.blockLabel(id, "")
}

func (e *Emitter) blockLabel(id ir.BlockID, fallbackName string) string {
	if e.currentFn != nil {
		for _, block := range e.currentFn.Blocks {
			if block.ID != id {
				continue
			}
			if block.Name != "" {
				return fmt.Sprintf("%s.%d", block.Name, id)
			}
			break
		}
	}

	if fallbackName != "" {
		return fmt.Sprintf("%s.%d", fallbackName, id)
	}

	return fmt.Sprintf("b%d", id)
}

func (e *Emitter) InstrEmit(out *strings.Builder, instr ir.Instr) {
	switch instr := instr.(type) {
	case ir.Add:
		e.AddEmit(out, instr)
	case ir.Sub:
		e.SubEmit(out, instr)
	case ir.Mul:
		e.MulEmit(out, instr)
	case ir.Div:
		e.DivEmit(out, instr)
	case ir.CmpEq:
		e.CmpEqEmit(out, instr)
	case ir.CmpNe:
		e.CmpNeEmit(out, instr)
	case ir.CmpLt:
		e.CmpLtEmit(out, instr)
	case ir.CmpGt:
		e.CmpGtEmit(out, instr)
	case ir.CmpLe:
		e.CmpLeEmit(out, instr)
	case ir.CmpGe:
		e.CmpGeEmit(out, instr)
	case ir.Mod:
		e.ModEmit(out, instr)
	case ir.LogicalOr:
		e.LogicalOrEmit(out, instr)
	case ir.LogicalAnd:
		e.LogicalAndEmit(out, instr)
	case ir.Alloca:
		e.AllocaEmit(out, instr)
	case ir.AllocaArray:
		e.AllocaArrayEmit(out, instr)
	case ir.Load:
		e.LoadEmit(out, instr)
	case ir.LoadGlobal:
		e.LoadGlobalEmit(out, instr)
	case ir.Store:
		e.StoreEmit(out, instr)
	case ir.StoreGlobal:
		e.StoreGlobalEmit(out, instr)
	case ir.AddressOf:
		e.AddressOfEmit(out, instr)
	case ir.AddressOfGlobal:
		e.AddressOfGlobalEmit(out, instr)
	case ir.FieldAddress:
		e.FieldAddressEmit(out, instr)
	case ir.ElementAddress:
		e.ElementAddressEmit(out, instr)
	case ir.LoadPtr:
		e.LoadPtrEmit(out, instr)
	case ir.StorePtr:
		e.StorePtrEmit(out, instr)
	case ir.Call:
		e.CallEmit(out, instr)
	case ir.Jump:
		e.JumpEmit(out, instr)
	case ir.Branch:
		e.BranchEmit(out, instr)
	case ir.Return:
		e.ReturnEmit(out, instr)
	case ir.Cast:
		e.CastEmit(out, instr)
	case ir.Sizeof:
		e.SizeofEmit(out, instr)
	case ir.StringConst:
		e.StringConstEmit(out, instr)
	default:
		panic("unreachable")
	}
}

func (e *Emitter) collectStringDefs(m *ir.Module) []string {
	defs := []string{}
	for _, fn := range m.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instr {
				s, ok := instr.(ir.StringConst)
				if !ok {
					continue
				}
				if _, exists := e.stringMap[s.Value]; exists {
					continue
				}

				global := fmt.Sprintf("@.str.%d", len(e.stringMap))
				e.stringMap[s.Value] = global

				encoded := encodeLLVMString(s.Value)
				length := len(s.Value) + 1
				defs = append(defs, fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\", align 1", global, length, encoded))
			}
		}
	}
	return defs
}

func encodeLLVMString(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		fmt.Fprintf(&b, "\\%02X", value[i])
	}
	b.WriteString("\\00")
	return b.String()
}

func (e *Emitter) StringConstEmit(out *strings.Builder, s ir.StringConst) {
	global, ok := e.stringMap[s.Value]
	if !ok {
		panic("missing string constant definition")
	}
	length := len(s.Value) + 1
	fmt.Fprintf(out, "%s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0", e.ValueIDEmit(s.Dest), length, global)
}

func (e *Emitter) AddEmit(out *strings.Builder, a ir.Add) {
	instr := "add"
	if types.IsFloat(a.Left.Type) {
		instr = "fadd"
	}
	fmt.Fprintf(out, "%s = %s %s %s, %s", e.ValueIDEmit(a.Dest), instr, e.TypeEmit(a.Left.Type), e.OperandEmit(a.Left), e.OperandEmit(a.Right))
}

func (e *Emitter) SubEmit(out *strings.Builder, s ir.Sub) {
	instr := "sub"
	if types.IsFloat(s.Left.Type) {
		instr = "fsub"
	}
	fmt.Fprintf(out, "%s = %s %s %s, %s", e.ValueIDEmit(s.Dest), instr, e.TypeEmit(s.Left.Type), e.OperandEmit(s.Left), e.OperandEmit(s.Right))
}

func (e *Emitter) MulEmit(out *strings.Builder, m ir.Mul) {
	instr := "mul"
	if types.IsFloat(m.Left.Type) {
		instr = "fmul"
	}
	fmt.Fprintf(out, "%s = %s %s %s, %s", e.ValueIDEmit(m.Dest), instr, e.TypeEmit(m.Left.Type), e.OperandEmit(m.Left), e.OperandEmit(m.Right))
}

func (e *Emitter) DivEmit(out *strings.Builder, d ir.Div) {
	instr := "sdiv"
	if types.IsFloat(d.Left.Type) {
		instr = "fdiv"
	} else if types.IsUnsigned(d.Left.Type) {
		instr = "udiv"
	}
	fmt.Fprintf(out, "%s = %s %s %s, %s", e.ValueIDEmit(d.Dest), instr, e.TypeEmit(d.Left.Type), e.OperandEmit(d.Left), e.OperandEmit(d.Right))
}

func (e *Emitter) ModEmit(out *strings.Builder, m ir.Mod) {
	instr := "srem"
	if types.IsFloat(m.Left.Type) {
		instr = "frem"
	} else if types.IsUnsigned(m.Left.Type) {
		instr = "urem"
	}
	fmt.Fprintf(out, "%s = %s %s %s, %s", e.ValueIDEmit(m.Dest), instr, e.TypeEmit(m.Left.Type), e.OperandEmit(m.Left), e.OperandEmit(m.Right))
}

func (e *Emitter) CmpEqEmit(out *strings.Builder, c ir.CmpEq) {
	instr := "icmp eq"
	if types.IsFloat(c.Left.Type) {
		instr = "fcmp oeq"
	}
	fmt.Fprintf(out, "%s = %s %s %s, %s", e.ValueIDEmit(c.Dest), instr, e.TypeEmit(c.Left.Type), e.OperandEmit(c.Left), e.OperandEmit(c.Right))
}

func (e *Emitter) CmpNeEmit(out *strings.Builder, c ir.CmpNe) {
	instr := "icmp ne"
	if types.IsFloat(c.Left.Type) {
		instr = "fcmp one"
	}
	fmt.Fprintf(out, "%s = %s %s %s, %s", e.ValueIDEmit(c.Dest), instr, e.TypeEmit(c.Left.Type), e.OperandEmit(c.Left), e.OperandEmit(c.Right))
}

func (e *Emitter) CmpLtEmit(out *strings.Builder, c ir.CmpLt) {
	instr := "icmp slt"
	if types.IsFloat(c.Left.Type) {
		instr = "fcmp olt"
	} else if types.IsUnsigned(c.Left.Type) {
		instr = "icmp ult"
	}
	fmt.Fprintf(out, "%s = %s %s %s, %s", e.ValueIDEmit(c.Dest), instr, e.TypeEmit(c.Left.Type), e.OperandEmit(c.Left), e.OperandEmit(c.Right))
}

func (e *Emitter) CmpGtEmit(out *strings.Builder, c ir.CmpGt) {
	instr := "icmp sgt"
	if types.IsFloat(c.Left.Type) {
		instr = "fcmp ogt"
	} else if types.IsUnsigned(c.Left.Type) {
		instr = "icmp ugt"
	}
	fmt.Fprintf(out, "%s = %s %s %s, %s", e.ValueIDEmit(c.Dest), instr, e.TypeEmit(c.Left.Type), e.OperandEmit(c.Left), e.OperandEmit(c.Right))
}

func (e *Emitter) CmpLeEmit(out *strings.Builder, c ir.CmpLe) {
	instr := "icmp sle"
	if types.IsFloat(c.Left.Type) {
		instr = "fcmp ole"
	} else if types.IsUnsigned(c.Left.Type) {
		instr = "icmp ule"
	}
	fmt.Fprintf(out, "%s = %s %s %s, %s", e.ValueIDEmit(c.Dest), instr, e.TypeEmit(c.Left.Type), e.OperandEmit(c.Left), e.OperandEmit(c.Right))
}

func (e *Emitter) CmpGeEmit(out *strings.Builder, c ir.CmpGe) {
	instr := "icmp sge"
	if types.IsFloat(c.Left.Type) {
		instr = "fcmp oge"
	} else if types.IsUnsigned(c.Left.Type) {
		instr = "icmp uge"
	}
	fmt.Fprintf(out, "%s = %s %s %s, %s", e.ValueIDEmit(c.Dest), instr, e.TypeEmit(c.Left.Type), e.OperandEmit(c.Left), e.OperandEmit(c.Right))
}

func (e *Emitter) LogicalOrEmit(out *strings.Builder, l ir.LogicalOr) {
	fmt.Fprintf(out, "%s = or i1 %s, %s", e.ValueIDEmit(l.Dest), e.OperandEmit(l.Left), e.OperandEmit(l.Right))
}

func (e *Emitter) LogicalAndEmit(out *strings.Builder, l ir.LogicalAnd) {
	fmt.Fprintf(out, "%s = and i1 %s, %s", e.ValueIDEmit(l.Dest), e.OperandEmit(l.Left), e.OperandEmit(l.Right))
}

func (e *Emitter) AllocaEmit(out *strings.Builder, a ir.Alloca) {
	fmt.Fprintf(out, "%s = alloca %s", e.SlotIDEmit(a.Slot), e.TypeEmit(e.slotType(a.Slot)))
}

func (e *Emitter) AllocaArrayEmit(out *strings.Builder, a ir.AllocaArray) {
	fmt.Fprintf(out, "%s = alloca %s, %s %s", e.ValueIDEmit(a.Dest), e.TypeEmit(a.Element), e.TypeEmit(a.Count.Type), e.OperandEmit(a.Count))
}

func (e *Emitter) LoadEmit(out *strings.Builder, l ir.Load) {
	fmt.Fprintf(out, "%s = load %s, ptr %s", e.ValueIDEmit(l.Dest), e.TypeEmit(e.slotType(l.Slot)), e.SlotIDEmit(l.Slot))
}

func (e *Emitter) LoadGlobalEmit(out *strings.Builder, l ir.LoadGlobal) {
	fmt.Fprintf(out, "%s = load %s, ptr @%s", e.ValueIDEmit(l.Dest), e.TypeEmit(l.Type), l.Name)
}

func (e *Emitter) StoreEmit(out *strings.Builder, s ir.Store) {
	fmt.Fprintf(out, "store %s %s, ptr %s", e.TypeEmit(e.slotType(s.Slot)), e.OperandEmit(s.Value), e.SlotIDEmit(s.Slot))
}

func (e *Emitter) StoreGlobalEmit(out *strings.Builder, s ir.StoreGlobal) {
	fmt.Fprintf(out, "store %s %s, ptr @%s", e.TypeEmit(s.Value.Type), e.OperandEmit(s.Value), s.Name)
}

func (e *Emitter) AddressOfEmit(out *strings.Builder, s ir.AddressOf) {
	fmt.Fprintf(out, "%s = getelementptr inbounds %s, ptr %s, i32 0", e.ValueIDEmit(s.Dest), e.TypeEmit(e.slotType(s.Slot)), e.SlotIDEmit(s.Slot))
}

func (e *Emitter) AddressOfGlobalEmit(out *strings.Builder, s ir.AddressOfGlobal) {
	fmt.Fprintf(out, "%s = getelementptr inbounds %s, ptr @%s, i32 0", e.ValueIDEmit(s.Dest), e.TypeEmit(s.Type), s.Name)
}

func (e *Emitter) LoadPtrEmit(out *strings.Builder, l ir.LoadPtr) {
	fmt.Fprintf(out, "%s = load %s, ptr %s", e.ValueIDEmit(l.Dest), e.TypeEmit(e.pointerBaseType(l.Ptr.Type)), e.OperandEmit(l.Ptr))
}

func (e *Emitter) StorePtrEmit(out *strings.Builder, s ir.StorePtr) {
	fmt.Fprintf(out, "store %s %s, ptr %s", e.TypeEmit(s.Value.Type), e.OperandEmit(s.Value), e.OperandEmit(s.Ptr))
}

func (e *Emitter) FieldAddressEmit(out *strings.Builder, f ir.FieldAddress) {
	baseTy := f.Base.Type
	if ptr, ok := baseTy.(types.PointerType); ok {
		baseTy = ptr.Base
	}
	fieldIndex := e.structFieldIndex(baseTy, f.Field)
	fmt.Fprintf(out, "%s = getelementptr inbounds %s, ptr %s, i32 0, i32 %d", e.ValueIDEmit(f.Dest), e.TypeEmit(baseTy), e.OperandEmit(f.Base), fieldIndex)
}

func (e *Emitter) ElementAddressEmit(out *strings.Builder, eaddr ir.ElementAddress) {
	fmt.Fprintf(
		out,
		"%s = getelementptr inbounds %s, ptr %s, %s %s",
		e.ValueIDEmit(eaddr.Dest),
		e.TypeEmit(eaddr.Element),
		e.OperandEmit(eaddr.Base),
		e.TypeEmit(eaddr.Index.Type),
		e.OperandEmit(eaddr.Index),
	)
}

func (e *Emitter) CallEmit(out *strings.Builder, c ir.Call) {
	fnName := c.Name
	if e.externMap != nil {
		if mapped, ok := e.externMap[c.Name]; ok && mapped != "" {
			fnName = mapped
		}
	}

	foreign := c.Signature.Attributes.Get(attributes.AttributeTypeForeign) != nil
	callType := e.TypeEmit(c.Signature.ReturnType)
	if foreign {
		callType = e.foreignABIReturnType(c.Signature.ReturnType)
	}
	if c.Signature.Variadic {
		var params strings.Builder
		for i, param := range c.Signature.ParamTypes {
			for j, abiType := range e.callABIParamTypes(param, foreign) {
				if i > 0 || j > 0 {
					params.WriteString(", ")
				}
				params.WriteString(abiType)
			}
		}
		if len(c.Signature.ParamTypes) > 0 {
			params.WriteString(", ")
		}
		params.WriteString("...")
		callType += " (" + params.String() + ")"
	}

	returnChunks := []abiChunk(nil)
	if foreign {
		returnChunks = e.foreignABIChunks(c.Signature.ReturnType)
	}
	var argPrelude strings.Builder
	loweredArgs := make([][]string, len(c.Args))
	for i, arg := range c.Args {
		loweredArgs[i] = e.lowerCallArgument(&argPrelude, arg, foreign)
	}
	if argPrelude.Len() > 0 {
		out.WriteString(argPrelude.String())
	}
	callResult := ""
	if !c.Signature.ReturnType.Equals(types.PrimitiveVoid) && len(returnChunks) > 0 {
		callResult = e.nextABITemp()
	}
	if c.Signature.ReturnType.Equals(types.PrimitiveVoid) {
		out.WriteString("call ")
		out.WriteString(callType)
		out.WriteString(" @")
		out.WriteString(fnName)
		out.WriteString("(")
	} else if callResult != "" {
		fmt.Fprintf(out, "%s = call %s @%s(", callResult, callType, fnName)
	} else {
		fmt.Fprintf(out, "%s = call %s @%s(", e.ValueIDEmit(c.Dest), callType, fnName)
	}
	for i := range c.Args {
		for j, lowered := range loweredArgs[i] {
			if i > 0 || j > 0 {
				out.WriteString(", ")
			}
			out.WriteString(lowered)
		}
	}
	out.WriteString(")")
	if callResult != "" {
		e.unpackForeignReturn(out, c, callResult, returnChunks)
	}

	noreturnAttr := c.Signature.Attributes.Get(attributes.AttributeTypeNoReturn)
	if noreturnAttr != nil {
		out.WriteString("\nunreachable")
	}
}

func (e *Emitter) callABIParamTypes(ty types.Type, foreign bool) []string {
	if !foreign {
		return []string{e.TypeEmit(ty)}
	}
	return e.foreignABIParamTypes(ty)
}

func (e *Emitter) nextABITemp() string {
	e.abiTemp++
	return fmt.Sprintf("%%abi%d", e.abiTemp)
}

func (e *Emitter) lowerCallArgument(out *strings.Builder, arg ir.Operand, foreign bool) []string {
	if !foreign {
		return []string{fmt.Sprintf("%s %s", e.TypeEmit(arg.Type), e.OperandEmit(arg))}
	}
	chunks := e.foreignABIChunks(arg.Type)
	if len(chunks) == 0 {
		return []string{fmt.Sprintf("%s %s", e.TypeEmit(arg.Type), e.OperandEmit(arg))}
	}
	slot := e.nextABITemp()
	allocationType := e.TypeEmit(arg.Type)
	zeroInitialize := false
	if st, ok := arg.Type.(types.StructType); ok && len(chunks) == 1 && chunks[0].offset == 0 && chunks[0].typeName == "i64" {
		size, _ := e.typeSizeAlign(st)
		if size < 8 {
			allocationType = "i64"
			zeroInitialize = true
		}
	}
	fmt.Fprintf(out, "%s = alloca %s\n  ", slot, allocationType)
	if zeroInitialize {
		fmt.Fprintf(out, "store i64 0, ptr %s\n  ", slot)
	}
	fmt.Fprintf(out, "store %s %s, ptr %s\n  ", e.TypeEmit(arg.Type), e.OperandEmit(arg), slot)
	if len(chunks) == 1 && chunks[0].offset == -1 {
		return []string{fmt.Sprintf("ptr %s", slot)}
	}
	result := make([]string, len(chunks))
	for i, chunk := range chunks {
		ptr := slot
		if chunk.offset != 0 {
			ptr = e.nextABITemp()
			fmt.Fprintf(out, "%s = getelementptr i8, ptr %s, i64 %d\n  ", ptr, slot, chunk.offset)
		}
		value := e.nextABITemp()
		fmt.Fprintf(out, "%s = load %s, ptr %s, align 1\n  ", value, chunk.typeName, ptr)
		result[i] = fmt.Sprintf("%s%s %s", chunk.typeName, chunk.attributes, value)
	}
	return result
}

func (e *Emitter) unpackForeignReturn(out *strings.Builder, c ir.Call, callResult string, chunks []abiChunk) {
	slot := e.nextABITemp()
	fmt.Fprintf(out, "\n  %s = alloca %s", slot, e.TypeEmit(c.Signature.ReturnType))
	for i, chunk := range chunks {
		value := callResult
		if len(chunks) > 1 {
			value = e.nextABITemp()
			fmt.Fprintf(out, "\n  %s = extractvalue %s %s, %d", value, e.foreignABIReturnType(c.Signature.ReturnType), callResult, i)
		}
		ptr := slot
		if chunk.offset != 0 {
			ptr = e.nextABITemp()
			fmt.Fprintf(out, "\n  %s = getelementptr i8, ptr %s, i64 %d", ptr, slot, chunk.offset)
		}
		fmt.Fprintf(out, "\n  store %s %s, ptr %s, align 1", chunk.typeName, value, ptr)
	}
	fmt.Fprintf(out, "\n  %s = load %s, ptr %s", e.ValueIDEmit(c.Dest), e.TypeEmit(c.Signature.ReturnType), slot)
}

func (e *Emitter) JumpEmit(out *strings.Builder, j ir.Jump) {
	fmt.Fprintf(out, "br label %s", e.BlockIDEmit(j.Target))
}

func (e *Emitter) BranchEmit(out *strings.Builder, b ir.Branch) {
	fmt.Fprintf(out, "br i1 %s, label %s, label %s", e.OperandEmit(b.Cond), e.BlockIDEmit(b.Then), e.BlockIDEmit(b.Else))
}

func (e *Emitter) ReturnEmit(out *strings.Builder, r ir.Return) {
	if e.isLLVMMainFunction(e.currentFn) {
		out.WriteString("ret i32 0")
		return
	}

	if r.HasValue {
		fmt.Fprintf(out, "ret %s %s", e.TypeEmit(r.Value.Type), e.OperandEmit(r.Value))
		return
	}
	out.WriteString("ret void")
}

func (e *Emitter) isLLVMMainFunction(fn *ir.Function) bool {
	if !e.Executable {
		return false
	}
	if fn == nil {
		return false
	}
	return e.ModuleName == e.MainModule && fn.Name == "main"
}

func (e *Emitter) CastEmit(out *strings.Builder, c ir.Cast) {
	from := c.From.Type
	to := c.To
	fromPrim, fromOK := from.(types.PrimitiveType)
	toPrim, toOK := to.(types.PrimitiveType)

	if fromSlice, ok := from.(types.SliceType); ok {
		if toPtr, ok := to.(types.PointerType); ok {
			if toPtr.Base.Equals(types.PrimitiveVoid) || fromSlice.Base.Equals(toPtr.Base) {
				fmt.Fprintf(out, "%s = extractvalue %s %s, 0", e.ValueIDEmit(c.Dest), e.TypeEmit(from), e.OperandEmit(c.From))
				return
			}
		}
	}

	if from.Equals(to) {
		fmt.Fprintf(out, "%s = bitcast %s %s to %s", e.ValueIDEmit(c.Dest), e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
		return
	}

	if fromOK && toOK {
		if types.IsInteger(fromPrim) && types.IsInteger(toPrim) {
			srcBits := types.IntegerRank(fromPrim)
			dstBits := types.IntegerRank(toPrim)
			op := "trunc"
			if srcBits == dstBits {
				op = "bitcast"
			} else if srcBits < dstBits {
				if types.IsUnsigned(fromPrim) {
					op = "zext"
				} else {
					op = "sext"
				}
			}
			fmt.Fprintf(out, "%s = %s %s %s to %s", e.ValueIDEmit(c.Dest), op, e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
			return
		}

		if types.IsFloat(fromPrim) && types.IsFloat(toPrim) {
			srcBits := types.FloatRank(fromPrim)
			dstBits := types.FloatRank(toPrim)
			op := "fptrunc"
			if srcBits == dstBits {
				op = "bitcast"
			} else if srcBits < dstBits {
				op = "fpext"
			}
			fmt.Fprintf(out, "%s = %s %s %s to %s", e.ValueIDEmit(c.Dest), op, e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
			return
		}

		if types.IsInteger(fromPrim) && types.IsFloat(toPrim) {
			op := "sitofp"
			if types.IsUnsigned(fromPrim) {
				op = "uitofp"
			}
			fmt.Fprintf(out, "%s = %s %s %s to %s", e.ValueIDEmit(c.Dest), op, e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
			return
		}

		if types.IsFloat(fromPrim) && types.IsInteger(toPrim) {
			op := "fptosi"
			if types.IsUnsigned(toPrim) {
				op = "fptoui"
			}
			fmt.Fprintf(out, "%s = %s %s %s to %s", e.ValueIDEmit(c.Dest), op, e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
			return
		}

		if fromPrim.Equals(types.PrimitiveBool) && types.IsInteger(toPrim) {
			fmt.Fprintf(out, "%s = zext %s %s to %s", e.ValueIDEmit(c.Dest), e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
			return
		}

		if types.IsInteger(fromPrim) && toPrim.Equals(types.PrimitiveBool) {
			fmt.Fprintf(out, "%s = trunc %s %s to %s", e.ValueIDEmit(c.Dest), e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
			return
		}
	}

	if types.IsPointer(from) && types.IsPointer(to) {
		fmt.Fprintf(out, "%s = bitcast %s %s to %s", e.ValueIDEmit(c.Dest), e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
		return
	}

	if types.IsPointer(from) && toOK && types.IsInteger(toPrim) {
		fmt.Fprintf(out, "%s = ptrtoint %s %s to %s", e.ValueIDEmit(c.Dest), e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
		return
	}

	if fromOK && types.IsInteger(fromPrim) && types.IsPointer(to) {
		fmt.Fprintf(out, "%s = inttoptr %s %s to %s", e.ValueIDEmit(c.Dest), e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
		return
	}

	if fromPrim.Equals(types.PrimitiveChar) && types.IsInteger(toPrim) {
		fmt.Fprintf(out, "%s = zext %s %s to %s", e.ValueIDEmit(c.Dest), e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
		return
	}

	if types.IsInteger(fromPrim) && toPrim.Equals(types.PrimitiveChar) {
		fmt.Fprintf(out, "%s = trunc %s %s to %s", e.ValueIDEmit(c.Dest), e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
		return
	}

	panic("unsupported cast")
}

func (e *Emitter) SizeofEmit(out *strings.Builder, s ir.Sizeof) {
	fmt.Fprintf(
		out,
		"%s = ptrtoint ptr getelementptr (%s, ptr null, i32 1) to %s",
		e.ValueIDEmit(s.Dest),
		e.TypeEmit(s.Type),
		e.TypeEmit(types.PrimitiveUsz),
	)
}
