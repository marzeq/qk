package loader

import (
	"fmt"
	"sort"
	"strings"

	"github.com/marzeq/qk/codegen/irgen"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/types"
)

const SpecializationModule = "__qk.specializations"

type specializationRequest struct {
	template ir.GenericTemplate
	args     []types.Type
	key      string
}

// ExtractGenericSpecializations replaces importer-demanded generic functions
// with standalone instantiated IR and removes their old copies from base
// modules. Base module implementations therefore no longer vary by importer.
func ExtractGenericSpecializations(modules map[string]*ir.Module, templates map[string][]ir.GenericTemplate, interfaces map[string]sema.ModuleInterface) (*ir.Module, error) {
	byName := make(map[string]ir.GenericTemplate)
	methodTargets := make(map[string]string)
	for _, iface := range interfaces {
		for _, method := range iface.Methods {
			methodTargets[method.MethodOwnerModule+"\x00"+method.MethodOwnerName+"\x00"+method.MethodLookupName] =
				irgen.MangleFunctionName(iface.Name, method.Name)
		}
	}
	for module, values := range templates {
		for _, template := range values {
			byName[module+"\x00"+template.Name] = template
		}
	}
	result := &ir.Module{}
	queued := make(map[string]bool)
	removed := make(map[string]bool)
	var queue []specializationRequest
	queueReference := func(reference *ir.GenericReference, rename func(string, string)) error {
		if reference == nil {
			return nil
		}
		arguments := append([]types.Type(nil), reference.TypeArguments...)
		for _, argument := range arguments {
			if types.HasTypeParameter(argument) {
				return nil
			}
		}
		template, exists := byName[reference.Module+"\x00"+reference.Name]
		if !exists {
			return fmt.Errorf("generic template %s.%s is unavailable", reference.Module, reference.Name)
		}
		key := specializationRequestKey(template, arguments)
		baseName := irgen.MangleFunctionName(template.Module, template.Name)
		specializedName := irgen.MangleFunctionName(template.Module, sema.SpecializationName(template.Module, template.Name, arguments))
		rename(baseName, specializedName)
		if !queued[key] {
			queued[key] = true
			queue = append(queue, specializationRequest{template: template, args: arguments, key: key})
		}
		return nil
	}
	var queueOperand func(*ir.Operand) error
	queueOperand = func(operand *ir.Operand) error {
		if operand == nil {
			return nil
		}
		if operand.Generic != nil {
			if err := queueReference(operand.Generic, func(oldName, newName string) {
				if !strings.HasPrefix(operand.FunctionName, newName) {
					operand.FunctionName = strings.Replace(operand.FunctionName, oldName, newName, 1)
				}
			}); err != nil {
				return err
			}
		}
		for index := range operand.Fields {
			if err := queueOperand(&operand.Fields[index]); err != nil {
				return err
			}
		}
		if err := queueOperand(operand.Left); err != nil {
			return err
		}
		return queueOperand(operand.Right)
	}
	queueCalls := func(function *ir.Function) error {
		for _, block := range function.Blocks {
			for index, instruction := range block.Instr {
				call, ok := instruction.(ir.Call)
				if !ok {
					continue
				}
				if call.Requirement != nil && !types.HasTypeParameter(call.Requirement.ReceiverType) {
					module, owner, ok := concreteTypeIdentity(call.Requirement.ReceiverType)
					if !ok {
						return fmt.Errorf("trait requirement %s has non-concrete receiver %v", call.Requirement.Name, call.Requirement.ReceiverType)
					}
					target := methodTargets[module+"\x00"+owner+"\x00"+call.Requirement.Name]
					if target == "" {
						return fmt.Errorf("trait requirement %s is unavailable for %s.%s", call.Requirement.Name, module, owner)
					}
					call.Name = target
					if call.Callee != nil && call.Callee.Kind == ir.OperandFunctionConst {
						call.Callee.FunctionName = target
					}
					call.Requirement = nil
					block.Instr[index] = call
				}
				if call.Generic == nil {
					continue
				}
				if err := queueReference(call.Generic, func(oldName, newName string) {
					renameCallTarget(&call, oldName, newName)
				}); err != nil {
					return err
				}
				block.Instr[index] = call
			}
		}
		return nil
	}
	moduleNames := make([]string, 0, len(modules))
	for name := range modules {
		moduleNames = append(moduleNames, name)
	}
	sort.Strings(moduleNames)
	for _, moduleName := range moduleNames {
		module := modules[moduleName]
		for index := range module.Globals {
			if err := queueOperand(&module.Globals[index].Value); err != nil {
				return nil, err
			}
		}
		for _, function := range module.Functions {
			if err := queueCalls(function); err != nil {
				return nil, err
			}
		}
	}
	for len(queue) != 0 {
		request := queue[0]
		queue = queue[1:]
		functions, err := ir.InstantiateGenericTemplate(request.template, request.args)
		if err != nil {
			return nil, err
		}
		baseName := irgen.MangleFunctionName(request.template.Module, request.template.Name)
		specializedName := irgen.MangleFunctionName(request.template.Module, sema.SpecializationName(request.template.Module, request.template.Name, request.args))
		renames := make(map[string]string, len(functions))
		for _, function := range functions {
			renames[function.Name] = strings.Replace(function.Name, baseName, specializedName, 1)
		}
		for _, function := range functions {
			function.Name = renames[function.Name]
			removed[function.Name] = true
			for _, block := range function.Blocks {
				for index, instruction := range block.Instr {
					call, ok := instruction.(ir.Call)
					if ok {
						for oldName, newName := range renames {
							renameCallTarget(&call, oldName, newName)
						}
						block.Instr[index] = call
					}
				}
			}
			if err := queueCalls(function); err != nil {
				return nil, err
			}
			result.Functions = append(result.Functions, function)
		}
	}
	for _, moduleName := range moduleNames {
		module := modules[moduleName]
		kept := module.Functions[:0]
		for _, function := range module.Functions {
			if !removed[function.Name] {
				kept = append(kept, function)
			}
		}
		module.Functions = kept
	}
	sort.Slice(result.Functions, func(i, j int) bool { return result.Functions[i].Name < result.Functions[j].Name })
	addSpecializationImports(modules, result)
	addSpecializationExterns(result)
	return result, nil
}

