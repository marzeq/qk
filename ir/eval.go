package ir

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/types"
)

// CompileTimeUnavailableError represents an operation outside the evaluator's
// capabilities. Calls add the innermost source-level call site while this error
// unwinds, allowing the frontend to report language syntax instead of IR names.
type CompileTimeUnavailableError struct {
	SourceName string
	SourceLoc  shared.Location
	Detail     string
	CallStack  []SourceOrigin
}

func (e *CompileTimeUnavailableError) Error() string {
	if e.SourceName != "" {
		return fmt.Sprintf("%s is not available in a compile-time context", e.SourceName)
	}
	return e.Detail
}

// EvalValue is the compile-time representation of a value carried by regular
// QK IR. The interpreter deliberately starts with the scalar and aggregate
// values needed by staging; unsupported executed instructions report an error.
type EvalValue struct {
	Type      types.Type
	Integer   *big.Int
	Float     *float64
	Boolean   bool
	Aggregate []EvalValue
	pointer   *evalPointer
}

type evalPointer struct {
	root *EvalValue
	path []evalPath
}

type evalPath struct {
	index int
	typ   types.Type
	union bool
}

func EvalInteger(value *big.Int, typ types.Type) EvalValue {
	return EvalValue{Type: typ, Integer: new(big.Int).Set(value)}
}

func EvalFloat(value float64, typ types.Type) EvalValue {
	return EvalValue{Type: typ, Float: &value}
}

func EvalBool(value bool) EvalValue { return EvalValue{Type: types.PrimitiveBool, Boolean: value} }

type Evaluator struct {
	Functions map[string]*Function
	Globals   map[string]EvalValue
	MaxDepth  int
}

func NewEvaluator(modules ...*Module) *Evaluator {
	evaluator := &Evaluator{Functions: make(map[string]*Function), Globals: make(map[string]EvalValue), MaxDepth: 1024}
	for _, module := range modules {
		for _, function := range module.Functions {
			if len(function.Blocks) != 0 {
				evaluator.Functions[function.Name] = function
			}
		}
		for _, global := range module.Globals {
			value, err := evaluator.operand(global.Value, nil)
			if err == nil {
				evaluator.Globals[global.Name] = value
			}
		}
	}
	return evaluator
}

func (e *Evaluator) Run(name string, arguments ...EvalValue) (EvalValue, error) {
	return e.run(name, arguments, 0)
}

