package typechecker

import (
	"fmt"
	"runtime"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

var (
	_ = fmt.Println
	_ = runtime.GOOS
)

type VarSig struct {
	Type    shared.Type
	Mutable bool
}

type FunctionSigArg struct {
	Name    string
	Type    shared.Type
	Mutable bool
}

type FunctionSig struct {
	Name           string
	ArgTypes       []FunctionSigArg
	HasVariadic    bool
	RetType        shared.Type
	ImplicitReturn bool
	ExternFrom     string
	Pub            bool
}

type FuncTable map[string]*FunctionSig

func (ft FuncTable) Define(name string, fsig *FunctionSig) bool {
	if _, ok := ft[name]; ok {
		return false
	}
	ft[name] = fsig
	return true
}

func (ft FuncTable) Lookup(name string) (*FunctionSig, bool) {
	fsig, ok := ft[name]
	return fsig, ok
}

type TypeChecker struct {
	VarTable *shared.SymbolTable[*VarSig]
	ModSigs  ModulesSignatures
	Mod      string
	Imports  []string
}

func NewTypeChecker(mod string, ms ModulesSignatures) *TypeChecker {
	return &TypeChecker{
		VarTable: shared.NewSymbolTable[*VarSig](),
		ModSigs:  ms,
		Mod:      mod,
	}
}

func CollectImports(rn *parser.RootNode) []string {
	imports := []string{}
	for _, n := range rn.Body {
		if imn, ok := n.(*parser.ImportNode); ok {
			imports = append(imports, imn.Modules...)
		}
	}
	return imports
}

func (tc *TypeChecker) TypeCheck(ast *parser.RootNode) (*parser.RootNode, error) {
	tc.Imports = CollectImports(ast)
	for _, n := range ast.Body {
		switch node := n.(type) {
		case *parser.FunctionDefNode:
			sig, ok := tc.ModSigs.LookupFunction(tc.Mod, node.Name, tc.Mod, tc.Imports, true)
			if !ok {
				return nil, shared.NewError(node.Loc, "fatal: function %s should have been in the signature table", node.Name)
			}

			tc.enterScope()
			if err := tc.typeCheckFunction(node, sig); err != nil {
				return nil, err
			}
			tc.exitScope()
		}
	}

	return ast, nil
}

func (tc *TypeChecker) enterScope() {
	tc.VarTable.EnterScope()
}

func (tc *TypeChecker) exitScope() {
	tc.VarTable.ExitScope()
}

func (tc *TypeChecker) typeCheckFunction(funcNode *parser.FunctionDefNode, sig *FunctionSig) error {
	for _, arg := range sig.ArgTypes {
		tc.VarTable.Define(arg.Name, &VarSig{
			Type:    arg.Type,
			Mutable: arg.Mutable,
		})
	}

	if funcNode.Body == nil {
		return nil
	}
	switch body := funcNode.Body.(type) {
	case *parser.BlockNode:
		_, err := tc.typeCheckBlock(body, sig, false, true)
		return err
	default:
		if expr, ok := body.(parser.ExpressionNode); ok {
			exprType, err := tc.typeCheckExpression(expr, sig.RetType)
			if err != nil {
				return err
			}

			if sig.RetType == shared.PRIMITIVE_VOID {
				sig.RetType = exprType
			}

			if !shared.CanCoerceTo(exprType, sig.RetType) {
				var loc shared.Location
				if funcNode.RetType.Type != nil {
					loc = funcNode.RetType.Type.Loc
				} else {
					loc = funcNode.Loc
				}
				return shared.NewError(loc, "function '%s' expects return type '%s' but returns '%s'",
					funcNode.Name, sig.RetType, exprType)
			}

			if expr.GetType() == shared.PRIMITIVE_UNTYPED_INT && sig.RetType != shared.PRIMITIVE_VOID {
				SetNodeType(expr, sig.RetType)
			}
			return nil
		}
		return shared.NewError(body.GetLoc(), "function body must be a block or an expression")
	}
}

func (tc *TypeChecker) typeCheckBlock(blockNode *parser.BlockNode, sig *FunctionSig, isLoop bool, isMainBody bool) (bool, error) {
	foundReturn := false

	for i, n := range blockNode.Body {
		switch node := n.(type) {
		case *parser.DeclarationNode:
			varName, varSig, err := tc.typeCheckDeclaration(node)
			if err != nil {
				return false, err
			}
			if ok := tc.VarTable.Define(varName, varSig); !ok {
				return false, shared.NewError(node.Loc,
					"variable '%s' is already declared in this scope", varName)
			}

		case *parser.FunctionDefNode:
			return false, shared.NewError(node.Loc, "closures are not supported")

		case *parser.AssignmentNode:
			if err := tc.typeCheckAssignment(node); err != nil {
				return false, err
			}

		case *parser.ArrayAssignmentNode:
			if err := tc.typeCheckArrayAssignment(node); err != nil {
				return false, err
			}

		case *parser.FunctionCallNode:
			if _, err := tc.typeCheckFunctionCall(node); err != nil {
				return false, err
			}

		case *parser.ControlKeywordNode:
			switch node.Keyword {
			case tokeniser.KEYWORD_RETURN:
				if sig == nil {
					return false, shared.NewError(node.Loc, "cannot return from here")
				}
				var retType shared.Type = shared.PRIMITIVE_VOID
				if node.ReturnValue != nil {
					var err error
					retType, err = tc.typeCheckExpression(node.ReturnValue, sig.RetType)
					if err != nil {
						return false, err
					}

					if !shared.CanCoerceTo(retType, sig.RetType) {
						return false, shared.NewError(node.Loc,
							"wrong return type for function, expected '%s' got '%s'", sig.RetType, retType)
					}
				} else if sig.RetType != shared.PRIMITIVE_VOID {
					return false, shared.NewError(node.Loc,
						"wrong return type for function, expected '%s' got void", sig.RetType)
				}
				foundReturn = true

				if i != len(blockNode.Body)-1 {
					return false, shared.NewError(blockNode.Body[i+1].GetLoc(), "dead code following return statement")
				}

			case tokeniser.KEYWORD_BREAK, tokeniser.KEYWORD_CONTINUE:
				if !isLoop {
					return false, shared.NewError(node.Loc,
						"'%s' statement outside a loop", node.Keyword)
				}
			}

		case *parser.BlockNode:
			if sig != nil {
				tc.enterScope()
			}
			returns, err := tc.typeCheckBlock(node, sig, isLoop, false)
			if err != nil {
				return false, err
			}
			if sig != nil {
				tc.exitScope()
			}

			if i == len(blockNode.Body)-1 && !foundReturn {
				foundReturn = returns
			}

		case *parser.ForNode:
			if err := tc.typeCheckForLoop(node, sig); err != nil {
				return false, err
			}

		case *parser.IfNode:
			if sig != nil {
				tc.enterScope()
			}
			returns, err := tc.typeCheckIfStatement(node, sig, isLoop)
			if err != nil {
				return false, err
			}
			if sig != nil {
				tc.exitScope()
			}

			if i == len(blockNode.Body)-1 && !foundReturn {
				foundReturn = returns
			}

		default:
			return false, shared.NewError(node.GetLoc(),
				"unexpected node in block when type checking")
		}
	}

	if isMainBody {
		if !foundReturn && sig.RetType != shared.PRIMITIVE_VOID {
			return false, shared.NewError(blockNode.Loc,
				"function with return type '%s' is missing a return statement", sig.RetType)
		}

		sig.ImplicitReturn = !foundReturn
	}

	return foundReturn, nil
}

func (tc *TypeChecker) typeCheckExpression(en parser.ExpressionNode, expectedType shared.Type) (shared.Type, error) {
	switch exprNode := en.(type) {
	case *parser.IdentifierNode:
		varSig, ok := tc.VarTable.Lookup(exprNode.Name)
		if !ok {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc,
				"undefined variable '%s'", exprNode.Name)
		}
		SetNodeType(exprNode, varSig.Type)
		gotType, err := ResolveFieldChain(exprNode, varSig.Type)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}
		return gotType, nil

	case *parser.IntegerLiteralNode:
		exprType := shared.PRIMITIVE_UNTYPED_INT
		if expectedType != shared.PRIMITIVE_VOID &&
			shared.CanCoerceTo(exprType, expectedType) {
			exprNode.ExprType = expectedType
			return expectedType, nil
		}
		exprNode.ExprType = exprType
		return exprType, nil

	case *parser.FloatLiteralNode:
		exprType := shared.PRIMITIVE_F64
		if expectedType != shared.PRIMITIVE_VOID &&
			shared.CanCoerceTo(exprType, expectedType) {
			exprNode.ExprType = expectedType
			return expectedType, nil
		}
		exprNode.ExprType = exprType
		return exprType, nil

	case *parser.CharLiteralNode:
		exprNode.ExprType = shared.PRIMITIVE_CHAR
		return shared.PRIMITIVE_CHAR, nil

	case *parser.StringLiteralNode:
		char_ptr := shared.Pointer{
			To: shared.PRIMITIVE_CHAR,
		}
		exprNode.ExprType = char_ptr
		return char_ptr, nil

	case *parser.BoolLiteralNode:
		exprNode.ExprType = shared.PRIMITIVE_BOOL
		return shared.PRIMITIVE_BOOL, nil

	case *parser.NilLiteralNode:
		return exprNode.GetType(), nil

	case *parser.FunctionCallNode:
		return tc.typeCheckFunctionCall(exprNode)

	case *parser.UnaryOpNode:
		operandType, err := tc.typeCheckExpression(exprNode.Operand, shared.PRIMITIVE_VOID)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}

		if expectedType != shared.PRIMITIVE_VOID &&
			operandType == shared.PRIMITIVE_UNTYPED_INT && shared.IsNumericType(expectedType) {
			SetNodeType(exprNode.Operand, expectedType)
			operandType = expectedType
		}

		switch exprNode.Op {
		case parser.UNARY_OP_LOGICAL_NOT:
			if operandType != shared.PRIMITIVE_BOOL {
				return shared.PRIMITIVE_VOID, shared.NewError(
					exprNode.Operand.GetLoc(), "unary operator 'not' expects a boolean operand, found '%s'", operandType)
			}
			exprNode.ExprType = shared.PRIMITIVE_BOOL
			return shared.PRIMITIVE_BOOL, nil
		case parser.UNARY_OP_NEGATE:
			if !shared.IsNumericType(operandType) {
				return shared.PRIMITIVE_VOID, shared.NewError(
					exprNode.Operand.GetLoc(), "unary operator '-' requires numeric operand, found '%s'", operandType)
			}
			exprNode.ExprType = operandType
			return operandType, nil
		case parser.UNARY_OP_DEREFERENCE:
			if !operandType.IsPointer() {
				return shared.PRIMITIVE_VOID, shared.NewError(
					exprNode.Operand.GetLoc(), "unary operator '*' requires a pointer operand, found '%s'", operandType)
			}
			t := operandType.(shared.Pointer).To
			exprNode.ExprType = t
			return t, nil
		case parser.UNARY_OP_REFERENCE:
			switch operand := exprNode.Operand.(type) {
			case *parser.IdentifierNode:
				varName := operand.Name
				varSig, ok := tc.VarTable.Lookup(varName)
				if !ok {
					return shared.PRIMITIVE_VOID, shared.NewError(
						exprNode.Operand.GetLoc(), "undefined variable '%s'", varName)
				}
				t := shared.Pointer{
					To:    operandType,
					Const: !varSig.Mutable,
				}
				exprNode.ExprType = t
				return t, nil
			case *parser.StructLiteralNode:
				t := shared.Pointer{
					To: operandType,
				}
				exprNode.ExprType = t
				return t, nil
			default:
				return shared.PRIMITIVE_VOID, shared.NewError(
					exprNode.Operand.GetLoc(), "unary operator '&' requires an identifier or struct literal operand")
			}
		case parser.UNARY_OP_ARRAY_LEN:
			if !operandType.IsArray() {
				return shared.PRIMITIVE_VOID, shared.NewError(
					exprNode.Operand.GetLoc(), "unary operator '[]' requires an array operand, found '%s'", operandType)
			}
			exprNode.ExprType = shared.PRIMITIVE_U64
			return shared.PRIMITIVE_U64, nil
		default:
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "unknown operator")
		}

	case *parser.BinaryOpNode:
		leftType, err := tc.typeCheckExpression(exprNode.Operand1, shared.PRIMITIVE_VOID)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}
		rightType, err := tc.typeCheckExpression(exprNode.Operand2, shared.PRIMITIVE_VOID)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}

		if expectedType != shared.PRIMITIVE_VOID {
			if leftType == shared.PRIMITIVE_UNTYPED_INT && shared.IsNumericType(expectedType) {
				SetNodeType(exprNode.Operand1, expectedType)
				leftType = expectedType
			}
			if rightType == shared.PRIMITIVE_UNTYPED_INT && shared.IsNumericType(expectedType) {
				SetNodeType(exprNode.Operand2, expectedType)
				rightType = expectedType
			}
		}

		switch exprNode.Op {
		case parser.BINARY_OP_LOGICAL_AND, parser.BINARY_OP_LOGICAL_OR:
			if leftType != shared.PRIMITIVE_BOOL || rightType != shared.PRIMITIVE_BOOL {
				return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "operator expects boolean operands")
			}
			exprNode.ExprType = shared.PRIMITIVE_BOOL
			return shared.PRIMITIVE_BOOL, nil

		case parser.BINARY_OP_EQUAL, parser.BINARY_OP_NOT_EQUAL,
			parser.BINARY_OP_LESS, parser.BINARY_OP_LESS_EQUAL,
			parser.BINARY_OP_GREATER, parser.BINARY_OP_GREATER_EQUAL:

			if !shared.IsNumericType(leftType) || !shared.IsNumericType(rightType) {
				return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc,
					"operator cannot be applied to operands")
			}

			exprNode.ExprType = shared.PRIMITIVE_BOOL
			return shared.PRIMITIVE_BOOL, nil

		case parser.BINARY_OP_ADD, parser.BINARY_OP_SUBTRACT,
			parser.BINARY_OP_MULTIPLY, parser.BINARY_OP_DIVIDE,
			parser.BINARY_OP_MODULO:

			if (leftType.IsPointer() && shared.IsIntegerType(rightType)) ||
				(rightType.IsPointer() && shared.IsIntegerType(leftType)) {

				if rightType.IsPointer() {
					if leftType.Compare(shared.PRIMITIVE_UNTYPED_INT) {
						SetNodeType(exprNode.Operand1, rightType)
					}
					exprNode.ExprType = rightType
				} else {
					if rightType.Compare(shared.PRIMITIVE_UNTYPED_INT) {
						SetNodeType(exprNode.Operand2, leftType)
					}
					exprNode.ExprType = leftType
				}
				return exprNode.ExprType, nil
			}

			if !(shared.IsNumericType(leftType) || shared.IsNumericType(rightType)) {
				return shared.PRIMITIVE_VOID, shared.NewError(
					exprNode.Loc,
					"operator requires numeric operands or a pointer and an integer",
				)
			}

			if exprNode.Op == parser.BINARY_OP_MODULO &&
				(shared.IsFloatType(leftType) || shared.IsFloatType(rightType)) {
				return shared.PRIMITIVE_VOID, shared.NewError(
					exprNode.Loc,
					"modulo operator requires integer operands",
				)
			}

			var commonType shared.Type
			if leftType.IsPointer() {
				commonType = leftType
			} else if rightType.IsPointer() {
				commonType = rightType
			} else {
				commonType = shared.BiggerNumericType(leftType, rightType)
			}

			exprNode.ExprType = commonType
			return commonType, nil

		default:
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "unknown operator")
		}

	case *parser.IndexExprNode:
		arrayType, err := tc.typeCheckExpression(exprNode.Subject, shared.PRIMITIVE_VOID)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}
		if !arrayType.IsArray() {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Subject.GetLoc(),
				"indexing operator requires an array type, found '%s'", arrayType)
		}
		indexType, err := tc.typeCheckExpression(exprNode.Index, shared.PRIMITIVE_U64)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}
		if !shared.IsIntegerType(indexType) {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Index.GetLoc(),
				"array index must be an integer type, found '%s'", indexType)
		}
		elemType := arrayType.(shared.Array).Of
		exprNode.ExprType = elemType
		return elemType, nil

	case *parser.CastNode:
		tpe, ok := tc.ModSigs.LookupType(exprNode.ToType, tc.Mod, tc.Imports)
		if !ok {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "no such type '%s'", tpe)
		}
		exTpe, err := tc.typeCheckExpression(exprNode.Operand, shared.PRIMITIVE_VOID)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}

		if exTpe == shared.PRIMITIVE_UNTYPED_INT {
			SetNodeType(exprNode.Operand, tpe)
		}

		if shared.CanCastTo(exTpe, tpe) {
			if exTpe.IsArray() && exTpe.(shared.Array).Len > 0 {
				tpe = shared.Array{
					Of:  tpe.(shared.Array).Of,
					Len: exTpe.(shared.Array).Len,
				}
				SetNodeType(exprNode.Operand, tpe)
			}
			SetNodeType(exprNode, tpe)
			return tpe, nil
		} else {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc,
				"cannot cast type '%s' to '%s'", exTpe, tpe)
		}

	case *parser.SizeOfNode:
		if expectedType != shared.PRIMITIVE_VOID && !shared.IsNumericType(expectedType) {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc,
				"sizeof expression produces a numeric type, but expected type is '%s'", expectedType)
		}

		tpe, ok := tc.ModSigs.LookupType(exprNode.Operand, tc.Mod, tc.Imports)
		if !ok {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "no such type '%s'", tpe)
		}
		if expectedType == shared.PRIMITIVE_VOID {
			exprNode.ExprType = shared.PRIMITIVE_UNTYPED_INT
		} else {
			exprNode.ExprType = expectedType
		}
		return exprNode.ExprType, nil

	case *parser.IfExprNode:
		condType, err := tc.typeCheckExpression(exprNode.IfBranch.Condition, shared.PRIMITIVE_BOOL)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}
		if condType != shared.PRIMITIVE_BOOL {
			return shared.PRIMITIVE_VOID, shared.NewError(
				exprNode.IfBranch.Condition.GetLoc(),
				"if condition must be boolean, found '%s'", condType)
		}

		ifType, err := tc.typeCheckExpression(exprNode.IfBranch.Node, expectedType)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}
		commonType := ifType

		for _, elseIf := range exprNode.ElseIfBranches {
			elseIfCondType, err := tc.typeCheckExpression(elseIf.Condition, shared.PRIMITIVE_BOOL)
			if err != nil {
				return shared.PRIMITIVE_VOID, err
			}
			if elseIfCondType != shared.PRIMITIVE_BOOL {
				return shared.PRIMITIVE_VOID, shared.NewError(
					elseIf.Condition.GetLoc(), "else-if condition must be boolean, found '%s'", elseIfCondType)
			}

			elseIfType, err := tc.typeCheckExpression(elseIf.Node, expectedType)
			if err != nil {
				return shared.PRIMITIVE_VOID, err
			}
			if elseIfType != commonType {
				return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc,
					"all branches must return same type, expected '%s' but found '%s'", commonType, elseIfType)
			}
		}

		elseType, err := tc.typeCheckExpression(exprNode.ElseBranch, expectedType)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}
		if elseType != commonType {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.ElseBranch.GetLoc(),
				"else branch must match type '%s', found '%s'", commonType, elseType)
		}

		exprNode.ExprType = commonType
		return commonType, nil

	case *parser.GivenExprNode:
		_, err := tc.typeCheckBlock(exprNode.Block, nil, false, false)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}
		exprType, err := tc.typeCheckExpression(exprNode.FinalExpr, expectedType)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}
		exprNode.ExprType = exprType
		return exprType, nil

	case *parser.StructLiteralNode:
		if exprNode.Name.Ident.Next != nil {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Name.Loc, "struct name cannot be qualified")
		}
		structType, ok := tc.ModSigs.LookupFlatType(exprNode.Name.ModName,
			exprNode.Name.Ident.Name, tc.Mod, tc.Imports)
		if !ok {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc,
				"no such struct type '%s'", exprNode.Name.Ident)
		}
		st, ok := structType.(shared.Struct)
		if !ok {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc,
				"'%s' is not a struct type", exprNode.Name.Ident)
		}
		if len(st.Fields) != len(exprNode.Fields) {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc,
				"struct '%s' expects %d fields, got %d (zero values are not allowed)",
				exprNode.Name.Ident, len(st.Fields), len(exprNode.Fields))
		}
		for _, field := range exprNode.Fields {
			var fieldType shared.Type = nil
			for _, f := range st.Fields {
				if f.L == field.L {
					fieldType = f.R
				}
			}
			if fieldType == nil {
				return shared.PRIMITIVE_VOID, shared.NewError(field.R.GetLoc(),
					"struct '%s' has no field named '%s'", exprNode.Name.Ident, field.L)
			}
			exprType, err := tc.typeCheckExpression(field.R, fieldType)
			if err != nil {
				return shared.PRIMITIVE_VOID, err
			}
			if !fieldType.Compare(exprType) {
				if !shared.CanCoerceTo(exprType, fieldType) {
					return shared.PRIMITIVE_VOID, shared.NewError(field.R.GetLoc(),
						"field '%s' of struct '%s' expects type '%s', got '%s'", field.L,
						exprNode.Name.Ident, fieldType, exprType)
				} else {
					SetNodeType(field.R, fieldType)
				}
			}
		}
		exprNode.ExprType = structType
		return structType, nil

	case *parser.ArrayLiteralNode:
		if len(exprNode.Elements) == 0 {
			return shared.PRIMITIVE_VOID, shared.NewError(exprNode.Loc, "array literals cannot be empty")
		}
		var elementType shared.Type = nil
		for i, elem := range exprNode.Elements {
			elemType, err := tc.typeCheckExpression(elem, shared.PRIMITIVE_VOID)
			if err != nil {
				return shared.PRIMITIVE_VOID, err
			}
			if elementType == nil {
				elementType = elemType
			} else if shared.IsNumericType(elementType) &&
				elemType == shared.PRIMITIVE_UNTYPED_INT {
				SetNodeType(elem, elementType)
			} else if shared.IsNumericType(elemType) &&
				elementType == shared.PRIMITIVE_UNTYPED_INT {
				elementType = elemType
				for j := range i {
					prevElem := exprNode.Elements[j]
					if prevElem.GetType() == shared.PRIMITIVE_UNTYPED_INT {
						SetNodeType(prevElem, elementType)
					}
				}
			} else if !elementType.Compare(elemType) {
				return shared.PRIMITIVE_VOID, shared.NewError(elem.GetLoc(),
					"array literal elements must be of the same type, expected '%s' but found '%s'", elementType, elemType)
			}
		}
		if elementType == shared.PRIMITIVE_UNTYPED_INT && expectedType.IsArray() &&
			shared.IsNumericType(expectedType.(shared.Array).Of) {
			for _, elem := range exprNode.Elements {
				SetNodeType(elem, expectedType.(shared.Array).Of)
			}
		}
		arrayType := shared.Array{
			Of:  elementType,
			Len: len(exprNode.Elements),
		}
		exprNode.ExprType = arrayType
		return arrayType, nil

	default:
		return shared.PRIMITIVE_VOID, shared.NewError(exprNode.GetLoc(), "unsupported expression type")
	}
}

