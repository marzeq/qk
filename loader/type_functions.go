package loader

import (
	"reflect"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
)

// Direct nominal constructors retain the type-binding path for owner identity
// and lexical methods. Other type-returning functions execute through staging.
func lowerTypeFunctions(root *parser.RootNode) error {
	var err error
	root.Body, err = lowerTypeFunctionsInBody(root.Body)
	return err
}

func lowerTypeFunctionsInBody(body []parser.Node) ([]parser.Node, error) {
	for index, node := range body {
		if when, ok := node.(*parser.WhenNode); ok {
			for branch := range when.Branches {
				var err error
				when.Branches[branch].Body, err = lowerTypeFunctionsInBody(when.Branches[branch].Body)
				if err != nil {
					return nil, err
				}
			}
			var err error
			when.ElseBody, err = lowerTypeFunctionsInBody(when.ElseBody)
			if err != nil {
				return nil, err
			}
		}
		function, ok := node.(*parser.FunctionDefNode)
		if !ok {
			continue
		}
		if !isTypeMarker(function.RetTypeNode) {
			if err := lowerBareTypeParameters(function, false); err != nil {
				return nil, err
			}
			continue
		}
		if function.HasMethodOwner() {
			function.CompileTimeOnly = true
			continue
		}
		result := function.Body
		if block, ok := result.(*parser.BlockNode); ok {
			if len(block.Body) != 1 {
				function.CompileTimeOnly = true
				continue
			}
			result = block.Body[0]
		}
		if ret, ok := result.(*parser.ControlKeywordNode); ok {
			result = ret.ReturnValue
		}
		expression, ok := result.(parser.ExpressionNode)
		if !ok {
			return nil, shared.NewError(function.Loc, "type-returning function must return a type")
		}
		typeNode, ok := returnedTypeExpression(expression)
		if !ok {
			function.CompileTimeOnly = true
			continue
		}
		if function.HasVariadic || function.TypedVariadic {
			function.CompileTimeOnly = true
			continue
		}
		parameters := append([]parser.ParameterizedParameterNode(nil), function.ParameterizedParameters...)
		simple := true
		for _, argument := range function.Args {
			valueParameter := !isTypeMarker(argument.Type)
			if valueParameter && !supportedRetainedParameterType(argument.Type) || argument.Default != nil || argument.Mutable {
				simple = false
				break
			}
			for _, parameter := range parameters {
				if parameter.Name == argument.Name {
					return nil, shared.NewError(argument.Loc, "duplicate type parameter %q", argument.Name)
				}
			}
			parameter := parser.ParameterizedParameterNode{Name: argument.Name, Loc: argument.Loc}
			if valueParameter {
				parameter.ValueType = argument.Type
			}
			parameters = append(parameters, parameter)
		}
		if !simple {
			function.CompileTimeOnly = true
			continue
		}
		transparent := true
		switch typeNode.(type) {
		case *parser.StructTypeNode, *parser.EnumTypeNode, *parser.FlagsTypeNode,
			*parser.UnionTypeNode, *parser.TraitTypeNode, *parser.OpaqueTypeNode, *parser.DistinctTypeNode:
			transparent = false
		}
		if transparent {
			for _, argument := range function.Args {
				if !isTypeMarker(argument.Type) {
					argument.CompileTime = true
				}
			}
			function.CompileTimeOnly = true
			if err := lowerBareTypeParameters(function, true); err != nil {
				return nil, err
			}
			continue
		}
		body[index] = &parser.TypeAliasNode{
			Name: function.Name, ParameterizedParameters: parameters, Type: typeNode,
			Transparent: transparent, Pub: function.Pub, Loc: function.Loc,
		}
	}
	for _, node := range body {
		alias, ok := node.(*parser.TypeAliasNode)
		if !ok {
			continue
		}
		var methods []*parser.FunctionDefNode
		if len(alias.Methods) != 0 {
			methods, alias.Methods = alias.Methods, nil
		}
		switch value := alias.Type.(type) {
		case *parser.StructTypeNode:
			methods, value.Methods = value.Methods, nil
		case *parser.EnumTypeNode:
			methods, value.Methods = value.Methods, nil
		case *parser.FlagsTypeNode:
			methods, value.Methods = value.Methods, nil
		case *parser.UnionTypeNode:
			methods, value.Methods = value.Methods, nil
		case *parser.DistinctTypeNode:
			methods, value.Methods = value.Methods, nil
		}
		for _, method := range methods {
			if err := lowerBareTypeParameters(method, false); err != nil {
				return nil, err
			}
		}
		body = append(body, methodNodes(methods)...)
	}
	return body, nil
}

func methodNodes(methods []*parser.FunctionDefNode) []parser.Node {
	result := make([]parser.Node, len(methods))
	for i, method := range methods {
		result[i] = method
	}
	return result
}

