package sema

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

// An anonymous constructor is nominally anchored to its source expression.
// Its name is deterministic across frontend runs and independent of alias use.
func (a *Analyser) typeValueFromSyntax(expression parser.ExpressionNode) types.Type {
	switch expression := expression.(type) {
	case *parser.TypeValueExprNode:
		value := a.resolveTypeNode(expression.Value)
		if types.HasError(value) {
			return value
		}
		if _, resolved := expression.Value.(*parser.ResolvedTypeNode); resolved {
			return value
		}
		switch expression.Value.(type) {
		case *parser.StructTypeNode, *parser.EnumTypeNode, *parser.FlagsTypeNode,
			*parser.UnionTypeNode, *parser.TraitTypeNode, *parser.OpaqueTypeNode, *parser.DistinctTypeNode:
		default:
			return value
		}
		location := expression.GetLoc()
		origin := fmt.Sprintf("%s:%d:%d", location.FilePath, location.Offset, location.EndOffset)
		digest := sha256.Sum256([]byte(origin))
		name := "$anon_" + hex.EncodeToString(digest[:12])
		switch value := value.(type) {
		case types.TraitType:
			value.Module, value.Name = a.currentMod, name
			return value
		default:
			return types.DefinedType{Module: a.currentMod, Name: name, Underlying: value}
		}
	case *parser.IdentifierNode:
		if expression.Symbol != nil && expression.Symbol.TypeInfo != nil &&
			(expression.Symbol.Kind == symbols.SymbolKindType || expression.Symbol.Type != nil && expression.Symbol.Type.Equals(types.MetaType{})) {
			return expression.Symbol.TypeInfo
		}
	case *parser.FieldAccessNode:
		if expression.ResolvedIdentifier != nil && expression.ResolvedIdentifier.Symbol != nil &&
			expression.ResolvedIdentifier.Symbol.Kind == symbols.SymbolKindType {
			return expression.ResolvedIdentifier.Symbol.TypeInfo
		}
	}
	return nil
}