func (tc *TypeChecker) typeCheckFunctionCall(funccallNode *parser.FunctionCallNode) (shared.Type, error) {
	fsig, ok := tc.ModSigs.LookupFunction(funccallNode.Name.ModName,
		funccallNode.Name.Ident.Name, tc.Mod, tc.Imports)
	if !ok {
		return shared.PRIMITIVE_VOID, shared.NewError(funccallNode.Name.Loc,
			"undefined function '%s'", funccallNode.Name)
	}

	if (len(funccallNode.Args) > len(fsig.ArgTypes) && !fsig.HasVariadic) ||
		len(funccallNode.Args) < len(fsig.ArgTypes) {
		return shared.PRIMITIVE_VOID, shared.NewError(funccallNode.Loc,
			"argument count mismatch, expected at least %d, got %d", len(fsig.ArgTypes), len(funccallNode.Args))
	}

	for i, arg := range funccallNode.Args {
		var fsigArgType shared.Type = shared.PRIMITIVE_VOID
		if i < len(fsig.ArgTypes) {
			fsigArgType = fsig.ArgTypes[i].Type
		}
		argType, err := tc.typeCheckExpression(arg, fsigArgType)
		if err != nil {
			return shared.PRIMITIVE_VOID, err
		}

		if !fsigArgType.Compare(argType) && !fsigArgType.Compare(shared.PRIMITIVE_VOID) {
			return shared.PRIMITIVE_VOID, shared.NewError(arg.GetLoc(),
				"argument %d of function '%s' has type '%s' but expected '%s'",
				i+1, funccallNode.Name, argType, fsigArgType,
			)
		}
	}

	funccallNode.ExprType = fsig.RetType
	return fsig.RetType, nil
}

