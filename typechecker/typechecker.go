package typechecker

import (
	"path/filepath"
	"strings"

	"github.com/marzeq/quokka/parser"
	"github.com/marzeq/quokka/shared"
	"github.com/marzeq/quokka/tokeniser"
)

type (
	Node = parser.Node
	Type = shared.Type
)

type VarSig struct {
	Type    Type
	Mutable bool
}

type FunctionSig struct {
	Name           string
	ArgTypes       []shared.Pair[string, Type]
	RetType        Type
	ImplicitReturn bool
}

type (
	VarTable  = *shared.SymbolTable[*VarSig]
	FuncTable = *shared.SymbolTable[*FunctionSig]
)

type TypeChecker struct {
	VarTable  VarTable
	FuncTable FuncTable
}

func NewTypeChecker() *TypeChecker {
	return &TypeChecker{
		VarTable:  shared.NewSymbolTable[*VarSig](),
		FuncTable: shared.NewSymbolTable[*FunctionSig](),
	}
}

func (tc *TypeChecker) TypeCheck(root *Node) (*Node, map[string]*FunctionSig, error) {
	loaded := make(map[string]*Node)
	recStack := make(map[string]bool)
	mergedRoot, err := tc.processImports(root, loaded, recStack)
	if err != nil {
		return nil, nil, err
	}
	root = mergedRoot

	for _, node := range root.Children {
		if node.Type != parser.NODE_TYPE_FUNCTION_DEF {
			continue
		}

		fsig, err := ExtractFunctionSig(node)
		if err != nil {
			return nil, nil, err
		}

		if ok := tc.FuncTable.Define(fsig.Name, fsig); !ok {
			return nil, nil, shared.NewError(node.Loc, "function '%s' is already defined", fsig.Name)
		}

		if fsig.Name == "main" && fsig.RetType != shared.BUILTIN_I32 {
			return nil, nil, shared.NewError(node.Loc, "'main' function must be of '%s' return type", shared.BUILTIN_I32)
		}
	}

	for _, node := range root.Children {
		if node.Type != parser.NODE_TYPE_FUNCTION_DEF {
			continue
		}

		name := IdentToStr(node.Value.(*parser.FunctionValue).Name)
		sig, _ := tc.FuncTable.Lookup(name)

		if len(node.Children) != 0 {
			tc.enterScope()
			if err := tc.typeCheckFunction(node, sig); err != nil {
				return nil, nil, err
			}
			tc.exitScope()
		}
	}

	return mergedRoot, tc.FuncTable.GetScope(), nil
}

func (tc *TypeChecker) processImports(root *Node, loaded map[string]*Node, recStack map[string]bool) (*Node, error) {
	filePath := root.Loc.FilePath

	if recStack[filePath] {
		return nil, shared.NewError(root.Loc, "import cycle detected for file '%s'", filePath)
	}

	if merged, ok := loaded[filePath]; ok {
		return merged, nil
	}

	recStack[filePath] = true

	merged := &Node{
		Type:     root.Type,
		Loc:      root.Loc,
		Children: []*Node{},
	}

	for _, node := range root.Children {
		if node.Type != parser.NODE_TYPE_IMPORT {
			continue
		}

		importPath := node.Right.Value.(string)

		resolvedPath := filepath.Join(filepath.Dir(filePath), importPath)

		t, err := tokeniser.NewTokeniserFromFile(resolvedPath)
		if err != nil {
			switch err.(type) {
			case shared.Error:
				return nil, err
			default:
				return nil, shared.NewError(node.Loc, "import failed: %v", err)
			}
		}
		toks, err := t.Tokenise()
		if err != nil {
			return nil, err
		}
		p := parser.NewParser(toks)
		ast, err := p.Parse()
		if err != nil {
			return nil, err
		}

		importedMerged, err := tc.processImports(ast, loaded, recStack)
		if err != nil {
			return nil, err
		}

		merged.Children = append(merged.Children, importedMerged.Children...)
	}

	for _, node := range root.Children {
		if node.Type != parser.NODE_TYPE_IMPORT {
			merged.Children = append(merged.Children, node)
		}
	}

	loaded[filePath] = merged
	delete(recStack, filePath)
	return merged, nil
}

func (tc *TypeChecker) enterScope() {
	tc.VarTable.EnterScope()
	tc.FuncTable.EnterScope()
}

