package sema

import (
	"fmt"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func traitDefaultKey(trait types.TraitType) string {
	return trait.Module + ":" + trait.Name
}

func (a *Analyser) registerTraitDefaults(node *parser.TypeAliasNode, trait types.TraitType, root *parser.RootNode) {
	if root == nil || len(node.ParameterizedParameters) != 0 && trait.Name == node.Name {
		return
	}
	key := traitDefaultKey(trait)
	if _, registered := a.traitDefaults[key]; registered {
		return
	}
	defaults := make([]*symbols.Symbol, len(trait.Methods))
	for slot, method := range trait.Methods {
		syntax := node.Type.(*parser.TraitTypeNode).Methods[slot]
		if syntax.Body == nil {
			continue
		}

		self := types.TypeParameter{
			Owner: parameterizedOwner(trait.Module, "<trait-default:"+trait.Name+"."+method.Name+">"),
			Name:  "Self", Index: 0, Constraint: trait,
		}
		receiverType := types.Type(self)
		switch method.Receiver {
		case types.TraitReceiverPointer:
			receiverType = types.PointerType{Base: self}
		case types.TraitReceiverMutablePointer:
			receiverType = types.PointerType{Base: self, Mutable: true}
		}
		parameters := make([]types.Type, len(method.Parameters)+1)
		parameters[0] = receiverType
		for i, parameter := range method.Parameters {
			parameters[i+1] = types.SubstituteSelf(parameter, self)
		}
		methodParameterizedParameters := append([]types.TypeParameter(nil), method.ParameterizedParameters...)
		for i := range methodParameterizedParameters {
			methodParameterizedParameters[i].Constraint = types.SubstituteSelf(methodParameterizedParameters[i].Constraint, self)
		}
		parameterizedParameters := append([]types.TypeParameter{self}, methodParameterizedParameters...)
		name := fmt.Sprintf("__trait_default_%s_%s_%d", trait.Name, method.Name, slot)
		symbol := &symbols.Symbol{
			Name: name, Kind: symbols.SymbolKindFunction,
			Signature: &symbols.FunctionSignature{
				Parameters: parameters, RequiredParameters: len(parameters),
				ReturnType: types.SubstituteSelf(method.ReturnType, self),
			},
			Method: true, MethodReceiver: method.Receiver, DefinitionModule: trait.Module,
			ParameterizedParameters: parameterizedParameters, Template: true, TraitDefault: true,
		}

		selfTypeNode := parser.TypeNode(&parser.NamedTypeNode{Name: "Self", Loc: syntax.Loc})
		if method.Receiver != types.TraitReceiverValue {
			selfTypeNode = &parser.PointerTypeNode{
				BaseType: selfTypeNode, Mutable: method.Receiver == types.TraitReceiverMutablePointer,
				Loc: syntax.Loc,
			}
		}
		args := make([]*parser.FunctionNodeArg, len(syntax.Args)+1)
		args[0] = &parser.FunctionNodeArg{
			Name: "self", Type: selfTypeNode,
			Mutable: method.Receiver == types.TraitReceiverMutablePointer, Loc: syntax.Loc,
		}
		for i, arg := range syntax.Args {
			args[i+1] = parser.CloneSyntax(arg).(*parser.FunctionNodeArg)
		}
		parameterizedNodes := make([]parser.ParameterizedParameterNode, len(syntax.ParameterizedParameters)+1)
		parameterizedNodes[0] = parser.ParameterizedParameterNode{Name: "Self", Loc: syntax.Loc}
		copy(parameterizedNodes[1:], syntax.ParameterizedParameters)
		definition := &parser.FunctionDefNode{
			Name: name, Receiver: parser.MethodReceiverKind(method.Receiver + 1),
			ParameterizedParameters: parameterizedNodes, Args: args, RetTypeNode: syntax.ReturnType,
			Body: parser.CloneSyntax(syntax.Body), ExpressionBody: syntax.ExpressionBody,
			Loc: syntax.Loc, Symbol: symbol,
		}
		root.Body = append(root.Body, definition)
		a.functionDefinitions[symbol] = &functionDefinitionInfo{node: definition, module: trait.Module}
		a.parameterizedFunctions[symbol] = &parameterizedFunctionInfo{
			node: definition, root: root, module: trait.Module,
			specializations: make(map[string]*parser.FunctionDefNode),
		}
		bindings := make(map[string]types.Type, len(a.typeParameterBindings))
		for name, binding := range a.typeParameterBindings {
			bindings[name] = binding
		}
		a.definitionTypeBindings[symbol] = bindings
		defaults[slot] = symbol
	}
	a.traitDefaults[key] = defaults
}

func (a *Analyser) defaultTraitMethod(trait types.TraitType, slot int, concrete types.Type, at parser.Node) *symbols.Symbol {
	defaults := a.traitDefaults[traitDefaultKey(trait)]
	if slot >= len(defaults) || defaults[slot] == nil {
		return nil
	}
	template := defaults[slot]
	if len(template.ParameterizedParameters) == 1 {
		arguments := []types.Type{concrete}
		result := a.specializeParameterizedFunctionSymbol(template, arguments, at)
		if result != nil && a.parameterizedFunctions[template] == nil {
			result.Name = SpecializationName(template.DefinitionModule, template.Name, arguments)
		}
		return result
	}

	result := *template
	result.Signature = substituteFunctionSignature(template.Signature, map[string]types.Type{
		template.ParameterizedParameters[0].Key(): concrete,
	})
	result.ParameterizedParameters = append([]types.TypeParameter(nil), template.ParameterizedParameters[1:]...)
	result.TraitDefaultTemplate = template
	result.TraitDefaultSelf = concrete
	result.TraitDefault = false
	return &result
}

func (a *Analyser) templateBindings(symbol *symbols.Symbol) map[string]types.Type {
	bindings := make(map[string]types.Type, len(a.definitionTypeBindings[symbol])+len(symbol.ParameterizedParameters))
	for name, binding := range a.definitionTypeBindings[symbol] {
		bindings[name] = binding
	}
	for _, parameter := range symbol.ParameterizedParameters {
		if parameter.ValueType == nil {
			bindings[parameter.Name] = parameter
		}
	}
	return bindings
}