func (e *Evaluator) run(name string, arguments []EvalValue, depth int) (EvalValue, error) {
	function := e.Functions[name]
	if function == nil {
		return EvalValue{}, &CompileTimeUnavailableError{Detail: fmt.Sprintf("compile-time call reached unavailable function %q", name)}
	}
	if depth >= e.MaxDepth {
		return EvalValue{}, fmt.Errorf("compile-time call depth exceeded while evaluating %q", name)
	}
	if len(arguments) != len(function.Parameters) {
		return EvalValue{}, fmt.Errorf("compile-time call to %q expects %d arguments, got %d", name, len(function.Parameters), len(arguments))
	}
	frame := &evalFrame{function: function, values: make(map[ValueID]EvalValue), slots: make(map[SlotID]*EvalValue)}
	for index, parameter := range function.Parameters {
		value := arguments[index]
		frame.slots[parameter.Slot] = &value
		for _, block := range function.Blocks {
			if block.ID != function.Entry {
				continue
			}
			for _, raw := range block.Instr {
				store, ok := raw.(Store)
				if ok && store.Slot == parameter.Slot && store.Value.Kind == OperandValue {
					frame.values[store.Value.Value] = arguments[index]
					break
				}
			}
		}
	}
	block := function.Entry
	for {
		current := frame.block(block)
		if current == nil {
			return EvalValue{}, fmt.Errorf("compile-time execution entered missing block %d in %q", block, name)
		}
		jumped := false
		for instructionIndex, instruction := range current.Instr {
			switch instruction := instruction.(type) {
			case Alloca:
				value := zeroEvalValue(frame.slotType(instruction.Slot))
				frame.slots[instruction.Slot] = &value
			case Load:
				value, ok := frame.slots[instruction.Slot]
				if !ok {
					return EvalValue{}, fmt.Errorf("compile-time load from uninitialized slot %d in %q", instruction.Slot, name)
				}
				frame.values[instruction.Dest] = *value
			case Store:
				value, err := e.operand(instruction.Value, frame)
				if err != nil {
					return EvalValue{}, err
				}
				if slot := frame.slots[instruction.Slot]; slot != nil {
					*slot = value
				} else {
					frame.slots[instruction.Slot] = &value
				}
			case AddressOf:
				slot := frame.slots[instruction.Slot]
				if slot == nil {
					return EvalValue{}, fmt.Errorf("compile-time address of unavailable slot %d in %q", instruction.Slot, name)
				}
				frame.values[instruction.Dest] = EvalValue{Type: frame.function.ValueType(instruction.Dest), pointer: &evalPointer{root: slot}}
			case FieldAddress:
				base, err := e.operand(instruction.Base, frame)
				if err != nil || base.pointer == nil {
					return EvalValue{}, fmt.Errorf("compile-time field address requires an addressable aggregate")
				}
				path, err := evalFieldPath(instruction.Base.Type, instruction.Field)
				if err != nil {
					return EvalValue{}, err
				}
				pointer := &evalPointer{root: base.pointer.root, path: append(append([]evalPath(nil), base.pointer.path...), path)}
				frame.values[instruction.Dest] = EvalValue{Type: frame.function.ValueType(instruction.Dest), pointer: pointer}
			case LoadPtr:
				pointer, err := e.operand(instruction.Ptr, frame)
				if err != nil || pointer.pointer == nil {
					return EvalValue{}, fmt.Errorf("compile-time load requires a valid pointer")
				}
				value, err := pointer.pointer.load()
				if err != nil {
					return EvalValue{}, err
				}
				frame.values[instruction.Dest] = value
			case StorePtr:
				pointer, err := e.operand(instruction.Ptr, frame)
				if err != nil || pointer.pointer == nil {
					return EvalValue{}, fmt.Errorf("compile-time store requires a valid pointer")
				}
				value, err := e.operand(instruction.Value, frame)
				if err != nil {
					return EvalValue{}, err
				}
				if err := pointer.pointer.store(value); err != nil {
					return EvalValue{}, err
				}
			case InsertValue:
				aggregate, err := e.operand(instruction.Aggregate, frame)
				if err != nil {
					return EvalValue{}, err
				}
				value, err := e.operand(instruction.Value, frame)
				if err != nil {
					return EvalValue{}, err
				}
				if instruction.Index < 0 || instruction.Index >= len(aggregate.Aggregate) {
					return EvalValue{}, fmt.Errorf("compile-time aggregate insertion index %d is out of range", instruction.Index)
				}
				aggregate.Aggregate[instruction.Index] = value
				aggregate.Type = frame.function.ValueType(instruction.Dest)
				frame.values[instruction.Dest] = aggregate
			case ExtractValue:
				aggregate, err := e.operand(instruction.Aggregate, frame)
				if err != nil {
					return EvalValue{}, err
				}
				if instruction.Index < 0 || instruction.Index >= len(aggregate.Aggregate) {
					return EvalValue{}, fmt.Errorf("compile-time aggregate extraction index %d is out of range", instruction.Index)
				}
				frame.values[instruction.Dest] = aggregate.Aggregate[instruction.Index]
			case LoadGlobal:
				value, ok := e.Globals[instruction.Name]
				if !ok {
					return EvalValue{}, fmt.Errorf("compile-time load reached unavailable global %q", instruction.Name)
				}
				frame.values[instruction.Dest] = value
			case StoreGlobal:
				value, err := e.operand(instruction.Value, frame)
				if err != nil {
					return EvalValue{}, err
				}
				e.Globals[instruction.Name] = value
			case Add:
				if err := e.binaryNumber(frame, instruction.Dest, instruction.Left, instruction.Right, new(big.Int).Add, func(a, b float64) float64 { return a + b }); err != nil {
					return EvalValue{}, err
				}
			case Sub:
				if err := e.binaryNumber(frame, instruction.Dest, instruction.Left, instruction.Right, new(big.Int).Sub, func(a, b float64) float64 { return a - b }); err != nil {
					return EvalValue{}, err
				}
			case Mul:
				if err := e.binaryNumber(frame, instruction.Dest, instruction.Left, instruction.Right, new(big.Int).Mul, func(a, b float64) float64 { return a * b }); err != nil {
					return EvalValue{}, err
				}
			case Div:
				leftValue, leftErr := e.operand(instruction.Left, frame)
				rightValue, rightErr := e.operand(instruction.Right, frame)
				if leftErr != nil || rightErr != nil {
					if leftErr != nil {
						return EvalValue{}, leftErr
					}
					return EvalValue{}, rightErr
				}
				if leftValue.Float != nil && rightValue.Float != nil {
					frame.values[instruction.Dest] = EvalFloat(*leftValue.Float / *rightValue.Float, frame.function.ValueType(instruction.Dest))
					break
				}
				left, right, err := e.integerOperands(instruction.Left, instruction.Right, frame)
				if err != nil {
					return EvalValue{}, err
				}
				if right.Sign() == 0 {
					return EvalValue{}, fmt.Errorf("division by zero during compile-time evaluation")
				}
				frame.values[instruction.Dest] = EvalInteger(new(big.Int).Quo(left, right), frame.function.ValueType(instruction.Dest))
			case Mod:
				left, right, err := e.integerOperands(instruction.Left, instruction.Right, frame)
				if err != nil {
					return EvalValue{}, err
				}
				if right.Sign() == 0 {
					return EvalValue{}, fmt.Errorf("division by zero during compile-time evaluation")
				}
				frame.values[instruction.Dest] = EvalInteger(new(big.Int).Rem(left, right), frame.function.ValueType(instruction.Dest))
			case BitwiseAnd:
				if err := e.binaryInteger(frame, instruction.Dest, instruction.Left, instruction.Right, new(big.Int).And); err != nil {
					return EvalValue{}, err
				}
			case BitwiseOr:
				if err := e.binaryInteger(frame, instruction.Dest, instruction.Left, instruction.Right, new(big.Int).Or); err != nil {
					return EvalValue{}, err
				}
			case BitwiseXor:
				if err := e.binaryInteger(frame, instruction.Dest, instruction.Left, instruction.Right, new(big.Int).Xor); err != nil {
					return EvalValue{}, err
				}
			case ShiftLeft:
				left, right, err := e.integerOperands(instruction.Left, instruction.Right, frame)
				if err != nil {
					return EvalValue{}, err
				}
				if !right.IsUint64() {
					return EvalValue{}, fmt.Errorf("invalid compile-time shift amount")
				}
				frame.values[instruction.Dest] = EvalInteger(new(big.Int).Lsh(left, uint(right.Uint64())), frame.function.ValueType(instruction.Dest))
			case ShiftRight:
				left, right, err := e.integerOperands(instruction.Left, instruction.Right, frame)
				if err != nil {
					return EvalValue{}, err
				}
				if !right.IsUint64() {
					return EvalValue{}, fmt.Errorf("invalid compile-time shift amount")
				}
				frame.values[instruction.Dest] = EvalInteger(new(big.Int).Rsh(left, uint(right.Uint64())), frame.function.ValueType(instruction.Dest))
			case BitwiseNot:
				value, err := e.operand(instruction.Operand, frame)
				if err != nil || value.Integer == nil {
					return EvalValue{}, fmt.Errorf("compile-time bitwise complement requires an integer")
				}
				frame.values[instruction.Dest] = EvalInteger(new(big.Int).Not(value.Integer), frame.function.ValueType(instruction.Dest))
			case CmpEq:
				if err := e.compare(frame, instruction.Dest, instruction.Left, instruction.Right, false, func(c int) bool { return c == 0 }); err != nil {
					return EvalValue{}, err
				}
			case CmpNe:
				if err := e.compare(frame, instruction.Dest, instruction.Left, instruction.Right, true, func(c int) bool { return c != 0 }); err != nil {
					return EvalValue{}, err
				}
			case CmpLt:
				if err := e.compare(frame, instruction.Dest, instruction.Left, instruction.Right, false, func(c int) bool { return c < 0 }); err != nil {
					return EvalValue{}, err
				}
			case CmpLe:
				if err := e.compare(frame, instruction.Dest, instruction.Left, instruction.Right, false, func(c int) bool { return c <= 0 }); err != nil {
					return EvalValue{}, err
				}
			case CmpGt:
				if err := e.compare(frame, instruction.Dest, instruction.Left, instruction.Right, false, func(c int) bool { return c > 0 }); err != nil {
					return EvalValue{}, err
				}
			case CmpGe:
				if err := e.compare(frame, instruction.Dest, instruction.Left, instruction.Right, false, func(c int) bool { return c >= 0 }); err != nil {
					return EvalValue{}, err
				}
			case LogicalNot:
				value, err := e.operand(instruction.Operand, frame)
				if err != nil {
					return EvalValue{}, err
				}
				frame.values[instruction.Dest] = EvalBool(!value.Boolean)
			case Negate:
				value, err := e.operand(instruction.Operand, frame)
				if err != nil {
					return EvalValue{}, err
				}
				if value.Float != nil {
					frame.values[instruction.Dest] = EvalFloat(-*value.Float, frame.function.ValueType(instruction.Dest))
					break
				}
				if value.Integer == nil {
					return EvalValue{}, fmt.Errorf("compile-time negation requires a number")
				}
				frame.values[instruction.Dest] = EvalInteger(new(big.Int).Neg(value.Integer), frame.function.ValueType(instruction.Dest))
			case Cast:
				value, err := e.operand(instruction.From, frame)
				if err != nil {
					return EvalValue{}, err
				}
				value.Type = instruction.To
				frame.values[instruction.Dest] = value
			case Call:
				if instruction.Callee != nil || instruction.Generic != nil || instruction.Requirement != nil {
					return EvalValue{}, annotateUnavailable(
						&CompileTimeUnavailableError{Detail: "indirect or unresolved calls are not available in a compile-time context"},
						instruction,
					)
				}
				args := make([]EvalValue, len(instruction.Args))
				for index, argument := range instruction.Args {
					value, err := e.operand(argument, frame)
					if err != nil {
						return EvalValue{}, err
					}
					args[index] = value
				}
				value, err := e.run(instruction.Name, args, depth+1)
				if err != nil {
					var unavailable *CompileTimeUnavailableError
					if errors.As(err, &unavailable) {
						return EvalValue{}, annotateUnavailable(unavailable, instruction)
					}
					return EvalValue{}, fmt.Errorf("in compile-time call to %s: %w", instruction.Name, err)
				}
				if instruction.Dest != 0 {
					frame.values[instruction.Dest] = value
				}
			case Jump:
				block, jumped = instruction.Target, true
			case Branch:
				condition, err := e.operand(instruction.Cond, frame)
				if err != nil {
					return EvalValue{}, err
				}
				block = instruction.Else
				if condition.Boolean {
					block = instruction.Then
				}
				jumped = true
			case Return:
				if !instruction.HasValue {
					return EvalValue{Type: types.PrimitiveVoid}, nil
				}
				return e.operand(instruction.Value, frame)
			case Unreachable:
				return EvalValue{}, fmt.Errorf("compile-time execution reached unreachable code in %q", name)
			default:
				unavailable := &CompileTimeUnavailableError{Detail: fmt.Sprintf("instruction %T is unavailable during compile-time evaluation", instruction)}
				if origin, ok := frame.function.Origins[InstructionLocation{Block: current.ID, Index: instructionIndex}]; ok {
					unavailable.SourceName = origin.Name
					unavailable.SourceLoc = origin.Loc
				}
				return EvalValue{}, unavailable
			}
			if jumped {
				break
			}
		}
		if !jumped {
			return EvalValue{}, fmt.Errorf("compile-time execution fell off block %q in %q", current.Name, name)
		}
	}
}

