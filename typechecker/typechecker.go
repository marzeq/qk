package typechecker

import (
	"path/filepath"

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
	ArgTypes []shared.Pair[string, Type]
	RetType  Type
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

func (tc *TypeChecker) TypeCheck(root *Node) (*Node, error) {
	loaded := make(map[string]*Node)
	recStack := make(map[string]bool)
	mergedRoot, err := tc.processImports(root, loaded, recStack)
	if err != nil {
		return nil, err
	}
	root = mergedRoot

	for _, node := range root.Children {
		if node.Type != parser.NODE_TYPE_FUNCTION_DEF {
			continue
		}

		fname, funcSig, err := extractFunctionSig(node)
		if err != nil {
			return nil, err
		}

		if ok := tc.FuncTable.Define(fname, funcSig); !ok {
			return nil, shared.NewError(node.Loc, "function '%s' is already defined", fname)
		}
	}

	for _, node := range root.Children {
		if node.Type != parser.NODE_TYPE_FUNCTION_DEF {
			continue
		}

		name := identToStr(node.Value.(*parser.FunctionValue).Name)
		sig, _ := tc.FuncTable.Lookup(name)

		tc.enterScope()
		if err := tc.typeCheckFunction(node, sig); err != nil {
			return nil, err
		}
		tc.exitScope()
	}

	return mergedRoot, nil
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
	name := identToStr(funcNode.Value.(*parser.FunctionValue).Name)

	for _, arg := range sig.ArgTypes {
		tc.VarTable.Define(arg.L, &VarSig{
			Type:    arg.R,
			Mutable: false,
		})
	}

	if body.Type == parser.NODE_TYPE_BLOCK {
		return tc.typeCheckBlock(body, sig, false)
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

func (tc *TypeChecker) typeCheckBlock(blockNode *Node, sig *FunctionSig, isLoop bool) error {
	foundReturn := false

	for _, node := range blockNode.Children {
		switch node.Type {
		case parser.NODE_TYPE_DECLARATION:
			varName, varSig, err := tc.typeCheckDeclaration(node)
			if err != nil {
				return err
			}
			if ok := tc.VarTable.Define(varName, varSig); !ok {
				return shared.NewError(node.Left.Loc, "variable '%s' is already declared in this scope", varName)
			}

		case parser.NODE_TYPE_FUNCTION_DEF:
			fname, fsig, err := extractFunctionSig(node)
			if err != nil {
				return err
			}
			if ok := tc.FuncTable.Define(fname, fsig); !ok {
				return shared.NewError(node.Loc, "function '%s' is already defined", fname)
			}

			tc.enterScope()
			if err := tc.typeCheckFunction(node, fsig); err != nil {
				return err
			}
			tc.exitScope()

		case parser.NODE_TYPE_ASSIGNMENT:
			if err := tc.typeCheckAssignment(node); err != nil {
				return err
			}

		case parser.NODE_TYPE_FUNCTION_CALL:
			if _, err := tc.typeCheckFunctionCall(node); err != nil {
				return err
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
						return err
					}

					if !shared.CanCoerceTo(retType, sig.RetType) {
						return shared.NewError(node.Loc, "wrong return type for function, expected '%s' got '%s'", sig.RetType, retType)
					}
					if retType == shared.BUILTIN_UNTYPED_INT {
						node.Right.ExprType = sig.RetType
					}
				}
				foundReturn = true

			case "break", "continue":
				if !isLoop {
					return shared.NewError(node.Loc, "'%s' statement outside a loop", kw)
				}
			}

		case parser.NODE_TYPE_BLOCK:
			tc.enterScope()
			if err := tc.typeCheckBlock(node, sig, isLoop); err != nil {
				return err
			}
			tc.exitScope()

		case parser.NODE_TYPE_FOR:
			if err := tc.typeCheckForLoop(node, sig); err != nil {
				return err
			}

		case parser.NODE_TYPE_IF:
			if err := tc.typeCheckIfStatement(node, sig); err != nil {
				return err
			}

		default:
			panic("unexpected node in block")
		}
	}

	if !foundReturn && sig.RetType != shared.BUILTIN_VOID {
		return shared.NewError(blockNode.Loc, "function with return type '%s' is missing a return statement", sig.RetType)
	}

	return nil
}

func (tc *TypeChecker) typeCheckExpression(exprNode *Node, expectedType Type) (Type, error) {
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
		return exprType, nil

	case parser.NODE_TYPE_BOOL_LITERAL:
		exprNode.ExprType = shared.BUILTIN_BOOL
		return shared.BUILTIN_BOOL, nil

	case parser.NODE_TYPE_FUNCTION_CALL:
		return tc.typeCheckFunctionCall(exprNode)

	case parser.NODE_TYPE_UNARY_OP:
		var op string
		switch v := exprNode.Value.(type) {
		case string:
			if v != "not" {
				return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "invalid unary operator '%s'", v)
			}
			op = v
		case tokeniser.TokenType:
			if v == tokeniser.TOKEN_TYPE_MINUS {
				op = "-"
			} else {
				return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "invalid unary operator token '%v'", v)
			}
		default:
			return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "invalid unary operator type '%T'", exprNode.Value)
		}

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
		var op string
		switch v := exprNode.Value.(type) {
		case string:
			if v != "and" && v != "or" {
				return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "invalid binary operator '%s'", v)
			}
			op = v
		case tokeniser.TokenType:
			op = tokenTypeToOperator(v)
			if op == "" {
				return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "unsupported binary operator token '%v'", v)
			}
		default:
			return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "invalid binary operator type '%T'", exprNode.Value)
		}

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
			return shared.BUILTIN_BOOL, nil

		case "==", "!=", "<", ">", "<=", ">=":
			compatible, _ := shared.AreCompatibleNumericTypes(leftType, rightType)
			if !compatible {
				return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "operator '%s' cannot be applied to operands of type '%s' and '%s'", op, leftType, rightType)
			}
			return shared.BUILTIN_BOOL, nil

		case "+", "-", "*", "/":
			compatible, commonType := shared.AreCompatibleNumericTypes(leftType, rightType)
			if !compatible {
				return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "operator '%s' requires numeric operands, found '%s' and '%s'", op, leftType, rightType)
			}
			return commonType, nil

		default:
			return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "unknown binary operator '%s'", op)
		}

	case parser.NODE_TYPE_IF_EXPR:
		ifExpr := exprNode.Value.(*parser.IfNodeValue)
		if ifExpr.IfBranch == nil {
			return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "if expression missing if branch")
		}

		condType, err := tc.typeCheckExpression(ifExpr.IfBranch.Condition, shared.BUILTIN_VOID)
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
			elseIfCondType, err := tc.typeCheckExpression(elseIf.Condition, shared.BUILTIN_VOID)
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

		return commonType, nil

	default:
		return shared.BUILTIN_VOID, shared.NewError(exprNode.Loc, "unsupported expression type: '%s'", exprNode.Type)
	}
}