func lowerBareTypeParameters(function *parser.FunctionDefNode, needsSpecialization bool) error {
	for _, argument := range function.Args {
		if !isTypeMarker(argument.Type) {
			continue
		}
		if typeNodeUsesName(reflect.ValueOf(function.Body), argument.Name) ||
			typeNodeUsesName(reflect.ValueOf(function.RetTypeNode), argument.Name) {
			needsSpecialization = true
			break
		}
		for _, other := range function.Args {
			if other != argument && typeNodeUsesName(reflect.ValueOf(other.Type), argument.Name) {
				needsSpecialization = true
				break
			}
		}
	}
	parameters := append([]parser.ParameterizedParameterNode(nil), function.ParameterizedParameters...)
	arguments := make([]*parser.FunctionNodeArg, 0, len(function.Args))
	seenValue := false
	for _, argument := range function.Args {
		if !isTypeMarker(argument.Type) {
			seenValue = true
			if argument.CompileTime {
				if !supportedRetainedParameterType(argument.Type) {
					return shared.NewError(argument.Loc, "retained compile-time parameters currently require a scalar bool or integer type")
				}
				for _, parameter := range parameters {
					if parameter.Name == argument.Name {
						return shared.NewError(argument.Loc, "duplicate compile-time parameter %q", argument.Name)
					}
				}
				parameters = append(parameters, parser.ParameterizedParameterNode{Name: argument.Name, ValueType: argument.Type, Loc: argument.Loc})
			}
			arguments = append(arguments, argument)
			continue
		}
		if function.HasMethodOwner() {
			function.CompileTimeOnly = true
			arguments = append(arguments, argument)
			continue
		}
		if !needsSpecialization {
			function.CompileTimeOnly = true
			arguments = append(arguments, argument)
			continue
		}
		if seenValue {
			return shared.NewError(argument.Loc, "type parameters must precede value parameters")
		}
		if argument.Default != nil || argument.Mutable {
			return shared.NewError(argument.Loc, "type parameters cannot be mutable or have defaults")
		}
		for _, parameter := range parameters {
			if parameter.Name == argument.Name {
				return shared.NewError(argument.Loc, "duplicate type parameter %q", argument.Name)
			}
		}
		parameters = append(parameters, parser.ParameterizedParameterNode{Name: argument.Name, Loc: argument.Loc})
		function.CompileTimeOnly = true
	}
	function.Args = arguments
	function.ParameterizedParameters = parameters
	return nil
}

func typeNodeUsesName(value reflect.Value, name string) bool {
	if !value.IsValid() {
		return false
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return false
		}
		if node, ok := value.Interface().(*parser.NamedTypeNode); ok && node.ModName == "" && node.Name == name {
			return true
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Struct:
		for index := range value.NumField() {
			field := value.Field(index)
			if field.CanInterface() && typeNodeUsesName(field, name) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for index := range value.Len() {
			if typeNodeUsesName(value.Index(index), name) {
				return true
			}
		}
	}
	return false
}

func supportedRetainedParameterType(node parser.TypeNode) bool {
	named, ok := node.(*parser.NamedTypeNode)
	if !ok || named.ModName != "" || len(named.TypeArguments) != 0 {
		return false
	}
	switch named.Name {
	case "bool", "i8", "i16", "i32", "i64", "isz", "u8", "u16", "u32", "u64", "usz":
		return true
	}
	return false
}

func isTypeMarker(node parser.TypeNode) bool {
	named, ok := node.(*parser.NamedTypeNode)
	return ok && named.ModName == "" && named.Name == "type" && len(named.TypeArguments) == 0
}

func returnedTypeExpression(expression parser.ExpressionNode) (parser.TypeNode, bool) {
	switch node := expression.(type) {
	case *parser.TypeValueExprNode:
		return node.Value, true
	case *parser.BlockNode:
		if len(node.Body) == 1 {
			if inner, ok := node.Body[0].(parser.ExpressionNode); ok {
				return returnedTypeExpression(inner)
			}
		}
	case *parser.IdentifierNode:
		return &parser.NamedTypeNode{Name: node.Name, ModName: node.Module, Loc: node.Loc}, true
	case *parser.FieldAccessNode:
		if subject, ok := node.Subject.(*parser.IdentifierNode); ok {
			return &parser.NamedTypeNode{ModName: subject.Name, Name: node.Field.Name, Loc: node.Loc}, true
		}
	case *parser.FunctionCallNode:
		callee, ok := returnedTypeExpression(node.Callee)
		if !ok {
			return nil, false
		}
		named, ok := callee.(*parser.NamedTypeNode)
		if !ok {
			return nil, false
		}
		for _, argument := range node.Args {
			if prefixed, ok := argument.(*parser.CompileTimeExpressionNode); ok {
				named.TypeArguments = append(named.TypeArguments, &parser.ConstantTypeNode{Value: prefixed, Loc: prefixed.Loc})
				continue
			}
			typeArgument, ok := returnedTypeExpression(argument)
			if !ok {
				return nil, false
			}
			named.TypeArguments = append(named.TypeArguments, typeArgument)
		}
		named.Loc = node.Loc
		return named, true
	case *parser.WhenNode:
		converted := parser.CloneSyntax(node).(*parser.WhenNode)
		converted.Context = parser.WhenType
		for index := range converted.Branches {
			value, ok := converted.Branches[index].Value.(parser.ExpressionNode)
			if !ok {
				return nil, false
			}
			typeValue, ok := returnedTypeExpression(value)
			if !ok {
				return nil, false
			}
			converted.Branches[index].Value = typeValue
		}
		value, ok := converted.ElseValue.(parser.ExpressionNode)
		if !ok {
			return nil, false
		}
		typeValue, ok := returnedTypeExpression(value)
		if !ok {
			return nil, false
		}
		converted.ElseValue = typeValue
		return converted, true
	}
	return nil, false
}
