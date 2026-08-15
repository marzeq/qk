package ir

import (
	"fmt"
	"math/big"

	"github.com/marzeq/qk/types"
)

// EvalValue is the compile-time representation of a value carried by regular
// QK IR. The interpreter deliberately starts with the scalar and aggregate
// values needed by staging; unsupported executed instructions report an error.
type EvalValue struct {
	Type      types.Type
	Integer   *big.Int
	Boolean   bool
	Aggregate []EvalValue
}

func EvalInteger(value *big.Int, typ types.Type) EvalValue {
	return EvalValue{Type: typ, Integer: new(big.Int).Set(value)}
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
			evaluator.Functions[function.Name] = function
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
		return EvalValue{}, fmt.Errorf("compile-time call reached unavailable function %q", name)
	}
	if depth >= e.MaxDepth {
		return EvalValue{}, fmt.Errorf("compile-time call depth exceeded while evaluating %q", name)
	}
	if len(arguments) != len(function.Parameters) {
		return EvalValue{}, fmt.Errorf("compile-time call to %q expects %d arguments, got %d", name, len(function.Parameters), len(arguments))
	}
	frame := &evalFrame{function: function, values: make(map[ValueID]EvalValue), slots: make(map[SlotID]EvalValue)}
	for index, parameter := range function.Parameters {
		frame.slots[parameter.Slot] = arguments[index]
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
		for _, instruction := range current.Instr {
			switch instruction := instruction.(type) {
			case Alloca:
				frame.slots[instruction.Slot] = zeroEvalValue(frame.slotType(instruction.Slot))
			case Load:
				value, ok := frame.slots[instruction.Slot]
				if !ok {
					return EvalValue{}, fmt.Errorf("compile-time load from uninitialized slot %d in %q", instruction.Slot, name)
				}
				frame.values[instruction.Dest] = value
			case Store:
				value, err := e.operand(instruction.Value, frame)
				if err != nil {
					return EvalValue{}, err
				}
				frame.slots[instruction.Slot] = value
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
				if err := e.binaryInteger(frame, instruction.Dest, instruction.Left, instruction.Right, new(big.Int).Add); err != nil {
					return EvalValue{}, err
				}
			case Sub:
				if err := e.binaryInteger(frame, instruction.Dest, instruction.Left, instruction.Right, new(big.Int).Sub); err != nil {
					return EvalValue{}, err
				}
			case Mul:
				if err := e.binaryInteger(frame, instruction.Dest, instruction.Left, instruction.Right, new(big.Int).Mul); err != nil {
					return EvalValue{}, err
				}
			case Div:
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
			case CmpEq:
				if err := e.compare(frame, instruction.Dest, instruction.Left, instruction.Right, func(c int) bool { return c == 0 }); err != nil {
					return EvalValue{}, err
				}
			case CmpNe:
				if err := e.compare(frame, instruction.Dest, instruction.Left, instruction.Right, func(c int) bool { return c != 0 }); err != nil {
					return EvalValue{}, err
				}
			case CmpLt:
				if err := e.compare(frame, instruction.Dest, instruction.Left, instruction.Right, func(c int) bool { return c < 0 }); err != nil {
					return EvalValue{}, err
				}
			case CmpLe:
				if err := e.compare(frame, instruction.Dest, instruction.Left, instruction.Right, func(c int) bool { return c <= 0 }); err != nil {
					return EvalValue{}, err
				}
			case CmpGt:
				if err := e.compare(frame, instruction.Dest, instruction.Left, instruction.Right, func(c int) bool { return c > 0 }); err != nil {
					return EvalValue{}, err
				}
			case CmpGe:
				if err := e.compare(frame, instruction.Dest, instruction.Left, instruction.Right, func(c int) bool { return c >= 0 }); err != nil {
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
				if err != nil || value.Integer == nil {
					return EvalValue{}, fmt.Errorf("compile-time negation requires an integer")
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
					return EvalValue{}, fmt.Errorf("compile-time execution reached unsupported indirect or unresolved call in %q", name)
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
				return EvalValue{}, fmt.Errorf("instruction %T is unavailable during compile-time evaluation of %q", instruction, name)
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
	slots    map[SlotID]EvalValue
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
		return EvalValue{}, fmt.Errorf("operand kind %d is unavailable during compile-time evaluation", operand.Kind)
	}
}

func zeroEvalValue(typ types.Type) EvalValue {
	if typ != nil && types.Underlying(typ).Equals(types.PrimitiveBool) {
		return EvalBool(false)
	}
	return EvalInteger(big.NewInt(0), typ)
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

func (e *Evaluator) compare(frame *evalFrame, dest ValueID, left, right Operand, predicate func(int) bool) error {
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
