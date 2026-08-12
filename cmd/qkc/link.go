package main

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/codegen/llvmbackend"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/types"
)

// linksForUsedForeignSymbols activates a source file's link attributes only
// when a reachable function or initializer references a foreign declaration
// from that same file. This keeps convenience binding files from eagerly
// adding native libraries merely because their package was loaded.
func linksForUsedForeignSymbols(
	partials []*loader.PartialModuleInfo,
	modules map[string]*ir.Module,
	rootAllExternal bool,
) []attributes.Link {
	referenced := reachableNativeSymbols(modules, rootAllExternal)
	var links []attributes.Link
	seen := map[attributes.Link]bool{}
	addProvider := func(providerLinks []attributes.Link, symbols []string) {
		used := false
		for _, symbol := range symbols {
			if referenced[symbol] {
				used = true
				break
			}
		}
		if !used {
			return
		}
		for _, link := range providerLinks {
			if !seen[link] {
				seen[link] = true
				links = append(links, link)
			}
		}
	}
	for _, partial := range partials {
		path := partial.Path
		if path == "" {
			path = partial.Name
		}
		if modules[path] == nil || len(partial.Links) == 0 {
			continue
		}
		addProvider(partial.Links, fileForeignSymbols(partial.Root))
	}
	return links
}

func fileForeignSymbols(root *parser.RootNode) []string {
	seen := make(map[string]bool)
	var result []string
	for _, node := range root.Body {
		var name string
		var attrs attributes.Attributes
		switch node := node.(type) {
		case *parser.FunctionDefNode:
			name, attrs = node.Name, node.Attributes
		case *parser.DeclarationNode:
			name, attrs = node.Name, node.Attributes
		default:
			continue
		}
		foreign, ok := attrs.Get(attributes.AttributeTypeForeign).(attributes.FunctionAttributeForeign)
		if !ok {
			continue
		}
		for _, symbol := range []string{name, foreign.From} {
			if symbol != "" && !seen[symbol] {
				seen[symbol] = true
				result = append(result, symbol)
			}
		}
	}
	return result
}

func reachableNativeSymbols(modules map[string]*ir.Module, rootAllExternal bool) map[string]bool {
	functions := map[string]*ir.Function{}
	functionModules := map[string]*ir.Module{}
	globals := map[string]*ir.Global{}
	globalModules := map[string]*ir.Module{}
	queue := []string{}
	for _, module := range modules {
		for _, fn := range module.Functions {
			functions[fn.Name] = fn
			functionModules[fn.Name] = module
			if fn.Linkage == ir.LinkageExternal && (rootAllExternal || fn.Visibility == ir.VisibilityDefault) {
				queue = append(queue, fn.Name)
			}
		}
		for index := range module.Globals {
			global := &module.Globals[index]
			globals[global.Name] = global
			globalModules[global.Name] = module
			if rootAllExternal && global.Linkage == ir.LinkageExternal {
				queue = append(queue, global.Name)
			}
		}
		if module.Entry != "" {
			queue = append(queue, module.Entry)
		}
		if module.Entry != "" && module.Initializer != "" {
			queue = append(queue, module.Initializer)
		}
	}

	referenced := map[string]bool{}
	visited := map[string]bool{}
	for len(queue) != 0 {
		name := queue[0]
		queue = queue[1:]
		if visited[name] {
			continue
		}
		visited[name] = true
		if global := globals[name]; global != nil {
			native := nativeDeclarations(globalModules[name])
			for _, target := range operandSymbolReferences(global.Value) {
				if functions[target] != nil || globals[target] != nil {
					queue = append(queue, target)
				} else if native[target] {
					referenced[target] = true
				}
			}
			continue
		}
		fn := functions[name]
		if fn == nil {
			continue
		}
		native := nativeDeclarations(functionModules[name])
		for _, block := range fn.Blocks {
			for _, instruction := range block.Instr {
				for _, target := range instructionSymbolReferences(instruction) {
					if functions[target] != nil || globals[target] != nil {
						queue = append(queue, target)
					} else if native[target] {
						referenced[target] = true
					}
				}
			}
		}
	}
	return referenced
}

func operandSymbolReferences(operand ir.Operand) []string {
	var names []string
	collectFunctionOperands(reflect.ValueOf(operand), &names)
	return names
}

func nativeDeclarations(module *ir.Module) map[string]bool {
	names := map[string]bool{}
	if module == nil {
		return names
	}
	for _, declaration := range module.Externs {
		if declaration.From != "" {
			names[declaration.Name] = true
			names[declaration.From] = true
		}
	}
	for _, declaration := range module.ExternGlobals {
		names[declaration.Name] = true
	}
	return names
}

