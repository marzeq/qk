package typechecker

import (
	"github.com/marzeq/quokka/parser"
	"github.com/marzeq/quokka/shared"
)

type ModuleSignatures struct {
	TypeTable shared.TypeTable
	Functions FuncTable
}

type ModulesSignatures map[string]*ModuleSignatures

func (ms ModulesSignatures) LookupType(module string, typeName string, lookingFrom string) (shared.Type, bool) {
	if module == "" && lookingFrom == "" {
		if m, ok := ms[""]; ok {
			return m.TypeTable.Lookup(typeName)
		}
		return nil, false
	}

	if module == "" {
		if m, ok := ms[lookingFrom]; ok {
			if t, ok := m.TypeTable.Lookup(typeName); ok {
				return t, true
			}
		}
		if m, ok := ms[""]; ok {
			return m.TypeTable.Lookup(typeName)
		}
		return nil, false
	}

	if m, ok := ms[module]; ok {
		return m.TypeTable.Lookup(typeName)
	}
	return nil, false
}

func (ms ModulesSignatures) LookupFunction(module string, funcName string, lookingFrom string) (*FunctionSig, bool) {
	if module == "" && lookingFrom == "" {
		if m, ok := ms[""]; ok {
			return m.Functions.Lookup(funcName)
		}
		return nil, false
	}

	if module == "" {
		if m, ok := ms[lookingFrom]; ok {
			if f, ok := m.Functions.Lookup(funcName); ok {
				return f, true
			}
		}
		if m, ok := ms[""]; ok {
			return m.Functions.Lookup(funcName)
		}
		return nil, false
	}

	if m, ok := ms[module]; ok {
		return m.Functions.Lookup(funcName)
	}
	return nil, false
}

func (ms ModulesSignatures) DefineType(module string, typeName string, typ shared.Type) bool {
	m, ok := ms[module]
	if !ok {
		return false
	}
	return m.TypeTable.Define(typeName, typ)
}

func (ms ModulesSignatures) DefineFunction(module string, funcSig *FunctionSig) bool {
	m, ok := ms[module]
	if !ok {
		return false
	}
	return m.Functions.Define(funcSig.Name, funcSig)
}

func (ms ModulesSignatures) CollectSignaturesFromRootNode(ast *parser.RootNode) (string, error) {
	moduleForAst := ""
	for i, n := range ast.Body {
		switch n.(type) {
		case *parser.ModuleNode:
		default:
			if _, exists := ms[moduleForAst]; !exists && i == 0 {
				ms[""] = &ModuleSignatures{
					TypeTable: shared.NewTypeTable(),
					Functions: make(FuncTable),
				}
			}
		}

		switch node := n.(type) {
		case *parser.ModuleNode:
			if moduleForAst != "" {
				return "", shared.NewError(node.Loc, "multiple module declarations found")
			}
			if i != 0 {
				return "", shared.NewError(node.Loc, "module declaration must be the first statement in the file")
			}
			moduleForAst = node.Name
			if _, exists := ms[moduleForAst]; !exists {
				ms[moduleForAst] = &ModuleSignatures{
					TypeTable: shared.NewTypeTable(),
					Functions: make(FuncTable),
				}
			}
		case *parser.FunctionDefNode:
			sig, err := ms.ExtractFunctionSig(node, moduleForAst)
			if err != nil {
				return "", err
			}
			if !ms.DefineFunction(moduleForAst, sig) {
				return "", shared.NewError(node.Loc, "function '%s' is already defined", sig.Name)
			}
		case *parser.StructDefNode:
			if _, ok := ms.LookupType(moduleForAst, node.Name, moduleForAst); ok {
				return "", shared.NewError(node.Loc, "struct '%s' is already defined", node.Name)
			}

			fields := make([]shared.Pair[string, shared.Type], len(node.Fields))

			for i, field := range node.Fields {
				resolved, ok := ms.LookupType(field.Type.ModName, field.Type.Name, moduleForAst)
				if !ok {
					return "", shared.NewError(field.Type.Loc, "field '%s' has undefined type '%s'", field.Name, field.Type)
				}
				for j := field.Type.PointerLevel; j > 0; j-- {
					resolved = shared.Pointer{
						To: resolved,
					}
				}
				fields[i] = shared.Pair[string, shared.Type]{L: field.Name, R: resolved}
			}

			if !ms.DefineType(moduleForAst, node.Name, shared.Struct{
				Fields: fields,
			}) {
				return "", shared.NewError(node.Loc, "struct '%s' is already defined", node.Name)
			}
		}
	}
	return moduleForAst, nil
}

func (ms ModulesSignatures) ExtractFunctionSig(functionNode *parser.FunctionDefNode, currMod string) (*FunctionSig, error) {
	var retType shared.Type
	rtNode := functionNode.RetType.Type
	if rtNode != nil {
		r, ok := ms.LookupType(rtNode.ModName, rtNode.Name, currMod)
		if !ok {
			return nil, shared.NewError(functionNode.RetType.Type.Loc, "function '%s' has undefined return type '%s'", functionNode.Name, rtNode)
		}
		if functionNode.RetType.Type.PointerLevel == 0 {
			retType = r
		} else {
			ptr := r
			for i := functionNode.RetType.Type.PointerLevel; i > 0; i-- {
				ptr = shared.Pointer{
					To: ptr,
				}
			}
			retType = ptr
		}
	} else {
		retType = shared.PRIMITIVE_VOID
	}

	argTypes := make([]FunctionSigArg, len(functionNode.Args))
	for i, arg := range functionNode.Args {
		resolved, ok := ms.LookupType(arg.Type.ModName, arg.Type.Name, currMod)
		if !ok {
			return nil, shared.NewError(arg.Type.Loc, "parameter '%s' has undefined type '%s'", arg.Name, arg.Type)
		}
		for i := arg.Type.PointerLevel; i > 0; i-- {
			resolved = shared.Pointer{
				To: resolved,
			}
		}
		argTypes[i] = FunctionSigArg{
			Name:    arg.Name,
			Type:    resolved,
			Mutable: arg.Mutable,
		}
	}

	return &FunctionSig{
		ArgTypes:    argTypes,
		RetType:     retType,
		Name:        functionNode.Name,
		HasVariadic: functionNode.HasVariadic,
		ExternFrom:  functionNode.ExternFrom,
	}, nil
}