func (tc *TypeChecker) typeCheckDeclaration(declNode *parser.DeclarationNode) (string, *VarSig, error) {
	mutable := declNode.Mutable
	var varType shared.Type = shared.PRIMITIVE_VOID

	if declNode.Type != nil {
		vt, ok := tc.ModSigs.LookupType(declNode.Type, tc.Mod, tc.Imports)
		if !ok {
			return "", nil, shared.NewError(declNode.Type.Loc,
				"variable '%s' has undefined type '%s'", declNode.Name, declNode.Type)
		}
		varType = vt
	}

	exprType, err := tc.typeCheckExpression(declNode.Value, varType)
	if err != nil {
		return "", nil, err
	}

	if varType == shared.PRIMITIVE_VOID {
		varType = exprType
	} else if !shared.CanCoerceTo(exprType, varType) {
		return "", nil, shared.NewError(declNode.Loc,
			"cannot assign value of type '%s' to variable '%s' of type '%s'",
			exprType, declNode.Name, varType,
		)
	}

	if varType == shared.PRIMITIVE_UNTYPED_INT {
		varType = shared.PRIMITIVE_I32
		SetNodeType(declNode.Value, varType)
		exprType = shared.PRIMITIVE_I32
	}
	if varType == shared.PRIMITIVE_VOID {
		return "", nil, shared.NewError(declNode.Loc, "a variable cannot be of type void")
	}

	if varType.IsArray() && varType.(shared.Array).Of.Compare(shared.PRIMITIVE_UNTYPED_INT) {
		if varType.(shared.Array).Of == shared.PRIMITIVE_UNTYPED_INT {
			return "", nil, shared.NewError(declNode.Loc,
				"a variable cannot be of type untyped int array")
		} else if shared.IsIntegerType(varType.(shared.Array).Of) {
			SetNodeType(declNode.Value, varType)
		} else {
			return "", nil, shared.NewError(declNode.Loc,
				"a variable cannot be of type untyped int array")
		}
	}

	if varType.IsArray() {
		switch dn := declNode.Value.(type) {
		case *parser.ArrayLiteralNode:
			varType = shared.Array{
				Of:  varType.(shared.Array).Of,
				Len: len(declNode.Value.(*parser.ArrayLiteralNode).Elements),
			}
		case *parser.IdentifierNode:
			idType, ok := tc.VarTable.Lookup(dn.Name)
			if ok {
				if arrType, ok := idType.Type.(shared.Array); ok {
					varType = shared.Array{
						Of:  varType.(shared.Array).Of,
						Len: arrType.Len,
					}
				}
			}
		}
		SetNodeType(declNode.Value, varType)
	}

	return declNode.Name, &VarSig{
		Type:    varType,
		Mutable: mutable,
	}, nil
}

