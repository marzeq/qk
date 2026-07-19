package sema

import (
	"fmt"
	"strconv"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/tokeniser"
	"github.com/marzeq/qk/types"
)

type Attributor struct {
	analyser *Analyser
	errors   []error
}

func (a *Analyser) NewAttributor() *Attributor {
	return &Attributor{analyser: a}
}

func (a *Attributor) errorf(node parser.Node, format string, args ...any) {
	a.errors = append(a.errors, shared.NewError(node.GetLoc(), format, args...))
}

func (a *Attributor) AttributeModule(root *parser.RootNode) {
	for _, node := range root.Body {
		if module, ok := node.(*parser.ModuleNode); ok {
			a.analyser.currentMod = module.Name
			if mod := a.analyser.modules[module.Name]; mod != nil {
				a.analyser.current = mod.Scope
				a.analyser.currentTrustedStandardLibrary = mod.TrustedStandardLibrary
			}
			break
		}
	}
	a.attributeNode(root)
}

func (a *Attributor) Errors() []error {
	return a.errors
}

func (a *Attributor) attributeNode(node parser.Node) {
	switch n := node.(type) {
	case *parser.RootNode:
		for _, stmt := range n.Body {
			a.attributeNode(stmt)
		}

	case *parser.ImportNode, *parser.ModuleNode:
		// pass

	case *parser.TypeAliasNode:
		// pass

	case *parser.FunctionDefNode:
		for _, arg := range n.Args {
			if arg.Default != nil {
				a.attributeExpr(arg.Default)
			}
		}
		if n.Body != nil {
			a.attributeNode(n.Body)
		}

		foreignAttr := n.Attributes.Get(attributes.AttributeTypeForeign)

		if foreignAttr != nil && n.RetTypeNode == nil {
			a.errorf(n, "foreign function must have a return type annotation")
		}

		if n.Symbol.Signature.ReturnType == nil {
			switch b := n.Body.(type) {
			case *parser.BlockNode:
				returnNodes := collectFunctionReturnNodes(b.Body)
				if len(returnNodes) > 0 {
					var current types.Type

					first := returnNodes[0]
					if first.ReturnValue == nil {
						current = types.PrimitiveVoid
					} else {
						current = first.ReturnValue.GetType()
					}

					for _, returnNode := range returnNodes[1:] {
						var t types.Type
						if returnNode.ReturnValue == nil {
							t = types.PrimitiveVoid
						} else {
							t = returnNode.ReturnValue.GetType()
						}

						if types.IsNumeric(current) && types.IsNumeric(t) {
							got := types.PromoteNumeric(current, t)
							if current.Equals(types.ErrorType{}) {
								a.errorf(returnNode, "inconsistent return types: expected %v, got %v", current, t)
							}
							current = got
							continue
						}

						if !current.Equals(t) {
							a.errorf(returnNode, "inconsistent return types: expected %v, got %v", current, t)
							current = types.ErrorType{}
							break
						}
					}

					n.Symbol.Signature.ReturnType = current
				} else {
					n.Symbol.Signature.ReturnType = types.PrimitiveVoid
				}

			case parser.ExpressionNode:
				n.Symbol.Signature.ReturnType = b.GetType()

			case nil:

			default:
				panic(fmt.Sprintf("unexpected function body type: %T\n", n.Body))
			}

			if types.HasUntyped(n.Symbol.Signature.ReturnType) {
				a.errorf(n, "cannot infer function return type from untyped numeric value; add a return type annotation or cast")
				n.Symbol.Signature.ReturnType = types.ErrorType{}
			}
		}

	case *parser.BlockNode:
		for _, stmt := range n.Body {
			a.attributeNode(stmt)
		}

	case *parser.DeclarationNode:
		if n.Value != nil {
			a.attributeExpr(n.Value)
		}

		if n.Value != nil && n.Symbol.Type == nil {
			n.Symbol.Type = n.Value.GetType()
		}

	case *parser.AssignmentNode:
		a.attributeExpr(n.Assignee)
		a.attributeExpr(n.Value)

	case *parser.IfNode:
		a.attributeExpr(n.IfBranch.Condition)
		a.attributeNode(n.IfBranch.Node)
		for _, elif := range n.ElseIfBranches {
			a.attributeExpr(elif.Condition)
			a.attributeNode(elif.Node)
		}
		if n.ElseBranch != nil {
			a.attributeNode(n.ElseBranch)
		}

	case *parser.ForNode:
		for _, node := range n.ExprsOrStmts {
			a.attributeNode(node)
		}
		a.attributeNode(n.Body)

	case *parser.RangeForNode:
		a.attributeExpr(n.Start)
		a.attributeExpr(n.End)
		if n.Symbol != nil {
			n.Symbol.Type = types.PrimitiveUsz
		}
		a.attributeNode(n.Body)

	case *parser.ForEachNode:
		a.attributeExpr(n.Iterable)
		if n.Symbol != nil {
			if slice, ok := types.Underlying(n.Iterable.GetType()).(types.SliceType); ok {
				n.Symbol.Type = slice.Base
			}
		}
		a.attributeNode(n.Body)

	case *parser.ControlKeywordNode:
		if n.ReturnValue != nil {
			a.attributeExpr(n.ReturnValue)
		}

	case *parser.DeferNode:
		a.attributeNode(n.Action)

	case parser.ExpressionNode:
		a.attributeExpr(n)

	default:
		panic(fmt.Sprintf("unexpected node type: %T\n", node))
	}
}