func (tc *TypeChecker) exitScope() {
	tc.VarTable.ExitScope()
	tc.FuncTable.ExitScope()
}

func (tc *TypeChecker) typeCheckFunction(funcNode *Node, sig *FunctionSig) error {
	val := funcNode.Value.(*parser.FunctionValue)
	body := funcNode.Children[0]
	name := IdentToStr(funcNode.Value.(*parser.FunctionValue).Name)

	for _, arg := range sig.ArgTypes {
		tc.VarTable.Define(arg.L, &VarSig{
			Type:    arg.R,
			Mutable: false,
		})
	}

	if body.Type == parser.NODE_TYPE_BLOCK {
		_, err := tc.typeCheckBlock(body, sig, false, true)
		return err
	}

	exprType, err := tc.typeCheckExpression(body, sig.RetType)
	if err != nil {
		return err
	}

	if !shared.CanCoerceTo(exprType, sig.RetType) {
		return shared.NewError(val.RetType.Loc, "function '%s' expects return type '%s' but returns '%s'",
			name, sig.RetType, exprType)
	}

	if exprType == shared.BUILTIN_UNTYPED_INT {
		body.ExprType = sig.RetType
	}
	return nil
}

func (tc *TypeChecker) typeCheckBlock(blockNode *Node, sig *FunctionSig, isLoop bool, isMainBody bool) (bool, error) { // (returns, error)
	foundReturn := false

	for i, node := range blockNode.Children {
		switch node.Type {
		case parser.NODE_TYPE_DECLARATION:
			varName, varSig, err := tc.typeCheckDeclaration(node)
			if err != nil {
				return false, err
			}
			if ok := tc.VarTable.Define(varName, varSig); !ok {
				return false, shared.NewError(node.Left.Loc, "variable '%s' is already declared in this scope", varName)
			}

		case parser.NODE_TYPE_FUNCTION_DEF:
			fsig, err := ExtractFunctionSig(node)
			if err != nil {
				return false, err
			}
			if ok := tc.FuncTable.Define(fsig.Name, fsig); !ok {
				return false, shared.NewError(node.Loc, "function '%s' is already defined", fsig.Name)
			}

			tc.enterScope()
			if err := tc.typeCheckFunction(node, fsig); err != nil {
				return false, err
			}
			tc.exitScope()

		case parser.NODE_TYPE_ASSIGNMENT:
			if err := tc.typeCheckAssignment(node); err != nil {
				return false, err
			}

		case parser.NODE_TYPE_FUNCTION_CALL:
			if _, err := tc.typeCheckFunctionCall(node); err != nil {
				return false, err
			}

		case parser.NODE_TYPE_CONTROL_KEYWORD:
			kw := node.Value.(string)
			switch kw {
			case "return":
				retType := shared.BUILTIN_VOID
				if node.Right != nil {
					var err error
					retType, err = tc.typeCheckExpression(node.Right, sig.RetType)
					if err != nil {
						return false, err
					}

					if !shared.CanCoerceTo(retType, sig.RetType) {
						return false, shared.NewError(node.Loc, "wrong return type for function, expected '%s' got '%s'", sig.RetType, retType)
					}
					if retType == shared.BUILTIN_UNTYPED_INT {
						node.Right.ExprType = sig.RetType
					}
				} else if sig.RetType != shared.BUILTIN_VOID {
					return false, shared.NewError(node.Loc, "wrong return type for function, expected '%s' got void", sig.RetType)
				}
				foundReturn = true

				if i != len(blockNode.Children)-1 {
					return false, shared.NewError(blockNode.Children[i+1].Loc, "dead code following return statement")
				}

			case "break", "continue":
				if !isLoop {
					return false, shared.NewError(node.Loc, "'%s' statement outside a loop", kw)
				}
			}

		case parser.NODE_TYPE_BLOCK:
			tc.enterScope()
			returns, err := tc.typeCheckBlock(node, sig, isLoop, false)
			if err != nil {
				return false, err
			}
			tc.exitScope()

			if i == len(blockNode.Children)-1 && !foundReturn {
				foundReturn = returns
			}

		case parser.NODE_TYPE_FOR:
			if err := tc.typeCheckForLoop(node, sig); err != nil {
				return false, err
			}

		case parser.NODE_TYPE_IF:
			tc.enterScope()
			returns, err := tc.typeCheckIfStatement(node, sig, isLoop)
			if err != nil {
				return false, err
			}
			tc.exitScope()

			if i == len(blockNode.Children)-1 && !foundReturn {
				foundReturn = returns
			}

		default:
			panic("unexpected node in block")
		}
	}

	if isMainBody {
		if !foundReturn && sig.RetType != shared.BUILTIN_VOID {
			return false, shared.NewError(blockNode.Loc, "function with return type '%s' is missing a return statement", sig.RetType)
		}

		sig.ImplicitReturn = !foundReturn
	}

	return foundReturn, nil
}

