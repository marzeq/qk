package ir

import (
	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/types"
)

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
	OperandStructConst
	OperandCStringConst
	OperandFunctionConst
	OperandZeroConst
	OperandSizeofConst
	OperandBinaryConst
)

type Operand struct {
	Kind OperandKind
	Type types.Type

	Value ValueID

	IntValue     string
	FloatValue   string
	BoolValue    bool
	Fields       []Operand
	StringValue  string
	FunctionName string
	SubjectType  types.Type
	Operator     string
	Left         *Operand
	Right        *Operand
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

func StructConstOperand(ty types.Type, fields []Operand) Operand {
	return Operand{Kind: OperandStructConst, Type: ty, Fields: fields}
}

func CStringConstOperand(value string) Operand {
	return Operand{
		Kind: OperandCStringConst, Type: types.PointerType{Base: types.PrimitiveU8}, StringValue: value,
	}
}

func FunctionConstOperand(name string, ty types.Type) Operand {
	return Operand{Kind: OperandFunctionConst, Type: ty, FunctionName: name}
}

func ZeroConstOperand(ty types.Type) Operand {
	return Operand{Kind: OperandZeroConst, Type: ty}
}

func SizeofConstOperand(subject types.Type) Operand {
	return Operand{Kind: OperandSizeofConst, Type: types.PrimitiveUsz, SubjectType: subject}
}

func BinaryConstOperand(operator string, left, right Operand, ty types.Type) Operand {
	return Operand{Kind: OperandBinaryConst, Type: ty, Operator: operator, Left: &left, Right: &right}
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
	Attributes attributes.Attributes
}

type Linkage uint8

const (
	LinkageInternal Linkage = iota
	LinkageExternal
)

type Visibility uint8

const (
	VisibilityDefault Visibility = iota
	VisibilityHidden
)

type Function struct {
	Name       string
	Linkage    Linkage
	Visibility Visibility
	Signature  FunctionSignature
	Parameters []Parameter
	Slots      []Slot
	Attributes attributes.Attributes
	Values     map[ValueID]types.Type

	Blocks []*Block
	Entry  BlockID

	nextValue ValueID
	nextSlot  SlotID
	nextBlock BlockID
}

func NewFunction(name string, linkage Linkage, attributes attributes.Attributes) *Function {
	return &Function{
		Name:       name,
		Values:     make(map[ValueID]types.Type),
		Linkage:    linkage,
		Attributes: attributes,
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
	Functions     []*Function
	Externs       []ExternDecl
	Globals       []Global
	ExternGlobals []ExternGlobal
	Initializer   string
	Entry         string
}

func (m *Module) AddFunction(fn *Function) {
	m.Functions = append(m.Functions, fn)
}

type ExternDecl struct {
	Name       string
	Signature  FunctionSignature
	From       string
	Visibility Visibility
}

func (m *Module) AddExtern(e ExternDecl) {
	m.Externs = append(m.Externs, e)
}

type Global struct {
	Name       string
	Type       types.Type
	Mutable    bool
	Linkage    Linkage
	Visibility Visibility
	Value      Operand
}

func (m *Module) AddGlobal(global Global) {
	m.Globals = append(m.Globals, global)
}

type ExternGlobal struct {
	Name       string
	Type       types.Type
	Mutable    bool
	Visibility Visibility
}

func (m *Module) AddExternGlobal(global ExternGlobal) {
	for _, existing := range m.ExternGlobals {
		if existing.Name == global.Name {
			return
		}
	}
	m.ExternGlobals = append(m.ExternGlobals, global)
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

type LogicalAnd struct {
	Dest        ValueID
	Left, Right Operand
}

func (LogicalAnd) isInstr() {}

type LogicalOr struct {
	Dest        ValueID
	Left, Right Operand
}

func (LogicalOr) isInstr() {}

type Mod struct {
	Dest        ValueID
	Left, Right Operand
}

func (Mod) isInstr() {}

type BitwiseAnd struct {
	Dest        ValueID
	Left, Right Operand
}

func (BitwiseAnd) isInstr() {}

type BitwiseOr struct {
	Dest        ValueID
	Left, Right Operand
}

func (BitwiseOr) isInstr() {}

type BitwiseXor struct {
	Dest        ValueID
	Left, Right Operand
}

func (BitwiseXor) isInstr() {}

type ShiftLeft struct {
	Dest        ValueID
	Left, Right Operand
}

func (ShiftLeft) isInstr() {}

type ShiftRight struct {
	Dest        ValueID
	Left, Right Operand
}

func (ShiftRight) isInstr() {}

type Negate struct {
	Dest    ValueID
	Operand Operand
}

func (Negate) isInstr() {}

type LogicalNot struct {
	Dest    ValueID
	Operand Operand
}

func (LogicalNot) isInstr() {}

type BitwiseNot struct {
	Dest    ValueID
	Operand Operand
}

func (BitwiseNot) isInstr() {}

type Alloca struct {
	Slot SlotID
}

func (Alloca) isInstr() {}

type AllocaArray struct {
	Dest    ValueID
	Element types.Type
	Count   Operand
}

func (AllocaArray) isInstr() {}

type Load struct {
	Dest ValueID
	Slot SlotID
}

func (Load) isInstr() {}

type LoadGlobal struct {
	Dest ValueID
	Name string
	Type types.Type
}

func (LoadGlobal) isInstr() {}

type Store struct {
	Slot  SlotID
	Value Operand
}

func (Store) isInstr() {}

type StoreGlobal struct {
	Name  string
	Value Operand
}

func (StoreGlobal) isInstr() {}

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

type InsertValue struct {
	Dest      ValueID
	Aggregate Operand
	Value     Operand
	Index     int
}

func (InsertValue) isInstr() {}

type ExtractValue struct {
	Dest      ValueID
	Aggregate Operand
	Index     int
}

func (ExtractValue) isInstr() {}

type AddressOf struct {
	Dest ValueID
	Slot SlotID
}

func (AddressOf) isInstr() {}

type AddressOfGlobal struct {
	Dest ValueID
	Name string
	Type types.Type
}

func (AddressOfGlobal) isInstr() {}

type FieldAddress struct {
	Dest  ValueID
	Base  Operand
	Field string
}

func (FieldAddress) isInstr() {}

type ElementAddress struct {
	Dest    ValueID
	Base    Operand
	Index   Operand
	Element types.Type
}

func (ElementAddress) isInstr() {}

type Call struct {
	Dest      ValueID
	Name      string
	Callee    *Operand
	Args      []Operand
	Signature FunctionSignature
}

func (Call) isInstr() {}

type InlineAsm struct {
	Dest        ValueID
	Template    string
	Constraints []string
	Clobbers    []string
	Args        []Operand
	ResultType  types.Type
	SideEffect  bool
}

func (InlineAsm) isInstr() {}

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

type Unreachable struct{}

func (Unreachable) isInstr() {}

type Cast struct {
	Dest ValueID
	From Operand
	To   types.Type
}

func (Cast) isInstr() {}

type Sizeof struct {
	Dest ValueID
	Type types.Type
}

func (Sizeof) isInstr() {}

type Alignof struct {
	Dest ValueID
	Type types.Type
}

func (Alignof) isInstr() {}

type Offsetof struct {
	Dest  ValueID
	Type  types.Type
	Field string
}

func (Offsetof) isInstr() {}

type StringConst struct {
	Dest           ValueID
	Value          string
	NullTerminated bool
}

func (StringConst) isInstr() {}
