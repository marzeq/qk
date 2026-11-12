package loader

import (
	"fmt"

	"github.com/marzeq/qk/codegen/irgen"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/symbols"
	"github.com/marzeq/qk/types"
)

type CompiledModules struct {
	IRModules     map[string]*ir.Module
	Templates     map[string][]ir.ParameterizedTemplate
	MethodTargets []sema.MethodTarget
	Warnings      []error
}

// CompileSelectedModules runs the one semantic-to-IR path after staged
// selection has produced the final module roots.
func CompileSelectedModules(mods map[string]*ModuleInfo, mainModule string, order []string, analyser *sema.Analyser, verbose, debug bool) (*CompiledModules, []error) {
	errors, warnings := RunSemanticPipeline(mods, analyser, order, verbose, debug)
	if len(errors) != 0 {
		return nil, errors
	}
	PropagateSpecializationDemands(mods, order)
	result := &CompiledModules{
		IRModules:     GenerateIRModules(mods, mainModule, order, verbose, debug),
		Templates:     make(map[string][]ir.ParameterizedTemplate, len(order)),
		MethodTargets: analyser.MethodTargets(),
		Warnings:      warnings,
	}
	for _, name := range order {
		result.Templates[name] = GenerateModuleParameterizedTemplateIR(mods[name])
	}
	return result, nil
}

func moduleRoots(mods map[string]*ModuleInfo, order []string) []*parser.RootNode {
	roots := make([]*parser.RootNode, 0, len(order))
	for _, name := range order {
		roots = append(roots, mods[name].Root)
	}
	return roots
}

func RunSemanticPipeline(mods map[string]*ModuleInfo, analyser *sema.Analyser, order []string, verbose, debug bool) (errors []error, warnings []error) {
	for _, name := range order {
		info := mods[name]
		analyser.DeclareModule(info.Root, name, info.TrustedStandardLibrary)
		if len(analyser.Errors()) > 0 {
			return analyser.Errors(), nil
		}
		analyser.AnalyseModuleBody(info.Root, name)
		if len(analyser.Errors()) > 0 {
			return analyser.Errors(), nil
		}
	}

	if verbose && debug {
		fmt.Println("completed analysis phase")
	}
	return runSemanticModuleBodies(mods, analyser, order, verbose, debug)
}

// RunSemanticModule analyses one source-backed module after its dependencies
// have been declared.
func RunSemanticModule(info *ModuleInfo, analyser *sema.Analyser, verbose, debug bool) (errors []error, warnings []error) {
	return RunSemanticPipeline(map[string]*ModuleInfo{info.Path: info}, analyser, []string{info.Path}, verbose, debug)
}

// runSemanticModuleBodies completes attribution and validation for modules
// whose declarations and analysed bodies have already been installed. This
// preserves the language's global structural-method declaration phase.
func runSemanticModuleBodies(mods map[string]*ModuleInfo, analyser *sema.Analyser, order []string, verbose, debug bool) (errors []error, warnings []error) {
	attributor := analyser.NewAttributor()
	for _, name := range order {
		attributor.AttributeParameterizedTemplates(mods[name].Root)
	}
	if len(analyser.Errors()) != 0 || len(attributor.Errors()) != 0 {
		out := append([]error(nil), analyser.Errors()...)
		return append(out, attributor.Errors()...), nil
	}
	analyser.InferFunctionContexts(moduleRoots(mods, order)...)
	templateValidator := analyser.NewValidator()
	for _, name := range order {
		templateValidator.ValidateParameterizedTemplates(mods[name].Root)
	}
	warnings = append(warnings, templateValidator.Warnings()...)
	if len(templateValidator.Errors()) != 0 {
		return templateValidator.Errors(), warnings
	}
	for _, name := range order {
		attributor.AttributeModule(mods[name].Root)
	}
	if len(analyser.Errors()) != 0 || len(attributor.Errors()) != 0 {
		out := append([]error(nil), analyser.Errors()...)
		return append(out, attributor.Errors()...), warnings
	}
	analyser.InferFunctionContexts(moduleRoots(mods, order)...)
	validator := analyser.NewValidator()
	for _, name := range order {
		validator.ValidateModule(mods[name].Root)
	}
	warnings = append(warnings, validator.Warnings()...)
	if len(validator.Errors()) != 0 {
		return validator.Errors(), warnings
	}
	if debug {
		for _, name := range order {
			analyser.DebugCheck(mods[name].Root)
		}
	}
	if verbose && debug {
		fmt.Println("completed semantic body pipeline")
	}
	return nil, warnings
}

func GenerateIRModules(mods map[string]*ModuleInfo, mainModule string, order []string, verbose bool, debug bool) map[string]*ir.Module {
	out := make(map[string]*ir.Module, len(order))

	for _, name := range order {
		info := mods[name]
		dependencyInitializers := make([]string, 0, len(info.Imports))
		for _, dependency := range info.Imports {
			if dependencyIR := out[dependency]; dependencyIR != nil && dependencyIR.Initializer != "" {
				dependencyInitializers = append(dependencyInitializers, dependencyIR.Initializer)
			}
		}
		gen := &irgen.Generator{
			ModuleName: name, MainModule: mainModule,
			DependencyInitializers: dependencyInitializers,
		}
		modIR := gen.Generate(info.Root)
		deduplicateIRDeclarations(modIR)
		out[name] = modIR
	}

	if verbose && debug {
		fmt.Println("completed ir generation phase")
	}

	return out
}