type evalFrame struct {
	function *Function
	values   map[ValueID]EvalValue
	slots    map[SlotID]*EvalValue
}

func (f *evalFrame) block(id BlockID) *Block {
	for _, block := range f.function.Blocks {
		if block.ID == id {
			return block
		}
	}
	return nil
}
func (f *evalFrame) slotType(id SlotID) types.Type {
	for _, slot := range f.function.Slots {
		if slot.ID == id {
			return slot.Type
		}
	}
	return nil
}

func (e *Evaluator) operand(operand Operand, frame *evalFrame) (EvalValue, error) {
	switch operand.Kind {
	case OperandValue:
		if frame == nil {
			return EvalValue{}, fmt.Errorf("value operand outside compile-time frame")
		}
		value, ok := frame.values[operand.Value]
		if !ok {
			return EvalValue{}, fmt.Errorf("compile-time use of undefined IR value %d", operand.Value)
		}
		return value, nil
	case OperandIntConst:
		value, ok := new(big.Int).SetString(operand.IntValue, 0)
		if !ok {
			return EvalValue{}, fmt.Errorf("invalid integer IR constant %q", operand.IntValue)
		}
		return EvalInteger(value, operand.Type), nil
	case OperandFloatConst:
		value, err := strconv.ParseFloat(operand.FloatValue, 64)
		if err != nil {
			return EvalValue{}, fmt.Errorf("invalid floating-point IR constant %q", operand.FloatValue)
		}
		if types.Underlying(operand.Type).Equals(types.PrimitiveF32) {
			value = float64(float32(value))
		}
		return EvalFloat(value, operand.Type), nil
	case OperandBoolConst:
		return EvalBool(operand.BoolValue), nil
	case OperandZeroConst, OperandNullConst:
		return zeroEvalValue(operand.Type), nil
	case OperandStructConst:
		value := EvalValue{Type: operand.Type, Aggregate: make([]EvalValue, len(operand.Fields))}
		for index, field := range operand.Fields {
			evaluated, err := e.operand(field, frame)
			if err != nil {
				return EvalValue{}, err
			}
			value.Aggregate[index] = evaluated
		}
		return value, nil
	default:
		return EvalValue{}, &CompileTimeUnavailableError{Detail: fmt.Sprintf("operand kind %d is unavailable during compile-time evaluation", operand.Kind)}
	}
}

