package codegen

import (
	"fmt"
	"strconv"
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
	cnstId   uint
	rootNode *Node
	funcSigs map[string]*typechecker.FunctionSig
}

func NewCodeGen(rootNode *Node, funcSigs map[string]*typechecker.FunctionSig) *CodeGen {
	return &CodeGen{
		tmpVarId: 0,
		tmpLblId: 0,
		cnstId:   0,
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

func (cg *CodeGen) GetCnst() string {
	val := fmt.Sprintf("$___cnst%d", cg.cnstId)
	cg.cnstId++
	return val
}

func (cg *CodeGen) EmitIR() (string, error) {
	ir := ""
	gsetups := []string{}

	for _, node := range cg.rootNode.Children {
		if node.Type != parser.NODE_TYPE_FUNCTION_DEF {
			return "", shared.NewError(node.Loc, "unexpected token")
		}

		if len(node.Children) == 0 {
			continue
		}

		funcIr, gstps, err := cg.GenerateFuncIR(node)
		if err != nil {
			return "", err
		}

		ir += funcIr
		gsetups = append(gsetups, gstps...)
	}

	for _, gsetup := range gsetups {
		ir = gsetup + "\n" + ir
	}

	return ir, nil
}

// returns value string, global setups []string, error
func (cg *CodeGen) GenerateFuncIR(funcNode *Node) (string, []string, error) {
	cg.tmpVarId = 0
	cg.tmpLblId = 0
	prologue := ""
	body := ""
	epilogue := ""
	gsetups := []string{}

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
		b, gstps, _, err := cg.GenerateBlockIR(child, "", "")
		if err != nil {
			return "", nil, err
		}
		body = b

		if len(child.Children) > 1 &&
			child.Children[len(child.Children)-1].Type == parser.NODE_TYPE_IF &&
			fsig.RetType != shared.BUILTIN_VOID { // case when every if body returns
			epilogue = "ret 0" + epilogue
		}
		gsetups = append(gsetups, gstps...)
	} else {
		val, setups, gstps, _, err := cg.GenerateExprIR(child)
		if err != nil {
			return "", nil, err
		}
		for _, setup := range setups {
			body += "\n" + setup
		}
		body += "\nret " + val
		gsetups = append(gsetups, gstps...)
	}

	return prologue + body + epilogue, gsetups, nil
}

// returns body value string, global setups []string, endsWithTerminator bool, error
func (cg *CodeGen) GenerateBlockIR(blockNode *Node, loopBegin, loopEnd string) (string, []string, bool, error) {
	body := ""
	endsWithTerminator := false
	gsetups := []string{}
	for i, node := range blockNode.Children {
		ir, setups, gstps, err := cg.GenerateStmtIR(node, i == len(blockNode.Children)-1, loopBegin, loopEnd)
		if err != nil {
			return "", nil, false, err
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
		gsetups = append(gsetups, gstps...)
	}

	return body, gsetups, endsWithTerminator, nil
}

func IsTerminatorInstruction(ir string) bool {
	return strings.HasPrefix(ir, "ret") ||
		strings.HasPrefix(ir, "jmp") ||
		strings.HasPrefix(ir, "jnz")
}

// returns value string, local setups []string, global setups []string, error
func (cg *CodeGen) GenerateStmtIR(stmtNode *Node, last bool, loopBegin, loopEnd string) (string, []string, []string, error) {
	line := ""
	setups := []string{}
	gsetups := []string{}
	switch stmtNode.Type {
	case parser.NODE_TYPE_CONTROL_KEYWORD:
		switch stmtNode.Value.(string) {
		case "return":
			line = "ret"
			if stmtNode.Right != nil {
				line += " "
				val, setps, gsetps, _, err := cg.GenerateExprIR(stmtNode.Right)
				if err != nil {
					return "", nil, nil, err
				}
				line += val
				setups = append(setups, setps...)
				gsetups = append(gsetups, gsetps...)
			}
		case "break":
			if loopEnd == "" {
				return "", nil, nil, shared.NewError(stmtNode.Loc, "used 'break' keyword outside loop")
			}
			line = fmt.Sprintf("jmp %s", loopEnd)
		case "continue":
			if loopBegin == "" {
				return "", nil, nil, shared.NewError(stmtNode.Loc, "used 'continue' keyword outside loop")
			}
			line = fmt.Sprintf("jmp %s", loopBegin)
		}

	case parser.NODE_TYPE_DECLARATION:
		fallthrough
	case parser.NODE_TYPE_ASSIGNMENT:
		line = fmt.Sprintf("%%%s =%s ", typechecker.IdentToStr(stmtNode.Left), mapTypeToIRType(stmtNode.ExprType))

		val, setps, gsetps, _, err := cg.GenerateExprIR(stmtNode.Right)
		val = "copy " + val
		if err != nil {
			return "", nil, nil, err
		}

		line += val
		setups = append(setups, setps...)
		gsetups = append(gsetups, gsetps...)

	case parser.NODE_TYPE_FUNCTION_CALL:
		fname := typechecker.IdentToStr(stmtNode.Value.(*Node))
		fsig := cg.funcSigs[fname]

		line = fmt.Sprintf("call $%s(", fsig.Name)
		for i, arg := range stmtNode.Children {
			if i == len(fsig.ArgTypes) {
				line += "..., "
			}

			argType := ""

			val, setps, gsetps, exprTpe, err := cg.GenerateExprIR(arg)
			if err != nil {
				return "", nil, nil, err
			}
			if i < len(fsig.ArgTypes) {
				argType = mapTypeToIRType(fsig.ArgTypes[i].R)
			} else {
				argType = exprTpe
			}
			line += fmt.Sprintf("%s %s", argType, val)
			if i != len(stmtNode.Children)-1 {
				line += ", "
			}

			setups = append(setups, setps...)
			gsetups = append(gsetups, gsetps...)
		}

		line += ")"

	case parser.NODE_TYPE_IF:
		ifVal := stmtNode.Value.(*parser.IfNodeValue)
		endLabel := cg.GetTmpLbl()

		condVal, condSetups, condGSetups, _, err := cg.GenerateExprIR(ifVal.IfBranch.Condition)
		if err != nil {
			return "", nil, nil, err
		}
		ifLabel := cg.GetTmpLbl()
		elseCheckLabel := cg.GetTmpLbl()

		setups = append(setups, condSetups...)
		gsetups = append(gsetups, condGSetups...)
		setups = append(setups, fmt.Sprintf("jnz %s, %s, %s", condVal, ifLabel, elseCheckLabel))

		ifBlockIR, blockGSetups, ifTerminated, err := cg.GenerateBlockIR(ifVal.IfBranch.Node, loopBegin, loopEnd)
		if err != nil {
			return "", nil, nil, err
		}
		setups = append(setups, fmt.Sprintf("%s", ifLabel))
		setups = append(setups, ifBlockIR)
		if !ifTerminated {
			setups = append(setups, fmt.Sprintf("jmp %s", endLabel))
		}
		gsetups = append(gsetups, blockGSetups...)

		currentCheckLabel := elseCheckLabel

		for _, elseIf := range ifVal.ElseIfBranches {
			elseIfCondVal, elseIfCondSetups, elseIfCondGSetups, _, err := cg.GenerateExprIR(elseIf.Condition)
			if err != nil {
				return "", nil, nil, err
			}

			elseIfLabel := cg.GetTmpLbl()
			nextCheckLabel := cg.GetTmpLbl()

			setups = append(setups, fmt.Sprintf("%s", currentCheckLabel))
			setups = append(setups, elseIfCondSetups...)
			gsetups = append(gsetups, elseIfCondGSetups...)
			setups = append(setups, fmt.Sprintf("jnz %s, %s, %s", elseIfCondVal, elseIfLabel, nextCheckLabel))

			elseIfBlockIR, blockGSetups, elseIfTerminated, err := cg.GenerateBlockIR(elseIf.Node, loopBegin, loopEnd)
			if err != nil {
				return "", nil, nil, err
			}
			setups = append(setups, fmt.Sprintf("%s", elseIfLabel))
			setups = append(setups, elseIfBlockIR)
			if !elseIfTerminated {
				setups = append(setups, fmt.Sprintf("jmp %s", endLabel))
			}
			gsetups = append(gsetups, blockGSetups...)

			currentCheckLabel = nextCheckLabel
		}

		if ifVal.ElseBranch != nil {
			elseLabel := cg.GetTmpLbl()
			setups = append(setups, fmt.Sprintf("%s", currentCheckLabel))
			setups = append(setups, fmt.Sprintf("jmp %s", elseLabel))

			elseBlockIR, blockGSetups, elseTerminated, err := cg.GenerateBlockIR(ifVal.ElseBranch.Node, loopBegin, loopEnd)
			if err != nil {
				return "", nil, nil, err
			}
			setups = append(setups, fmt.Sprintf("%s", elseLabel))
			setups = append(setups, elseBlockIR)
			if !elseTerminated {
				setups = append(setups, fmt.Sprintf("jmp %s", endLabel))
			}
			gsetups = append(gsetups, blockGSetups...)
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
		body, blockGSetups, _, err := cg.GenerateBlockIR(value.Body, beginLbl, endLbl)
		if err != nil {
			return "", nil, nil, err
		}

		if len(value.ExprsOrStmts) == 1 {
			cond, setps, gsetps, _, err := cg.GenerateExprIR(value.ExprsOrStmts[0])
			if err != nil {
				return "", nil, nil, err
			}

			for _, stp := range setps {
				line += stp + "\n"
			}
			gsetups = append(gsetups, gsetps...)
			line += fmt.Sprintf("jnz %s %s, %s\n", cond, bodyLbl, endLbl)
			line += bodyLbl + "\n" + body + "\n"
			line += fmt.Sprintf("jmp %s\n", beginLbl)
		} else if len(value.ExprsOrStmts) == 3 {
			l, setps, gsetps, err := cg.GenerateStmtIR(value.ExprsOrStmts[0], last, beginLbl, endLbl)
			if err != nil {
				return "", nil, nil, err
			}
			setups = append(setups, l)

			setups = append(setups, setps...)
			gsetups = append(gsetups, gsetps...)

			cond, setps, gsetps, _, err := cg.GenerateExprIR(value.ExprsOrStmts[1])
			if err != nil {
				return "", nil, nil, err
			}

			for _, stp := range setps {
				line += stp + "\n"
			}
			line += fmt.Sprintf("jnz %s, %s, %s\n", cond, bodyLbl, endLbl)
			line += bodyLbl + "\n" + body + "\n"

			reass, setps, gsetps, err := cg.GenerateStmtIR(value.ExprsOrStmts[2], last, beginLbl, endLbl)
			if err != nil {
				return "", nil, nil, err
			}

			for _, stp := range setps {
				line += stp + "\n"
			}
			gsetups = append(gsetups, gsetps...)
			line += reass + "\n"
			line += fmt.Sprintf("jmp %s\n", beginLbl)
		} else {
			line += bodyLbl + "\n" + body + "\n"
		}

		line += endLbl
		gsetups = append(gsetups, blockGSetups...)
	default:
		return "", nil, nil, shared.NewError(stmtNode.Loc, "unsupported statement %s", stmtNode.Type)
	}

	return line, setups, gsetups, nil
}

// returns value string, local setups []string, global setups []string, error
func (cg *CodeGen) GenerateExprIR(exprNode *Node) (string, []string, []string, string, error) {
	val := ""
	setups := []string{}
	gsetups := []string{}
	tpe := mapTypeToIRType(exprNode.ExprType)
	switch exprNode.Type {
	case parser.NODE_TYPE_IDENTIFIER:
		val = fmt.Sprintf("%%%s", exprNode.Value.(string))

	case parser.NODE_TYPE_NUMBER_LITERAL:
		val = exprNode.Value.(string)

	case parser.NODE_TYPE_CHAR_LITERAL:
		val = strconv.Itoa(int(exprNode.Value.(byte)))

	case parser.NODE_TYPE_STRING_LITERAL:
		nme := cg.GetCnst()
		val = nme
		gsetups = append(gsetups, fmt.Sprintf("data %s = { b \"%s\", b 0 }", nme, strings.NewReplacer("\\", `\\`,
			"\"", `\"`,
			"\n", `\n`,
			"\r", `\r`,
			"\t", `\t`,
			"\b", `\b`,
			"\f", `\f`,
			"\v", `\v`,
			"\a", `\a`,
			string(rune(0)), `\0`,
		).Replace(exprNode.Value.(string))))

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
		tpe = mapTypeToIRType(fsig.RetType)
		setup := fmt.Sprintf("%s =%s call $%s(", val, tpe, fname)
		for i, arg := range exprNode.Children {
			if i == len(fsig.ArgTypes) {
				setup += "..., "
			}

			argType := ""

			v, st, gst, exprTpe, err := cg.GenerateExprIR(arg)
			if err != nil {
				return "", nil, nil, "", err
			}
			if i < len(fsig.ArgTypes) {
				argType = mapTypeToIRType(fsig.ArgTypes[i].R)
			} else {
				argType = exprTpe
			}
			setup += fmt.Sprintf("%s %s", argType, v)
			if i != len(exprNode.Children)-1 {
				setup += ", "
			}

			setups = append(setups, st...)
			gsetups = append(gsetups, gst...)
		}
		setup += ")"
		setups = append(setups, setup)

	case parser.NODE_TYPE_BINARY_OP:
		tpe = mapTypeToIRType(exprNode.Left.ExprType)

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
			op = "ceq" + tpe
		case "!=":
			op = "cne" + tpe
		case "<":
			if shared.IsUnsignedType(exprNode.Left.ExprType) {
				op = "cult" + tpe
			} else {
				op = "cslt" + tpe
			}
		case ">":
			if shared.IsUnsignedType(exprNode.Left.ExprType) {
				op = "cugt" + tpe
			} else {
				op = "csgt" + tpe
			}
		case "<=":
			if shared.IsUnsignedType(exprNode.Left.ExprType) {
				op = "cule" + tpe
			} else {
				op = "csle" + tpe
			}
		case ">=":
			if shared.IsUnsignedType(exprNode.Left.ExprType) {
				op = "cuge" + tpe
			} else {
				op = "csge" + tpe
			}
		}

		nm := cg.GetTmpVar()
		val = fmt.Sprintf("%s", nm)
		v1, st1, gst1, _, err := cg.GenerateExprIR(exprNode.Left)
		if err != nil {
			return "", nil, nil, "", err
		}
		v2, st2, gst2, _, err := cg.GenerateExprIR(exprNode.Right)
		if err != nil {
			return "", nil, nil, "", err
		}
		setup := fmt.Sprintf("%s =%s %s %s, %s", val, tpe, op, v1, v2)
		setups = append(setups, st1...)
		setups = append(setups, st2...)
		gsetups = append(gsetups, gst1...)
		gsetups = append(gsetups, gst2...)
		setups = append(setups, setup)

	case parser.NODE_TYPE_UNARY_OP:
		nm := cg.GetTmpVar()
		val = fmt.Sprintf("%s", nm)
		setup := ""
		v, st, gst, _, err := cg.GenerateExprIR(exprNode.Right)
		if err != nil {
			return "", nil, nil, "", err
		}
		switch exprNode.Value.(string) {
		case "-":
			setup = fmt.Sprintf("%s =%s neg %s", val, mapTypeToIRType(exprNode.Right.ExprType), v)
		case "not":
			tpe := mapTypeToIRType(exprNode.Right.ExprType)
			setup = fmt.Sprintf("%s =%s cne%s %s, 1", val, tpe, tpe, v)

		}

		setups = append(setups, st...)
		gsetups = append(gsetups, gst...)
		setups = append(setups, setup)

	case parser.NODE_TYPE_IF_EXPR:
		ifEVal := exprNode.Value.(*parser.IfNodeValue)
		endLabel := cg.GetTmpLbl()
		nm := cg.GetTmpVar()
		val = fmt.Sprintf("%s", nm)
		tpe := mapTypeToIRType(exprNode.ExprType)

		condVal, condSetups, condGSetups, _, err := cg.GenerateExprIR(ifEVal.IfBranch.Condition)
		if err != nil {
			return "", nil, nil, "", err
		}

		ifLabel := cg.GetTmpLbl()
		elseCheckLabel := cg.GetTmpLbl()

		setups = append(setups, condSetups...)
		gsetups = append(gsetups, condGSetups...)
		setups = append(setups, fmt.Sprintf("jnz %s, %s, %s", condVal, ifLabel, elseCheckLabel))

		ifVal, ifSetups, ifGSetups, _, err := cg.GenerateExprIR(ifEVal.IfBranch.Node)
		if err != nil {
			return "", nil, nil, "", err
		}
		ifBlockCode := fmt.Sprintf("%s", ifLabel)
		for _, s := range ifSetups {
			ifBlockCode += "\n" + s
		}
		gsetups = append(gsetups, ifGSetups...)
		ifBlockCode += fmt.Sprintf("\n%s =%s copy %s", nm, tpe, ifVal)
		ifBlockCode += fmt.Sprintf("\njmp %s", endLabel)
		setups = append(setups, ifBlockCode)

		currentCheckLabel := elseCheckLabel

		for _, elseIf := range ifEVal.ElseIfBranches {
			elseIfCondVal, elseIfCondSetups, elseIfCondGSetups, _, err := cg.GenerateExprIR(elseIf.Condition)
			if err != nil {
				return "", nil, nil, "", err
			}

			elseIfLabel := cg.GetTmpLbl()
			nextCheckLabel := cg.GetTmpLbl()

			setups = append(setups, fmt.Sprintf("%s", currentCheckLabel))
			setups = append(setups, elseIfCondSetups...)
			gsetups = append(gsetups, elseIfCondGSetups...)
			setups = append(setups, fmt.Sprintf("jnz %s, %s, %s", elseIfCondVal, elseIfLabel, nextCheckLabel))

			elseIfVal, elseIfSetups, elseIfGSetups, _, err := cg.GenerateExprIR(elseIf.Node)
			if err != nil {
				return "", nil, nil, "", err
			}

			elseIfBlockCode := fmt.Sprintf("%s", elseIfLabel)
			for _, s := range elseIfSetups {
				elseIfBlockCode += "\n" + s
			}
			gsetups = append(gsetups, elseIfGSetups...)
			elseIfBlockCode += fmt.Sprintf("\n%s =%s copy %s", nm, tpe, elseIfVal)
			elseIfBlockCode += fmt.Sprintf("\njmp %s", endLabel)
			setups = append(setups, elseIfBlockCode)

			currentCheckLabel = nextCheckLabel
		}

		elseLabel := cg.GetTmpLbl()
		setups = append(setups, fmt.Sprintf("%s", currentCheckLabel))
		setups = append(setups, fmt.Sprintf("jmp %s", elseLabel))

		elseVal, elseSetups, elseGSetups, _, err := cg.GenerateExprIR(ifEVal.ElseBranch.Node)
		if err != nil {
			return "", nil, nil, "", err
		}
		elseBlockCode := fmt.Sprintf("%s", elseLabel)
		for _, s := range elseSetups {
			elseBlockCode += "\n" + s
		}
		gsetups = append(gsetups, elseGSetups...)
		elseBlockCode += fmt.Sprintf("\n%s =%s copy %s", nm, tpe, elseVal)
		elseBlockCode += fmt.Sprintf("\njmp %s", endLabel)
		setups = append(setups, elseBlockCode)

		setups = append(setups, fmt.Sprintf("%s", endLabel))

	case parser.NODE_TYPE_CAST:
		targetTypeIdent := exprNode.Left.Value.(string)
		targetType, _ := shared.ResolveType(targetTypeIdent)
		targetIRType := mapTypeToIRType(targetType)
		sourceType := exprNode.Right.ExprType
		sourceIRType := mapTypeToIRType(sourceType)

		sourceVal, sourceSetups, sourceGSetups, _, err := cg.GenerateExprIR(exprNode.Right)
		if err != nil {
			return "", nil, nil, "", err
		}
		setups = append(setups, sourceSetups...)
		gsetups = append(setups, sourceGSetups...)

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
				return "", nil, nil, "", shared.NewError(exprNode.Loc, "unsupported cast from %s to %s", sourceIRType, targetIRType)
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
		return "", nil, nil, "", shared.NewError(exprNode.Loc, "unsupported expression %s", exprNode.Type)
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

	return val, setups, gsetups, tpe, nil
}

func mapTypeToIRType(t Type) string {
	switch t {
	case shared.BUILTIN_VOID:
		return ""
	case shared.BUILTIN_BOOL:
		return "w"
	case shared.BUILTIN_CHAR:
		return "w"
	case shared.BUILTIN_STRING:
		return "l"
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