func (tc *TypeChecker) typeCheckExpression(exprNode *Node, expectedType Type) (Type, error) {
	exprNode.ExprType = expectedType
	switch exprNode.Type {
	case parser.NODE_TYPE_IDENTIFIER:
		varName := exprNode.Value.(string)
		varSig, ok := tc.VarTable.Lookup(varName)
		if !ok {
			return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "undefined variable '%s'", varName)
		}
		exprNode.ExprType = varSig.Type
		return varSig.Type, nil

	case parser.NODE_TYPE_NUMBER_LITERAL:
		exprType := shared.BUILTIN_UNTYPED_INT
		if expectedType != shared.BUILTIN_VOID && shared.CanCoerceTo(exprType, expectedType) {
			exprNode.ExprType = expectedType
			return expectedType, nil
		}
		exprNode.ExprType = exprType
		return exprType, nil

	case parser.NODE_TYPE_CHAR_LITERAL:
		return shared.BUILTIN_CHAR, nil

	case parser.NODE_TYPE_STRING_LITERAL:
		return shared.BUILTIN_STRING, nil

	case parser.NODE_TYPE_BOOL_LITERAL:
		exprNode.ExprType = shared.BUILTIN_BOOL
		return shared.BUILTIN_BOOL, nil

	case parser.NODE_TYPE_FUNCTION_CALL:
		return tc.typeCheckFunctionCall(exprNode)

	case parser.NODE_TYPE_UNARY_OP:
		op := exprNode.Value.(string)
		operandType, err := tc.typeCheckExpression(exprNode.Right, shared.BUILTIN_VOID)
		if err != nil {
			return shared.BUILTIN_VOID, err
		}

		if expectedType != shared.BUILTIN_VOID && operandType == shared.BUILTIN_UNTYPED_INT && shared.IsNumericType(expectedType) {
			exprNode.Right.ExprType = expectedType
			operandType = expectedType
		}

		switch op {
		case "not":
			if operandType != shared.BUILTIN_BOOL {
				return shared.BUILTIN_VOID, shared.NewError(exprNode.Right.Loc, "unary operator 'not' expects a boolean operand, found '%s'", operandType)
			}
			return shared.BUILTIN_BOOL, nil
		case "-":
			if !shared.IsNumericType(operandType) {
				return shared.BUILTIN_VOID, shared.NewError(exprNode.Right.Loc, "unary operator '-' requires numeric operand, found '%s'", operandType)
			}
			return operandType, nil
		default:
			return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "unknown unary operator '%s'", op)
		}

	case parser.NODE_TYPE_BINARY_OP:
		op := exprNode.Value.(string)
		leftType, err := tc.typeCheckExpression(exprNode.Left, shared.BUILTIN_VOID)
		if err != nil {
			return shared.BUILTIN_VOID, err
		}
		rightType, err := tc.typeCheckExpression(exprNode.Right, shared.BUILTIN_VOID)
		if err != nil {
			return shared.BUILTIN_VOID, err
		}

		if expectedType != shared.BUILTIN_VOID {
			if leftType == shared.BUILTIN_UNTYPED_INT && shared.IsNumericType(expectedType) {
				exprNode.Left.ExprType = expectedType
				leftType = expectedType
			}
			if rightType == shared.BUILTIN_UNTYPED_INT && shared.IsNumericType(expectedType) {
				exprNode.Right.ExprType = expectedType
				rightType = expectedType
			}
		}

		switch op {
		case "and", "or":
			if leftType != shared.BUILTIN_BOOL || rightType != shared.BUILTIN_BOOL {
				return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "operator '%s' expects boolean operands, found '%s' and '%s'", op, leftType, rightType)
			}
			exprNode.ExprType = shared.BUILTIN_BOOL
			return shared.BUILTIN_BOOL, nil

		case "==", "!=", "<", ">", "<=", ">=":
			compatible, commonType := shared.AreCompatibleNumericTypes(leftType, rightType)
			if !compatible {
				return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "operator '%s' cannot be applied to operands of type '%s' and '%s'", op, leftType, rightType)
			}
			exprNode.Left.ExprType = commonType
			exprNode.Right.ExprType = commonType
			exprNode.ExprType = shared.BUILTIN_BOOL
			return shared.BUILTIN_BOOL, nil

		case "+", "-", "*", "/", "%":
			compatible, commonType := shared.AreCompatibleNumericTypes(leftType, rightType)
			if !compatible {
				return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "operator '%s' requires numeric operands, found '%s' and '%s'", op, leftType, rightType)
			}
			exprNode.Left.ExprType = commonType
			exprNode.Right.ExprType = commonType
			exprNode.ExprType = commonType
			return commonType, nil

		default:
			return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "unknown binary operator '%s'", op)
		}

	case parser.NODE_TYPE_CAST:
		tpeName := IdentToStr(exprNode.Left)
		tpe, ok := shared.ResolveType(tpeName)
		if !ok {
			return shared.BUILTIN_VOID, shared.NewError(exprNode.Left.Loc, "no such type '%s'", tpeName)
		}
		exTpe, err := tc.typeCheckExpression(exprNode.Right, shared.BUILTIN_VOID)
		if err != nil {
			return shared.BUILTIN_VOID, err
		}

		if exTpe == shared.BUILTIN_UNTYPED_INT {
			exprNode.Right.ExprType = tpe
		}

		compatible, _ := shared.AreCompatibleTypes(tpe, exTpe)
		if compatible {
			exprNode.ExprType = tpe
			return tpe, nil
		} else {
			return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "cannot cast type '%s' to '%s'", exTpe, tpe)
		}

	case parser.NODE_TYPE_IF_EXPR:
		ifExpr := exprNode.Value.(*parser.IfNodeValue)
		if ifExpr.IfBranch == nil {
			return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "if expression missing if branch")
		}

		condType, err := tc.typeCheckExpression(ifExpr.IfBranch.Condition, shared.BUILTIN_BOOL)
		if err != nil {
			return shared.BUILTIN_VOID, err
		}
		if condType != shared.BUILTIN_BOOL {
			return shared.BUILTIN_VOID, shared.NewError(ifExpr.IfBranch.Condition.Loc, "if condition must be boolean, found '%s'", condType)
		}

		ifType, err := tc.typeCheckExpression(ifExpr.IfBranch.Node, expectedType)
		if err != nil {
			return shared.BUILTIN_VOID, err
		}
		commonType := ifType

		for _, elseIf := range ifExpr.ElseIfBranches {
			elseIfCondType, err := tc.typeCheckExpression(elseIf.Condition, shared.BUILTIN_BOOL)
			if err != nil {
				return shared.BUILTIN_VOID, err
			}
			if elseIfCondType != shared.BUILTIN_BOOL {
				return shared.BUILTIN_VOID, shared.NewError(elseIf.Condition.Loc, "else-if condition must be boolean, found '%s'", elseIfCondType)
			}

			elseIfType, err := tc.typeCheckExpression(elseIf.Node, expectedType)
			if err != nil {
				return shared.BUILTIN_VOID, err
			}
			if elseIfType != commonType {
				return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "all branches must return same type, expected '%s' but found '%s'", commonType, elseIfType)
			}
		}

		if ifExpr.ElseBranch == nil {
			return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "if expression requires else branch")
		}
		elseType, err := tc.typeCheckExpression(ifExpr.ElseBranch.Node, expectedType)
		if err != nil {
			return shared.BUILTIN_VOID, err
		}
		if elseType != commonType {
			return shared.BUILTIN_VOID, shared.NewError(ifExpr.ElseBranch.Node.Loc, "else branch must match type '%s', found '%s'", commonType, elseType)
		}

		exprNode.ExprType = commonType
		return commonType, nil

	default:
		return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "unsupported expression type: '%s'", exprNode.Type)
	}
}