func annotateUnavailable(err *CompileTimeUnavailableError, call Call) error {
	if call.SourceLoc.FilePath == "" {
		return err
	}
	annotated := *err
	if annotated.SourceLoc.FilePath == "" {
		annotated.SourceName = call.SourceName
		annotated.SourceLoc = call.SourceLoc
		return &annotated
	}
	if !sameEvalLocation(annotated.SourceLoc, call.SourceLoc) {
		annotated.CallStack = append(append([]SourceOrigin(nil), err.CallStack...), SourceOrigin{Name: call.SourceName, Loc: call.SourceLoc})
	}
	return &annotated
}

func sameEvalLocation(left, right shared.Location) bool {
	return left.FilePath == right.FilePath && left.Offset == right.Offset && left.EndOffset == right.EndOffset
}

func zeroEvalValue(typ types.Type) EvalValue {
	if typ != nil && types.Underlying(typ).Equals(types.PrimitiveBool) {
		return EvalBool(false)
	}
	if typ != nil && types.IsFloat(types.Underlying(typ)) {
		return EvalFloat(0, typ)
	}
	if typ != nil {
		switch aggregate := types.Underlying(typ).(type) {
		case types.StructType:
			value := EvalValue{Type: typ, Aggregate: make([]EvalValue, len(aggregate.Fields))}
			for i, field := range aggregate.Fields {
				value.Aggregate[i] = zeroEvalValue(field.R)
			}
			return value
		case types.UnionType:
			return EvalValue{Type: typ, Aggregate: []EvalValue{EvalInteger(big.NewInt(0), types.PrimitiveU64)}}
		case types.MultipleReturnType:
			value := EvalValue{Type: typ, Aggregate: make([]EvalValue, len(aggregate.Types))}
			for i, field := range aggregate.Types {
				value.Aggregate[i] = zeroEvalValue(field)
			}
			return value
		}
	}
	return EvalInteger(big.NewInt(0), typ)
}