func (a *Attributor) attributeExpr(node parser.ExpressionNode) {
	switch n := node.(type) {
	case *parser.IdentifierNode:
		if n.Symbol != nil && n.Symbol.Kind == symbols.SymbolKindFunction && n.Symbol.Signature != nil {
			ret := n.Symbol.Signature.ReturnType
			if ret == nil {
				ret = types.PrimitiveVoid
			}
			n.SetType(types.PointerType{Base: types.FunctionType{Parameters: n.Symbol.Signature.Parameters, ReturnType: ret, TypedVariadic: n.Symbol.Signature.TypedVariadic, VariadicElement: n.Symbol.Signature.VariadicElement}})
		} else if n.Symbol == nil || n.Symbol.Type == nil {
			a.errorf(n, "undefined identifier: %s", n.String())
			n.SetType(types.ErrorType{})
		} else {
			n.SetType(n.Symbol.Type)
		}

	case *parser.IntegerLiteralNode:
		n.SetType(types.UntypedInt{})

	case *parser.FloatLiteralNode:
		n.SetType(types.UntypedFloat{})

	case *parser.BoolLiteralNode:
		n.SetType(types.PrimitiveBool)

	case *parser.StringLiteralNode:
		n.SetType(types.SliceType{
			Base: types.PrimitiveChar,
			Size: len(n.Value),
		})

	case *parser.CStringLiteralNode:
		n.SetType(types.PointerType{Base: types.PrimitiveChar})

	case *parser.CharLiteralNode:
		n.SetType(types.PrimitiveChar)

	case *parser.NilLiteralNode:
		n.SetType(types.PointerType{
			Base:    types.PrimitiveVoid,
			Mutable: false,
		})

	case *parser.EnumLiteralNode:
		// Leading-dot enum literals are resolved later from an expected type.
		n.SetType(types.UnresolvedEnum{})

	case *parser.StructLiteralNode:
		for _, field := range n.Fields {
			a.attributeExpr(field.R)
		}

		if n.Symbol != nil {
			n.SetType(n.Symbol.TypeInfo)
		} else {
			fields := make([]shared.Pair[string, types.Type], 0, len(n.Fields))
			for _, field := range n.Fields {
				fields = append(fields, shared.Pair[string, types.Type]{
					L: field.L,
					R: field.R.GetType(),
				})
			}
			n.SetType(types.StructType{Fields: fields})
		}

	case *parser.SliceLiteralNode:
		if n.RepeatValue != nil {
			a.attributeExpr(n.RepeatValue)
			a.attributeExpr(n.RepeatAmount)
			size := -1
			if amount, ok := n.RepeatAmount.(*parser.IntegerLiteralNode); ok {
				if parsed, err := strconv.Atoi(amount.Value); err == nil {
					size = parsed
				}
			}
			n.SetType(types.SliceType{Base: n.RepeatValue.GetType(), Size: size})
			break
		}
		typs := []types.Type{}
		for _, elem := range n.Elements {
			a.attributeExpr(elem)
			typs = append(typs, elem.GetType())
		}
		if len(typs) > 0 {
			currentType := typs[0]
			for _, t := range typs[1:] {
				got := types.PromoteNumeric(currentType, t)
				if got.Equals(types.ErrorType{}) {
					a.errorf(n, "inconsistent slice element types: expected %v, got %v", currentType, t)
				}
				currentType = got
			}
			n.SetType(types.SliceType{
				Base: currentType,
				Size: len(n.Elements),
			})
		} else {
			n.SetType(types.SliceType{
				Base: types.PrimitiveVoid,
				Size: 0,
			})
		}

	case *parser.FunctionCallNode:
		if n.Symbol == nil {
			a.attributeMethodCall(n)
		}
		if n.Symbol == nil {
			a.attributeExpr(n.Callee)
			if member, ok := n.Callee.(*parser.FieldAccessNode); ok && member.MethodSymbol != nil && member.MethodSymbol.StaticMethod {
				n.Symbol = member.MethodSymbol
				n.Name = &parser.IdentifierNode{
					Name:   member.MethodSymbol.Name,
					Symbol: member.MethodSymbol,
					Loc:    member.Loc,
				}
				if member.MethodModule != a.analyser.currentMod {
					n.Name.Module = member.MethodModule
					n.Name.ResolvedModuleName = member.MethodModule
				}
			}
		} else if n.Symbol.Signature.ReturnType != nil {
			n.SetType(n.Symbol.Signature.ReturnType)
		} else {
			n.SetType(types.PrimitiveVoid)
		}
		if n.Symbol != nil {
			if n.Symbol.Signature.ReturnType != nil {
				n.SetType(n.Symbol.Signature.ReturnType)
			} else {
				n.SetType(types.PrimitiveVoid)
			}
		}

		for _, arg := range n.Args {
			a.attributeExpr(arg)
		}

	case *parser.IfExprNode:
		a.attributeExpr(n.IfBranch.Condition)

		var current types.Type = types.ErrorType{}

		if n.IfBranch.Node != nil {
			a.attributeExpr(n.IfBranch.Node)
			current = n.IfBranch.Node.GetType()
		}

		for _, elif := range n.ElseIfBranches {
			a.attributeExpr(elif.Condition)
			a.attributeExpr(elif.Node)

			t := elif.Node.GetType()

			if types.IsNumeric(current) && types.IsNumeric(t) {
				current = types.PromoteNumeric(current, t)
				continue
			}

			if !current.Equals(t) {
				current = types.ErrorType{}
				break
			}
		}

		if n.ElseBranch != nil {
			a.attributeExpr(n.ElseBranch)
			t := n.ElseBranch.GetType()

			if types.IsNumeric(current) && types.IsNumeric(t) {
				current = types.PromoteNumeric(current, t)
			} else if !current.Equals(t) {
				current = types.ErrorType{}
			}
		}

		if current.Equals(types.ErrorType{}) {
			a.errorf(n, "inconsistent types in if expression branches: expected %v, got %v", current, n.GetType())
		}
		n.SetType(current)

	case *parser.GivenExprNode:
		a.attributeNode(n.Block)
		a.attributeExpr(n.FinalExpr)
		n.SetType(n.FinalExpr.GetType())

	case *parser.UnaryOpNode:
		a.attributeExpr(n.Operand)
		switch n.Op {
		case parser.UnaryOpNegate:
			if types.IsSigned(n.Operand.GetType()) || types.IsFloat(n.Operand.GetType()) {
				n.SetType(n.Operand.GetType())
			} else {
				a.errorf(n, "cannot apply negation operator to non-numeric type: %v", n.Operand.GetType())
				n.SetType(types.ErrorType{})
			}
		case parser.UnaryOpLogicalNot:
			if n.Operand.GetType().Equals(types.PrimitiveBool) {
				n.SetType(types.PrimitiveBool)
			} else {
				a.errorf(n, "cannot apply logical not operator to non-boolean type: %v", n.Operand.GetType())
				n.SetType(types.ErrorType{})
			}
		case parser.UnaryOpReference:
			n.SetType(types.PointerType{
				Base:    n.Operand.GetType(),
				Mutable: false,
			})
		case parser.UnaryOpMutableReference:
			n.SetType(types.PointerType{
				Base:    n.Operand.GetType(),
				Mutable: true,
			})
		case parser.UnaryOpDereference:
			if ptr, ok := types.Underlying(n.Operand.GetType()).(types.PointerType); ok {
				n.SetType(ptr.Base)
			} else {
				a.errorf(n, "cannot dereference non-pointer type: %v", n.Operand.GetType())
				n.SetType(types.ErrorType{})
			}
		case parser.UnaryOpSliceLen:
			switch types.Underlying(n.Operand.GetType()).(type) {
			case types.SliceType:
				n.SetType(types.PrimitiveUsz)
			default:
				a.errorf(n, "cannot get length of non-slice type: %v", n.Operand.GetType())
				n.SetType(types.ErrorType{})
			}
		case parser.UnaryOpBitwiseNot:
			if types.IsInteger(n.Operand.GetType()) {
				n.SetType(n.Operand.GetType())
			} else {
				a.errorf(n, "cannot apply bitwise not operator to non-integer type: %v", n.Operand.GetType())
				n.SetType(types.ErrorType{})
			}
		default:
			panic("unhandled unary operator")
		}

	case *parser.IndexExprNode:
		a.attributeExpr(n.Subject)
		a.attributeExpr(n.Index)

		switch t := types.Underlying(n.Subject.GetType()).(type) {
		case types.SliceType:
			n.SetType(t.Base)
		case types.PointerType:
			n.SetType(t.Base)
		default:
			a.errorf(n, "cannot index type %v", n.Subject.GetType())
			n.SetType(types.ErrorType{})
		}

		switch n.Index.GetType().(type) {
		case types.UntypedInt:
			n.Index = &parser.CastNode{
				Operand: n.Index,
				Type:    types.PrimitiveUsz,
			}
		case types.UntypedFloat:
			a.errorf(n, "cannot use untyped float as index; cast to integer type")
			n.SetType(types.ErrorType{})
		}

	case *parser.FieldAccessNode:
		if a.attributeMethodValue(n) {
			break
		}
		if ident, ok := n.Subject.(*parser.IdentifierNode); ok && ident.Symbol != nil && ident.Symbol.Kind == symbols.SymbolKindType {
			if enumType, ok := types.Underlying(ident.Symbol.TypeInfo).(types.EnumType); ok {
				ident.SetType(enumType)
				value, exists := enumType.VariantValue(n.Field.Name)
				if !exists {
					a.errorf(n, "enum %s has no variant %q", enumType, n.Field.Name)
					n.SetType(types.ErrorType{})
				} else {
					n.IsEnumValue = true
					n.EnumValue = value
					n.SetType(ident.Symbol.TypeInfo)
				}
				break
			}
		}
		if ident, ok := n.Subject.(*parser.IdentifierNode); ok &&
			ident.Symbol != nil && ident.Symbol.Kind == symbols.SymbolKindModule {
			a.errorf(n, "module-qualified names use ':' rather than '.'")
			n.SetType(types.ErrorType{})
			break
		}

		a.attributeExpr(n.Subject)
		subjectType := types.Underlying(n.Subject.GetType())
		if ptr, ok := subjectType.(types.PointerType); ok {
			subjectType = types.Underlying(ptr.Base)
		}
		switch t := subjectType.(type) {
		case types.StructType:
			found := false
			for _, field := range t.Fields {
				if field.L == n.Field.Name {
					n.SetType(field.R)
					found = true
					break
				}
				if field.L == "" {
					if embedded, ok := field.R.(types.UnionType); ok {
						for _, unionField := range embedded.Fields {
							if unionField.L == n.Field.Name {
								n.SetType(unionField.R)
								found = true
								break
							}
						}
					}
				}
				if found {
					break
				}
			}
			if !found {
				a.errorf(n, "type %v does not have a field named %q", fieldOwnerDisplayType(n.Subject.GetType()), n.Field.Name)
				n.SetType(types.ErrorType{})
			}
		case types.UnionType:
			found := false
			for _, field := range t.Fields {
				if field.L == n.Field.Name {
					n.SetType(field.R)
					found = true
					break
				}
			}
			if !found {
				a.errorf(n, "type %v does not have a field named %q", fieldOwnerDisplayType(n.Subject.GetType()), n.Field.Name)
				n.SetType(types.ErrorType{})
			}
		default:
			a.errorf(n, "cannot access field of type %v", n.Subject.GetType())
			n.SetType(types.ErrorType{})
		}

	case *parser.BinaryOpNode:
		a.attributeExpr(n.Operand1)
		a.attributeExpr(n.Operand2)
		if literal, ok := n.Operand1.(*parser.EnumLiteralNode); ok && isUnresolvedEnum(literal.GetType()) {
			if _, ok := types.Underlying(n.Operand2.GetType()).(types.EnumType); ok {
				a.resolveEnumLiteral(literal, n.Operand2.GetType())
			}
		}
		if literal, ok := n.Operand2.(*parser.EnumLiteralNode); ok && isUnresolvedEnum(literal.GetType()) {
			if _, ok := types.Underlying(n.Operand1.GetType()).(types.EnumType); ok {
				a.resolveEnumLiteral(literal, n.Operand1.GetType())
			}
		}
		for _, operand := range []parser.ExpressionNode{n.Operand1, n.Operand2} {
			if literal, ok := operand.(*parser.EnumLiteralNode); ok && isUnresolvedEnum(literal.GetType()) {
				a.errorf(literal, "cannot infer enum type for .%s", literal.Variant)
				literal.SetType(types.ErrorType{})
			}
		}

		t1 := n.Operand1.GetType()
		t2 := n.Operand2.GetType()

		switch n.Op {
		case parser.BinaryOpAdd, parser.BinaryOpSubtract, parser.BinaryOpMultiply, parser.BinaryOpDivide, parser.BinaryOpModulo:
			p1, pointer1 := types.Underlying(t1).(types.PointerType)
			p2, pointer2 := types.Underlying(t2).(types.PointerType)
			if n.Op == parser.BinaryOpAdd && pointer1 && types.IsInteger(t2) {
				n.SetType(t1)
			} else if n.Op == parser.BinaryOpAdd && types.IsInteger(t1) && pointer2 {
				n.SetType(t2)
			} else if n.Op == parser.BinaryOpSubtract && pointer1 && types.IsInteger(t2) {
				n.SetType(t1)
			} else if n.Op == parser.BinaryOpSubtract && pointer1 && pointer2 && p1.Base.Equals(p2.Base) {
				n.SetType(types.PrimitiveIsz)
			} else if types.IsNumeric(t1) && types.IsNumeric(t2) {
				got := types.PromoteNumeric(t1, t2)
				if got.Equals(types.ErrorType{}) {
					a.errorf(n, "incompatible types for binary operator: %v and %v", t1, t2)
					n.SetType(types.ErrorType{})
				} else {
					n.SetType(got)
				}
			} else {
				a.errorf(n, "cannot apply arithmetic operator to non-numeric types: %v and %v", t1, t2)
				n.SetType(types.ErrorType{})
			}
		case parser.BinaryOpEqual, parser.BinaryOpNotEqual, parser.BinaryOpLess, parser.BinaryOpLessEqual, parser.BinaryOpGreater, parser.BinaryOpGreaterEqual:
			if types.IsNumeric(t1) && types.IsNumeric(t2) {
				n.SetType(types.PrimitiveBool)
			} else if t1.Equals(t2) {
				n.SetType(types.PrimitiveBool)
			} else if types.IsPointer(t1) && types.IsPointer(t2) {
				n.SetType(types.PrimitiveBool)
			} else {
				a.errorf(n, "cannot compare values of types %v and %v", t1, t2)
				n.SetType(types.ErrorType{})
			}
		case parser.BinaryOpLogicalAnd, parser.BinaryOpLogicalOr:
			if t1.Equals(types.PrimitiveBool) && t2.Equals(types.PrimitiveBool) {
				n.SetType(types.PrimitiveBool)
			} else {
				a.errorf(n, "cannot apply logical operator to non-boolean types: %v and %v", t1, t2)
				n.SetType(types.ErrorType{})
			}
		case parser.BinaryOpBitwiseAnd, parser.BinaryOpBitwiseXor, parser.BinaryOpBitwiseOr,
			parser.BinaryOpShiftLeft, parser.BinaryOpShiftRight:
			if types.IsInteger(t1) && types.IsInteger(t2) {
				got := types.PromoteNumeric(t1, t2)
				if got.Equals(types.ErrorType{}) {
					a.errorf(n, "incompatible integer types for bitwise operator: %v and %v", t1, t2)
					n.SetType(types.ErrorType{})
				} else {
					n.SetType(got)
				}
			} else {
				a.errorf(n, "cannot apply bitwise operator to non-integer types: %v and %v", t1, t2)
				n.SetType(types.ErrorType{})
			}
		default:
			panic("unhandled binary operator")
		}

	case *parser.CastNode:
		a.attributeExpr(n.Operand)
		target := a.analyser.resolveTypeNode(n.ToType)
		n.SetType(target)

	case *parser.TypeTestNode:
		a.attributeExpr(n.Operand)
		n.TargetType = a.analyser.resolveTypeNode(n.Target)
		n.SetType(types.PrimitiveBool)

	case *parser.ImplementsTestNode:
		if ident, ok := n.Operand.(*parser.IdentifierNode); ok && ident.Symbol != nil && ident.Symbol.Kind == symbols.SymbolKindType {
			n.CompileTime = true
			n.ConcreteType = ident.Symbol.TypeInfo
		} else {
			a.attributeExpr(n.Operand)
		}
		n.TargetType = a.analyser.resolveTypeNode(n.Target)
		n.SetType(types.PrimitiveBool)

	case *parser.SizeOfNode:
		n.SetType(types.PrimitiveUsz)
		n.OperandType = a.analyser.resolveTypeNode(n.Operand)

	case *parser.SizeOfExprNode:
		a.attributeExpr(n.Operand)
		n.SetType(types.PrimitiveUsz)
		n.OperandType = n.Operand.GetType()

	case *parser.AlignOfNode:
		n.SetType(types.PrimitiveUsz)
		if n.Expression != nil {
			a.attributeExpr(n.Expression)
			n.OperandType = n.Expression.GetType()
		} else {
			n.OperandType = a.analyser.resolveTypeNode(n.Operand)
		}

	case *parser.OffsetOfNode:
		n.SetType(types.PrimitiveUsz)
		n.OperandType = a.analyser.resolveTypeNode(n.Operand)

	default:
		panic(fmt.Sprintf("unexpected expression type: %T\n", node))
	}

	if node.GetType() == nil {
		panic("expression without type")
	}
}