func (tc *TypeChecker) typeCheckFunctionCall(funccallNode *Node) (Type, error) {
	nameNode := funccallNode.Value.(*Node)
	fname := IdentToStr(nameNode)

	fsig, ok := tc.FuncTable.Lookup(fname)
	if !ok {
		return shared.BUILTIN_VOID, shared.NewError(nameNode.Loc, "undefined function '%s'", fname)
	}

	for i, arg := range funccallNode.Children {
		fsigArgType := fsig.ArgTypes[i].R
		argType, err := tc.typeCheckExpression(arg, fsigArgType)
		if err != nil {
			return shared.BUILTIN_VOID, err
		}
		if fsigArgType != argType {
			return shared.BUILTIN_VOID, shared.NewError(arg.Loc,
				"argument %d of function '%s' has type '%s' but expected '%s'",
				i+1, fname, argType, fsig.ArgTypes[i].R,
			)
		}
	}

	funccallNode.ExprType = fsig.RetType
	return fsig.RetType, nil
}

func (tc *TypeChecker) typeCheckDeclaration(declNode *Node) (string, *VarSig, error) {
	varName := IdentToStr(declNode.Left)
	if strings.HasPrefix(varName, "___") {
		return "", nil, shared.NewError(declNode.Loc, "variables starting with '___' are reserved for the compiler")
	}
	info := declNode.Value.(*parser.DeclarationValue)
	mutable := info.Mutable
	varType := shared.BUILTIN_VOID

	if info.Type != nil {
		varTypeStr := IdentToStr(info.Type)
		vt, ok := shared.ResolveType(varTypeStr)
		if !ok {
			return "", nil, shared.NewError(info.Type.Loc, "variable '%s' has undefined type '%s'", varName, varTypeStr)
		}
		varType = vt
	}

	exprType, err := tc.typeCheckExpression(declNode.Right, varType)
	if err != nil {
		return "", nil, err
	}

	if varType == shared.BUILTIN_VOID {
		varType = exprType
	} else if !shared.CanCoerceTo(exprType, varType) {
		return "", nil, shared.NewError(declNode.Loc,
			"cannot assign value of type '%s' to variable '%s' of type '%s'",
			exprType, varName, varType,
		)
	}

	if varType == shared.BUILTIN_UNTYPED_INT {
		return "", nil, shared.NewError(declNode.Loc, "ambiguous number type, specify type explicitly")
	}
	if varType == shared.BUILTIN_VOID {
		return "", nil, shared.NewError(declNode.Loc, "a variable cannot be of type void")
	}

	declNode.ExprType = exprType

	return varName, &VarSig{
		Type:    varType,
		Mutable: mutable,
	}, nil
}

