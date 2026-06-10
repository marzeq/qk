package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/types"
)

func dumpIRModules(mods map[string]*ir.Module) {
	names := make([]string, 0, len(mods))
	for name := range mods {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		mod := mods[name]
		fmt.Printf("module %s\n", name)

		for _, fn := range mod.Functions {
			fmt.Printf("  fn %s%s -> %s (entry=b%d)\n", fn.Name, formatSignatureParams(fn.Signature.ParamTypes), formatType(fn.Signature.ReturnType), fn.Entry)

			for _, param := range fn.Parameters {
				typeName := "<nil>"
				if param.Type != nil {
					typeName = param.Type.String()
				}
				fmt.Printf("    param %d %s %s -> s%d\n", param.Index, typeName, param.Name, param.Slot)
			}

			for _, slot := range fn.Slots {
				typeName := "<nil>"
				if slot.Type != nil {
					typeName = slot.Type.String()
				}
				fmt.Printf("    slot s%d %s %s\n", slot.ID, typeName, slot.Name)
			}

			for _, block := range fn.Blocks {
				fmt.Printf("    b%d (%s):\n", block.ID, block.Name)
				for _, inst := range block.Instr {
					fmt.Printf("      %s\n", formatInstr(inst))
				}
			}
		}
	}
}

func formatInstr(inst ir.Instr) string {
	switch i := inst.(type) {
	case ir.Add:
		return fmt.Sprintf("v%d = add %s, %s", i.Dest, formatOperand(i.Left), formatOperand(i.Right))
	case ir.Sub:
		return fmt.Sprintf("v%d = sub %s, %s", i.Dest, formatOperand(i.Left), formatOperand(i.Right))
	case ir.Mul:
		return fmt.Sprintf("v%d = mul %s, %s", i.Dest, formatOperand(i.Left), formatOperand(i.Right))
	case ir.Div:
		return fmt.Sprintf("v%d = div %s, %s", i.Dest, formatOperand(i.Left), formatOperand(i.Right))
	case ir.CmpEq:
		return fmt.Sprintf("v%d = cmpeq %s, %s", i.Dest, formatOperand(i.Left), formatOperand(i.Right))
	case ir.CmpNe:
		return fmt.Sprintf("v%d = cmpne %s, %s", i.Dest, formatOperand(i.Left), formatOperand(i.Right))
	case ir.CmpLt:
		return fmt.Sprintf("v%d = cmplt %s, %s", i.Dest, formatOperand(i.Left), formatOperand(i.Right))
	case ir.CmpLe:
		return fmt.Sprintf("v%d = cmple %s, %s", i.Dest, formatOperand(i.Left), formatOperand(i.Right))
	case ir.CmpGt:
		return fmt.Sprintf("v%d = cmpgt %s, %s", i.Dest, formatOperand(i.Left), formatOperand(i.Right))
	case ir.CmpGe:
		return fmt.Sprintf("v%d = cmpge %s, %s", i.Dest, formatOperand(i.Left), formatOperand(i.Right))
	case ir.Alloca:
		return fmt.Sprintf("alloca s%d", i.Slot)
	case ir.Load:
		return fmt.Sprintf("v%d = load s%d", i.Dest, i.Slot)
	case ir.Store:
		return fmt.Sprintf("store s%d, %s", i.Slot, formatOperand(i.Value))
	case ir.LoadPtr:
		return fmt.Sprintf("v%d = loadptr %s", i.Dest, formatOperand(i.Ptr))
	case ir.StorePtr:
		return fmt.Sprintf("storeptr %s, %s", formatOperand(i.Ptr), formatOperand(i.Value))
	case ir.AddressOf:
		return fmt.Sprintf("v%d = addrof s%d", i.Dest, i.Slot)
	case ir.FieldAddress:
		return fmt.Sprintf("v%d = fieldaddr %s, .%s", i.Dest, formatOperand(i.Base), i.Field)
	case ir.Call:
		args := strings.Builder{}
		for idx, arg := range i.Args {
			if idx > 0 {
				args.WriteString(", ")
			}
			args.WriteString(formatOperand(arg))
		}
		sig := formatSignatureParams(i.Signature.ParamTypes) + " -> " + formatType(i.Signature.ReturnType)
		if i.Dest == 0 {
			return fmt.Sprintf("call %s(%s) ; sig %s", i.Name, args.String(), sig)
		}
		return fmt.Sprintf("v%d = call %s(%s) ; sig %s", i.Dest, i.Name, args.String(), sig)
	case ir.Jump:
		return fmt.Sprintf("jmp b%d", i.Target)
	case ir.Branch:
		return fmt.Sprintf("br %s, b%d, b%d", formatOperand(i.Cond), i.Then, i.Else)
	case ir.Return:
		if !i.HasValue {
			return "ret void"
		}
		return fmt.Sprintf("ret %s", formatOperand(i.Value))
	case ir.Cast:
		to := "<nil>"
		if i.To != nil {
			to = i.To.String()
		}
		return fmt.Sprintf("v%d = cast %s to %s", i.Dest, formatOperand(i.From), to)
	case ir.StringConst:
		return fmt.Sprintf("v%d = strconst %q", i.Dest, i.Value)
	default:
		return fmt.Sprintf("<unknown %T>", i)
	}
}

func formatOperand(op ir.Operand) string {
	valueSuffix := ""
	if op.Type != nil {
		valueSuffix = ":" + op.Type.String()
	}

	switch op.Kind {
	case ir.OperandValue:
		return fmt.Sprintf("v%d%s", op.Value, valueSuffix)
	case ir.OperandIntConst:
		return op.IntValue + valueSuffix
	case ir.OperandFloatConst:
		return op.FloatValue + valueSuffix
	case ir.OperandBoolConst:
		if op.BoolValue {
			return "true" + valueSuffix
		}
		return "false" + valueSuffix
	case ir.OperandNullConst:
		return "null" + valueSuffix
	default:
		return "<unknown-op>"
	}
}

func formatType(t types.Type) string {
	if t == nil {
		return "void"
	}
	return t.String()
}

func formatSignatureParams(params []types.Type) string {
	if len(params) == 0 {
		return "()"
	}
	out := strings.Builder{}
	out.WriteString("(")
	for i, p := range params {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(formatType(p))
	}
	out.WriteString(")")
	return out.String()
}