func GenerateModuleParameterizedTemplateIR(info *ModuleInfo) []ir.ParameterizedTemplate {
	module := &ir.Module{}
	generator := &irgen.Generator{ModuleName: info.Path, Module: module}
	var result []ir.ParameterizedTemplate
	for _, node := range info.Root.Body {
		function, ok := node.(*parser.FunctionDefNode)
		if !ok || !function.IsParameterized() || function.Body == nil || function.DeferredStage {
			continue
		}
		if generated := generator.GenerateParameterizedTemplate(function); len(generated) != 0 {
			result = append(result, ir.ParameterizedTemplate{
				Module: info.Path, Name: function.Symbol.Name,
				Parameters: append([]types.TypeParameter(nil), function.Symbol.ParameterizedParameters...),
				Functions:  generated,
			})
		}
	}
	return result
}

// PropagateSpecializationDemands carries constant-argument and fixed typed-
// variadic demands through forwarding functions before their IR is generated.
// The semantic validator records the forwarding relationships; this fixed-
// point pass makes wrapper selection independent of module generation order.
func PropagateSpecializationDemands(mods map[string]*ModuleInfo, order []string) {
	propagateSpecializationConstants(mods, order)
	propagateTypedVariadicArities(mods, order)
}

func propagateSpecializationConstants(mods map[string]*ModuleInfo, order []string) {
	var signatures []*symbols.FunctionSignature
	for _, name := range order {
		for _, node := range mods[name].Root.Body {
			if function, ok := node.(*parser.FunctionDefNode); ok && function.Symbol != nil &&
				function.Symbol.Signature != nil {
				signatures = append(signatures, function.Symbol.Signature)
			}
		}
	}
	changed := true
	for changed {
		changed = false
		for _, signature := range signatures {
			for _, forward := range signature.ConstantForwards {
				for key, constant := range signature.ConstantArguments[forward.CallerParameter] {
					if forward.Callee.ConstantArguments == nil {
						forward.Callee.ConstantArguments = make(map[int]map[string]symbols.SpecializationConstant)
					}
					if forward.Callee.ConstantArguments[forward.CalleeParameter] == nil {
						forward.Callee.ConstantArguments[forward.CalleeParameter] = make(map[string]symbols.SpecializationConstant)
					}
					if _, exists := forward.Callee.ConstantArguments[forward.CalleeParameter][key]; !exists {
						forward.Callee.ConstantArguments[forward.CalleeParameter][key] = constant
						changed = true
					}
				}
			}
		}
	}
}

func propagateTypedVariadicArities(mods map[string]*ModuleInfo, order []string) {
	var signatures []*symbols.FunctionSignature
	for _, name := range order {
		for _, node := range mods[name].Root.Body {
			if function, ok := node.(*parser.FunctionDefNode); ok && function.Symbol != nil &&
				function.Symbol.Signature != nil && function.Symbol.Signature.TypedVariadic {
				signatures = append(signatures, function.Symbol.Signature)
			}
		}
	}
	changed := true
	for changed {
		changed = false
		for _, signature := range signatures {
			for arity := range signature.TypedVariadicArities {
				for _, forwarded := range signature.TypedVariadicForwards {
					if forwarded.TypedVariadicArities == nil {
						forwarded.TypedVariadicArities = make(map[int]bool)
					}
					if !forwarded.TypedVariadicArities[arity] {
						forwarded.TypedVariadicArities[arity] = true
						changed = true
					}
				}
			}
		}
	}
}

func deduplicateIRDeclarations(module *ir.Module) {
	definedFunctions := make(map[string]bool, len(module.Functions))
	for _, fn := range module.Functions {
		definedFunctions[fn.Name] = true
	}
	seenFunctions := make(map[string]bool)
	externs := module.Externs[:0]
	for _, extern := range module.Externs {
		name := extern.Name
		if extern.From != "" {
			name = extern.From
		}
		if definedFunctions[name] || seenFunctions[name] {
			continue
		}
		seenFunctions[name] = true
		externs = append(externs, extern)
	}
	module.Externs = externs

	definedGlobals := make(map[string]bool, len(module.Globals))
	for _, global := range module.Globals {
		definedGlobals[global.Name] = true
	}
	seenGlobals := make(map[string]bool)
	externGlobals := module.ExternGlobals[:0]
	for _, global := range module.ExternGlobals {
		if definedGlobals[global.Name] || seenGlobals[global.Name] {
			continue
		}
		seenGlobals[global.Name] = true
		externGlobals = append(externGlobals, global)
	}
	module.ExternGlobals = externGlobals
}