func (tc *TypeChecker) typeCheckAssignment(asNode *Node) error {
	varName := IdentToStr(asNode.Left)
	varSig, ok := tc.VarTable.Lookup(varName)
	if !ok {
		return shared.NewError(asNode.Left.Loc, "undefined variable '%s'", varName)
	}

	if !varSig.Mutable {
		return shared.NewError(asNode.Loc, "cannot assign to immutable variable '%s'", varName)
	}

	exprType, err := tc.typeCheckExpression(asNode.Right, varSig.Type)
	if err != nil {
		return err
	}

	if exprType != varSig.Type {
		return shared.NewError(asNode.Loc,
			"cannot assign value of type '%s' to variable '%s' of type '%s'",
			exprType, varName, varSig.Type,
		)
	}

	asNode.ExprType = varSig.Type

	return nil
}

func (tc *TypeChecker) typeCheckForLoop(loopNode *Node, sig *FunctionSig) error {
	value := loopNode.Value.(*parser.ForLoopValue)

	tc.enterScope()

	if len(value.ExprsOrStmts) == 1 {
		expr := value.ExprsOrStmts[0]
		if !expr.Type.IsExpression() {
			return shared.NewError(expr.Loc, "for loop condition must be a boolean expression")
		}
		exprType, err := tc.typeCheckExpression(expr, shared.BUILTIN_BOOL)
		if err != nil {
			return err
		}

		if exprType != shared.BUILTIN_BOOL {
			return shared.NewError(expr.Loc, "for loop condition must be a boolean expression, found '%s'", exprType)
		}
	} else if len(value.ExprsOrStmts) == 3 {
		init := value.ExprsOrStmts[0]
		if init.Type == parser.NODE_TYPE_DECLARATION {
			varName, varSig, err := tc.typeCheckDeclaration(init)
			if err != nil {
				return err
			}

			tc.VarTable.Define(varName, varSig)
		} else if init.Type == parser.NODE_TYPE_ASSIGNMENT {
			if err := tc.typeCheckAssignment(init); err != nil {
				return err
			}
		} else {
			return shared.NewError(init.Loc, "for loop initialiser must be a declaration or assignment statement")
		}

		cond := value.ExprsOrStmts[1]
		if !cond.Type.IsExpression() {
			return shared.NewError(cond.Loc, "for loop condition must be a boolean expression")
		}
		exprType, err := tc.typeCheckExpression(cond, shared.BUILTIN_BOOL)
		if err != nil {
			return err
		}

		if exprType != shared.BUILTIN_BOOL {
			return shared.NewError(cond.Loc, "for loop condition must be a boolean expression, found '%s'", exprType)
		}

		reass := value.ExprsOrStmts[2]
		if reass.Type != parser.NODE_TYPE_ASSIGNMENT {
			return shared.NewError(reass.Loc, "for loop 'after' step must be an assignment statement")
		}

		if err := tc.typeCheckAssignment(reass); err != nil {
			return err
		}
	} else if len(value.ExprsOrStmts) != 0 {
		return shared.NewError(loopNode.Loc, "for loop must have either:\n  - no conditions\n  - a condition\n  - an initialiser, a condition and an 'after' assignment")
	}

	if _, err := tc.typeCheckBlock(value.Body, sig, true, false); err != nil {
		return err
	}

	tc.exitScope()
	return nil
}

