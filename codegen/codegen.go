package codegen

import (
	"fmt"
	"strings"

	"github.com/marzeq/quokka/parser"
	"github.com/marzeq/quokka/shared"
	"github.com/marzeq/quokka/typechecker"
)

type (
	Node = parser.Node
	Type = shared.Type
)

type CodeGen struct {
	tmpVarId uint
	tmpLblId uint
	rootNode *Node
	funcSigs map[string]*typechecker.FunctionSig
}

func NewCodeGen(rootNode *Node, funcSigs map[string]*typechecker.FunctionSig) *CodeGen {
	return &CodeGen{
		tmpVarId: 0,
		tmpLblId: 0,
		rootNode: rootNode,
		funcSigs: funcSigs,
	}
}

func (cg *CodeGen) GetTmpVar() string {
	val := fmt.Sprintf("%%___tmpvar%d", cg.tmpVarId)
	cg.tmpVarId++
	return val
}

func (cg *CodeGen) GetTmpLbl() string {
	val := fmt.Sprintf("@___tmplbl%d", cg.tmpLblId)
	cg.tmpLblId++
	return val
}

func (cg *CodeGen) EmitIR() (string, error) {
	ir := ""

	for _, node := range cg.rootNode.Children {
		if node.Type != parser.NODE_TYPE_FUNCTION_DEF {
			return "", shared.NewError(node.Loc, "unexpected token")
		}

		funcIr, err := cg.GenerateFuncIR(node)
		if err != nil {
			return "", err
		}

		ir += funcIr
	}

	// TODO: REMOVE THIS
	ir += "\ndata $printfmt = { b \"%d\\n\", b 0 }"

	return ir, nil
}

func (cg *CodeGen) GenerateFuncIR(funcNode *Node) (string, error) {
	cg.tmpVarId = 0
	cg.tmpLblId = 0
	prologue := ""
	body := ""
	epilogue := ""

	fname := typechecker.IdentToStr(funcNode.Value.(*parser.FunctionValue).Name)
	fsig := cg.funcSigs[fname]

	if fsig.Name == "main" {
		prologue += "export function w $main() {\n@start"
	} else {
		prologue += fmt.Sprintf("function %s $%s(", mapTypeToIRType(fsig.RetType), fsig.Name)
		for i, arg := range fsig.ArgTypes {
			prologue += fmt.Sprintf("%s %%%s", mapTypeToIRType(arg.R), arg.L)
			if i != len(fsig.ArgTypes)-1 {
				prologue += ", "
			}
		}

		prologue += ") {\n@start"

		if fsig.ImplicitReturn {
			epilogue = "\nret"
		}
	}

	epilogue += "\n}\n"

	child := funcNode.Children[0]

	if child.Type == parser.NODE_TYPE_BLOCK {
		b, _, err := cg.GenerateBlockIR(child, "", "")
		if err != nil {
			return "", err
		}
		body = b

		if len(child.Children) > 1 &&
			child.Children[len(child.Children)-1].Type == parser.NODE_TYPE_IF &&
			fsig.RetType != shared.BUILTIN_VOID { // case when every if body returns
			epilogue = "ret 0" + epilogue
		}
	} else {
		val, setups, err := cg.GenerateExprIR(child)
		if err != nil {
			return "", err
		}
		for _, setup := range setups {
			body += "\n" + setup
		}
		body += "\nret " + val
	}

	return prologue + body + epilogue, nil
}

func (cg *CodeGen) GenerateBlockIR(blockNode *Node, loopBegin, loopEnd string) (string, bool, error) {
	body := ""
	endsWithTerminator := false
	for i, node := range blockNode.Children {
		ir, setups, err := cg.GenerateStmtIR(node, i == len(blockNode.Children)-1, loopBegin, loopEnd)
		if err != nil {
			return "", false, err
		}

		for _, setup := range setups {
			body += "\n" + setup
		}
		body += "\n" + ir

		if IsTerminatorInstruction(ir) {
			endsWithTerminator = true
		} else {
			endsWithTerminator = false
		}
	}

	return body, endsWithTerminator, nil
}

