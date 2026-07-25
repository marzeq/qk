package parser

import (
	"reflect"

	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

var (
	typeInterface = reflect.TypeOf((*types.Type)(nil)).Elem()
	nodeInterface = reflect.TypeOf((*Node)(nil)).Elem()
	symbolPointer = reflect.TypeOf((*symbols.Symbol)(nil))
)

// CloneSyntax makes an independent copy of an AST node while discarding all
// semantic information attached by analysis.
func CloneSyntax(node Node) Node {
	if node == nil {
		return nil
	}
	return cloneSyntaxValue(reflect.ValueOf(node), make(map[clonePointer]reflect.Value)).Interface().(Node)
}

// CloneSemantic copies an attributed AST while allowing declaration-scoped
// types and symbols to be replaced for a generic instantiation.
func CloneSemantic(
	node Node,
	substituteType func(types.Type) types.Type,
	mapSymbol func(*symbols.Symbol) *symbols.Symbol,
	postprocess func(Node),
) Node {
	if node == nil {
		return nil
	}
	return cloneSemanticValue(
		reflect.ValueOf(node),
		make(map[clonePointer]reflect.Value),
		substituteType,
		mapSymbol,
		postprocess,
	).Interface().(Node)
}

type clonePointer struct {
	typ reflect.Type
	ptr uintptr
}

func cloneSyntaxValue(value reflect.Value, pointers map[clonePointer]reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	if value.Type() == symbolPointer {
		return reflect.Zero(value.Type())
	}
	if value.Type().Implements(typeInterface) {
		return reflect.Zero(value.Type())
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		if value.Elem().Type().Implements(typeInterface) {
			return reflect.Zero(value.Type())
		}
		cloned := cloneSyntaxValue(value.Elem(), pointers)
		result := reflect.New(value.Type()).Elem()
		result.Set(cloned)
		return result
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		key := clonePointer{typ: value.Type(), ptr: value.Pointer()}
		if existing, ok := pointers[key]; ok {
			return existing
		}
		result := reflect.New(value.Type().Elem())
		pointers[key] = result
		result.Elem().Set(cloneSyntaxValue(value.Elem(), pointers))
		return result
	case reflect.Struct:
		result := reflect.New(value.Type()).Elem()
		for i := 0; i < value.NumField(); i++ {
			result.Field(i).Set(cloneSyntaxValue(value.Field(i), pointers))
		}
		return result
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			result.Index(i).Set(cloneSyntaxValue(value.Index(i), pointers))
		}
		return result
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			result.SetMapIndex(cloneSyntaxValue(iter.Key(), pointers), cloneSyntaxValue(iter.Value(), pointers))
		}
		return result
	default:
		return value
	}
}

func cloneSemanticValue(
	value reflect.Value,
	pointers map[clonePointer]reflect.Value,
	substituteType func(types.Type) types.Type,
	mapSymbol func(*symbols.Symbol) *symbols.Symbol,
	postprocess func(Node),
) reflect.Value {
	if !value.IsValid() {
		return value
	}
	if value.Type() == symbolPointer {
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		return reflect.ValueOf(mapSymbol(value.Interface().(*symbols.Symbol)))
	}
	if value.Type().Implements(typeInterface) {
		if value.Kind() == reflect.Interface && value.IsNil() {
			return reflect.Zero(value.Type())
		}
		substituted := substituteType(value.Interface().(types.Type))
		if substituted == nil {
			return reflect.Zero(value.Type())
		}
		result := reflect.ValueOf(substituted)
		if result.Type().AssignableTo(value.Type()) {
			return result
		}
		if value.Type().Kind() == reflect.Interface && result.Type().Implements(value.Type()) {
			wrapped := reflect.New(value.Type()).Elem()
			wrapped.Set(result)
			return wrapped
		}
		panic("semantic type substitution changed a concrete field's type")
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := cloneSemanticValue(value.Elem(), pointers, substituteType, mapSymbol, postprocess)
		result := reflect.New(value.Type()).Elem()
		result.Set(cloned)
		return result
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		key := clonePointer{typ: value.Type(), ptr: value.Pointer()}
		if existing, ok := pointers[key]; ok {
			return existing
		}
		result := reflect.New(value.Type().Elem())
		pointers[key] = result
		result.Elem().Set(cloneSemanticValue(value.Elem(), pointers, substituteType, mapSymbol, postprocess))
		if postprocess != nil && result.Type().Implements(nodeInterface) {
			postprocess(result.Interface().(Node))
		}
		return result
	case reflect.Struct:
		result := reflect.New(value.Type()).Elem()
		for i := 0; i < value.NumField(); i++ {
			result.Field(i).Set(cloneSemanticValue(value.Field(i), pointers, substituteType, mapSymbol, postprocess))
		}
		return result
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			result.Index(i).Set(cloneSemanticValue(value.Index(i), pointers, substituteType, mapSymbol, postprocess))
		}
		return result
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			result.SetMapIndex(
				cloneSemanticValue(iter.Key(), pointers, substituteType, mapSymbol, postprocess),
				cloneSemanticValue(iter.Value(), pointers, substituteType, mapSymbol, postprocess),
			)
		}
		return result
	default:
		return value
	}
}