func fieldOwnerDisplayType(t types.Type) types.Type {
	if ptr, ok := types.Underlying(t).(types.PointerType); ok {
		return ptr.Base
	}
	return t
}

func (a *Attributor) attributeMethodValue(n *parser.FieldAccessNode) bool {
	ident, ok := n.Subject.(*parser.IdentifierNode)
	if !ok || ident.Symbol == nil || ident.Symbol.Kind != symbols.SymbolKindType {
		return false
	}
	module, owner, _, ok := methodOwnerIdentity(ident.Symbol.TypeInfo)
	if !ok {
		return false
	}
	method := a.analyser.methods[module+":"+owner][n.Field.Name]
	if method == nil {
		return false
	}
	if method.DefinitionModule != a.analyser.currentMod && !method.Public {
		a.errorf(n, "method %q is not public", n.Field.Name)
		n.SetType(types.ErrorType{})
		return true
	}
	ret := method.Signature.ReturnType
	if ret == nil {
		ret = types.PrimitiveVoid
	}
	n.MethodSymbol = method
	n.MethodModule = method.DefinitionModule
	n.SetType(types.PointerType{Base: types.FunctionType{Parameters: method.Signature.Parameters, ReturnType: ret, TypedVariadic: method.Signature.TypedVariadic, VariadicElement: method.Signature.VariadicElement}})
	return true
}

