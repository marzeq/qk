package loader

import (
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/types"
)

func ConfigureDeferredStage(analyser *sema.Analyser, config StageConfig, environmentFor ...func(string) *StageEnvironment) {
	analyser.SetDeferredStageSelector(func(function *parser.FunctionDefNode, root *parser.RootNode, bindings map[string]types.Type) error {
		selector := &stageSelector{
			config: config, module: function.Symbol.DefinitionModule,
			bindings: make(map[string]stageValue), evaluated: make(map[parser.ExpressionNode]stageValue),
			unresolved: make(map[parser.ExpressionNode]struct{}), typeBindings: bindings,
		}
		if len(environmentFor) != 0 {
			selector.environment = environmentFor[0](selector.module)
		}
		for _, node := range root.Body {
			if imported, ok := node.(*parser.ImportNode); ok {
				for _, module := range imported.Modules {
					if module == "core.build" {
						selector.coreBuildImported = true
					}
				}
			}
		}
		if _, err := selector.selectNode(function); err != nil {
			return err
		}
		if selector.needsIR {
			if err := selector.evaluateUnresolved(root); err != nil {
				return err
			}
			selector.needsIR = false
			selector.unresolved = make(map[parser.ExpressionNode]struct{})
			if _, err := selector.selectNode(function); err != nil {
				return err
			}
			if selector.needsIR {
				return shared.NewError(function.Loc, "specialization-dependent compile-time condition cannot be evaluated")
			}
		}
		return nil
	})
}