func (tc *TypeChecker) typeCheckAssignment(asNode *parser.AssignmentNode) error {
	switch assignee := asNode.Assignee.(type) {
	case *parser.IdentifierNode:
		return tc.typeCheckIdentifierAssignment(asNode, assignee)
	case *parser.UnaryOpNode:
		if assignee.Op == parser.UNARY_OP_DEREFERENCE {
			return tc.typeCheckPointerAssignment(asNode, assignee)
		}
	}

	return shared.NewError(asNode.Assignee.GetLoc(), "left side of assignment must be a variable or dereferenced pointer")
}

func (tc *TypeChecker) typeCheckArrayAssignment(asNode *parser.ArrayAssignmentNode) error {
	varNameNode, ok := asNode.Assignee.(*parser.IdentifierNode)
	if !ok {
		return shared.NewError(asNode.Assignee.GetLoc(),
			"left side of array assignment must be an identifier")
	}
	varName := varNameNode.Name
	varSig, ok := tc.VarTable.Lookup(varName)
	if !ok {
		return shared.NewError(varNameNode.Loc, "undefined variable '%s'", varName)
	}
	if !varSig.Mutable {
		return shared.NewError(asNode.Loc, "cannot assign to immutable variable '%s'", varName)
	}
	currType, err := ResolveFieldChain(varNameNode, varSig.Type)
	if err != nil {
		return err
	}
	arrayType, ok := currType.(shared.Array)
	if !ok {
		return shared.NewError(asNode.Loc,
			fmt.Sprintf("variable '%s' is not an array", varName))
	}
	indexType, err := tc.typeCheckExpression(asNode.Index, shared.PRIMITIVE_U64)
	if err != nil {
		return err
	}
	if !shared.IsIntegerType(indexType) {
		return shared.NewError(asNode.Index.GetLoc(),
			"array index must be an integer type, found '%s'", indexType)
	}
	exprType, err := tc.typeCheckExpression(asNode.Value, arrayType.Of)
	if err != nil {
		return err
	}
	if !exprType.Compare(arrayType.Of) {
		return shared.NewError(asNode.Loc,
			"cannot assign value of type '%s' to array element of type '%s'",
			exprType, arrayType.Of,
		)
	}
	return nil
}