func (a *Attributor) attributeMethodCall(n *parser.FunctionCallNode) bool {
	member, ok := n.Callee.(*parser.FieldAccessNode)
	if !ok {
		return false
	}
	if ident, ok := member.Subject.(*parser.IdentifierNode); ok && ident.Symbol != nil && ident.Symbol.Kind == symbols.SymbolKindType {
		return false
	}
	a.attributeExpr(member.Subject)
	if traitPtr, ok := traitPointer(member.Subject.GetType()); ok {
		for slot, requirement := range traitPtr.Trait.Methods {
			if requirement.Name != member.Field.Name {
				continue
			}
			if requirement.Receiver == types.TraitReceiverMutablePointer && !traitPtr.Mutable {
				a.errorf(n, "method %q requires mutable trait access", requirement.Name)
				n.SetType(types.ErrorType{})
				return true
			}
			params := append([]types.Type{member.Subject.GetType()}, requirement.Parameters...)
			n.Symbol = symbols.NewFunction(requirement.Name, &symbols.FunctionSignature{Parameters: params, RequiredParameters: len(params), ReturnType: requirement.ReturnType})
			n.Args = append([]parser.ExpressionNode{member.Subject}, n.Args...)
			n.Method = true
			n.TraitCall = true
			n.TraitSlot = slot
			return true
		}
		return false
	}
	module, owner, receiverIsPointer, ok := methodOwnerIdentity(member.Subject.GetType())
	if !ok {
		return false
	}
	method := a.analyser.methods[module+":"+owner][member.Field.Name]
	if method == nil {
		return false
	}
	if method.StaticMethod {
		return false
	}
	if method.DefinitionModule != a.analyser.currentMod && !method.Public {
		a.errorf(n, "method %q is not public", member.Field.Name)
		n.SetType(types.ErrorType{})
		return true
	}
	receiver := member.Subject
	expected := method.Signature.Parameters[0]
	_, expectsPointer := types.Underlying(expected).(types.PointerType)
	if expectsPointer && !receiverIsPointer {
		op := parser.UnaryOpReference
		if ptr := types.Underlying(expected).(types.PointerType); ptr.Mutable {
			op = parser.UnaryOpMutableReference
		}
		receiver = &parser.UnaryOpNode{Op: op, Operand: receiver, Loc: receiver.GetLoc()}
	} else if !expectsPointer && receiverIsPointer {
		receiver = &parser.UnaryOpNode{Op: parser.UnaryOpDereference, Operand: receiver, Loc: receiver.GetLoc()}
	}
	n.Args = append([]parser.ExpressionNode{receiver}, n.Args...)
	n.Symbol = method
	n.Method = true
	if method.DefinitionModule != a.analyser.currentMod {
		n.Name = &parser.IdentifierNode{Name: method.Name, Module: method.DefinitionModule, ResolvedModuleName: method.DefinitionModule, Loc: member.Loc, Symbol: method}
	}
	return true
}

