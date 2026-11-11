package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/marzeq/qk/shared"
	qktarget "github.com/marzeq/qk/target"
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
	Type         types.Type
	TypeValue    types.Type
	Integer      *big.Int
	Float        *float64
	Boolean      bool
	Aggregate    []EvalValue
	builder      *evalStructBuilder
	enumBuilder  *evalEnumBuilder
	unionBuilder *evalUnionBuilder
	pointer      *evalPointer
}

type evalStructBuilder struct {
	origin        string
	fields        []shared.Pair[string, types.Type]
	packed        bool
	reflectPublic bool
	self          *types.AliasRef
}

type evalEnumBuilder struct {
	origin        string
	flags         bool
	backing       types.PrimitiveType
	names         []string
	values        []string
	explicit      bool
	implicit      bool
	reflectPublic bool
}

type evalUnionBuilder struct {
	origin        string
	mode          string
	tag           types.Type
	fields        []shared.Pair[string, types.Type]
	variants      []types.TaggedUnionVariant
	reflectPublic bool
	self          *types.AliasRef
}

func evalIdentityKey(value EvalValue) string {
	key := types.Identity(value.Type)
	if value.TypeValue != nil {
		return key + "=type:" + types.Identity(value.TypeValue)
	}
	if value.Integer != nil {
		return key + "=int:" + value.Integer.String()
	}
	if value.Float != nil {
		return key + "=float:" + strconv.FormatFloat(*value.Float, 'g', -1, 64)
	}
	if value.Type != nil && value.Type.Equals(types.PrimitiveBool) {
		return key + "=bool:" + strconv.FormatBool(value.Boolean)
	}
	if value.Type != nil && value.Type.Equals(types.StrType()) {
		if text, err := evalString(value); err == nil {
			return key + "=str:" + text
		}
	}
	if len(value.Aggregate) != 0 {
		parts := make([]string, len(value.Aggregate))
		for index, field := range value.Aggregate {
			parts[index] = evalIdentityKey(field)
		}
		return key + "={" + strings.Join(parts, ",") + "}"
	}
	return key
}

func cloneEvalValue(value EvalValue) EvalValue {
	if value.Integer != nil {
		value.Integer = new(big.Int).Set(value.Integer)
	}
	if value.Float != nil {
		copied := *value.Float
		value.Float = &copied
	}
	if value.Aggregate != nil {
		fields := make([]EvalValue, len(value.Aggregate))
		for i, field := range value.Aggregate {
			fields[i] = cloneEvalValue(field)
		}
		value.Aggregate = fields
	}
	if value.builder != nil {
		copied := *value.builder
		copied.fields = append([]shared.Pair[string, types.Type](nil), copied.fields...)
		if copied.self != nil {
			previous := copied.self
			copied.self = &types.AliasRef{Module: previous.Module, Name: previous.Name}
			for index := range copied.fields {
				copied.fields[index].R = remapBuilderSelf(copied.fields[index].R, previous, copied.self)
			}
		}
		value.builder = &copied
	}
	if value.enumBuilder != nil {
		copied := *value.enumBuilder
		copied.names = append([]string(nil), copied.names...)
		copied.values = append([]string(nil), copied.values...)
		value.enumBuilder = &copied
	}
	if value.unionBuilder != nil {
		copied := *value.unionBuilder
		copied.fields = append([]shared.Pair[string, types.Type](nil), copied.fields...)
		copied.variants = append([]types.TaggedUnionVariant(nil), copied.variants...)
		for index := range copied.variants {
			copied.variants[index].Fields = append([]shared.Pair[string, types.Type](nil), copied.variants[index].Fields...)
		}
		if copied.self != nil {
			previous := copied.self
			copied.self = &types.AliasRef{Module: previous.Module, Name: previous.Name}
			for index := range copied.fields {
				copied.fields[index].R = remapBuilderSelf(copied.fields[index].R, previous, copied.self)
			}
			for variant := range copied.variants {
				for field := range copied.variants[variant].Fields {
					copied.variants[variant].Fields[field].R = remapBuilderSelf(copied.variants[variant].Fields[field].R, previous, copied.self)
				}
			}
		}
		value.unionBuilder = &copied
	}
	return value
}

func remapBuilderSelf(value types.Type, previous, replacement *types.AliasRef) types.Type {
	switch value := value.(type) {
	case *types.AliasRef:
		if value == previous {
			return replacement
		}
	case types.PointerType:
		value.Base = remapBuilderSelf(value.Base, previous, replacement)
		return value
	case types.SliceType:
		value.Base = remapBuilderSelf(value.Base, previous, replacement)
		return value
	case types.ArrayType:
		value.Base = remapBuilderSelf(value.Base, previous, replacement)
		return value
	case types.DefinedType:
		value.Underlying = remapBuilderSelf(value.Underlying, previous, replacement)
		return value
	case types.StructType:
		value.Fields = append([]shared.Pair[string, types.Type](nil), value.Fields...)
		for index := range value.Fields {
			value.Fields[index].R = remapBuilderSelf(value.Fields[index].R, previous, replacement)
		}
		if value.TaggedUnion != nil {
			info := *value.TaggedUnion
			info.Tag = remapBuilderSelf(info.Tag, previous, replacement)
			info.Variants = append([]types.TaggedUnionVariant(nil), info.Variants...)
			for index := range info.Variants {
				info.Variants[index].Fields = append([]shared.Pair[string, types.Type](nil), info.Variants[index].Fields...)
				for field := range info.Variants[index].Fields {
					info.Variants[index].Fields[field].R = remapBuilderSelf(info.Variants[index].Fields[field].R, previous, replacement)
				}
			}
			value.TaggedUnion = &info
		}
		return value
	case types.UnionType:
		value.Fields = append([]shared.Pair[string, types.Type](nil), value.Fields...)
		for index := range value.Fields {
			value.Fields[index].R = remapBuilderSelf(value.Fields[index].R, previous, replacement)
		}
		return value
	}
	return value
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
	Functions     map[string]*Function
	Globals       map[string]EvalValue
	MaxDepth      int
	TargetTriple  string
	TypeSatisfies func(types.Type, types.Type) (bool, error)
}