func instructionSymbolReferences(instruction ir.Instr) []string {
	var names []string
	switch instruction := instruction.(type) {
	case ir.Call:
		if instruction.Name != "" {
			names = append(names, instruction.Name)
		}
	case ir.LoadGlobal:
		names = append(names, instruction.Name)
	case ir.StoreGlobal:
		names = append(names, instruction.Name)
	case ir.AddressOfGlobal:
		names = append(names, instruction.Name)
	}
	collectFunctionOperands(reflect.ValueOf(instruction), &names)
	return names
}

func collectFunctionOperands(value reflect.Value, names *[]string) {
	if !value.IsValid() {
		return
	}
	if value.Type() == reflect.TypeFor[types.Type]() {
		return
	}
	if value.Type() == reflect.TypeFor[ir.Operand]() {
		operand := value.Interface().(ir.Operand)
		if operand.Kind == ir.OperandFunctionConst && operand.FunctionName != "" {
			*names = append(*names, operand.FunctionName)
		}
		for index := range operand.Fields {
			collectFunctionOperands(reflect.ValueOf(operand.Fields[index]), names)
		}
		if operand.Left != nil {
			collectFunctionOperands(reflect.ValueOf(*operand.Left), names)
		}
		if operand.Right != nil {
			collectFunctionOperands(reflect.ValueOf(*operand.Right), names)
		}
		return
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if !value.IsNil() {
			collectFunctionOperands(value.Elem(), names)
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			collectFunctionOperands(value.Field(i), names)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			collectFunctionOperands(value.Index(i), names)
		}
	}
}

func linkObjects(objFiles []string, moduleLinks []attributes.Link, roots []string, config *Args) error {
	args, err := buildLinkArgs(objFiles, moduleLinks, roots, config)
	if err != nil {
		return err
	}

	// ld64.lld does not implement Mach-O relocatable linking. Use Apple's
	// linker on Darwin hosts for this one output mode while retaining embedded
	// LLD for executable and shared-library links.
	if config.outputType == OutputObject && targetIsApple(config.target) {
		if runtime.GOOS != "darwin" {
			return fmt.Errorf("Mach-O relocatable object output requires an Apple linker on the host")
		}
		return linkDarwinRelocatable(args, config.verbose)
	}

	return llvmbackend.Link(args, config.verbose)
}

func linkDarwinRelocatable(args []string, verbose bool) error {
	clang, err := exec.LookPath("clang")
	if err != nil {
		return fmt.Errorf("find Clang for Mach-O relocatable link: %w", err)
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "> %s %s\n", clang, strings.Join(args, " "))
	}
	command := exec.Command(clang, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("Apple relocatable linking failed: %w", err)
	}
	return nil
}

func moduleLinksContainLibc(moduleLinks []attributes.Link) bool {
	for _, link := range moduleLinks {
		if link.Kind == attributes.LinkSystem && (link.Value == "c" || link.Value == "System") {
			return true
		}
	}
	return false
}

func withoutLibcLinks(moduleLinks []attributes.Link) []attributes.Link {
	result := make([]attributes.Link, 0, len(moduleLinks))
	for _, link := range moduleLinks {
		if link.Kind == attributes.LinkSystem && (link.Value == "c" || link.Value == "System") {
			continue
		}
		result = append(result, link)
	}
	return result
}

