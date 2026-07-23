package parser

import (
	"reflect"

	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

var (
	typeInterface = reflect.TypeOf((*types.Type)(nil)).Elem()
	symbolPointer = reflect.TypeOf((*symbols.Symbol)(nil))
)

// CloneSyntax makes an independent copy of an AST node while discarding all
// semantic information attached by analysis. Generic specializations use it
// to give every concrete body independent symbols and attributed types.
func CloneSyntax(node Node) Node {
	if node == nil {
		return nil
	}
	return cloneSyntaxValue(reflect.ValueOf(node), make(map[clonePointer]reflect.Value)).Interface().(Node)
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
