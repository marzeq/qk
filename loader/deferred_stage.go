package loader

import (
	"math/big"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/types"
)

func ConfigureDeferredStage(analyser *sema.Analyser, config StageConfig, environmentFor ...func(string) *StageEnvironment) {
	newSelector := func(module string, root *parser.RootNode, bindings map[string]types.Type) *stageSelector {
		selector := &stageSelector{
			config: config, module: module, specializing: true,
			bindings: make(map[string]stageValue), evaluated: make(map[parser.ExpressionNode]stageValue),
			unresolved: make(map[parser.ExpressionNode]struct{}), typeBindings: bindings,
			spreadAliases: make(map[string]bool), spreadExpressions: make(map[parser.ExpressionNode]struct{}),
		}
		for name, bound := range bindings {
			if constant, ok := bound.(types.ConstantType); ok {
				if constant.ValueType.Equals(types.PrimitiveBool) {
					selector.bindings[name] = stageValue{kind: stageBool, boolean: constant.Value == "true", typ: types.PrimitiveBool}
				} else if integer, ok := new(big.Int).SetString(constant.Value, 10); ok {
					selector.bindings[name] = stageValue{kind: stageInteger, integer: integer, typ: constant.ValueType}
				}
			} else {
				selector.bindings[name] = stageValue{kind: stageType, typeValue: bound, typ: types.MetaType{}}
			}
		}
		if len(environmentFor) != 0 {
			selector.environment = environmentFor[0](module)
		}
		for _, node := range root.Body {
			if imported, ok := node.(*parser.ImportNode); ok {
				for _, dependency := range imported.Modules {
					if dependency == "core.build" {
						selector.coreBuildImported = true
					}
				}
			}
		}
		return selector
	}
	selectNode := func(selector *stageSelector, node parser.Node, root *parser.RootNode) error {
		if _, err := selector.selectNode(node); err != nil {
			return err
		}
		if !selector.needsIR {
			return nil
		}
		if err := selector.evaluateUnresolved(root); err != nil {
			return err
		}
		selector.needsIR = false
		selector.unresolved = make(map[parser.ExpressionNode]struct{})
		if _, err := selector.selectNode(node); err != nil {
			return err
		}
		if selector.needsIR {
			return shared.NewError(node.GetLoc(), "specialization-dependent compile-time condition cannot be evaluated")
		}
		return nil
	}
	analyser.SetDeferredStageSelector(func(function *parser.FunctionDefNode, root *parser.RootNode, bindings map[string]types.Type) error {
		return selectNode(newSelector(function.Symbol.DefinitionModule, root, bindings), function, root)
	})
	analyser.SetDeferredTypeSelector(func(alias *parser.TypeAliasNode, root *parser.RootNode, bindings map[string]types.Type) (parser.TypeNode, error) {
		selector := newSelector(alias.Symbol.DefinitionModule, root, bindings)
		selector.stageMinimal = true
		selector.stagingAliasName = alias.Name
		if err := selectNode(selector, alias, root); err != nil {
			return nil, err
		}
		return alias.Type, nil
	})
}
