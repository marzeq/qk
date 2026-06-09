package ir

import "github.com/marzeq/qk/types"

type ValueID uint32
type SlotID uint32
type BlockID uint32

type OperandKind uint8

const (
	OperandValue OperandKind = iota
	OperandIntConst
	OperandFloatConst
	OperandBoolConst
	OperandNullConst
)

type Operand struct {
	Kind OperandKind
	Type types.Type

	Value ValueID

	IntValue   string
	FloatValue string
	BoolValue  bool
}

func ValueOperand(id ValueID, ty types.Type) Operand {
	return Operand{Kind: OperandValue, Value: id, Type: ty}
}

func IntConstOperand(value string, ty types.Type) Operand {
	return Operand{Kind: OperandIntConst, IntValue: value, Type: ty}
}

func FloatConstOperand(value string, ty types.Type) Operand {
	return Operand{Kind: OperandFloatConst, FloatValue: value, Type: ty}
}

func BoolConstOperand(value bool) Operand {
	return Operand{Kind: OperandBoolConst, BoolValue: value, Type: types.PrimitiveBool}
}

func NullConstOperand(ty types.Type) Operand {
	return Operand{Kind: OperandNullConst, Type: ty}
}

type Slot struct {
	ID   SlotID
	Type types.Type
	Name string
}

type Block struct {
	ID    BlockID
	Name  string
	Instr []Instr
}

type Parameter struct {
	Index int
	Name  string
	Type  types.Type
	Slot  SlotID
}

type FunctionSignature struct {
	ParamTypes []types.Type
	ReturnType types.Type
	Variadic   bool
}

type Function struct {
	Name       string
	Signature  FunctionSignature
	Parameters []Parameter
	Slots      []Slot
	Values     map[ValueID]types.Type

	Blocks []*Block
	Entry  BlockID

	nextValue ValueID
	nextSlot  SlotID
	nextBlock BlockID
}

func NewFunction(name string) *Function {
	return &Function{
		Name:   name,
		Values: make(map[ValueID]types.Type),
	}
}

func (f *Function) NewValue() ValueID {
	return f.NewValueOfType(nil)
}

func (f *Function) NewValueOfType(ty types.Type) ValueID {
	f.nextValue++
	id := f.nextValue
	f.Values[id] = ty
	return id
}

func (f *Function) ValueType(id ValueID) types.Type {
	return f.Values[id]
}

func (f *Function) NewSlot(ty types.Type, name string) SlotID {
	f.nextSlot++
	id := f.nextSlot
	f.Slots = append(f.Slots, Slot{ID: id, Type: ty, Name: name})
	return id
}

func (f *Function) NewBlock(name string) *Block {
	f.nextBlock++
	b := &Block{ID: f.nextBlock, Name: name}
	f.Blocks = append(f.Blocks, b)
	return b
}

func (f *Function) AddParameter(name string, ty types.Type, slot SlotID) {
	f.Parameters = append(f.Parameters, Parameter{
		Index: len(f.Parameters),
		Name:  name,
		Type:  ty,
		Slot:  slot,
	})
}

type Module struct {
	Functions []*Function
}

func (m *Module) AddFunction(fn *Function) {
	m.Functions = append(m.Functions, fn)
}

type Instr interface {
	isInstr()
}

type Add struct {
	Dest        ValueID
	Left, Right Operand
}

func (Add) isInstr() {}

type Sub struct {
	Dest        ValueID
	Left, Right Operand
}

func (Sub) isInstr() {}

type Mul struct {
	Dest        ValueID
	Left, Right Operand
}

func (Mul) isInstr() {}

type Div struct {
	Dest        ValueID
	Left, Right Operand
}

func (Div) isInstr() {}

type CmpEq struct {
	Dest        ValueID
	Left, Right Operand
}

func (CmpEq) isInstr() {}

type CmpNe struct {
	Dest        ValueID
	Left, Right Operand
}

func (CmpNe) isInstr() {}

type CmpLt struct {
	Dest        ValueID
	Left, Right Operand
}

func (CmpLt) isInstr() {}

type CmpLe struct {
	Dest        ValueID
	Left, Right Operand
}

func (CmpLe) isInstr() {}

type CmpGt struct {
	Dest        ValueID
	Left, Right Operand
}

func (CmpGt) isInstr() {}

type CmpGe struct {
	Dest        ValueID
	Left, Right Operand
}

func (CmpGe) isInstr() {}

type Alloca struct {
	Slot SlotID
}

func (Alloca) isInstr() {}

type Load struct {
	Dest ValueID
	Slot SlotID
}

func (Load) isInstr() {}

type Store struct {
	Slot  SlotID
	Value Operand
}

func (Store) isInstr() {}

type LoadPtr struct {
	Dest ValueID
	Ptr  Operand
}

func (LoadPtr) isInstr() {}

type StorePtr struct {
	Ptr   Operand
	Value Operand
}

func (StorePtr) isInstr() {}

type AddressOf struct {
	Dest ValueID
	Slot SlotID
}

func (AddressOf) isInstr() {}

type FieldAddress struct {
	Dest  ValueID
	Base  Operand
	Field string
}

func (FieldAddress) isInstr() {}

type Call struct {
	Dest      ValueID
	Name      string
	Args      []Operand
	Signature FunctionSignature
}

func (Call) isInstr() {}

type Jump struct {
	Target BlockID
}

func (Jump) isInstr() {}

type Branch struct {
	Cond       Operand
	Then, Else BlockID
}

func (Branch) isInstr() {}

type Return struct {
	HasValue bool
	Value    Operand
}

func (Return) isInstr() {}

type Cast struct {
	Dest ValueID
	From Operand
	To   types.Type
}

func (Cast) isInstr() {}