func (tc *TypeChecker) typeCheckIfStatement(ifNode *Node, sig *FunctionSig, isLoop bool) (bool, error) {
	value := ifNode.Value.(*parser.IfNodeValue)

	ifCondType, err := tc.typeCheckExpression(value.IfBranch.Condition, shared.BUILTIN_BOOL)
	if err != nil {
		return false, err
	}
	if ifCondType != shared.BUILTIN_BOOL {
		return false, shared.NewError(value.ElseBranch.Condition.Loc, "if condition must be boolean, found '%s'", ifCondType)
	}

	ifReturns, err := tc.typeCheckBlock(value.IfBranch.Node, sig, isLoop, false)
	if err != nil {
		return false, err
	}
	alwaysReturns := ifReturns && value.ElseBranch != nil

	for _, elseIfBranch := range value.ElseIfBranches {
		elseIfCondType, err := tc.typeCheckExpression(elseIfBranch.Condition, shared.BUILTIN_BOOL)
		if err != nil {
			return false, err
		}
		if elseIfCondType != shared.BUILTIN_BOOL {
			return false, shared.NewError(elseIfBranch.Condition.Loc, "else-if condition must be boolean, found '%s'", elseIfCondType)
		}

		elseIfReturns, err := tc.typeCheckBlock(elseIfBranch.Node, sig, isLoop, false)
		if err != nil {
			return false, err
		}
		if !alwaysReturns {
			alwaysReturns = elseIfReturns
		}
	}

	if value.ElseBranch != nil {
		elseReturns, err := tc.typeCheckBlock(value.ElseBranch.Node, sig, isLoop, false)
		if err != nil {
			return false, err
		}
		if !alwaysReturns {
			alwaysReturns = elseReturns
		}
	}

	return alwaysReturns, nil
}

func ExtractFunctionSig(functionNode *Node) (*FunctionSig, error) {
	functionVal := functionNode.Value.(*parser.FunctionValue)
	name := IdentToStr(functionVal.Name)
	retTypeStr := IdentToStr(functionVal.RetType)

	retType, ok := shared.ResolveType(retTypeStr)
	if !ok {
		return nil, shared.NewError(functionVal.RetType.Loc, "function '%s' has undefined return type '%s'", name, retTypeStr)
	}

	argTypes := make([]shared.Pair[string, Type], len(functionVal.Args))
	for i, arg := range functionVal.Args {
		tpe := IdentToStr(arg.R)
		resolved, ok := shared.ResolveType(tpe)
		if !ok {
			return nil, shared.NewError(arg.R.Loc, "parameter '%s' has undefined type '%s'", IdentToStr(arg.L), tpe)
		}
		argTypes[i] = shared.Pair[string, Type]{L: IdentToStr(arg.L), R: resolved}
	}

	return &FunctionSig{
		ArgTypes: argTypes,
		RetType:  retType,
		Name:     name,
	}, nil
}

func IdentToStr(identNode *Node) string {
	return identNode.Value.(string)
}