func methodOwnerIdentity(t types.Type) (module, name string, pointer bool, ok bool) {
	if slice, isSlice := types.Underlying(t).(types.SliceType); isSlice && slice.Base.Equals(types.PrimitiveChar) {
		return "builtin", "str", false, true
	}
	if ptr, isPointer := types.Underlying(t).(types.PointerType); isPointer && !ptr.Mutable && ptr.Base.Equals(types.PrimitiveChar) {
		return "builtin", "cstr", true, true
	}
	if ptr, isPointer := types.Underlying(t).(types.PointerType); isPointer {
		t = ptr.Base
		pointer = true
	}
	switch t := t.(type) {
	case types.DefinedType:
		return t.Module, t.Name, pointer, true
	case *types.AliasRef:
		return t.Module, t.Name, pointer, true
	case types.PrimitiveType:
		return "builtin", t.String(), pointer, true
	default:
		return "", "", pointer, false
	}
}

func isUnresolvedEnum(t types.Type) bool {
	_, ok := t.(types.UnresolvedEnum)
	return ok
}

func (a *Attributor) resolveEnumLiteral(n *parser.EnumLiteralNode, expected types.Type) {
	enumType := types.Underlying(expected).(types.EnumType)
	value, ok := enumType.VariantValue(n.Variant)
	if !ok {
		a.errorf(n, "enum %s has no variant %q", enumType, n.Variant)
		n.SetType(types.ErrorType{})
		return
	}
	n.Value = value
	n.SetType(expected)
}

func collectFunctionReturnNodes(body []parser.Node) []*parser.ControlKeywordNode {
	var returnNodes []*parser.ControlKeywordNode

	for _, stmt := range body {
		switch n := stmt.(type) {
		case *parser.ControlKeywordNode:
			if n.Keyword == tokeniser.KeywordReturn {
				returnNodes = append(returnNodes, n)
			}
		case *parser.IfNode:
			returnNodes = append(returnNodes, collectFunctionReturnNodes(n.IfBranch.Node.Body)...)
			for _, elif := range n.ElseIfBranches {
				returnNodes = append(returnNodes, collectFunctionReturnNodes(elif.Node.Body)...)
			}
			if n.ElseBranch != nil {
				returnNodes = append(returnNodes, collectFunctionReturnNodes(n.ElseBranch.Body)...)
			}
		case *parser.ForNode:
			returnNodes = append(returnNodes, collectFunctionReturnNodes(n.Body.Body)...)
		case *parser.BlockNode:
			returnNodes = append(returnNodes, collectFunctionReturnNodes(n.Body)...)
		}
	}

	return returnNodes
}