func evalFieldPath(pointerType types.Type, field string) (evalPath, error) {
	pointer, ok := types.Underlying(pointerType).(types.PointerType)
	if !ok {
		return evalPath{}, fmt.Errorf("compile-time field access requires a pointer")
	}
	switch aggregate := types.Underlying(pointer.Base).(type) {
	case types.StructType:
		for index, candidate := range aggregate.Fields {
			if candidate.L == field {
				return evalPath{index: index, typ: candidate.R}, nil
			}
		}
	case types.UnionType:
		for _, candidate := range aggregate.Fields {
			if candidate.L == field {
				return evalPath{typ: candidate.R, union: true}, nil
			}
		}
	}
	return evalPath{}, fmt.Errorf("compile-time field %q is unavailable", field)
}

func (p *evalPointer) load() (EvalValue, error) {
	value := *p.root
	for _, step := range p.path {
		if step.union {
			if len(value.Aggregate) == 0 {
				return EvalValue{}, fmt.Errorf("compile-time read from empty union storage")
			}
			return reinterpretEvalValue(value.Aggregate[0], step.typ)
		}
		if step.index < 0 || step.index >= len(value.Aggregate) {
			return EvalValue{}, fmt.Errorf("compile-time aggregate field is out of range")
		}
		value = value.Aggregate[step.index]
	}
	return value, nil
}

func (p *evalPointer) store(value EvalValue) error {
	return storeEvalPath(p.root, p.path, value)
}

