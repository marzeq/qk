package loader

import (
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
func CompileSelectedModules(mods map[string]*ModuleInfo, mainModule string, order []string, analyser *sema.Analyser) (*CompiledModules, []error) {
	return compileSelectedModules(mods, mainModule, order, analyser, false, false)
}

// CompileSelectedModulesLazyTemplates leaves parameterized bodies in their
// validated semantic form until a concrete specialization requests their IR.
func CompileSelectedModulesLazyTemplates(mods map[string]*ModuleInfo, mainModule string, order []string, analyser *sema.Analyser, executable bool, phaseMark ...func(string)) (*CompiledModules, []error) {
	return compileSelectedModules(mods, mainModule, order, analyser, true, executable, phaseMark...)
}

func compileSelectedModules(mods map[string]*ModuleInfo, mainModule string, order []string, analyser *sema.Analyser, lazyTemplates, executable bool, phaseMark ...func(string)) (*CompiledModules, []error) {
	errors, warnings := RunSemanticPipeline(mods, analyser, order, phaseMark...)
	if len(errors) != 0 {
		return nil, errors
	}
	PropagateSpecializationDemands(mods, order)
	result := &CompiledModules{
		IRModules:     GenerateIRModules(mods, mainModule, order, executable),
		Templates:     make(map[string][]ir.ParameterizedTemplate, len(order)),
		MethodTargets: analyser.MethodTargets(),
		Warnings:      warnings,
	}
	if len(phaseMark) != 0 {
		phaseMark[0]("IR generation")
	}
	if !lazyTemplates {
		for _, name := range order {
			result.Templates[name] = GenerateModuleParameterizedTemplateIR(mods[name])
		}
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

func RunSemanticPipeline(mods map[string]*ModuleInfo, analyser *sema.Analyser, order []string, phaseMark ...func(string)) (errors []error, warnings []error) {
	for _, name := range order {
		info := mods[name]
		analyser.DeclareModule(info.Root, name, info.TrustedStandardLibrary)
		if len(analyser.Errors()) > 0 {
			return analyser.Errors(), nil
		}
		analyser.ResolveModuleAliases(name)
		if len(analyser.Errors()) > 0 {
			return analyser.Errors(), nil
		}
	}
	if len(phaseMark) != 0 {
		phaseMark[0]("declarations and aliases")
	}
	for _, name := range order {
		info := mods[name]
		analyser.AnalyseModuleBody(info.Root, name)
		if len(analyser.Errors()) > 0 {
			return analyser.Errors(), nil
		}
	}
	if len(phaseMark) != 0 {
		phaseMark[0]("body resolution")
	}

	return runSemanticModuleBodies(mods, analyser, order, phaseMark...)
}

// RunSemanticModule analyses one source-backed module after its dependencies
// have been declared.
func RunSemanticModule(info *ModuleInfo, analyser *sema.Analyser) (errors []error, warnings []error) {
	return RunSemanticPipeline(map[string]*ModuleInfo{info.Path: info}, analyser, []string{info.Path})
}

// runSemanticModuleBodies completes attribution and validation for modules
// whose declarations and analysed bodies have already been installed. This
// preserves the language's global structural-method declaration phase.
func runSemanticModuleBodies(mods map[string]*ModuleInfo, analyser *sema.Analyser, order []string, phaseMark ...func(string)) (errors []error, warnings []error) {
	attributor := analyser.NewAttributor()
	for _, name := range order {
		attributor.AttributeParameterizedTemplates(mods[name].Root)
	}
	if len(phaseMark) != 0 {
		phaseMark[0]("template attribution")
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
	if len(phaseMark) != 0 {
		phaseMark[0]("template validation")
	}
	warnings = append(warnings, templateValidator.Warnings()...)
	if len(templateValidator.Errors()) != 0 {
		return templateValidator.Errors(), warnings
	}
	for _, name := range order {
		attributor.AttributeModule(mods[name].Root)
	}
	if len(phaseMark) != 0 {
		phaseMark[0]("body attribution")
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
	if len(phaseMark) != 0 {
		phaseMark[0]("body validation")
	}
	warnings = append(warnings, validator.Warnings()...)
	if len(validator.Errors()) != 0 {
		return validator.Errors(), warnings
	}
	return nil, warnings
}

func GenerateIRModules(mods map[string]*ModuleInfo, mainModule string, order []string, skipUnreferenced ...bool) map[string]*ir.Module {
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
			DependencyInitializers:    dependencyInitializers,
			SkipUnreferencedFunctions: len(skipUnreferenced) != 0 && skipUnreferenced[0],
		}
		modIR := gen.Generate(info.Root)
		deduplicateIRDeclarations(modIR)
		out[name] = modIR
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

// NewTemplateResolver indexes validated source definitions and lowers only
// templates requested by concrete calls. It retains no state across builds.
func NewTemplateResolver(mods map[string]*ModuleInfo) func(string, string) (ir.ParameterizedTemplate, bool) {
	type sourceTemplate struct {
		module   string
		function *parser.FunctionDefNode
	}
	sources := make(map[string]sourceTemplate)
	for module, info := range mods {
		for _, node := range info.Root.Body {
			function, ok := node.(*parser.FunctionDefNode)
			if !ok || !function.IsParameterized() || function.Body == nil || function.DeferredStage {
				continue
			}
			sources[module+"\x00"+function.Symbol.Name] = sourceTemplate{module, function}
		}
	}
	generated := make(map[string]ir.ParameterizedTemplate)
	return func(module, name string) (ir.ParameterizedTemplate, bool) {
		key := module + "\x00" + name
		if template, ok := generated[key]; ok {
			return template, true
		}
		source, ok := sources[key]
		if !ok {
			return ir.ParameterizedTemplate{}, false
		}
		generator := &irgen.Generator{ModuleName: source.module, Module: &ir.Module{}}
		functions := generator.GenerateParameterizedTemplate(source.function)
		if len(functions) == 0 {
			return ir.ParameterizedTemplate{}, false
		}
		template := ir.ParameterizedTemplate{
			Module: source.module, Name: source.function.Symbol.Name,
			Parameters: append([]types.TypeParameter(nil), source.function.Symbol.ParameterizedParameters...),
			Functions:  functions,
		}
		generated[key] = template
		return template, true
	}
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