func (tc *TypeChecker) typeCheckIdentifierAssignment(asNode *parser.AssignmentNode, assignee *parser.IdentifierNode) error {
	varName := assignee.Name
	varSig, ok := tc.VarTable.Lookup(varName)
	if !ok {
		return shared.NewError(assignee.Loc, "undefined variable '%s'", varName)
	}

	if !varSig.Mutable && !varSig.Type.IsPointer() && assignee.Next == nil {
		return shared.NewError(asNode.Loc, "cannot assign to immutable variable '%s'", varName)
	}

	currType, err := ResolveFieldChain(assignee, varSig.Type)
	if err != nil {
		return err
	}

	exprType, err := tc.typeCheckExpression(asNode.Value, currType)
	if err != nil {
		return err
	}

	if !exprType.Compare(currType) {
		return shared.NewError(asNode.Loc,
			"cannot assign value of type '%s' to variable '%s' of type '%s'",
			exprType, varName, varSig.Type,
		)
	}
	return nil
}

func (tc *TypeChecker) typeCheckPointerAssignment(asNode *parser.AssignmentNode, assignee *parser.UnaryOpNode) error {
	typeWereDereferencing, err := tc.typeCheckExpression(assignee.Operand, shared.PRIMITIVE_VOID)
	if err != nil {
		return err
	}

	ptrType, ok := typeWereDereferencing.(shared.Pointer)
	if !ok {
		panic("expected pointer type after dereference")
	}

	exprType, err := tc.typeCheckExpression(asNode.Value, typeWereDereferencing)
	if err != nil {
		return err
	}

	if !exprType.Compare(ptrType.To) {
		if exprType == shared.PRIMITIVE_UNTYPED_INT && shared.IsNumericType(ptrType.To) {
			SetNodeType(asNode.Value, ptrType.To)
		} else {
			return shared.NewError(asNode.Loc,
				"cannot assign value of type '%s' to variable of type '%s'",
				exprType, ptrType.To,
			)
		}
	}
	assignee.ExprType = typeWereDereferencing
	return nil
}