func buildLinkArgs(objFiles []string, moduleLinks []attributes.Link, roots []string, config *Args) ([]string, error) {
	args := append([]string{}, objFiles...)

	switch config.outputType {
	case OutputExecutable:
	case OutputObject:
		if targetIsWindows(config.target) {
			return nil, fmt.Errorf("relocatable object output for Windows targets is unavailable")
		}
		args = append(args, "-r")
	case OutputSharedLib:
		if config.static {
			return nil, fmt.Errorf("cannot use --static with shared lib output")
		}
		args = append(args, "-shared")
	case OutputWebAssembly:
		args = append(args, "-Wl,--no-entry")
	default:
		return nil, fmt.Errorf("unknown output type")
	}
	if config.outputType != OutputObject || (!targetIsWebAssembly(config.target) && !targetIsApple(config.target)) {
		args = append(args, deadStripLinkerFlag(config.target))
		if config.outputType != OutputObject && config.outputType != OutputWebAssembly {
			args = append(args, unusedDynamicLibrariesFlag(config.target))
		}
	}
	if config.outputType == OutputObject && !targetIsWebAssembly(config.target) && !targetIsApple(config.target) {
		for _, root := range roots {
			args = append(args, linkerUndefinedFlag(config.target, root))
		}
	}
	if config.outputType == OutputWebAssembly {
		for _, root := range roots {
			args = append(args, "-Wl,--export="+root)
		}
	}

	if config.static {
		args = append(args, "-static")
	}
	linksLibc := moduleLinksContainLibc(moduleLinks)
	if config.outputType == OutputObject || config.outputType == OutputWebAssembly {
		args = append(args, defaultLibrarySuppressionArgs(config.target, config.outputType)...)
	} else if !linksLibc {
		args = append(args, defaultLibrarySuppressionArgs(config.target, config.outputType)...)
		if config.outputType == OutputExecutable && targetIsLinuxX8664(config.target) {
			args = append(args, "-nostartfiles", "-Wl,-e,_start")
		}
	}
	sysroot := config.sysroot
	if sysroot == "" && targetIsApple(config.target) {
		var err error
		sysroot, err = hostAppleSysroot()
		if err != nil {
			return nil, err
		}
	}
	if sysroot != "" {
		args = append([]string{"--sysroot=" + sysroot}, args...)
	}
	if config.target != "" {
		args = append([]string{"-target", config.target}, args...)
	}
	args = append(args, config.linkArgs...)

	for _, link := range moduleLinks {
		switch link.Kind {
		case attributes.LinkSystem:
			// A Mach-O relocatable link cannot consume a dylib text stub. Keep
			// libc references unresolved for the final executable or dylib link.
			if config.outputType == OutputObject && targetIsApple(config.target) && (link.Value == "c" || link.Value == "System") {
				continue
			}
			args = append(args, "-l"+link.Value)
		case attributes.LinkPath:
			args = append(args, link.Value)
		case attributes.LinkSearchPath:
			args = append(args, "-L"+link.Value)
		case attributes.LinkFramework:
			args = append(args, "-framework", link.Value)
		default:
			return nil, fmt.Errorf("unknown module link kind %d", link.Kind)
		}
	}
	for _, lib := range config.libs {
		args = append(args, "-l"+lib)
	}
	if targetIsWindows(config.target) {
		// The thin runtime uses the stable Win32 kernel ABI, never the CRT.
		args = append(args, "-lkernel32")
	}
	for _, path := range config.libraryPaths {
		args = append(args, "-L"+path)
	}

	args = append(args, "-o", config.output)
	return args, nil
}

func defaultLibrarySuppressionArgs(target string, outputType OutputType) []string {
	if outputType == OutputObject || outputType == OutputWebAssembly {
		return []string{"-nostdlib"}
	}
	// Darwin's loader requires every executable to load libSystem, even when
	// the program uses only direct syscalls. Clang adds that load command by
	// default; suppressing its default libraries produces an image that dyld
	// refuses to launch.
	if targetIsApple(target) {
		return nil
	}
	if targetIsWindows(target) {
		return []string{"-nodefaultlibs"}
	}
	return []string{"-nolibc"}
}

func isWindowsGNUTarget(target string) bool {
	target = effectiveTargetName(target)
	windows := strings.Contains(target, "windows") || strings.Contains(target, "mingw")
	return windows && (strings.Contains(target, "gnu") || strings.Contains(target, "mingw"))
}

func deadStripLinkerFlag(target string) string {
	target = effectiveTargetName(target)
	switch {
	case targetIsApple(target):
		return "-Wl,-dead_strip"
	case strings.Contains(target, "windows"), strings.Contains(target, "mingw"), strings.Contains(target, "msvc"):
		return "-Wl,/OPT:REF"
	default:
		return "-Wl,--gc-sections"
	}
}

func unusedDynamicLibrariesFlag(target string) string {
	target = effectiveTargetName(target)
	switch {
	case targetIsApple(target):
		return "-Wl,-dead_strip_dylibs"
	case targetIsWindows(target):
		return "-Wl,/OPT:REF"
	default:
		return "-Wl,--as-needed"
	}
}

func linkerUndefinedFlag(target, symbol string) string {
	target = effectiveTargetName(target)
	switch {
	case targetIsApple(target):
		return "-Wl,-u,_" + symbol
	case isWindowsGNUTarget(target):
		return "-Wl,-u," + symbol
	case strings.Contains(target, "windows"), strings.Contains(target, "msvc"):
		return "-Wl,/INCLUDE:" + symbol
	default:
		return "-Wl,-u," + symbol
	}
}