func (tc *TypeChecker) typeCheckFunctionCall(funccallNode *Node) (Type, error) {
	nameNode := funccallNode.Value.(*Node)
	fname := identToStr(nameNode)

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
	varName := identToStr(declNode.Left)
	info := declNode.Value.(*parser.DeclarationValue)
	mutable := info.Mutable
	varType := shared.BUILTIN_VOID

	if info.Type != nil {
		varTypeStr := identToStr(info.Type)
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
		varType = shared.BUILTIN_I32
	}

	if exprType == shared.BUILTIN_UNTYPED_INT {
		declNode.Right.ExprType = varType
	}

	return varName, &VarSig{
		Type:    varType,
		Mutable: mutable,
	}, nil
}

func (tc *TypeChecker) typeCheckAssignment(asNode *Node) error {
	varName := identToStr(asNode.Left)
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

	return nil
}

func (tc *TypeChecker) typeCheckForLoop(loopNode *Node, sig *FunctionSig) error {
	value := loopNode.Value.(*parser.ForLoopValue)

	tc.enterScope()

	for _, exOrSt := range value.ExprsOrStmts {
		if exOrSt.Type.IsStatement() {
			if exOrSt.Type == parser.NODE_TYPE_DECLARATION {
				varName, varSig, err := tc.typeCheckDeclaration(exOrSt)
				if err != nil {
					return err
				}

				tc.VarTable.Define(varName, varSig)
			} else if exOrSt.Type == parser.NODE_TYPE_ASSIGNMENT {
				if err := tc.typeCheckAssignment(exOrSt); err != nil {
					return err
				}
			} else {
				return shared.NewError(exOrSt.Loc, "for loop initializer must be a declaration or assignment statement")
			}
		} else {
			exprType, err := tc.typeCheckExpression(exOrSt, shared.BUILTIN_BOOL)
			if err != nil {
				return err
			}

			if exprType != shared.BUILTIN_BOOL {
				return shared.NewError(exOrSt.Loc, "for loop condition must be a boolean expression, found '%s'", exprType)
			}
		}
	}

	if err := tc.typeCheckBlock(value.Body, sig, true); err != nil {
		return err
	}

	tc.exitScope()
	return nil
}