func concreteTypeIdentity(value types.Type) (string, string, bool) {
	if pointer, ok := value.(types.PointerType); ok {
		value = pointer.Base
	}
	switch value := value.(type) {
	case types.DefinedType:
		name := value.Name
		if value.GenericName != "" {
			name = value.GenericName
		}
		return value.Module, name, true
	case *types.AliasRef:
		return value.Module, value.Name, true
	case types.PrimitiveType:
		return "builtin", value.String(), true
	default:
		return "", "", false
	}
}

func addSpecializationImports(modules map[string]*ir.Module, specializations *ir.Module) {
	defined := make(map[string]bool, len(specializations.Functions))
	for _, function := range specializations.Functions {
		defined[function.Name] = true
	}
	var visitOperand func(*ir.Module, *ir.Operand)
	visitOperand = func(module *ir.Module, operand *ir.Operand) {
		if operand == nil {
			return
		}
		if operand.Kind == ir.OperandFunctionConst && defined[operand.FunctionName] {
			alreadyAvailable := false
			for _, external := range module.Externs {
				if external.Name == operand.FunctionName {
					alreadyAvailable = true
					break
				}
			}
			if !alreadyAvailable {
				for _, function := range module.Functions {
					if function.Name == operand.FunctionName {
						alreadyAvailable = true
						break
					}
				}
			}
			if !alreadyAvailable {
				pointer, ok := types.Underlying(operand.Type).(types.PointerType)
				if !ok {
					return
				}
				if function, ok := types.Underlying(pointer.Base).(types.FunctionType); ok {
					module.AddExtern(ir.ExternDecl{
						Name:       operand.FunctionName,
						Signature:  ir.FunctionSignature{ParamTypes: function.Parameters, ReturnType: function.ReturnType, Variadic: function.TypedVariadic},
						Visibility: ir.VisibilityHidden,
					})
				}
			}
		}
		for index := range operand.Fields {
			visitOperand(module, &operand.Fields[index])
		}
		visitOperand(module, operand.Left)
		visitOperand(module, operand.Right)
	}
	for _, module := range modules {
		for index := range module.Globals {
			visitOperand(module, &module.Globals[index].Value)
		}
	}
}

func renameCallTarget(call *ir.Call, oldName, newName string) {
	if call.Name != "" && !strings.HasPrefix(call.Name, newName) {
		call.Name = strings.Replace(call.Name, oldName, newName, 1)
	}
	if call.Callee != nil && call.Callee.Kind == ir.OperandFunctionConst &&
		!strings.HasPrefix(call.Callee.FunctionName, newName) {
		call.Callee.FunctionName = strings.Replace(call.Callee.FunctionName, oldName, newName, 1)
	}
}

func specializationRequestKey(template ir.GenericTemplate, arguments []types.Type) string {
	parts := make([]string, len(arguments))
	for index, argument := range arguments {
		parts[index] = types.Identity(argument)
	}
	return template.Module + "\x00" + template.Name + "\x00" + strings.Join(parts, ",")
}

func addSpecializationExterns(module *ir.Module) {
	defined := make(map[string]bool)
	for _, function := range module.Functions {
		defined[function.Name] = true
	}
	seenFunctions := make(map[string]bool)
	seenGlobals := make(map[string]bool)
	for _, function := range module.Functions {
		for _, block := range function.Blocks {
			for _, instruction := range block.Instr {
				switch instruction := instruction.(type) {
				case ir.Call:
					name := instruction.Name
					if instruction.Callee != nil && instruction.Callee.Kind == ir.OperandFunctionConst {
						name = instruction.Callee.FunctionName
					}
					if name != "" && !defined[name] && !seenFunctions[name] {
						seenFunctions[name] = true
						module.Externs = append(module.Externs, ir.ExternDecl{Name: name, Signature: instruction.Signature, Visibility: ir.VisibilityHidden})
					}
				case ir.LoadGlobal:
					if !seenGlobals[instruction.Name] {
						seenGlobals[instruction.Name] = true
						module.ExternGlobals = append(module.ExternGlobals, ir.ExternGlobal{Name: instruction.Name, Type: instruction.Type, Visibility: ir.VisibilityHidden})
					}
				case ir.StoreGlobal:
					if !seenGlobals[instruction.Name] {
						seenGlobals[instruction.Name] = true
						module.ExternGlobals = append(module.ExternGlobals, ir.ExternGlobal{Name: instruction.Name, Type: instruction.Value.Type, Mutable: true, Visibility: ir.VisibilityHidden})
					}
				case ir.AddressOfGlobal:
					if !seenGlobals[instruction.Name] {
						seenGlobals[instruction.Name] = true
						module.ExternGlobals = append(module.ExternGlobals, ir.ExternGlobal{Name: instruction.Name, Type: instruction.Type, Visibility: ir.VisibilityHidden})
					}
				}
			}
		}
	}
}
