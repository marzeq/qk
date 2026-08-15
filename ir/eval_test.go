package ir

import (
	"math/big"
	"strings"
	"testing"

	"github.com/marzeq/qk/types"
)

func TestEvaluatorRunsRegularIRCallsAndBranches(t *testing.T) {
	double := NewFunction("double", LinkageInternal, nil)
	double.Signature = FunctionSignature{ParamTypes: []types.Type{types.PrimitiveI32}, ReturnType: types.PrimitiveI32}
	doubleEntry := double.NewBlock("entry")
	double.Entry = doubleEntry.ID
	argumentSlot := double.NewSlot(types.PrimitiveI32, "value")
	double.AddParameter("value", types.PrimitiveI32, argumentSlot)
	incoming := double.NewValueOfType(types.PrimitiveI32)
	loaded := double.NewValueOfType(types.PrimitiveI32)
	result := double.NewValueOfType(types.PrimitiveI32)
	doubleEntry.Instr = []Instr{
		Alloca{Slot: argumentSlot}, Store{Slot: argumentSlot, Value: ValueOperand(incoming, types.PrimitiveI32)},
		Load{Dest: loaded, Slot: argumentSlot},
		Mul{Dest: result, Left: ValueOperand(loaded, types.PrimitiveI32), Right: IntConstOperand("2", types.PrimitiveI32)},
		Return{HasValue: true, Value: ValueOperand(result, types.PrimitiveI32)},
	}

	module := &Module{Functions: []*Function{double}}
	value, err := NewEvaluator(module).Run("double", EvalInteger(big.NewInt(21), types.PrimitiveI32))
	if err != nil {
		t.Fatal(err)
	}
	if value.Integer == nil || value.Integer.Int64() != 42 {
		t.Fatalf("got %#v", value)
	}
}

func TestEvaluatorRejectsOnlyExecutedUnavailableInstruction(t *testing.T) {
	function := NewFunction("choose", LinkageInternal, nil)
	function.Signature = FunctionSignature{ParamTypes: []types.Type{types.PrimitiveBool}, ReturnType: types.PrimitiveI32}
	entry := function.NewBlock("entry")
	good := function.NewBlock("good")
	bad := function.NewBlock("bad")
	function.Entry = entry.ID
	slot := function.NewSlot(types.PrimitiveBool, "condition")
	function.AddParameter("condition", types.PrimitiveBool, slot)
	incoming := function.NewValueOfType(types.PrimitiveBool)
	loaded := function.NewValueOfType(types.PrimitiveBool)
	entry.Instr = []Instr{Alloca{Slot: slot}, Store{Slot: slot, Value: ValueOperand(incoming, types.PrimitiveBool)}, Load{Dest: loaded, Slot: slot}, Branch{Cond: ValueOperand(loaded, types.PrimitiveBool), Then: good.ID, Else: bad.ID}}
	good.Instr = []Instr{Return{HasValue: true, Value: IntConstOperand("7", types.PrimitiveI32)}}
	bad.Instr = []Instr{InlineAsm{}}
	evaluator := NewEvaluator(&Module{Functions: []*Function{function}})
	value, err := evaluator.Run("choose", EvalBool(true))
	if err != nil || value.Integer.Int64() != 7 {
		t.Fatalf("selected valid path: value=%#v err=%v", value, err)
	}
	_, err = evaluator.Run("choose", EvalBool(false))
	if err == nil || !strings.Contains(err.Error(), "InlineAsm") {
		t.Fatalf("expected executed unsupported instruction, got %v", err)
	}
}