func IsTerminatorInstruction(ir string) bool {
	return strings.HasPrefix(ir, "ret") ||
		strings.HasPrefix(ir, "jmp") ||
		strings.HasPrefix(ir, "jnz")
}

func (cg *CodeGen) GenerateStmtIR(stmtNode *Node, last bool, loopBegin, loopEnd string) (string, []string, error) {
	line := ""
	setups := []string{}
	switch stmtNode.Type {
	case parser.NODE_TYPE_CONTROL_KEYWORD:
		switch stmtNode.Value.(string) {
		case "return":
			line = "ret"
			if stmtNode.Right != nil {
				line += " "
				val, stps, err := cg.GenerateExprIR(stmtNode.Right)
				if err != nil {
					return "", nil, err
				}
				line += val
				setups = append(setups, stps...)
			}
		case "break":
			if loopEnd == "" {
				return "", nil, shared.NewError(stmtNode.Loc, "used 'break' keyword outside loop")
			}
			line = fmt.Sprintf("jmp %s", loopEnd)
		case "continue":
			if loopBegin == "" {
				return "", nil, shared.NewError(stmtNode.Loc, "used 'continue' keyword outside loop")
			}
			line = fmt.Sprintf("jmp %s", loopBegin)
		}

	case parser.NODE_TYPE_DECLARATION:
		fallthrough
	case parser.NODE_TYPE_ASSIGNMENT:
		line = fmt.Sprintf("%%%s =%s ", typechecker.IdentToStr(stmtNode.Left), mapTypeToIRType(stmtNode.ExprType))

		val, stps, err := cg.GenerateExprIR(stmtNode.Right)
		val = "copy " + val
		if err != nil {
			return "", nil, err
		}

		line += val
		setups = append(setups, stps...)

	case parser.NODE_TYPE_FUNCTION_CALL:
		fname := typechecker.IdentToStr(stmtNode.Value.(*Node))
		fsig := cg.funcSigs[fname]
		// TODO: REMOVE THIS
		if fname == "_print" {
			line = "call $printf(l $printfmt, ..., "
			if len(stmtNode.Children) != 1 {
				return "", nil, shared.NewError(stmtNode.Loc, "temporary '_print' function expects exactly one argument")
			}
			arg := stmtNode.Children[0]
			line += mapTypeToIRType(arg.ExprType) + " "
			val, stps, err := cg.GenerateExprIR(arg)
			if err != nil {
				return "", nil, err
			}
			line += val

			setups = append(setups, stps...)
		} else {
			line = fmt.Sprintf("call $%s(", fsig.Name)
			for i, arg := range stmtNode.Children {
				argType := mapTypeToIRType(fsig.ArgTypes[i].R)
				val, stps, err := cg.GenerateExprIR(arg)
				if err != nil {
					return "", nil, err
				}
				line += fmt.Sprintf("%s %s", argType, val)
				if i != len(stmtNode.Children)-1 {
					line += ", "
				}

				setups = append(setups, stps...)
			}
		}
		line += ")"

	case parser.NODE_TYPE_IF:
		ifVal := stmtNode.Value.(*parser.IfNodeValue)
		endLabel := cg.GetTmpLbl()

		condVal, condSetups, err := cg.GenerateExprIR(ifVal.IfBranch.Condition)
		if err != nil {
			return "", nil, err
		}
		ifLabel := cg.GetTmpLbl()
		elseCheckLabel := cg.GetTmpLbl()

		setups = append(setups, condSetups...)
		setups = append(setups, fmt.Sprintf("jnz %s, %s, %s", condVal, ifLabel, elseCheckLabel))

		ifBlockIR, ifTerminated, err := cg.GenerateBlockIR(ifVal.IfBranch.Node, loopBegin, loopEnd)
		if err != nil {
			return "", nil, err
		}
		setups = append(setups, fmt.Sprintf("%s", ifLabel))
		setups = append(setups, ifBlockIR)
		if !ifTerminated {
			setups = append(setups, fmt.Sprintf("jmp %s", endLabel))
		}

		currentCheckLabel := elseCheckLabel

		for _, elseIf := range ifVal.ElseIfBranches {
			elseIfCondVal, elseIfCondSetups, err := cg.GenerateExprIR(elseIf.Condition)
			if err != nil {
				return "", nil, err
			}

			elseIfLabel := cg.GetTmpLbl()
			nextCheckLabel := cg.GetTmpLbl()

			setups = append(setups, fmt.Sprintf("%s", currentCheckLabel))
			setups = append(setups, elseIfCondSetups...)
			setups = append(setups, fmt.Sprintf("jnz %s, %s, %s", elseIfCondVal, elseIfLabel, nextCheckLabel))

			elseIfBlockIR, elseIfTerminated, err := cg.GenerateBlockIR(elseIf.Node, loopBegin, loopEnd)
			if err != nil {
				return "", nil, err
			}
			setups = append(setups, fmt.Sprintf("%s", elseIfLabel))
			setups = append(setups, elseIfBlockIR)
			if !elseIfTerminated {
				setups = append(setups, fmt.Sprintf("jmp %s", endLabel))
			}

			currentCheckLabel = nextCheckLabel
		}

		if ifVal.ElseBranch != nil {
			elseLabel := cg.GetTmpLbl()
			setups = append(setups, fmt.Sprintf("%s", currentCheckLabel))
			setups = append(setups, fmt.Sprintf("jmp %s", elseLabel))

			elseBlockIR, elseTerminated, err := cg.GenerateBlockIR(ifVal.ElseBranch.Node, loopBegin, loopEnd)
			if err != nil {
				return "", nil, err
			}
			setups = append(setups, fmt.Sprintf("%s", elseLabel))
			setups = append(setups, elseBlockIR)
			if !elseTerminated {
				setups = append(setups, fmt.Sprintf("jmp %s", endLabel))
			}
		} else {
			setups = append(setups, fmt.Sprintf("%s", currentCheckLabel))
			setups = append(setups, fmt.Sprintf("jmp %s", endLabel))
		}

		setups = append(setups, fmt.Sprintf("%s", endLabel))

	case parser.NODE_TYPE_FOR:
		value := stmtNode.Value.(*parser.ForLoopValue)
		beginLbl := cg.GetTmpLbl()
		bodyLbl := cg.GetTmpLbl()
		endLbl := cg.GetTmpLbl()
		line = beginLbl + "\n"
		body, _, err := cg.GenerateBlockIR(value.Body, beginLbl, endLbl)
		if err != nil {
			return "", nil, err
		}

		if len(value.ExprsOrStmts) == 1 {
			cond, stps, err := cg.GenerateExprIR(value.ExprsOrStmts[0])
			if err != nil {
				return "", nil, err
			}

			for _, stp := range stps {
				line += stp + "\n"
			}
			line += fmt.Sprintf("jnz %s %s, %s\n", cond, bodyLbl, endLbl)
			line += bodyLbl + "\n" + body + "\n"
			line += fmt.Sprintf("jmp %s\n", beginLbl)
		} else if len(value.ExprsOrStmts) == 3 {
			l, stps, err := cg.GenerateStmtIR(value.ExprsOrStmts[0], last, beginLbl, endLbl)
			if err != nil {
				return "", nil, err
			}
			setups = append(setups, l)

			setups = append(setups, stps...)

			cond, stps, err := cg.GenerateExprIR(value.ExprsOrStmts[1])
			if err != nil {
				return "", nil, err
			}

			for _, stp := range stps {
				line += stp + "\n"
			}
			line += fmt.Sprintf("jnz %s, %s, %s\n", cond, bodyLbl, endLbl)
			line += bodyLbl + "\n" + body + "\n"

			reass, stps, err := cg.GenerateStmtIR(value.ExprsOrStmts[2], last, beginLbl, endLbl)
			if err != nil {
				return "", nil, err
			}

			for _, stp := range stps {
				line += stp + "\n"
			}
			line += reass + "\n"
			line += fmt.Sprintf("jmp %s\n", beginLbl)
		} else {
			line += bodyLbl + "\n" + body + "\n"
		}

		line += endLbl
	default:
		return "", nil, shared.NewError(stmtNode.Loc, "unsupported statement %s", stmtNode.Type)
	}

	return line, setups, nil
}

