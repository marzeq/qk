package loader

import (
	"testing"

	"github.com/marzeq/qk/codegen/irgen"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/types"
)

func TestExtractParameterizedSpecializationsRenamesFunctionConstantCallee(t *testing.T) {
	parameter := types.TypeParameter{Owner: "example.make", Name: "T", Index: 0}
	baseName := irgen.MangleFunctionName("example", "make")
	signature := ir.FunctionSignature{ReturnType: types.PrimitiveVoid}
	templateFunction := ir.NewFunction(baseName, ir.LinkageInternal, nil)
	templateFunction.Signature = signature
	templateFunction.Entry = templateFunction.NewBlock("entry").ID
	templateFunction.Blocks[0].Instr = append(templateFunction.Blocks[0].Instr, ir.Return{})
	template := ir.ParameterizedTemplate{
		Module: "example", Name: "make", Parameters: []types.TypeParameter{parameter},
		Functions: []*ir.Function{templateFunction},
	}

	caller := ir.NewFunction("caller", ir.LinkageInternal, nil)
	caller.Signature = signature
	caller.Entry = caller.NewBlock("entry").ID
	callee := ir.FunctionConstOperand(baseName, types.PointerType{Base: types.FunctionType{ReturnType: types.PrimitiveVoid}})
	caller.Blocks[0].Instr = append(caller.Blocks[0].Instr,
		ir.Call{Callee: &callee, Signature: signature, Parameterized: &ir.ParameterizedReference{
			Module: "example", Name: "make", TypeArguments: []types.Type{types.PrimitiveU8},
		}},
		ir.Return{},
	)

	specializations, err := ExtractParameterizedSpecializations(
		map[string]*ir.Module{"main": {Functions: []*ir.Function{caller}}},
		map[string][]ir.ParameterizedTemplate{"example": {template}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	call := caller.Blocks[0].Instr[0].(ir.Call)
	if call.Parameterized == nil {
		t.Fatal("parameterized demand metadata was not preserved")
	}
	if call.Callee == nil || call.Callee.FunctionName == baseName {
		t.Fatalf("function-constant callee was not specialized: %#v", call.Callee)
	}
	if len(specializations.Functions) != 1 || specializations.Functions[0].Name != call.Callee.FunctionName {
		t.Fatalf("specialization target %q does not match generated function", call.Callee.FunctionName)
	}
}
