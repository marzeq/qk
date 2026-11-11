package sema

import (
	"reflect"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

// InferFunctionContexts propagates compile-time-only requirements through
// attributed calls. A function with no such requirement remains usable in
// either context; evaluation still checks whether reached operations exist.
func (a *Analyser) InferFunctionContexts(roots ...*parser.RootNode) {
	type entry struct {
		function *parser.FunctionDefNode
		calls    []*symbols.Symbol
	}
	var functions []entry
	for _, root := range roots {
		for _, raw := range root.Body {
			function, ok := raw.(*parser.FunctionDefNode)
			if !ok || function.Symbol == nil || function.Body == nil || function.GenericInstance || function.DeferredStage {
				continue
			}
			current := entry{function: function}
			if types.HasMetaType(function.Symbol.Signature.ReturnType) {
				function.Symbol.CompileTimeOnly = true
			}
			for _, parameter := range function.Symbol.Signature.Parameters {
				if types.HasMetaType(parameter) {
					function.Symbol.CompileTimeOnly = true
				}
			}
			walkRuntimeSyntax(function.Body, func(node parser.Node) {
				switch node := node.(type) {
				case *parser.IdentifierNode:
					if node.Symbol != nil && node.Symbol.Kind == symbols.SymbolKindType && node.GetType() != nil && node.GetType().Equals(types.MetaType{}) {
						function.Symbol.CompileTimeOnly = true
					}
				case *parser.DeclarationNode:
					if node.Symbol != nil && types.HasMetaType(node.Symbol.Type) {
						function.Symbol.CompileTimeOnly = true
					}
				case *parser.SizeOfNode:
					if !node.CompileTime {
						function.Symbol.CompileTimeOnly = true
					}
				case *parser.SizeOfExprNode:
					if !node.CompileTime {
						function.Symbol.CompileTimeOnly = true
					}
				case *parser.AlignOfNode:
					if !node.CompileTime {
						function.Symbol.CompileTimeOnly = true
					}
				case *parser.OffsetOfNode:
					if !node.CompileTime {
						function.Symbol.CompileTimeOnly = true
					}
				case *parser.EmbedNode:
					if !node.CompileTime {
						function.Symbol.CompileTimeOnly = true
					}
				case *parser.TypeValueExprNode:
					function.Symbol.CompileTimeOnly = true
				case *parser.StaticTypeNode:
					function.Symbol.CompileTimeOnly = true
				case *parser.TypePointerExprNode:
					function.Symbol.CompileTimeOnly = true
				case *parser.FunctionCallNode:
					if node.Symbol != nil {
						current.calls = append(current.calls, node.Symbol)
					}
				}
			})
			functions = append(functions, current)
		}
	}
	for changed := true; changed; {
		changed = false
		for _, current := range functions {
			if current.function.Symbol.CompileTimeOnly {
				continue
			}
			for _, called := range current.calls {
				if called.CompileTimeOnly || called.TemplateSymbol != nil && called.TemplateSymbol.CompileTimeOnly {
					current.function.Symbol.CompileTimeOnly = true
					changed = true
					break
				}
			}
		}
	}
	for _, current := range functions {
		current.function.CompileTimeOnly = current.function.Symbol.CompileTimeOnly
	}
}

func walkRuntimeSyntax(node parser.Node, visit func(parser.Node)) {
	if node == nil {
		return
	}
	switch node := node.(type) {
	case *parser.CompileTimeExpressionNode, *parser.CompilerDirectiveNode, *parser.FunctionDefNode:
		return
	case *parser.FieldAccessNode:
		if node.MethodSymbol != nil && node.MethodSymbol.StaticMethod {
			// The owner in Type.method is a name resolved during compilation,
			// not a type value evaluated by the runtime function.
			visit(node)
			return
		}
	case *parser.WhenNode:
		for _, branch := range node.Branches {
			for _, item := range branch.Body {
				walkRuntimeSyntax(item, visit)
			}
			walkRuntimeSyntax(branch.Value, visit)
		}
		for _, item := range node.ElseBody {
			walkRuntimeSyntax(item, visit)
		}
		walkRuntimeSyntax(node.ElseValue, visit)
		return
	}
	visit(node)
	walkRuntimeValue(reflect.ValueOf(node).Elem(), visit)
}

func walkRuntimeValue(value reflect.Value, visit func(parser.Node)) {
	if !value.IsValid() {
		return
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			return
		}
		if node, ok := value.Interface().(parser.Node); ok {
			walkRuntimeSyntax(node, visit)
			return
		}
		walkRuntimeValue(value.Elem(), visit)
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			walkRuntimeValue(value.Index(index), visit)
		}
	case reflect.Struct:
		if value.Type().PkgPath() != reflect.TypeOf(parser.RootNode{}).PkgPath() {
			return
		}
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			if field.Name == "Symbol" || field.Name == "CompileTimeApplication" {
				continue
			}
			walkRuntimeValue(value.Field(index), visit)
		}
	}
}