func (tc *TypeChecker) typeCheckIfStatement(ifNode *Node, sig *FunctionSig) error {
	value := ifNode.Value.(*parser.IfNodeValue)

	ifCondType, err := tc.typeCheckExpression(value.IfBranch.Condition, shared.BUILTIN_BOOL)
	if err != nil {
		return err
	}
	if ifCondType != shared.BUILTIN_BOOL {
		return shared.NewError(value.ElseBranch.Condition.Loc, "if condition must be boolean, found '%s'", ifCondType)
	}

	if err := tc.typeCheckBlock(value.IfBranch.Node, sig, false); err != nil {
		return err
	}

	for _, elseIfBranch := range value.ElseIfBranches {
		elseIfCondType, err := tc.typeCheckExpression(elseIfBranch.Condition, shared.BUILTIN_BOOL)
		if err != nil {
			return err
		}
		if elseIfCondType != shared.BUILTIN_BOOL {
			return shared.NewError(elseIfBranch.Condition.Loc, "else-if condition must be boolean, found '%s'", elseIfCondType)
		}

		if err := tc.typeCheckBlock(elseIfBranch.Node, sig, false); err != nil {
			return err
		}
	}

	if value.ElseBranch != nil {
		if err := tc.typeCheckBlock(value.ElseBranch.Node, sig, false); err != nil {
			return err
		}
	}

	return nil
}

func extractFunctionSig(functionNode *Node) (string, *FunctionSig, error) {
	functionVal := functionNode.Value.(*parser.FunctionValue)
	name := identToStr(functionVal.Name)
	retTypeStr := identToStr(functionVal.RetType)

	retType, ok := shared.ResolveType(retTypeStr)
	if !ok {
		return "", nil, shared.NewError(functionVal.RetType.Loc, "function '%s' has undefined return type '%s'", name, retTypeStr)
	}

	argTypes := make([]shared.Pair[string, Type], len(functionVal.Args))
	for i, arg := range functionVal.Args {
		tpe := identToStr(arg.R)
		resolved, ok := shared.ResolveType(tpe)
		if !ok {
			return "", nil, shared.NewError(arg.R.Loc, "parameter '%s' has undefined type '%s'", identToStr(arg.L), tpe)
		}
		argTypes[i] = shared.Pair[string, Type]{L: identToStr(arg.L), R: resolved}
	}

	return name, &FunctionSig{
		ArgTypes: argTypes,
		RetType:  retType,
	}, nil
}

func identToStr(identNode *Node) string {
	return identNode.Value.(string)
}

func tokenTypeToOperator(t tokeniser.TokenType) string {
	switch t {
	case tokeniser.TOKEN_TYPE_PLUS:
		return "+"
	case tokeniser.TOKEN_TYPE_MINUS:
		return "-"
	case tokeniser.TOKEN_TYPE_ASTERISK:
		return "*"
	case tokeniser.TOKEN_TYPE_SLASH:
		return "/"
	case tokeniser.TOKEN_TYPE_EQUALS_EQUALS:
		return "=="
	case tokeniser.TOKEN_TYPE_NOT_EQUALS:
		return "!="
	case tokeniser.TOKEN_TYPE_LESS:
		return "<"
	case tokeniser.TOKEN_TYPE_GREATER:
		return ">"
	case tokeniser.TOKEN_TYPE_LESS_EQUALS:
		return "<="
	case tokeniser.TOKEN_TYPE_GREATER_EQUALS:
		return ">="
	default:
		panic("how")
	}
}