func NewEvaluator(modules ...*Module) *Evaluator {
	return NewEvaluatorForTarget("", modules...)
}

func NewEvaluatorForTarget(triple string, modules ...*Module) *Evaluator {
	evaluator := &Evaluator{Functions: make(map[string]*Function), Globals: make(map[string]EvalValue), MaxDepth: 1024, TargetTriple: triple}
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
	identity := name
	for _, argument := range arguments {
		identity += ":" + evalIdentityKey(argument)
	}
	identityDigest := sha256.Sum256([]byte(identity))
	identity = hex.EncodeToString(identityDigest[:16])
	frame := &evalFrame{function: function, values: make(map[ValueID]EvalValue), slots: make(map[SlotID]*EvalValue), identity: identity}
	for index, parameter := range function.Parameters {
		value := cloneEvalValue(arguments[index])
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
					*slot = cloneEvalValue(value)
				} else {
					copied := cloneEvalValue(value)
					frame.slots[instruction.Slot] = &copied
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
			case ElementAddress:
				base, err := e.operand(instruction.Base, frame)
				if err != nil || base.pointer == nil {
					return EvalValue{}, fmt.Errorf("compile-time index requires an addressable sequence")
				}
				indexValue, err := e.operand(instruction.Index, frame)
				if err != nil || indexValue.Integer == nil || !indexValue.Integer.IsInt64() {
					return EvalValue{}, fmt.Errorf("compile-time index requires an integer")
				}
				index := indexValue.Integer.Int64()
				if index < 0 {
					return EvalValue{}, fmt.Errorf("compile-time index is out of range")
				}
				path := append([]evalPath(nil), base.pointer.path...)
				if instruction.ArrayObject || len(path) == 0 {
					path = append(path, evalPath{index: int(index), typ: instruction.Element})
				} else {
					path[len(path)-1].index += int(index)
					path[len(path)-1].typ = instruction.Element
				}
				pointer := &evalPointer{root: base.pointer.root, path: path}
				if _, err := pointer.load(); err != nil {
					return EvalValue{}, fmt.Errorf("compile-time index is out of range")
				}
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
			case StringConst:
				bytes := []byte(instruction.Value)
				if instruction.NullTerminated {
					bytes = append(bytes, 0)
				}
				storage := EvalValue{Type: types.ArrayType{Base: types.PrimitiveU8, Length: len(bytes)}, Aggregate: make([]EvalValue, len(bytes))}
				for index, value := range bytes {
					storage.Aggregate[index] = EvalInteger(big.NewInt(int64(value)), types.PrimitiveU8)
				}
				frame.values[instruction.Dest] = EvalValue{Type: frame.function.ValueType(instruction.Dest), pointer: &evalPointer{root: &storage}}
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
			case Sizeof:
				size, _, err := qktarget.LayoutOf(instruction.Type, e.TargetTriple)
				if err != nil {
					return EvalValue{}, err
				}
				frame.values[instruction.Dest] = EvalInteger(big.NewInt(int64(size)), types.PrimitiveUsz)
			case SizeofValue:
				value, err := e.operand(instruction.Value, frame)
				if err != nil {
					return EvalValue{}, err
				}
				if value.TypeValue == nil {
					return EvalValue{}, fmt.Errorf("sizeof requires a type value")
				}
				size, _, err := qktarget.LayoutOf(value.TypeValue, e.TargetTriple)
				if err != nil {
					return EvalValue{}, err
				}
				frame.values[instruction.Dest] = EvalInteger(big.NewInt(int64(size)), types.PrimitiveUsz)
			case Alignof:
				_, align, err := qktarget.LayoutOf(instruction.Type, e.TargetTriple)
				if err != nil {
					return EvalValue{}, err
				}
				frame.values[instruction.Dest] = EvalInteger(big.NewInt(int64(align)), types.PrimitiveUsz)
			case AlignofValue:
				value, err := e.operand(instruction.Value, frame)
				if err != nil {
					return EvalValue{}, err
				}
				if value.TypeValue == nil {
					return EvalValue{}, fmt.Errorf("alignof requires a type value")
				}
				_, align, err := qktarget.LayoutOf(value.TypeValue, e.TargetTriple)
				if err != nil {
					return EvalValue{}, err
				}
				frame.values[instruction.Dest] = EvalInteger(big.NewInt(int64(align)), types.PrimitiveUsz)
			case TypePointer:
				base, err := e.operand(instruction.Base, frame)
				if err != nil {
					return EvalValue{}, err
				}
				if base.TypeValue == nil {
					return EvalValue{}, fmt.Errorf("pointer type constructor requires a type value")
				}
				frame.values[instruction.Dest] = EvalValue{Type: types.MetaType{}, TypeValue: types.PointerType{Base: base.TypeValue, Mutable: instruction.Mutable}}
			case Offsetof:
				offset, err := qktarget.OffsetOf(instruction.Type, instruction.Field, e.TargetTriple)
				if err != nil {
					return EvalValue{}, err
				}
				frame.values[instruction.Dest] = EvalInteger(big.NewInt(int64(offset)), types.PrimitiveUsz)
			case Call:
				if instruction.Callee != nil || instruction.Requirement != nil {
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
				intrinsicArgs := args
				var err error
				if len(instruction.CompileTimeArgs) != 0 {
					intrinsicArgs = make([]EvalValue, len(args)+len(instruction.CompileTimeArgs))
					ordinary := 0
					for index := range intrinsicArgs {
						if operand, exists := instruction.CompileTimeArgs[index]; exists {
							intrinsicArgs[index], err = e.operand(operand, frame)
							if err != nil {
								return EvalValue{}, err
							}
						} else {
							intrinsicArgs[index] = args[ordinary]
							ordinary++
						}
					}
				}
				value, handled, err := e.coreTypesCall(instruction, intrinsicArgs, frame.identity)
				if !handled {
					value, err = e.run(instruction.Name, args, depth+1)
				}
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
				value, err := e.operand(instruction.Value, frame)
				return cloneEvalValue(value), err
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
	identity string
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
	case OperandTypeConst:
		return EvalValue{Type: types.MetaType{}, TypeValue: operand.SubjectType}, nil
	case OperandZeroConst, OperandNullConst:
		return zeroEvalValue(operand.Type), nil
	case OperandSizeofConst:
		size, _, err := qktarget.LayoutOf(operand.SubjectType, e.TargetTriple)
		if err != nil {
			return EvalValue{}, err
		}
		return EvalInteger(big.NewInt(int64(size)), types.PrimitiveUsz), nil
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

const coreTypesStructBuilder = "__qk_0_4_core5_types_StructBuilder_"
const coreTypesEnumBuilder = "__qk_0_4_core5_types_EnumBuilder_"
const coreTypesFlagsBuilder = "__qk_0_4_core5_types_FlagsBuilder_"
const coreTypesUnionBuilder = "__qk_0_4_core5_types_UnionBuilder_"
const coreTypesInspect = "__qk_0_4_core5_types_inspect"
const coreTypesSatisfies = "__qk_0_4_core5_types_satisfies"

func (e *Evaluator) coreTypesCall(call Call, args []EvalValue, invocation string) (EvalValue, bool, error) {
	if call.Name == coreTypesSatisfies || strings.HasPrefix(call.Name, coreTypesSatisfies+"_") {
		if len(args) != 2 || args[0].TypeValue == nil || args[1].TypeValue == nil {
			return EvalValue{}, true, fmt.Errorf("core.types.satisfies requires two completed type values")
		}
		if e.TypeSatisfies == nil {
			return EvalValue{}, true, fmt.Errorf("core.types.satisfies requires semantic type information")
		}
		result, err := e.TypeSatisfies(args[0].TypeValue, args[1].TypeValue)
		return EvalBool(result), true, err
	}
	if call.Name == coreTypesInspect || strings.HasPrefix(call.Name, coreTypesInspect+"_") {
		var input types.Type
		if len(args) == 1 {
			input = args[0].TypeValue
		}
		if input == nil && call.Generic != nil && len(call.Generic.TypeArguments) == 1 {
			input = call.Generic.TypeArguments[0]
		}
		if input == nil {
			return EvalValue{}, true, fmt.Errorf("core.types.inspect requires a completed type value")
		}
		value, err := inspectType(input, call.Signature.ReturnType)
		return value, true, err
	}
	if strings.HasPrefix(call.Name, coreTypesEnumBuilder) {
		return e.enumBuilderCall(call, args, false, invocation)
	}
	if strings.HasPrefix(call.Name, coreTypesFlagsBuilder) {
		return e.enumBuilderCall(call, args, true, invocation)
	}
	if strings.HasPrefix(call.Name, coreTypesUnionBuilder) {
		return e.unionBuilderCall(call, args, invocation)
	}
	if len(call.Name) < len(coreTypesStructBuilder) || call.Name[:len(coreTypesStructBuilder)] != coreTypesStructBuilder {
		return EvalValue{}, false, nil
	}
	method := call.Name[len(coreTypesStructBuilder):]
	if index := strings.Index(method, "__const_"); index >= 0 {
		method = method[:index]
	}
	if method == "new" {
		if len(args) != 0 {
			return EvalValue{}, true, fmt.Errorf("StructBuilder.new expects no arguments")
		}
		loc := call.SourceLoc
		origin := fmt.Sprintf("%s:%d:%d:%s", loc.FilePath, loc.Offset, loc.EndOffset, invocation)
		digest := sha256.Sum256([]byte(origin))
		self := &types.AliasRef{Module: "core.types", Name: "$pending_" + hex.EncodeToString(digest[:12])}
		return EvalValue{Type: call.Signature.ReturnType, builder: &evalStructBuilder{origin: origin, self: self}}, true, nil
	}
	if len(args) == 0 {
		return EvalValue{}, true, fmt.Errorf("StructBuilder.%s requires a receiver", method)
	}
	receiver := args[0]
	if receiver.pointer != nil {
		var err error
		receiver, err = receiver.pointer.load()
		if err != nil {
			return EvalValue{}, true, err
		}
	}
	if receiver.builder == nil {
		return EvalValue{}, true, fmt.Errorf("StructBuilder.%s received an invalid builder", method)
	}
	state := receiver.builder
	switch method {
	case "add_field":
		if len(args) != 3 || args[2].TypeValue == nil {
			return EvalValue{}, true, fmt.Errorf("StructBuilder.add_field requires a name and type (got %d arguments, generic %#v)", len(args), call.Generic)
		}
		name, err := evalString(args[1])
		if err != nil {
			return EvalValue{}, true, err
		}
		for _, field := range state.fields {
			if field.L == name {
				return EvalValue{}, true, fmt.Errorf("duplicate struct field %q", name)
			}
		}
		state.fields = append(state.fields, shared.Pair[string, types.Type]{L: name, R: args[2].TypeValue})
	case "add_fields":
		if len(args) != 2 {
			return EvalValue{}, true, fmt.Errorf("StructBuilder.add_fields requires a field slice")
		}
		members, err := evalSlice(args[1])
		if err != nil {
			return EvalValue{}, true, err
		}
		for _, member := range members {
			if len(member.Aggregate) != 2 || member.Aggregate[1].TypeValue == nil {
				return EvalValue{}, true, fmt.Errorf("StructBuilder.add_fields requires FieldSpec values")
			}
			name, err := evalString(member.Aggregate[0])
			if err != nil {
				return EvalValue{}, true, err
			}
			for _, field := range state.fields {
				if field.L == name {
					return EvalValue{}, true, fmt.Errorf("duplicate struct field %q", name)
				}
			}
			state.fields = append(state.fields, shared.Pair[string, types.Type]{L: name, R: member.Aggregate[1].TypeValue})
		}
	case "set_packed":
		if len(args) != 2 {
			return EvalValue{}, true, fmt.Errorf("StructBuilder.set_packed requires a boolean")
		}
		state.packed = args[1].Boolean
	case "reflect_public":
		state.reflectPublic = true
	case "fields":
		slice, ok := types.Underlying(call.Signature.ReturnType).(types.SliceType)
		if !ok {
			return EvalValue{}, true, fmt.Errorf("StructBuilder.fields has an invalid return type")
		}
		members := EvalValue{Type: types.ArrayType{Base: slice.Base, Length: len(state.fields)}, Aggregate: make([]EvalValue, len(state.fields))}
		for index, field := range state.fields {
			members.Aggregate[index] = EvalValue{Type: slice.Base, Aggregate: []EvalValue{evalStringValue(field.L), {Type: types.MetaType{}, TypeValue: field.R}}}
		}
		return EvalValue{Type: call.Signature.ReturnType, Aggregate: []EvalValue{
			{Type: types.PointerType{Base: slice.Base}, pointer: &evalPointer{root: &members}},
			EvalInteger(big.NewInt(int64(len(state.fields))), types.PrimitiveUsz),
		}}, true, nil
	case "finish", "self_type":
		if method == "self_type" {
			return EvalValue{Type: types.MetaType{}, TypeValue: state.self}, true, nil
		}
		shape := types.StructType{Fields: append([]shared.Pair[string, types.Type](nil), state.fields...), Packed: state.packed}
		for _, field := range shape.Fields {
			if field.L == "" {
				return EvalValue{}, true, fmt.Errorf("struct field name cannot be empty")
			}
			if types.HasError(field.R) {
				return EvalValue{}, true, fmt.Errorf("struct field %q has an invalid type", field.L)
			}
		}
		identity := fmt.Sprintf("%s:%s", state.origin, shape.String())
		digest := sha256.Sum256([]byte(identity))
		name := "$built_" + hex.EncodeToString(digest[:12])
		finishedSelf := &types.AliasRef{Module: "core.types", Name: name}
		for index := range shape.Fields {
			shape.Fields[index].R = remapBuilderSelf(shape.Fields[index].R, state.self, finishedSelf)
		}
		finished := types.DefinedType{Module: "core.types", Name: name, Underlying: shape}
		var target types.Type = finished
		finishedSelf.Target = &target
		if !types.IsComplete(finished) {
			return EvalValue{}, true, fmt.Errorf("struct builder contains a by-value recursive field")
		}
		if _, _, err := qktarget.LayoutOf(finished, e.TargetTriple); err != nil {
			return EvalValue{}, true, err
		}
		return EvalValue{Type: types.MetaType{}, TypeValue: finished}, true, nil
	default:
		return EvalValue{}, true, fmt.Errorf("unsupported StructBuilder operation %s", method)
	}
	if args[0].pointer == nil {
		return EvalValue{}, true, fmt.Errorf("StructBuilder.%s requires a mutable receiver", method)
	}
	if err := args[0].pointer.store(receiver); err != nil {
		return EvalValue{}, true, err
	}
	return EvalValue{Type: types.PrimitiveVoid}, true, nil
}

func evalFieldSpecSlice(fields []shared.Pair[string, types.Type], sliceType types.Type) EvalValue {
	base := types.Underlying(sliceType).(types.SliceType).Base
	items := make([]EvalValue, len(fields))
	for index, field := range fields {
		items[index] = EvalValue{Type: base, Aggregate: []EvalValue{evalStringValue(field.L), {Type: types.MetaType{}, TypeValue: field.R}}}
	}
	return evalValuesSlice(items, sliceType)
}

func evalValuesSlice(items []EvalValue, sliceType types.Type) EvalValue {
	base := types.Underlying(sliceType).(types.SliceType).Base
	storage := EvalValue{Type: types.ArrayType{Base: base, Length: len(items)}, Aggregate: items}
	return EvalValue{Type: sliceType, Aggregate: []EvalValue{
		{Type: types.PointerType{Base: base}, pointer: &evalPointer{root: &storage}},
		EvalInteger(big.NewInt(int64(len(items))), types.PrimitiveUsz),
	}}
}

func inspectType(input, resultType types.Type) (EvalValue, error) {
	if _, incomplete := types.Underlying(input).(*types.AliasRef); incomplete {
		return EvalValue{}, fmt.Errorf("cannot inspect an incomplete type")
	}
	shape, ok := types.Underlying(resultType).(types.StructType)
	if !ok || len(shape.Fields) != 9 {
		return EvalValue{}, fmt.Errorf("core.types.TypeShape has an invalid representation")
	}
	kind := "opaque"
	var fields []shared.Pair[string, types.Type]
	var variants []types.TaggedUnionVariant
	var members []shared.Pair[string, string]
	var element types.Type = types.PrimitiveU8
	hasElement := false
	length := 0
	mutable, packed := false, false
	underlying := types.Underlying(input)
	switch t := underlying.(type) {
	case types.PrimitiveType:
		kind = "primitive"
	case types.MetaType:
		kind = "metatype"
	case types.PointerType:
		kind, element, hasElement, mutable = "pointer", t.Base, true, t.Mutable
	case types.SliceType:
		kind, element, hasElement, mutable = "slice", t.Base, true, t.Mutable
	case types.ArrayType:
		kind, element, hasElement, length = "array", t.Base, true, t.Length
	case types.StructType:
		if t.TaggedUnion == nil {
			kind, fields, packed = "struct", t.Fields, t.Packed
		} else {
			kind, variants, element, hasElement, packed = "tagged_union", t.TaggedUnion.Variants, t.TaggedUnion.Tag, true, t.Packed
		}
	case types.UnionType:
		kind, fields = "union", t.Fields
	case types.EnumType:
		kind = "enum"
		for index, name := range t.Variants {
			members = append(members, shared.Pair[string, string]{L: name, R: t.Values[index]})
		}
	case types.FlagsType:
		kind, element, hasElement = "flags", t.Underlying, true
		for index, name := range t.Variants {
			members = append(members, shared.Pair[string, string]{L: name, R: t.Values[index]})
		}
	case types.TraitType:
		kind = "trait"
	case types.FunctionType:
		kind = "function"
	}
	if _, nominal := input.(types.DefinedType); nominal && kind == "primitive" {
		kind, element, hasElement = "distinct", underlying, true
	}
	variantType := shape.Fields[2].R
	variantBase := types.Underlying(variantType).(types.SliceType).Base
	variantFields := types.Underlying(variantBase).(types.StructType).Fields
	variantValues := make([]EvalValue, len(variants))
	for index, variant := range variants {
		variantValues[index] = EvalValue{Type: variantBase, Aggregate: []EvalValue{
			evalStringValue(variant.Name), evalFieldSpecSlice(variant.Fields, variantFields[1].R),
		}}
	}
	memberType := shape.Fields[8].R
	memberBase := types.Underlying(memberType).(types.SliceType).Base
	memberValues := make([]EvalValue, len(members))
	for index, member := range members {
		memberValues[index] = EvalValue{Type: memberBase, Aggregate: []EvalValue{evalStringValue(member.L), evalStringValue(member.R)}}
	}
	return EvalValue{Type: resultType, Aggregate: []EvalValue{
		evalStringValue(kind),
		evalFieldSpecSlice(fields, shape.Fields[1].R),
		evalValuesSlice(variantValues, variantType),
		{Type: types.MetaType{}, TypeValue: element},
		EvalBool(hasElement),
		EvalInteger(big.NewInt(int64(length)), types.PrimitiveUsz),
		EvalBool(mutable),
		EvalBool(packed),
		evalValuesSlice(memberValues, memberType),
	}}, nil
}

func (e *Evaluator) enumBuilderCall(call Call, args []EvalValue, flags bool, invocation string) (EvalValue, bool, error) {
	prefix := coreTypesEnumBuilder
	kind := "EnumBuilder"
	if flags {
		prefix, kind = coreTypesFlagsBuilder, "FlagsBuilder"
	}
	method := strings.TrimPrefix(call.Name, prefix)
	if index := strings.Index(method, "__const_"); index >= 0 {
		method = method[:index]
	}
	if method == "new" {
		state := &evalEnumBuilder{flags: flags}
		loc := call.SourceLoc
		state.origin = fmt.Sprintf("%s:%d:%d:%s", loc.FilePath, loc.Offset, loc.EndOffset, invocation)
		if flags {
			if len(args) != 1 || args[0].TypeValue == nil {
				return EvalValue{}, true, fmt.Errorf("FlagsBuilder.new requires an integer backing type")
			}
			backing, ok := types.Underlying(args[0].TypeValue).(types.PrimitiveType)
			if !ok || !types.IsInteger(backing) || backing == types.PrimitiveIsz || backing == types.PrimitiveUsz {
				return EvalValue{}, true, fmt.Errorf("flags backing type must be an integer")
			}
			state.backing = backing
		} else if len(args) != 0 {
			return EvalValue{}, true, fmt.Errorf("EnumBuilder.new expects no arguments")
		}
		return EvalValue{Type: call.Signature.ReturnType, enumBuilder: state}, true, nil
	}
	if len(args) == 0 {
		return EvalValue{}, true, fmt.Errorf("%s.%s requires a receiver", kind, method)
	}
	receiver := args[0]
	if receiver.pointer != nil {
		var err error
		receiver, err = receiver.pointer.load()
		if err != nil {
			return EvalValue{}, true, err
		}
	}
	if receiver.enumBuilder == nil || receiver.enumBuilder.flags != flags {
		return EvalValue{}, true, fmt.Errorf("%s.%s received an invalid builder", kind, method)
	}
	state := receiver.enumBuilder
	switch method {
	case "add", "add_value":
		if len(args) < 2 {
			return EvalValue{}, true, fmt.Errorf("%s.%s requires a name", kind, method)
		}
		name, err := evalString(args[1])
		if err != nil {
			return EvalValue{}, true, err
		}
		if name == "" {
			return EvalValue{}, true, fmt.Errorf("%s member name cannot be empty", kind)
		}
		for _, prior := range state.names {
			if prior == name {
				return EvalValue{}, true, fmt.Errorf("duplicate %s member %q", kind, name)
			}
		}
		var value *big.Int
		if method == "add_value" {
			if len(args) != 3 || args[2].Integer == nil {
				return EvalValue{}, true, fmt.Errorf("%s.add_value requires an integer", kind)
			}
			if state.implicit {
				return EvalValue{}, true, fmt.Errorf("cannot mix implicit and explicit %s values", kind)
			}
			state.explicit = true
			value = args[2].Integer
		} else {
			if len(args) != 2 {
				return EvalValue{}, true, fmt.Errorf("%s.add requires one name", kind)
			}
			if state.explicit {
				return EvalValue{}, true, fmt.Errorf("cannot mix implicit and explicit %s values", kind)
			}
			state.implicit = true
			value = big.NewInt(int64(len(state.names)))
			if flags {
				value = new(big.Int).Lsh(big.NewInt(1), uint(len(state.names)))
			}
		}
		state.names = append(state.names, name)
		state.values = append(state.values, value.String())
	case "reflect_public":
		state.reflectPublic = true
	case "finish":
		if flags {
			widths := map[types.PrimitiveType]uint{types.PrimitiveI8: 8, types.PrimitiveU8: 8, types.PrimitiveI16: 16, types.PrimitiveU16: 16, types.PrimitiveI32: 32, types.PrimitiveU32: 32, types.PrimitiveI64: 64, types.PrimitiveU64: 64}
			limit := new(big.Int).Lsh(big.NewInt(1), widths[state.backing])
			for index, raw := range state.values {
				value, _ := new(big.Int).SetString(raw, 10)
				if value.Sign() < 0 || value.Cmp(limit) >= 0 {
					return EvalValue{}, true, fmt.Errorf("flag %q value does not fit in %s", state.names[index], state.backing)
				}
			}
		}
		identity := fmt.Sprintf("%s:%t:%s:%v:%v", state.origin, flags, state.backing, state.names, state.values)
		digest := sha256.Sum256([]byte(identity))
		name := "$built_" + hex.EncodeToString(digest[:12])
		if flags {
			shape := types.FlagsType{Underlying: state.backing, Variants: append([]string(nil), state.names...), Values: append([]string(nil), state.values...)}
			return EvalValue{Type: types.MetaType{}, TypeValue: types.DefinedType{Module: "core.types", Name: name, Underlying: shape}}, true, nil
		}
		return EvalValue{Type: types.MetaType{}, TypeValue: types.EnumType{Module: "core.types", Name: name, Variants: append([]string(nil), state.names...), Values: append([]string(nil), state.values...)}}, true, nil
	default:
		return EvalValue{}, true, fmt.Errorf("unsupported %s operation %s", kind, method)
	}
	if args[0].pointer == nil {
		return EvalValue{}, true, fmt.Errorf("%s.%s requires a mutable receiver", kind, method)
	}
	if err := args[0].pointer.store(receiver); err != nil {
		return EvalValue{}, true, err
	}
	return EvalValue{Type: types.PrimitiveVoid}, true, nil
}

func (e *Evaluator) unionBuilderCall(call Call, args []EvalValue, invocation string) (EvalValue, bool, error) {
	method := strings.TrimPrefix(call.Name, coreTypesUnionBuilder)
	if index := strings.Index(method, "__const_"); index >= 0 {
		method = method[:index]
	}
	switch method {
	case "untagged", "auto_tagged", "tagged":
		loc := call.SourceLoc
		state := &evalUnionBuilder{origin: fmt.Sprintf("%s:%d:%d:%s", loc.FilePath, loc.Offset, loc.EndOffset, invocation), mode: method}
		pending := sha256.Sum256([]byte(state.origin))
		state.self = &types.AliasRef{Module: "core.types", Name: "$pending_" + hex.EncodeToString(pending[:12])}
		if method == "tagged" {
			if len(args) != 1 || args[0].TypeValue == nil {
				return EvalValue{}, true, fmt.Errorf("UnionBuilder.tagged requires an enum type")
			}
			if _, ok := types.Underlying(args[0].TypeValue).(types.EnumType); !ok {
				return EvalValue{}, true, fmt.Errorf("UnionBuilder.tagged requires an enum type")
			}
			state.tag = args[0].TypeValue
		} else if len(args) != 0 {
			return EvalValue{}, true, fmt.Errorf("UnionBuilder.%s expects no arguments", method)
		}
		return EvalValue{Type: call.Signature.ReturnType, unionBuilder: state}, true, nil
	}
	if len(args) == 0 {
		return EvalValue{}, true, fmt.Errorf("UnionBuilder.%s requires a receiver", method)
	}
	receiver := args[0]
	if receiver.pointer != nil {
		var err error
		receiver, err = receiver.pointer.load()
		if err != nil {
			return EvalValue{}, true, err
		}
	}
	if receiver.unionBuilder == nil {
		return EvalValue{}, true, fmt.Errorf("UnionBuilder.%s received an invalid builder", method)
	}
	state := receiver.unionBuilder
	switch method {
	case "add_field":
		if state.mode != "untagged" {
			return EvalValue{}, true, fmt.Errorf("add_field requires an untagged union builder")
		}
		if len(args) != 3 || args[2].TypeValue == nil {
			return EvalValue{}, true, fmt.Errorf("UnionBuilder.add_field requires a name and type")
		}
		name, err := evalString(args[1])
		if err != nil {
			return EvalValue{}, true, err
		}
		if name == "" {
			return EvalValue{}, true, fmt.Errorf("union field name cannot be empty")
		}
		for _, field := range state.fields {
			if field.L == name {
				return EvalValue{}, true, fmt.Errorf("duplicate union field %q", name)
			}
		}
		state.fields = append(state.fields, shared.Pair[string, types.Type]{L: name, R: args[2].TypeValue})
	case "add_variant":
		if state.mode == "untagged" {
			return EvalValue{}, true, fmt.Errorf("add_variant requires a tagged union builder")
		}
		if len(args) != 2 || len(args[1].Aggregate) != 2 {
			return EvalValue{}, true, fmt.Errorf("UnionBuilder.add_variant requires a VariantSpec")
		}
		name, err := evalString(args[1].Aggregate[0])
		if err != nil {
			return EvalValue{}, true, err
		}
		if name == "" {
			return EvalValue{}, true, fmt.Errorf("variant name cannot be empty")
		}
		for _, variant := range state.variants {
			if variant.Name == name {
				return EvalValue{}, true, fmt.Errorf("duplicate union variant %q", name)
			}
		}
		members, err := evalSlice(args[1].Aggregate[1])
		if err != nil {
			return EvalValue{}, true, err
		}
		variant := types.TaggedUnionVariant{Name: name}
		for index, member := range members {
			if len(member.Aggregate) != 2 || member.Aggregate[1].TypeValue == nil {
				return EvalValue{}, true, fmt.Errorf("variant fields must be FieldSpec values")
			}
			fieldName, err := evalString(member.Aggregate[0])
			if err != nil {
				return EvalValue{}, true, err
			}
			if fieldName == "" {
				fieldName = strconv.Itoa(index)
			}
			for _, prior := range variant.Fields {
				if prior.L == fieldName {
					return EvalValue{}, true, fmt.Errorf("duplicate field %q in variant %q", fieldName, name)
				}
			}
			variant.Fields = append(variant.Fields, shared.Pair[string, types.Type]{L: fieldName, R: member.Aggregate[1].TypeValue})
		}
		state.variants = append(state.variants, variant)
	case "reflect_public":
		state.reflectPublic = true
	case "self_type":
		return EvalValue{Type: types.MetaType{}, TypeValue: state.self}, true, nil
	case "finish":
		identity := fmt.Sprintf("%s:%s:%v:%v:%v", state.origin, state.mode, state.tag, state.fields, state.variants)
		digest := sha256.Sum256([]byte(identity))
		name := "$built_" + hex.EncodeToString(digest[:12])
		var shape types.Type
		if state.mode == "untagged" {
			shape = types.UnionType{Fields: append([]shared.Pair[string, types.Type](nil), state.fields...)}
		} else {
			tag := state.tag
			variants := append([]types.TaggedUnionVariant(nil), state.variants...)
			if state.mode == "auto_tagged" {
				names, values := make([]string, len(variants)), make([]string, len(variants))
				for index, variant := range variants {
					names[index], values[index] = variant.Name, strconv.Itoa(index)
					variants[index].TagValue = values[index]
				}
				tag = types.EnumType{IdentityName: "core.types:" + name + ":tag", Variants: names, Values: values}
			} else {
				tagEnum := types.Underlying(tag).(types.EnumType)
				if len(tagEnum.Variants) != len(variants) {
					return EvalValue{}, true, fmt.Errorf("tagged union variants must match tag enum members")
				}
				for index := range variants {
					value, ok := tagEnum.VariantValue(variants[index].Name)
					if !ok {
						return EvalValue{}, true, fmt.Errorf("tag enum has no member %q", variants[index].Name)
					}
					variants[index].TagValue = value
				}
			}
			payload := make([]shared.Pair[string, types.Type], 0, len(variants))
			for _, variant := range variants {
				if len(variant.Fields) != 0 {
					payload = append(payload, shared.Pair[string, types.Type]{L: variant.Name, R: types.StructType{Fields: variant.Fields}})
				}
			}
			shape = types.StructType{Fields: []shared.Pair[string, types.Type]{{L: "$tag", R: tag}, {L: "$payload", R: types.UnionType{Fields: payload}}}, TaggedUnion: &types.TaggedUnionInfo{Tag: tag, Variants: variants, Auto: state.mode == "auto_tagged"}}
		}
		finishedSelf := &types.AliasRef{Module: "core.types", Name: name}
		shape = remapBuilderSelf(shape, state.self, finishedSelf)
		finished := types.DefinedType{Module: "core.types", Name: name, Underlying: shape}
		var target types.Type = finished
		finishedSelf.Target = &target
		if !types.IsComplete(finished) {
			return EvalValue{}, true, fmt.Errorf("union builder contains a by-value recursive field")
		}
		if _, _, err := qktarget.LayoutOf(finished, e.TargetTriple); err != nil {
			return EvalValue{}, true, err
		}
		return EvalValue{Type: types.MetaType{}, TypeValue: finished}, true, nil
	default:
		return EvalValue{}, true, fmt.Errorf("unsupported UnionBuilder operation %s", method)
	}
	if args[0].pointer == nil {
		return EvalValue{}, true, fmt.Errorf("UnionBuilder.%s requires a mutable receiver", method)
	}
	if err := args[0].pointer.store(receiver); err != nil {
		return EvalValue{}, true, err
	}
	return EvalValue{Type: types.PrimitiveVoid}, true, nil
}

func evalString(value EvalValue) (string, error) {
	if len(value.Aggregate) != 2 || value.Aggregate[1].Integer == nil || value.Aggregate[0].pointer == nil {
		return "", fmt.Errorf("compile-time string value is unavailable")
	}
	length := value.Aggregate[1].Integer.Int64()
	storage, err := value.Aggregate[0].pointer.load()
	if err != nil {
		return "", err
	}
	if length < 0 || length > int64(len(storage.Aggregate)) {
		return "", fmt.Errorf("compile-time string length is invalid")
	}
	data := make([]byte, length)
	for i := range data {
		if storage.Aggregate[i].Integer == nil {
			return "", fmt.Errorf("compile-time string byte is unavailable")
		}
		data[i] = byte(storage.Aggregate[i].Integer.Uint64())
	}
	return string(data), nil
}

func evalSlice(value EvalValue) ([]EvalValue, error) {
	if len(value.Aggregate) != 2 || value.Aggregate[1].Integer == nil {
		return nil, fmt.Errorf("compile-time slice value is unavailable")
	}
	length := value.Aggregate[1].Integer.Int64()
	if length == 0 {
		return nil, nil
	}
	if value.Aggregate[0].pointer == nil {
		return nil, fmt.Errorf("compile-time slice value is unavailable")
	}
	pointer := value.Aggregate[0].pointer
	storage, err := pointer.load()
	if err != nil {
		return nil, err
	}
	if len(pointer.path) != 0 && pointer.path[len(pointer.path)-1].index == 0 {
		parent := &evalPointer{root: pointer.root, path: pointer.path[:len(pointer.path)-1]}
		if container, parentErr := parent.load(); parentErr == nil && len(container.Aggregate) >= int(length) {
			storage = container
		}
	}
	if length < 0 || length > int64(len(storage.Aggregate)) {
		return nil, fmt.Errorf("compile-time slice length is invalid")
	}
	return append([]EvalValue(nil), storage.Aggregate[:length]...), nil
}

func (value EvalValue) FieldSpecs() ([]shared.Pair[string, types.Type], error) {
	members, err := evalSlice(value)
	if err != nil {
		return nil, err
	}
	result := make([]shared.Pair[string, types.Type], 0, len(members))
	for _, member := range members {
		if len(member.Aggregate) != 2 || member.Aggregate[1].TypeValue == nil {
			return nil, fmt.Errorf("field spread requires []core.types.FieldSpec")
		}
		name, err := evalString(member.Aggregate[0])
		if err != nil {
			return nil, err
		}
		result = append(result, shared.Pair[string, types.Type]{L: name, R: member.Aggregate[1].TypeValue})
	}
	return result, nil
}

func evalStringValue(value string) EvalValue {
	bytes := []byte(value)
	storage := EvalValue{Type: types.ArrayType{Base: types.PrimitiveU8, Length: len(bytes)}, Aggregate: make([]EvalValue, len(bytes))}
	for index, item := range bytes {
		storage.Aggregate[index] = EvalInteger(big.NewInt(int64(item)), types.PrimitiveU8)
	}
	return EvalValue{Type: types.StrType(), Aggregate: []EvalValue{
		{Type: types.PointerType{Base: types.PrimitiveU8}, pointer: &evalPointer{root: &storage}},
		EvalInteger(big.NewInt(int64(len(bytes))), types.PrimitiveUsz),
	}}
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
		case types.SliceType:
			return EvalValue{Type: typ, Aggregate: []EvalValue{{Type: types.PointerType{Base: aggregate.Base}}, EvalInteger(big.NewInt(0), types.PrimitiveUsz)}}
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
	case types.SliceType:
		if field == "0" {
			return evalPath{index: 0, typ: types.PointerType{Base: aggregate.Base}}, nil
		}
		if field == "1" {
			return evalPath{index: 1, typ: types.PrimitiveUsz}, nil
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
	} else if l.TypeValue != nil && r.TypeValue != nil {
		if !l.TypeValue.Equals(r.TypeValue) {
			comparison = 1
		}
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