func (tc *TypeChecker) typeCheckForLoop(loopNode *parser.ForNode, sig *FunctionSig) error {
	tc.enterScope()

	if len(loopNode.ExprsOrStmts) == 1 {
		condMb := loopNode.ExprsOrStmts[0]
		if _, ok := condMb.(parser.ExpressionNode); !ok {
			return shared.NewError(condMb.GetLoc(),
				"for loop condition must be a boolean expression")
		}
		cond := condMb.(parser.ExpressionNode)
		exprType, err := tc.typeCheckExpression(cond, shared.PRIMITIVE_BOOL)
		if err != nil {
			return err
		}

		if exprType != shared.PRIMITIVE_BOOL {
			return shared.NewError(cond.GetLoc(), "for loop condition must be a boolean expression, found '%s'", exprType)
		}
	} else if len(loopNode.ExprsOrStmts) == 3 {
		switch init := loopNode.ExprsOrStmts[0].(type) {
		case *parser.DeclarationNode:
			varName, varSig, err := tc.typeCheckDeclaration(init)
			if err != nil {
				return err
			}

			tc.VarTable.Define(varName, varSig)
		case *parser.AssignmentNode:
			if err := tc.typeCheckAssignment(init); err != nil {
				return err
			}
		default:
			return shared.NewError(init.GetLoc(), "for loop initialiser must be a declaration or assignment statement")
		}

		condMb := loopNode.ExprsOrStmts[1]
		if _, ok := condMb.(parser.ExpressionNode); !ok {
			return shared.NewError(condMb.GetLoc(),
				"for loop condition must be a boolean expression")
		}
		cond := condMb.(parser.ExpressionNode)
		exprType, err := tc.typeCheckExpression(cond, shared.PRIMITIVE_BOOL)
		if err != nil {
			return err
		}

		if exprType != shared.PRIMITIVE_BOOL {
			return shared.NewError(cond.GetLoc(), "for loop condition must be a boolean expression, found '%s'", exprType)
		}

		switch reass := loopNode.ExprsOrStmts[2].(type) {
		case *parser.AssignmentNode:
			if err := tc.typeCheckAssignment(reass); err != nil {
				return err
			}
		default:
			return shared.NewError(reass.GetLoc(),
				"for loop 'after' step must be an assignment statement")
		}
	} else if len(loopNode.ExprsOrStmts) != 0 {
		return shared.NewError(loopNode.Loc, "for loop must have either: no conditions, a condition or an initlaiser, a condition and an 'after' assignment")
	}

	if _, err := tc.typeCheckBlock(loopNode.Body, sig, true, false); err != nil {
		return err
	}

	tc.exitScope()
	return nil
}