func (cg *CodeGen) GenerateExprIR(exprNode *Node) (string, []string, error) {
	val := ""
	setups := []string{}
	switch exprNode.Type {

	case parser.NODE_TYPE_IDENTIFIER:
		val = fmt.Sprintf("%%%s", exprNode.Value.(string))

	case parser.NODE_TYPE_NUMBER_LITERAL:
		val = exprNode.Value.(string)

	case parser.NODE_TYPE_BOOL_LITERAL:
		if exprNode.Value.(string) == "true" {
			val = "1"
		} else {
			val = "0"
		}

	case parser.NODE_TYPE_FUNCTION_CALL:
		fname := typechecker.IdentToStr(exprNode.Value.(*Node))
		fsig := cg.funcSigs[fname]
		nm := cg.GetTmpVar()
		val = fmt.Sprintf("%s", nm)
		setup := fmt.Sprintf("%s =%s call $%s(", val, mapTypeToIRType(fsig.RetType), fname)
		for i, arg := range exprNode.Children {
			argType := mapTypeToIRType(fsig.ArgTypes[i].R)
			v, st, err := cg.GenerateExprIR(arg)
			if err != nil {
				return "", nil, err
			}

			setup += fmt.Sprintf("%s %s", argType, v)
			if i != len(exprNode.Children)-1 {
				setup += ", "
			}

			setups = append(setups, st...)
		}
		setup += ")"
		setups = append(setups, setup)

	case parser.NODE_TYPE_BINARY_OP:
		exprType := mapTypeToIRType(exprNode.Left.ExprType)

		op := ""
		switch exprNode.Value.(string) {
		case "+":
			op = "add"
		case "-":
			op = "sub"
		case "*":
			op = "mul"
		case "/":
			if shared.IsUnsignedType(exprNode.Left.ExprType) {
				op = "udiv"
			} else {
				op = "div"
			}
		case "%":
			if shared.IsUnsignedType(exprNode.Left.ExprType) {
				op = "urem"
			} else {
				op = "rem"
			}
		case "==":
			op = "ceq" + exprType
		case "!=":
			op = "cne" + exprType
		case "<":
			if shared.IsUnsignedType(exprNode.Left.ExprType) {
				op = "cult" + exprType
			} else {
				op = "cslt" + exprType
			}
		case ">":
			if shared.IsUnsignedType(exprNode.Left.ExprType) {
				op = "cugt" + exprType
			} else {
				op = "csgt" + exprType
			}
		case "<=":
			if shared.IsUnsignedType(exprNode.Left.ExprType) {
				op = "cule" + exprType
			} else {
				op = "csle" + exprType
			}
		case ">=":
			if shared.IsUnsignedType(exprNode.Left.ExprType) {
				op = "cuge" + exprType
			} else {
				op = "csge" + exprType
			}
		}

		nm := cg.GetTmpVar()
		val = fmt.Sprintf("%s", nm)
		v1, st1, err := cg.GenerateExprIR(exprNode.Left)
		if err != nil {
			return "", nil, err
		}
		v2, st2, err := cg.GenerateExprIR(exprNode.Right)
		if err != nil {
			return "", nil, err
		}
		setup := fmt.Sprintf("%s =%s %s %s, %s", val, exprType, op, v1, v2)
		setups = append(setups, st1...)
		setups = append(setups, st2...)
		setups = append(setups, setup)

	case parser.NODE_TYPE_UNARY_OP:
		nm := cg.GetTmpVar()
		val = fmt.Sprintf("%s", nm)
		setup := ""
		v, st, err := cg.GenerateExprIR(exprNode.Right)
		if err != nil {
			return "", nil, err
		}
		switch exprNode.Value.(string) {
		case "-":
			setup = fmt.Sprintf("%s =%s neg %s", val, mapTypeToIRType(exprNode.Right.ExprType), v)
		case "not":
			exprType := mapTypeToIRType(exprNode.Right.ExprType)
			setup = fmt.Sprintf("%s =%s cne%s %s, 1", val, exprType, exprType, v)

		}

		setups = append(setups, st...)
		setups = append(setups, setup)

	case parser.NODE_TYPE_IF_EXPR:
		ifEVal := exprNode.Value.(*parser.IfNodeValue)
		endLabel := cg.GetTmpLbl()
		nm := cg.GetTmpVar()
		val = fmt.Sprintf("%s", nm)
		exprType := mapTypeToIRType(exprNode.ExprType)

		condVal, condSetups, err := cg.GenerateExprIR(ifEVal.IfBranch.Condition)
		if err != nil {
			return "", nil, err
		}

		ifLabel := cg.GetTmpLbl()
		elseCheckLabel := cg.GetTmpLbl()

		setups = append(setups, condSetups...)
		setups = append(setups, fmt.Sprintf("jnz %s, %s, %s", condVal, ifLabel, elseCheckLabel))

		ifVal, ifSetups, err := cg.GenerateExprIR(ifEVal.IfBranch.Node)
		if err != nil {
			return "", nil, err
		}
		ifBlockCode := fmt.Sprintf("%s", ifLabel)
		for _, s := range ifSetups {
			ifBlockCode += "\n" + s
		}
		ifBlockCode += fmt.Sprintf("\n%s =%s copy %s", nm, exprType, ifVal)
		ifBlockCode += fmt.Sprintf("\njmp %s", endLabel)
		setups = append(setups, ifBlockCode)

		currentCheckLabel := elseCheckLabel

		for _, elseIf := range ifEVal.ElseIfBranches {
			elseIfCondVal, elseIfCondSetups, err := cg.GenerateExprIR(elseIf.Condition)
			if err != nil {
				return "", nil, err
			}

			elseIfLabel := cg.GetTmpLbl()
			nextCheckLabel := cg.GetTmpLbl()

			setups = append(setups, fmt.Sprintf("%s", currentCheckLabel))
			setups = append(setups, elseIfCondSetups...)
			setups = append(setups, fmt.Sprintf("jnz %s, %s, %s", elseIfCondVal, elseIfLabel, nextCheckLabel))

			elseIfVal, elseIfSetups, err := cg.GenerateExprIR(elseIf.Node)
			if err != nil {
				return "", nil, err
			}

			elseIfBlockCode := fmt.Sprintf("%s", elseIfLabel)
			for _, s := range elseIfSetups {
				elseIfBlockCode += "\n" + s
			}
			elseIfBlockCode += fmt.Sprintf("\n%s =%s copy %s", nm, exprType, elseIfVal)
			elseIfBlockCode += fmt.Sprintf("\njmp %s", endLabel)
			setups = append(setups, elseIfBlockCode)

			currentCheckLabel = nextCheckLabel
		}

		elseLabel := cg.GetTmpLbl()
		setups = append(setups, fmt.Sprintf("%s", currentCheckLabel))
		setups = append(setups, fmt.Sprintf("jmp %s", elseLabel))

		elseVal, elseSetups, err := cg.GenerateExprIR(ifEVal.ElseBranch.Node)
		if err != nil {
			return "", nil, err
		}
		elseBlockCode := fmt.Sprintf("%s", elseLabel)
		for _, s := range elseSetups {
			elseBlockCode += "\n" + s
		}
		elseBlockCode += fmt.Sprintf("\n%s =%s copy %s", nm, exprType, elseVal)
		elseBlockCode += fmt.Sprintf("\njmp %s", endLabel)
		setups = append(setups, elseBlockCode)

		setups = append(setups, fmt.Sprintf("%s", endLabel))

	case parser.NODE_TYPE_CAST:
		targetTypeIdent := exprNode.Left.Value.(string)
		targetType, _ := shared.ResolveType(targetTypeIdent)
		targetIRType := mapTypeToIRType(targetType)
		sourceType := exprNode.Right.ExprType
		sourceIRType := mapTypeToIRType(sourceType)

		sourceVal, sourceSetups, err := cg.GenerateExprIR(exprNode.Right)
		if err != nil {
			return "", nil, err
		}
		setups = append(setups, sourceSetups...)

		currentVal := sourceVal

		if sourceIRType != targetIRType {
			tmpVar := cg.GetTmpVar()

			if sourceIRType == "w" && targetIRType == "l" {
				if shared.IsUnsignedType(sourceType) {
					setups = append(setups, fmt.Sprintf("%s =l extuw %s", tmpVar, currentVal))
				} else {
					setups = append(setups, fmt.Sprintf("%s =l extsw %s", tmpVar, currentVal))
				}
				currentVal = tmpVar
			} else if sourceIRType == "l" && targetIRType == "w" {
				setups = append(setups, fmt.Sprintf("%s =w copy %s", tmpVar, currentVal))
				currentVal = tmpVar
			} else {
				return "", nil, shared.NewError(exprNode.Loc, "unsupported cast from %s to %s", sourceIRType, targetIRType)
			}
		}

		mask, ext := getTruncateMaskAndExt(targetType)
		if mask != 0 {
			tmpTrunc := cg.GetTmpVar()
			setups = append(setups, fmt.Sprintf("%s =w and %s, %d", tmpTrunc, currentVal, mask))
			currentVal = tmpTrunc

			if ext != "" {
				tmpExt := cg.GetTmpVar()
				setups = append(setups, fmt.Sprintf("%s =w %s %s", tmpExt, ext, currentVal))
				currentVal = tmpExt
			}
		}

		val = currentVal

	default:
		return "", nil, shared.NewError(exprNode.Loc, "unsupported expression %s", exprNode.Type)
	}

	mask, ext := getTruncateMaskAndExt(exprNode.ExprType)
	if mask != 0 {
		truncTmp := cg.GetTmpVar()
		setups = append(setups, fmt.Sprintf("%s =w and %s, %d", truncTmp, val, mask))
		val = truncTmp
		if ext != "" {
			extTmp := cg.GetTmpVar()
			setups = append(setups, fmt.Sprintf("%s =w %s %s", extTmp, ext, val))
			val = extTmp
		}
	}

	return val, setups, nil
}

func mapTypeToIRType(t Type) string {
	switch t {
	case shared.BUILTIN_VOID:
		return ""
	case shared.BUILTIN_BOOL:
		return "w"
	case shared.BUILTIN_I8, shared.BUILTIN_U8:
		return "w"
	case shared.BUILTIN_I16, shared.BUILTIN_U16:
		return "w"
	case shared.BUILTIN_I32, shared.BUILTIN_U32:
		return "w"
	case shared.BUILTIN_I64, shared.BUILTIN_U64:
		return "l"
	case shared.BUILTIN_UNTYPED_INT:
		return "l" // just in case
	default:
		panic("unknown type")
	}
}

func getTruncateMaskAndExt(t Type) (mask int, extOp string) {
	switch t {
	case shared.BUILTIN_U8:
		return 0xFF, ""
	case shared.BUILTIN_I8:
		return 0xFF, "extsb"
	case shared.BUILTIN_U16:
		return 0xFFFF, ""
	case shared.BUILTIN_I16:
		return 0xFFFF, "extsh"
	default:
		return 0, ""
	}
}