func storeEvalPath(target *EvalValue, path []evalPath, value EvalValue) error {
	if len(path) == 0 {
		*target = value
		return nil
	}
	step := path[0]
	if step.union {
		if len(target.Aggregate) == 0 {
			target.Aggregate = make([]EvalValue, 1)
		}
		target.Aggregate[0] = value
		return nil
	}
	if step.index < 0 || step.index >= len(target.Aggregate) {
		return fmt.Errorf("compile-time aggregate field is out of range")
	}
	return storeEvalPath(&target.Aggregate[step.index], path[1:], value)
}

func reinterpretEvalValue(value EvalValue, target types.Type) (EvalValue, error) {
	underlying := types.Underlying(target)
	if value.Float != nil && types.IsInteger(underlying) {
		bits := math.Float64bits(*value.Float)
		if underlying.Equals(types.PrimitiveU32) || underlying.Equals(types.PrimitiveI32) {
			bits = uint64(math.Float32bits(float32(*value.Float)))
		}
		return EvalInteger(new(big.Int).SetUint64(bits), target), nil
	}
	if value.Integer != nil && types.IsFloat(underlying) {
		bits := value.Integer.Uint64()
		if underlying.Equals(types.PrimitiveF32) {
			return EvalFloat(float64(math.Float32frombits(uint32(bits))), target), nil
		}
		return EvalFloat(math.Float64frombits(bits), target), nil
	}
	value.Type = target
	return value, nil
}

func (e *Evaluator) integerOperands(left, right Operand, frame *evalFrame) (*big.Int, *big.Int, error) {
	l, err := e.operand(left, frame)
	if err != nil {
		return nil, nil, err
	}
	r, err := e.operand(right, frame)
	if err != nil {
		return nil, nil, err
	}
	if l.Integer == nil || r.Integer == nil {
		return nil, nil, fmt.Errorf("compile-time integer operation requires integer operands")
	}
	return l.Integer, r.Integer, nil
}

func (e *Evaluator) binaryInteger(frame *evalFrame, dest ValueID, left, right Operand, operation func(*big.Int, *big.Int) *big.Int) error {
	l, r, err := e.integerOperands(left, right, frame)
	if err != nil {
		return err
	}
	frame.values[dest] = EvalInteger(operation(new(big.Int).Set(l), r), frame.function.ValueType(dest))
	return nil
}

func (e *Evaluator) binaryNumber(frame *evalFrame, dest ValueID, left, right Operand, integerOp func(*big.Int, *big.Int) *big.Int, floatOp func(float64, float64) float64) error {
	l, err := e.operand(left, frame)
	if err != nil {
		return err
	}
	r, err := e.operand(right, frame)
	if err != nil {
		return err
	}
	if l.Float != nil && r.Float != nil {
		value := floatOp(*l.Float, *r.Float)
		if types.Underlying(frame.function.ValueType(dest)).Equals(types.PrimitiveF32) {
			value = float64(float32(value))
		}
		frame.values[dest] = EvalFloat(value, frame.function.ValueType(dest))
		return nil
	}
	if l.Integer == nil || r.Integer == nil {
		return fmt.Errorf("compile-time numeric operation requires matching numeric operands")
	}
	frame.values[dest] = EvalInteger(integerOp(new(big.Int).Set(l.Integer), r.Integer), frame.function.ValueType(dest))
	return nil
}

func (e *Evaluator) compare(frame *evalFrame, dest ValueID, left, right Operand, unorderedResult bool, predicate func(int) bool) error {
	l, err := e.operand(left, frame)
	if err != nil {
		return err
	}
	r, err := e.operand(right, frame)
	if err != nil {
		return err
	}
	comparison := 0
	if l.Integer != nil && r.Integer != nil {
		comparison = l.Integer.Cmp(r.Integer)
	} else if l.Float != nil && r.Float != nil {
		if math.IsNaN(*l.Float) || math.IsNaN(*r.Float) {
			frame.values[dest] = EvalBool(unorderedResult)
			return nil
		}
		if *l.Float < *r.Float {
			comparison = -1
		} else if *l.Float > *r.Float {
			comparison = 1
		}
	} else if l.Boolean != r.Boolean {
		if l.Boolean {
			comparison = 1
		} else {
			comparison = -1
		}
	}
	frame.values[dest] = EvalBool(predicate(comparison))
	return nil
}
