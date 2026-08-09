package sema

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

func hasErrorSignature(signature *symbols.FunctionSignature) bool {
	if signature == nil {
		return false
	}
	for _, parameter := range signature.Parameters {
		if types.HasError(parameter) {
			return true
		}
	}
	return types.HasError(signature.ReturnType) || types.HasError(signature.VariadicElement)
}

func anyErrorExpression(expressions ...parser.ExpressionNode) bool {
	for _, expression := range expressions {
		if expression != nil && types.HasError(expression.GetType()) {
			return true
		}
	}
	return false
}
