package llvm

import (
	"fmt"
	"strings"

	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

type Emitter struct {
	Variables  map[ir.SlotID]*symbols.Symbol
	SlotTypes  map[ir.SlotID]types.Type
	ModuleName string
	currentFn  *ir.Function
}

func (e *Emitter) EmitModule(out *strings.Builder, m *ir.Module) {

	if e.ModuleName != "" {
		fmt.Fprintf(out, "; module %s\n\n", e.ModuleName)
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

func (e *Emitter) EmitFunction(out *strings.Builder, fn *ir.Function) {
	e.currentFn = fn
	returnType := fn.Signature.ReturnType
	if e.isLLVMMainFunction(fn) {
		returnType = types.PrimitiveI32
	}

	fmt.Fprintf(out, "define %s @%s(", e.TypeEmit(returnType), fn.Name)
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
	out.WriteString(") {\n")

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

	label := block.Name
	if label == "" {
		label = fmt.Sprintf("b%d", block.ID)
	}
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
		panic("unreachable")
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
		return op.FloatValue
	case ir.OperandBoolConst:
		if op.BoolValue {
			return "1"
		}
		return "0"
	case ir.OperandNullConst:
		return "null"
	default:
		panic("unreachable")
	}
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
	return fmt.Sprintf("%%b%d", id)
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
	case ir.Alloca:
		e.AllocaEmit(out, instr)
	case ir.Load:
		e.LoadEmit(out, instr)
	case ir.Store:
		e.StoreEmit(out, instr)
	case ir.AddressOf:
		e.AddressOfEmit(out, instr)
	case ir.FieldAddress:
		e.FieldAddressEmit(out, instr)
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
	default:
		panic("unreachable")
	}
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

func (e *Emitter) AllocaEmit(out *strings.Builder, a ir.Alloca) {
	fmt.Fprintf(out, "%s = alloca %s", e.SlotIDEmit(a.Slot), e.TypeEmit(e.slotType(a.Slot)))
}

func (e *Emitter) LoadEmit(out *strings.Builder, l ir.Load) {
	fmt.Fprintf(out, "%s = load %s, ptr %s", e.ValueIDEmit(l.Dest), e.TypeEmit(e.slotType(l.Slot)), e.SlotIDEmit(l.Slot))
}

func (e *Emitter) StoreEmit(out *strings.Builder, s ir.Store) {
	fmt.Fprintf(out, "store %s %s, ptr %s", e.TypeEmit(e.slotType(s.Slot)), e.OperandEmit(s.Value), e.SlotIDEmit(s.Slot))
}

func (e *Emitter) AddressOfEmit(out *strings.Builder, s ir.AddressOf) {
	fmt.Fprintf(out, "%s = getelementptr inbounds %s, ptr %s, i32 0", e.ValueIDEmit(s.Dest), e.TypeEmit(e.slotType(s.Slot)), e.SlotIDEmit(s.Slot))
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

func (e *Emitter) CallEmit(out *strings.Builder, c ir.Call) {
	if c.Signature.ReturnType.Equals(types.PrimitiveVoid) {
		out.WriteString("call void @")
		out.WriteString(c.Name)
		out.WriteString("(")
	} else {
		fmt.Fprintf(out, "%s = call %s @%s(", e.ValueIDEmit(c.Dest), e.TypeEmit(c.Signature.ReturnType), c.Name)
	}
	for i, arg := range c.Args {
		if i > 0 {
			out.WriteString(", ")
		}
		fmt.Fprintf(out, "%s %s", e.TypeEmit(arg.Type), e.OperandEmit(arg))
	}
	out.WriteString(")")
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
	if fn == nil {
		return false
	}
	return e.ModuleName == "main" && fn.Name == "main"
}

func (e *Emitter) CastEmit(out *strings.Builder, c ir.Cast) {
	from := c.From.Type
	to := c.To
	fromPrim, fromOK := from.(types.PrimitiveType)
	toPrim, toOK := to.(types.PrimitiveType)

	if from.Equals(to) {
		fmt.Fprintf(out, "%s = bitcast %s %s to %s", e.ValueIDEmit(c.Dest), e.TypeEmit(from), e.OperandEmit(c.From), e.TypeEmit(to))
		return
	}

	if fromOK && toOK {
		if types.IsInteger(fromPrim) && types.IsInteger(toPrim) {
			srcBits := types.IntegerRank(fromPrim)
			dstBits := types.IntegerRank(toPrim)
			op := "trunc"
			if srcBits < dstBits {
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
			op := "fptrunc"
			if types.FloatRank(fromPrim) < types.FloatRank(toPrim) {
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

	panic("unsupported cast")
}
