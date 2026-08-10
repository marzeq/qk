package ir

import (
	"fmt"
	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/types"
	"reflect"
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
	Kind    OperandKind
	Type    types.Type
	Generic *GenericReference

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

// GenericTemplate is target-independent, attributed QK IR. Parameters remain
// symbolic until a specialization unit substitutes concrete arguments.
type GenericTemplate struct {
	Module     string
	Name       string
	Parameters []types.TypeParameter
	Functions  []*Function
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
	// ArrayObject selects an element within one inline array value. Without it,
	// a pointer whose pointee is an array advances by whole array values.
	ArrayObject bool
}

func (ElementAddress) isInstr() {}

type Call struct {
	Dest        ValueID
	Name        string
	Callee      *Operand
	Args        []Operand
	Signature   FunctionSignature
	Generic     *GenericReference
	Requirement *TraitRequirementReference
}

func (Call) isInstr() {}

type GenericReference struct {
	Module        string
	Name          string
	TypeArguments []types.Type
}

type TraitRequirementReference struct {
	Name         string
	ReceiverType types.Type
}

// InstantiateGenericTemplate deep-copies symbolic IR and substitutes its type
// parameters. Generic call references remain explicit for recursive scheduling.
func InstantiateGenericTemplate(template GenericTemplate, arguments []types.Type) ([]*Function, error) {
	if len(arguments) != len(template.Parameters) {
		return nil, fmt.Errorf("template %s.%s expects %d type arguments, got %d", template.Module, template.Name, len(template.Parameters), len(arguments))
	}
	bindings := make(map[string]types.Type, len(arguments))
	for index, parameter := range template.Parameters {
		bindings[parameter.Key()] = arguments[index]
	}
	cloned := cloneIRWithTypes(reflect.ValueOf(template.Functions), bindings)
	if !cloned.IsValid() || cloned.IsNil() {
		return nil, fmt.Errorf("template %s.%s has no body", template.Module, template.Name)
	}
	return cloned.Interface().([]*Function), nil
}

var irTypeReflection = reflect.TypeFor[types.Type]()

func cloneIRWithTypes(value reflect.Value, bindings map[string]types.Type) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}
	if value.Type() == irTypeReflection {
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		replaced := types.SubstituteParameters(value.Interface().(types.Type), bindings)
		return reflect.ValueOf(replaced)
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		item := cloneIRWithTypes(value.Elem(), bindings)
		result := reflect.New(value.Type()).Elem()
		result.Set(item)
		return result
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.New(value.Type().Elem())
		result.Elem().Set(cloneIRWithTypes(value.Elem(), bindings))
		return result
	case reflect.Struct:
		result := reflect.New(value.Type()).Elem()
		for index := 0; index < value.NumField(); index++ {
			if result.Field(index).CanSet() && value.Type().Field(index).IsExported() {
				result.Field(index).Set(cloneIRWithTypes(value.Field(index), bindings))
			}
		}
		return result
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			result.Index(index).Set(cloneIRWithTypes(value.Index(index), bindings))
		}
		return result
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			result.SetMapIndex(iterator.Key(), cloneIRWithTypes(iterator.Value(), bindings))
		}
		return result
	default:
		return value
	}
}

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
