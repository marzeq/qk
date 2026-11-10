package loader

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
)

// Type-returning functions currently have no operations on type values. A
// function that merely selects and returns a type can use the existing
// parameterized transparent-type resolver, which also handles calls in type
// annotations and preserves definition-module ownership.
func lowerTypeFunctions(root *parser.RootNode) error {
	for index, node := range root.Body {
		function, ok := node.(*parser.FunctionDefNode)
		if !ok {
			continue
		}
		if !isTypeMarker(function.RetTypeNode) {
			if err := lowerBareTypeParameters(function); err != nil {
				return err
			}
			continue
		}
		if function.HasMethodOwner() || function.HasVariadic || function.TypedVariadic {
			return shared.NewError(function.Loc, "type-returning functions cannot be methods or variadic")
		}
		parameters := append([]parser.GenericParameterNode(nil), function.GenericParameters...)
		for _, argument := range function.Args {
			if !isTypeMarker(argument.Type) {
				return shared.NewError(argument.Loc, "type-returning functions currently accept only type parameters")
			}
			if argument.Default != nil || argument.Mutable {
				return shared.NewError(argument.Loc, "type parameters cannot be mutable or have defaults")
			}
			for _, parameter := range parameters {
				if parameter.Name == argument.Name {
					return shared.NewError(argument.Loc, "duplicate type parameter %q", argument.Name)
				}
			}
			parameters = append(parameters, parser.GenericParameterNode{Name: argument.Name, Loc: argument.Loc})
		}
		result := function.Body
		if block, ok := result.(*parser.BlockNode); ok {
			if len(block.Body) != 1 {
				return shared.NewError(block.Loc, "type-returning function body must return one type")
			}
			result = block.Body[0]
		}
		if ret, ok := result.(*parser.ControlKeywordNode); ok {
			result = ret.ReturnValue
		}
		expression, ok := result.(parser.ExpressionNode)
		if !ok {
			return shared.NewError(function.Loc, "type-returning function must return a type")
		}
		typeNode, ok := returnedTypeExpression(expression)
		if !ok {
			return shared.NewError(expression.GetLoc(), "type-returning function currently supports type names and type-function calls")
		}
		root.Body[index] = &parser.TypeAliasNode{
			Name: function.Name, GenericParameters: parameters, Type: typeNode,
			Transparent: true, Pub: function.Pub, Loc: function.Loc,
		}
	}
	return nil
}

func lowerBareTypeParameters(function *parser.FunctionDefNode) error {
	parameters := append([]parser.GenericParameterNode(nil), function.GenericParameters...)
	arguments := make([]*parser.FunctionNodeArg, 0, len(function.Args))
	seenValue := false
	for _, argument := range function.Args {
		if !isTypeMarker(argument.Type) {
			seenValue = true
			arguments = append(arguments, argument)
			continue
		}
		if function.HasMethodOwner() {
			return shared.NewError(argument.Loc, "bare type parameters in methods are not supported yet")
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
		parameters = append(parameters, parser.GenericParameterNode{Name: argument.Name, Loc: argument.Loc})
		function.CompileTimeOnly = true
	}
	function.Args = arguments
	function.GenericParameters = parameters
	return nil
}

func isTypeMarker(node parser.TypeNode) bool {
	named, ok := node.(*parser.NamedTypeNode)
	return ok && named.ModName == "" && named.Name == "type" && len(named.TypeArguments) == 0
}

func returnedTypeExpression(expression parser.ExpressionNode) (parser.TypeNode, bool) {
	switch node := expression.(type) {
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
			typeArgument, ok := returnedTypeExpression(argument)
			if !ok {
				return nil, false
			}
			named.TypeArguments = append(named.TypeArguments, typeArgument)
		}
		named.Loc = node.Loc
		return named, true
	}
	return nil, false
}