func (tc *TypeChecker) typeCheckIfStatement(ifNode *parser.IfNode, sig *FunctionSig, isLoop bool) (bool, error) {
	ifCondType, err := tc.typeCheckExpression(ifNode.IfBranch.Condition, shared.PRIMITIVE_BOOL)
	if err != nil {
		return false, err
	}
	if ifCondType != shared.PRIMITIVE_BOOL {
		return false, shared.NewError(ifNode.IfBranch.Condition.GetLoc(),
			"if condition must be boolean, found '%s'", ifCondType)
	}

	ifReturns, err := tc.typeCheckBlock(ifNode.IfBranch.Node, sig, isLoop, false)
	if err != nil {
		return false, err
	}
	alwaysReturns := ifReturns && ifNode.ElseBranch != nil

	for _, elseIfBranch := range ifNode.ElseIfBranches {
		elseIfCondType, err := tc.typeCheckExpression(elseIfBranch.Condition, shared.PRIMITIVE_BOOL)
		if err != nil {
			return false, err
		}
		if elseIfCondType != shared.PRIMITIVE_BOOL {
			return false, shared.NewError(elseIfBranch.Condition.GetLoc(),
				"else-if condition must be boolean, found '%s'", elseIfCondType)
		}

		elseIfReturns, err := tc.typeCheckBlock(elseIfBranch.Node, sig, isLoop, false)
		if err != nil {
			return false, err
		}
		if !alwaysReturns {
			alwaysReturns = elseIfReturns
		}
	}

	if ifNode.ElseBranch != nil {
		elseReturns, err := tc.typeCheckBlock(ifNode.ElseBranch, sig, isLoop, false)
		if err != nil {
			return false, err
		}
		if !alwaysReturns {
			alwaysReturns = elseReturns
		}
	}

	return alwaysReturns, nil
}

