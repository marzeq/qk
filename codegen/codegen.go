package codegen

import (
  "fmt"
  "strconv"
  "strings"

  "github.com/marzeq/quokka/parser"
  "github.com/marzeq/quokka/shared"
  "github.com/marzeq/quokka/typechecker"
)

type CodeGen struct {
  tmpVarId uint
  tmpLblId uint
  cnstId uint
  rootNode *parser.RootNode
  mod string
  modSigs typechecker.ModulesSignatures
}

func NewCodeGen(rootNode *parser.RootNode, mod string, ms typechecker.ModulesSignatures) *CodeGen {
  return &CodeGen{
    tmpVarId: 0,
    tmpLblId: 0,
    cnstId: 0,
    rootNode: rootNode,
    mod: mod,
    modSigs: ms,
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

func modFieldToString(mod string, name string) string {
  if mod != "" {
    return fmt.Sprintf("___%s_%s", mod, name)
  } 
  return fmt.Sprintf("___%s", name)
}

func (cg *CodeGen) EmitIR() (string, error) {
  ir := ""
  gsetups := []string{}

  for mod, ms := range cg.modSigs {
    for name, tpe := range ms.TypeTable {
      if !tpe.IsStruct() {
        continue
      }

      ir += fmt.Sprintf("type :%s = {", modFieldToString(mod, name))
      for _, field := range tpe.(shared.Struct).Fields {
        ir += fmt.Sprintf(" %s,", mapTypeToIRType(field.R))
      }
      ir += "}\n"
    }
  }

  for _, n := range cg.rootNode.Body {
    switch node := n.(type) {
    case *parser.ModuleNode:
      continue
    case *parser.StructDefNode:
      continue
    case *parser.FunctionDefNode:
      if node.Body == nil {
        continue
      }

      funcIr, gstps, err := cg.GenerateFuncIR(node)
      if err != nil {
        return "", err
      }

      ir += funcIr
      gsetups = append(gsetups, gstps...)
    default:
      return "", shared.NewError(node.GetLoc(), "unexpected node")
    }
  }

  for _, gsetup := range gsetups {
    ir = gsetup + "\n" + ir
  }

  return ir, nil
}

func (cg *CodeGen) GenerateFuncIR(funcNode *parser.FunctionDefNode) (string, []string, error) {
  cg.tmpVarId = 0
  cg.tmpLblId = 0
  prologue := ""
  body := ""
  epilogue := ""
  gsetups := []string{}

  fsig, ok := cg.modSigs.LookupFunction(cg.mod, funcNode.Name, cg.mod)
  if !ok {
    return "", nil, shared.NewError(funcNode.Loc, "fatal: function %s should have been in the signature table", funcNode.Name)
  }

  if funcNode.Exported {
    prologue += "export "
  }

  prologue += fmt.Sprintf("function %s $%s(", mapTypeToIRType(fsig.RetType), modFieldToString(cg.mod, fsig.Name))
  after := ""
  for i, arg := range fsig.ArgTypes {
    tnm := cg.GetTmpVar()
    tpe := ""
    if arg.Type.IsStruct() {
      found := false
      for mod, ms := range cg.modSigs {
        for name, st := range ms.TypeTable {
          if st.Compare(arg.Type) {
            tpe = fmt.Sprintf(":%s", modFieldToString(mod, name))
            found = true
            break
          }
        }
        if found {
          break
        }
      }
      if tpe == "" {
        tpe = "l"
      }
    } else {
      tpe = mapTypeToIRType(arg.Type)
    }
    prologue += fmt.Sprintf("%s %s", tpe, tnm)
    if i != len(fsig.ArgTypes)-1 {
      prologue += ", "
    }

    after += fmt.Sprintf("%%%s =l %s\n", arg.Name, emitAllocForType(arg.Type, 1))
    switch argR := arg.Type.(type) {
    case shared.Struct:
      after += fmt.Sprintf("blit %s, %%%s, %d\n", tnm, arg.Name, argR.GetLayout().Size)
    default:
      after += fmt.Sprintf("store%s %s, %%%s\n", tpe, tnm, arg.Name)
    }
  }

  prologue += ") {\n@start\n"
  prologue += after

  if fsig.ImplicitReturn {
    epilogue = "\nret"
  }

  epilogue += "\n}\n"


  switch funcBody := funcNode.Body.(type) {
  case *parser.BlockNode:
    b, gstps, _, err := cg.GenerateBlockIR(funcBody, "", "")
    if err != nil {
      return "", nil, err
    }
    body = b

    if len(funcBody.Body) > 1 {
      switch funcBody.Body[len(funcBody.Body)-1].(type) {
      case *parser.IfNode:
        if fsig.RetType != shared.PRIMITIVE_VOID {
          epilogue = "ret 0" + epilogue
        }
      }
    }
    gsetups = append(gsetups, gstps...)
  default:
    if expr, ok := funcNode.Body.(parser.ExpressionNode); ok {
      val, setups, gstps, _, err := cg.GenerateExprIR(expr)
      if err != nil {
        return "", nil, err
      }
      for _, setup := range setups {
        body += "\n" + setup
      }
      body += "\nret " + val
      gsetups = append(gsetups, gstps...)
    }
  }

  return prologue + body + epilogue, gsetups, nil
}

func (cg *CodeGen) GenerateBlockIR(blockNode *parser.BlockNode, loopBegin, loopEnd string) (string, []string, bool, error) {
  body := ""
  endsWithTerminator := false
  gsetups := []string{}
  for i, node := range blockNode.Body {
    ir, setups, gstps, err := cg.GenerateStmtIR(node, i == len(blockNode.Body)-1, loopBegin, loopEnd)
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

func (cg *CodeGen) GenerateStmtIR(stmtNd parser.Node, last bool, loopBegin, loopEnd string) (string, []string, []string, error) {
  line := ""
  setups := []string{}
  gsetups := []string{}
  switch stmtNode := stmtNd.(type) {
  case *parser.ControlKeywordNode:
    switch stmtNode.Keyword {
    case parser.KEYWORD_TYPE_RETURN:
      line = "ret"
      if stmtNode.ReturnValue != nil {
        line += " "
        val, setps, gsetps, _, err := cg.GenerateExprIR(stmtNode.ReturnValue)
        if err != nil {
          return "", nil, nil, err
        }
        line += val
        setups = append(setups, setps...)
        gsetups = append(gsetups, gsetps...)
      }
    case parser.KEYWORD_TYPE_BREAK:
      if loopEnd == "" {
        return "", nil, nil, shared.NewError(stmtNode.GetLoc(), "used 'break' keyword outside loop")
      }
      line = fmt.Sprintf("jmp %s", loopEnd)
    case parser.KEYWORD_TYPE_CONTINUE:
      if loopBegin == "" {
        return "", nil, nil, shared.NewError(stmtNode.GetLoc(), "used 'continue' keyword outside loop")
      }
      line = fmt.Sprintf("jmp %s", loopBegin)
    }

  case *parser.DeclarationNode:
    nme := stmtNode.Name
    switch assignee := stmtNode.Value.(type) {
    case *parser.IdentifierNode:
      lastId := getLastIdentifierInChain(assignee)
      setups = append(setups, fmt.Sprintf("%%%s =l %s", nme, emitAllocForType(lastId.GetType(), 1)))

      val, setps, gsetps, _, err := cg.GenerateExprIR(assignee)
      if err != nil {
        return "", nil, nil, err
      }

      switch strct := lastId.GetType().(type) {
      case shared.Struct:
        line += fmt.Sprintf("blit %s, %%%s, %d", val, nme, strct.GetLayout().Size)
      default:
        line += fmt.Sprintf("store%s %s, %%%s", mapTypeToIRType(lastId.GetType()), val, nme)
      }
      
      setups = append(setups, setps...)
      gsetups = append(gsetups, gsetps...)
    default:
      setups = append(setups, fmt.Sprintf("%%%s =l %s", nme, emitAllocForType(assignee.GetType(), 1)))

      val, setps, gsetps, _, err := cg.GenerateExprIR(assignee)
      if err != nil {
        return "", nil, nil, err
      }

      switch strct := assignee.GetType().(type) {
      case shared.Struct:
        line += fmt.Sprintf("blit %s, %%%s, %d", val, nme, strct.GetLayout().Size)
      default:
        line += fmt.Sprintf("store%s %s, %%%s", mapTypeToIRType(assignee.GetType()), val, nme)
      }

      setups = append(setups, setps...)
      gsetups = append(gsetups, gsetps...)
    }

  case *parser.AssignmentNode:
    switch assignee := stmtNode.Assignee.(type) {
    case *parser.IdentifierNode:
      lastId := getLastIdentifierInChain(assignee)
      val, setps, gsetps, _, err := cg.GenerateExprIR(stmtNode.Value)
      if err != nil {
        return "", nil, nil, err
      }

      deepname, tpe, deepstps, err := cg.EmitFieldAccess("%"+assignee.Name, assignee, assignee.ExprType, true)
      if err != nil {
        return "", nil, nil, err
      }

      switch strct := lastId.GetType().(type) {
      case shared.Struct:
        line += fmt.Sprintf("blit %s, %s, %d", val, deepname, strct.GetLayout().Size)
      default:
        line += fmt.Sprintf("store%s %s, %s", mapTypeToIRType(tpe), val, deepname)
      }

      setups = append(setups, setps...)
      gsetups = append(gsetups, gsetps...)
      setups = append(setups, deepstps...)
    case *parser.UnaryOpNode:
      val, setps, gsetps, _, err := cg.GenerateExprIR(stmtNode.Value)
      if err != nil {
        return "", nil, nil, err
      }
      ptrVal, ptrSetps, ptrGSetps, _, err := cg.GenerateExprIR(assignee.Operand)
      if err != nil {
        return "", nil, nil, err
      }
      line = fmt.Sprintf("store%s %s, %s", mapTypeToIRType(assignee.GetType()), val, ptrVal)
      setups = append(setups, ptrSetps...)
      setups = append(setups, setps...)
      gsetups = append(gsetups, ptrGSetps...)
      gsetups = append(gsetups, gsetps...)
    default:
      return "", nil, nil, shared.NewError(stmtNode.GetLoc(), "invalid assignment target")
    }

  case *parser.FunctionCallNode:
    fsig, ok := cg.modSigs.LookupFunction(stmtNode.Name.ModName, stmtNode.Name.Ident.Name, cg.mod)
    if !ok {
      return "", nil, nil, shared.NewError(stmtNode.GetLoc(), "fatal: function %s not found in signature table", stmtNode.Name)
    }

    if fsig.ExternFrom != "" {
      line = fmt.Sprintf("call $%s(", fsig.ExternFrom)
    } else {
      line = fmt.Sprintf("call $%s(", modFieldToString(stmtNode.Name.ModName, fsig.Name))
    }
    for i, arg := range stmtNode.Args {
      if i == len(fsig.ArgTypes) {
        line += "..., "
      }

      argType := ""

      val, setps, gsetps, exprTpe, err := cg.GenerateExprIR(arg)
      if err != nil {
        return "", nil, nil, err
      }
      if i < len(fsig.ArgTypes) {
        if fsig.ArgTypes[i].Type.IsStruct() {
          found := false
          for mod, ms := range cg.modSigs {
            for name, st := range ms.TypeTable {
              if st.Compare(fsig.ArgTypes[i].Type) {
                argType  = fmt.Sprintf(":%s", modFieldToString(mod, name))
                found = true
                break
              }
            }
            if found {
              break
            }
          }
          if argType == "" {
            argType = "l"
          }
        } else {
          argType  = mapTypeToIRType(fsig.ArgTypes[i].Type)
        }
      } else {
        argType = exprTpe
      }
      line += fmt.Sprintf("%s %s", argType, val)
      if i != len(stmtNode.Args)-1 {
        line += ", "
      }

      setups = append(setups, setps...)
      gsetups = append(gsetups, gsetps...)
    }

    line += ")"

  case *parser.IfNode:
    endLabel := cg.GetTmpLbl()

    condVal, condSetups, condGSetups, _, err := cg.GenerateExprIR(stmtNode.IfBranch.Condition)
    if err != nil {
      return "", nil, nil, err
    }
    ifLabel := cg.GetTmpLbl()
    elseCheckLabel := cg.GetTmpLbl()

    setups = append(setups, condSetups...)
    gsetups = append(gsetups, condGSetups...)
    setups = append(setups, fmt.Sprintf("jnz %s, %s, %s", condVal, ifLabel, elseCheckLabel))

    ifBlockIR, blockGSetups, ifTerminated, err := cg.GenerateBlockIR(stmtNode.IfBranch.Node, loopBegin, loopEnd)
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

    for _, elseIf := range stmtNode.ElseIfBranches {
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

    if stmtNode.ElseBranch != nil {
      elseLabel := cg.GetTmpLbl()
      setups = append(setups, fmt.Sprintf("%s", currentCheckLabel))
      setups = append(setups, fmt.Sprintf("jmp %s", elseLabel))

      elseBlockIR, blockGSetups, elseTerminated, err := cg.GenerateBlockIR(stmtNode.ElseBranch, loopBegin, loopEnd)
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

  case *parser.ForNode:
    beginLbl := cg.GetTmpLbl()
    bodyLbl := cg.GetTmpLbl()
    endLbl := cg.GetTmpLbl()
    line = beginLbl + "\n"
    body, blockGSetups, _, err := cg.GenerateBlockIR(stmtNode.Body, beginLbl, endLbl)
    if err != nil {
      return "", nil, nil, err
    }

    if len(stmtNode.ExprsOrStmts) == 1 {
      if condExpr, ok := stmtNode.ExprsOrStmts[0].(parser.ExpressionNode); ok {
        cond, setps, gsetps, _, err := cg.GenerateExprIR(condExpr)
        if err != nil {
          return "", nil, nil, err
        }

        for _, stp := range setps {
          line += stp + "\n"
        }
        gsetups = append(gsetups, gsetps...)
        line += fmt.Sprintf("jnz %s, %s, %s\n", cond, bodyLbl, endLbl)
        line += bodyLbl + "\n" + body + "\n"
        line += fmt.Sprintf("jmp %s\n", beginLbl)
      } else {
        return "", nil, nil, shared.NewError(stmtNode.GetLoc(), "unexpected statement in for loop")
      }
    } else if len(stmtNode.ExprsOrStmts) == 3 {
      l, setps, gsetps, err := cg.GenerateStmtIR(stmtNode.ExprsOrStmts[0], last, beginLbl, endLbl)
      if err != nil {
        return "", nil, nil, err
      }

      setups = append(setups, setps...)
      setups = append(setups, l)
      gsetups = append(gsetups, gsetps...)

      if condExpr, ok := stmtNode.ExprsOrStmts[1].(parser.ExpressionNode); ok {
        cond, setps, gsetps, _, err := cg.GenerateExprIR(condExpr)
        if err != nil {
          return "", nil, nil, err
        }

        for _, stp := range setps {
          line += stp + "\n"
        }
        gsetups = append(gsetups, gsetps...)
        line += fmt.Sprintf("jnz %s, %s, %s\n", cond, bodyLbl, endLbl)
        line += bodyLbl + "\n" + body + "\n"
      } else {
        return "", nil, nil, shared.NewError(stmtNode.GetLoc(), "unexpected statement in for loop")
      }

      reass, setps, gsetps, err := cg.GenerateStmtIR(stmtNode.ExprsOrStmts[2], last, beginLbl, endLbl)
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
      line += fmt.Sprintf("jmp %s\n", bodyLbl)
      line += bodyLbl + "\n" + body + "\n"
      line += fmt.Sprintf("jmp %s\n", beginLbl)
    }

    line += endLbl
    gsetups = append(gsetups, blockGSetups...)
  default:
    return "", nil, nil, shared.NewError(stmtNode.GetLoc(), "unexpected statement")
  }

  return line, setups, gsetups, nil
}

func (cg *CodeGen) EmitFieldAccess(baseVar string, field *parser.IdentifierNode, tpe shared.Type, getAddress bool) (string, shared.Type, []string, error) {
  var irs []string
  if field.Next == nil {
    if getAddress {
      return baseVar, tpe, irs, nil
    }
    irt := mapTypeToIRType(tpe)
    tmp := cg.GetTmpVar()
    irs = append(irs, fmt.Sprintf("%s =%s load%s %s", tmp, irt, irt, baseVar))
    return tmp, tpe, irs, nil
  }

  var strct shared.Struct
  if tpe.IsStruct() {
    strct = tpe.(shared.Struct)
  } else if tpe.IsPointer() && tpe.(shared.Pointer).To.IsStruct() {
    strct = tpe.(shared.Pointer).To.(shared.Struct)
  } else {
    return "", nil, nil, shared.NewError(field.Loc, "cannot access field on non-struct type")
  }

  var f shared.Pair[string, shared.Type]
  found := false
  for _, fld := range strct.Fields {
    if fld.L == field.Next.Name {
      f = fld
      found = true
      break
    }
  }
  if !found {
    return "", nil, nil, shared.NewError(field.Next.Loc, "type has no field named '%s'", field.Next.Name)
  }

  layout := strct.GetLayout()
  fieldIdx := -1
  for i, fld := range strct.Fields {
    if fld.L == f.L {
      fieldIdx = i
      break
    }
  }
  offset := layout.Offsets[fieldIdx]

  if tpe.IsPointer() {
    loadedTmp := cg.GetTmpVar()
    irs = append(irs, fmt.Sprintf("%s =l loadl %s", loadedTmp, baseVar))

    addrTmp := cg.GetTmpVar()
    irs = append(irs, fmt.Sprintf("%s =l add %s, %d", addrTmp, loadedTmp, offset))

    tmp, ftype, nextIrs, err := cg.EmitFieldAccess(addrTmp, field.Next, f.R, getAddress)

    if err != nil {
      return "", nil, nil, err
    }
    irs = append(irs, nextIrs...)
    return tmp, ftype, irs, nil
  } else {
    addrTmp := cg.GetTmpVar()
    irs = append(irs, fmt.Sprintf("%s =l add %s, %d", addrTmp, baseVar, offset))

    tmp, ftype, nextIrs, err := cg.EmitFieldAccess(addrTmp, field.Next, f.R, getAddress)
    if err != nil {
      return "", nil, nil, err
    }
    irs = append(irs, nextIrs...)
    return tmp, ftype, irs, nil
  }
}

func (cg *CodeGen) GenerateExprIR(eNode parser.ExpressionNode) (string, []string, []string, string, error) {
  val := ""
  setups := []string{}
  gsetups := []string{}
  tpe := mapTypeToIRType(eNode.GetType())
  switch exprNode := eNode.(type) {
  case *parser.IdentifierNode:
    tnme := ""
    irs := []string{}

    if exprNode.Next != nil {
      gotname, gottpe, gotirs, err := cg.EmitFieldAccess("%"+exprNode.Name, exprNode, exprNode.ExprType, false)
      if err != nil {
        return "", nil, nil, "", err
      }
      tpe = mapTypeToIRType(gottpe)
      irs = append(irs, gotirs...)
      tnme = gotname
    } else {
      tnme = cg.GetTmpVar()
      irt := mapTypeToIRType(exprNode.GetType())
      if exprNode.GetType().IsStruct() {
        irs = append(irs, fmt.Sprintf("%s =%s copy %%%s", tnme, irt, exprNode.Name))
      } else {
        irs = append(irs, fmt.Sprintf("%s =%s load%s %%%s", tnme, irt, irt, exprNode.Name))
      }
    }

    setups = append(setups, irs...)
    val = tnme

  case *parser.NumberLiteralNode:
    val = exprNode.Value

  case *parser.NilLiteralNode:
    val = "0"

  case *parser.CharLiteralNode:
    val = strconv.Itoa(int(exprNode.Value))

  case *parser.StringLiteralNode:
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
      ).Replace(exprNode.Value)))

  case *parser.BoolLiteralNode:
    if exprNode.Value == "true" {
      val = "1"
    } else {
      val = "0"
    }

  case *parser.FunctionCallNode:
    fsig, ok := cg.modSigs.LookupFunction(exprNode.Name.ModName, exprNode.Name.Ident.Name, cg.mod)
    if !ok {
      return "", nil, nil, "", shared.NewError(exprNode.GetLoc(), "fatal: function %s not found in signature table", exprNode.Name)
    }
    val = cg.GetTmpVar()
    tpe = mapTypeToIRType(fsig.RetType)
    setup := ""
    if fsig.ExternFrom != "" {
      setup = fmt.Sprintf("%s =%s call $%s(", val, tpe, fsig.ExternFrom)
    } else {
      setup = fmt.Sprintf("%s =%s call $%s(", val, tpe, modFieldToString(exprNode.Name.ModName, fsig.Name))
    }
    for i, arg := range exprNode.Args {
      if i == len(fsig.ArgTypes) {
        setup += "..., "
      }

      argType := ""

      v, st, gst, exprTpe, err := cg.GenerateExprIR(arg)
      if err != nil {
        return "", nil, nil, "", err
      }
      if i < len(fsig.ArgTypes) {
        if fsig.ArgTypes[i].Type.IsStruct() {
          found := false
          for mod, ms := range cg.modSigs {
            for name, st := range ms.TypeTable {
              if st.Compare(fsig.ArgTypes[i].Type) {
                argType  = fmt.Sprintf(":%s", modFieldToString(mod, name))
                found = true
                break
              }
            }
            if found {
              break
            }
          }
          if argType == "" {
            argType = "l"
          }
        } else {
          argType  = mapTypeToIRType(fsig.ArgTypes[i].Type)
        }
      } else {
        argType = exprTpe
      }
      setup += fmt.Sprintf("%s %s", argType, v)
      if i != len(exprNode.Args)-1 {
        setup += ", "
      }

      setups = append(setups, st...)
      gsetups = append(gsetups, gst...)
    }
    setup += ")"
    setups = append(setups, setup)

  case *parser.BinaryOpNode:
    tpe = mapTypeToIRType(exprNode.Operand1.GetType())

    op := ""
    switch exprNode.Op {
    case parser.BINARY_OP_ADD:
      op = "add"
    case parser.BINARY_OP_SUBTRACT:
      op = "sub"
    case parser.BINARY_OP_MULTIPLY:
      op = "mul"
    case parser.BINARY_OP_DIVIDE:
      if shared.IsUnsignedType(exprNode.Operand1.GetType()) {
        op = "udiv"
      } else {
        op = "div"
      }
    case parser.BINARY_OP_MODULO:
      if shared.IsUnsignedType(exprNode.Operand1.GetType()) {
        op = "urem"
      } else {
        op = "rem"
      }
    case parser.BINARY_OP_EQUAL:
      op = "ceq" + tpe
    case parser.BINARY_OP_NOT_EQUAL:
      op = "cne" + tpe
    case parser.BINARY_OP_LESS:
      if shared.IsUnsignedType(exprNode.Operand1.GetType()) {
        op = "cult" + tpe
      } else {
        op = "cslt" + tpe
      }
    case parser.BINARY_OP_GREATER:
      if shared.IsUnsignedType(exprNode.Operand1.GetType()) {
        op = "cugt" + tpe
      } else {
        op = "csgt" + tpe
      }
    case parser.BINARY_OP_LESS_EQUAL:
      if shared.IsUnsignedType(exprNode.Operand1.GetType()) {
        op = "cule" + tpe
      } else {
        op = "csle" + tpe
      }
    case parser.BINARY_OP_GREATER_EQUAL:
      if shared.IsUnsignedType(exprNode.Operand1.GetType()) {
        op = "cuge" + tpe
      } else {
        op = "csge" + tpe
      }
    }

    nm := cg.GetTmpVar()
    val = fmt.Sprintf("%s", nm)
    v1, st1, gst1, _, err := cg.GenerateExprIR(exprNode.Operand1)
    if err != nil {
      return "", nil, nil, "", err
    }
    v2, st2, gst2, _, err := cg.GenerateExprIR(exprNode.Operand2)
    if err != nil {
      return "", nil, nil, "", err
    }
    setup := fmt.Sprintf("%s =%s %s %s, %s", val, tpe, op, v1, v2)
    setups = append(setups, st1...)
    setups = append(setups, st2...)
    gsetups = append(gsetups, gst1...)
    gsetups = append(gsetups, gst2...)
    setups = append(setups, setup)

  case *parser.UnaryOpNode:
    nm := cg.GetTmpVar()
    val = fmt.Sprintf("%s", nm)

    switch exprNode.Op {
    case parser.UNARY_OP_REFERENCE:
      switch identNode := exprNode.Operand.(type) {
      case *parser.IdentifierNode:
        setups = append(setups, fmt.Sprintf("%s =l copy %%%s", val, identNode.Name))
      default:
        return "", nil, nil, "", shared.NewError(exprNode.Loc, "cannot take address of non-variable expression")
      }
    case parser.UNARY_OP_DEREFERENCE:
      v, st, gst, _, err := cg.GenerateExprIR(exprNode.Operand)
      if err != nil {
        return "", nil, nil, "", err
      }
      setups = append(setups, st...)
      gsetups = append(gsetups, gst...)
      setups = append(setups, fmt.Sprintf("%s =%s load%s %s", val, mapTypeToIRType(exprNode.ExprType), mapTypeToIRType(exprNode.ExprType), v))
    default:
      setup := ""
      v, st, gst, _, err := cg.GenerateExprIR(exprNode.Operand)
      if err != nil {
        return "", nil, nil, "", err
      }
      switch exprNode.Op {
      case parser.UNARY_OP_NEGATE:
        setup = fmt.Sprintf("%s =%s neg %s", val, mapTypeToIRType(exprNode.Operand.GetType()), v)
      case parser.UNARY_OP_LOGICAL_NOT:
        tpe := mapTypeToIRType(exprNode.Operand.GetType())
        setup = fmt.Sprintf("%s =%s cne%s %s, 1", val, tpe, tpe, v)
      }

      setups = append(setups, st...)
      gsetups = append(gsetups, gst...)
      setups = append(setups, setup)
    }

  case *parser.IfExprNode:
    endLabel := cg.GetTmpLbl()
    nm := cg.GetTmpVar()
    val = fmt.Sprintf("%s", nm)
    tpe := mapTypeToIRType(exprNode.ExprType)

    condVal, condSetups, condGSetups, _, err := cg.GenerateExprIR(exprNode.IfBranch.Condition)
    if err != nil {
      return "", nil, nil, "", err
    }

    ifLabel := cg.GetTmpLbl()
    elseCheckLabel := cg.GetTmpLbl()

    setups = append(setups, condSetups...)
    gsetups = append(gsetups, condGSetups...)
    setups = append(setups, fmt.Sprintf("jnz %s, %s, %s", condVal, ifLabel, elseCheckLabel))

    ifVal, ifSetups, ifGSetups, _, err := cg.GenerateExprIR(exprNode.IfBranch.Node)
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

    for _, elseIf := range exprNode.ElseIfBranches {
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

    elseVal, elseSetups, elseGSetups, _, err := cg.GenerateExprIR(exprNode.ElseBranch)
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

  case *parser.GivenExprNode:
    blockIR, blockGSetups, _, err := cg.GenerateBlockIR(exprNode.Block, "", "")
    if err != nil {
      return "", nil, nil, "", err
    }
    setups = append(setups, blockIR)
    gsetups = append(gsetups, blockGSetups...)
    givenVal, givenSetups, givenGSetups, _, err := cg.GenerateExprIR(exprNode.FinalExpr)
    if err != nil {
      return "", nil, nil, "", err
    }
    setups = append(setups, givenSetups...)
    gsetups = append(gsetups, givenGSetups...)
    val = givenVal
    tpe = mapTypeToIRType(exprNode.FinalExpr.GetType())

  case *parser.CastNode:
    targetType, _ := cg.modSigs.LookupType(exprNode.ToType.ModName, exprNode.ToType.Name, cg.mod)
    for i := exprNode.ToType.PointerLevel; i > 0; i-- {
      targetType = shared.Pointer{To: targetType}
    }
    targetIRType := mapTypeToIRType(targetType)
    sourceType := exprNode.Operand.GetType()
    sourceIRType := mapTypeToIRType(sourceType)

    sourceVal, sourceSetups, sourceGSetups, _, err := cg.GenerateExprIR(exprNode.Operand)
    if err != nil {
      return "", nil, nil, "", err
    }
    setups = append(setups, sourceSetups...)
    gsetups = append(gsetups, sourceGSetups...)

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

  case *parser.StructLiteralNode:
    nm := cg.GetTmpVar()
    val = fmt.Sprintf("%s", nm)
    setups = append(setups, fmt.Sprintf("%s =l %s", val, emitAllocForType(eNode.GetType(), 1)))

    strct := eNode.GetType().(shared.Struct)
    layout := strct.GetLayout()
    for i, fld := range strct.Fields {
      found := false
      var expr parser.ExpressionNode
      for _, initFld := range exprNode.Fields {
        if initFld.L == fld.L {
          expr = initFld.R
          found = true
          break
        }
      }
      if !found {
        return "", nil, nil, "", shared.NewError(exprNode.GetLoc(), "missing initializer for field '%s'", fld.L)
      }

      exprVal, exprSetups, exprGSetups, _, err := cg.GenerateExprIR(expr)
      if err != nil {
        return "", nil, nil, "", err
      }
      gsetups = append(gsetups, exprGSetups...)
      for _, s := range exprSetups {
        setups = append(setups, s)
      }

      addrTmp := cg.GetTmpVar()
      setups = append(setups, fmt.Sprintf("%s =l add %s, %d", addrTmp, val, layout.Offsets[i]))
      setups = append(setups, fmt.Sprintf("store%s %s, %s", mapTypeToIRType(fld.R), exprVal, addrTmp))
    }

  default:
    return "", nil, nil, "", shared.NewError(exprNode.GetLoc(), "unexpected expression")
  }

  mask, ext := getTruncateMaskAndExt(eNode.GetType())
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

func mapTypeToIRType(t shared.Type) string {
  switch t {
  case shared.PRIMITIVE_VOID:
    return ""
  case shared.PRIMITIVE_BOOL:
    return "w"
  case shared.PRIMITIVE_CHAR:
    return "w"
  case shared.PRIMITIVE_I8, shared.PRIMITIVE_U8:
    return "w"
  case shared.PRIMITIVE_I16, shared.PRIMITIVE_U16:
    return "w"
  case shared.PRIMITIVE_I32, shared.PRIMITIVE_U32:
    return "w"
  case shared.PRIMITIVE_I64, shared.PRIMITIVE_U64:
    return "l"
  case shared.PRIMITIVE_UNTYPED_INT:
    return "l"
  default:
    if t.IsStruct() {
      return "l"
    }
    if t.IsPointer() {
      return "l"
    }
    panic("unknown type")
  }
}

func emitAllocForType(t shared.Type, count int) string {
  align := shared.GetAlignOfType(t)
  size := shared.GetSizeOfType(t) * count

  switch {
  case align <= 4:
    return fmt.Sprintf("alloc4 %d", size)
  case align <= 8:
    return fmt.Sprintf("alloc8 %d", size)
  case align <= 16:
    return fmt.Sprintf("alloc16 %d", size)
  default:
    panic(fmt.Sprintf("unsupported alignment %d", align))
  }
}

func getLastIdentifierInChain(ident *parser.IdentifierNode) *parser.IdentifierNode {
  current := ident
  for current.Next != nil {
    current = current.Next
  }
  return current
}

func getTruncateMaskAndExt(t shared.Type) (mask int, extOp string) {
  switch t {
  case shared.PRIMITIVE_U8:
    return 0xFF, ""
  case shared.PRIMITIVE_I8:
    return 0xFF, "extsb"
  case shared.PRIMITIVE_U16:
    return 0xFFFF, ""
  case shared.PRIMITIVE_I16:
    return 0xFFFF, "extsh"
  default:
    return 0, ""
  }
}