func ResolveFieldChain(field *parser.IdentifierNode, tpe shared.Type) (shared.Type, error) {
	field.ExprType = tpe
	if field.Next == nil {
		return tpe, nil
	}

	var strct shared.Struct
	if tpe.IsStruct() {
		strct = tpe.(shared.Struct)
	} else if tpe.IsPointer() && tpe.(shared.Pointer).To.IsStruct() {
		strct = tpe.(shared.Pointer).To.(shared.Struct)
	}

	for _, f := range strct.Fields {
		if f.L == field.Next.Name {
			return ResolveFieldChain(field.Next, f.R)
		}
	}

	return nil, shared.NewError(field.Next.Loc, "type has no field named '%s'", field.Next.Name)
}

func SetNodeType(n parser.ExpressionNode, t shared.Type) {
	switch node := n.(type) {
	case *parser.IdentifierNode:
		node.ExprType = t
	case *parser.BoolLiteralNode:
		node.ExprType = t
	case *parser.IntegerLiteralNode:
		node.ExprType = t
	case *parser.FloatLiteralNode:
		node.ExprType = t
	case *parser.StringLiteralNode:
		node.ExprType = t
	case *parser.CharLiteralNode:
		node.ExprType = t
	case *parser.StructLiteralNode:
		node.ExprType = t
	case *parser.ArrayLiteralNode:
		node.ExprType = t
		if !t.IsArray() {
			panic("SetNodeType argument for ArrayLiteralNode is not an array type")
		}
		arrt := t.(shared.Array)

		for _, element := range node.Elements {
			SetNodeType(element, arrt.Of)
		}
	case *parser.FunctionCallNode:
		node.ExprType = t
	case *parser.IfExprNode:
		node.ExprType = t
	case *parser.GivenExprNode:
		node.ExprType = t
	case *parser.UnaryOpNode:
		node.ExprType = t
	case *parser.BinaryOpNode:
		node.ExprType = t
	case *parser.CastNode:
		node.ExprType = t
	case *parser.SizeOfNode:
		node.ExprType = t
	default:
		panic("SetNodeType: unknown node type")
	}
}
