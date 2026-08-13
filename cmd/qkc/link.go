package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/marzeq/qk/attributes"
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
	switch config.outputType {
	case OutputStaticLibrary:
		return archiveObjects(objFiles, config)
	case OutputObject:
		return partiallyLinkObjects(objFiles, roots, config)
	case OutputWebAssembly:
		args, err := buildWasmLinkArgs(objFiles, moduleLinks, roots, config)
		if err != nil {
			return err
		}
		return runExternalTool("wasm-ld", args, config.verbose, config.quietLink)
	case OutputExecutable, OutputSharedLib:
		if targetIsWindowsMSVC(config.target) {
			if runtime.GOOS != "windows" {
				return fmt.Errorf("Windows MSVC linking requires link.exe on a Windows host; emit a static library or target object instead")
			}
			if _, err := hostWindowsToolchainArgs(config.target, config.sysroot); err != nil {
				return err
			}
			args, err := buildMSVCLinkArgs(objFiles, moduleLinks, config)
			if err != nil {
				return err
			}
			return runExternalTool("link.exe", args, config.verbose, config.quietLink)
		}
		if err := validateExternalLinkTarget(config.target, config.sysroot); err != nil {
			return err
		}
		args, err := buildLinkArgs(objFiles, moduleLinks, config)
		if err != nil {
			return err
		}
		if targetIsApple(config.target) {
			return runExternalTool("xcrun", append([]string{"clang"}, args...), config.verbose, config.quietLink)
		}
		return runExternalTool("clang", args, config.verbose, config.quietLink)
	default:
		return fmt.Errorf("unknown output type")
	}
}

func runExternalTool(tool string, args []string, verbose, quiet bool) error {
	if verbose {
		fmt.Fprintf(os.Stderr, "> %s %s\n", tool, strings.Join(args, " "))
	}
	command := exec.Command(tool, args...)
	if quiet {
		command.Stdout = io.Discard
		command.Stderr = io.Discard
	} else {
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
	}
	if err := command.Run(); err != nil {
		if _, ok := err.(*exec.Error); ok {
			return fmt.Errorf("required external tool %q was not found in PATH", tool)
		}
		return fmt.Errorf("%s failed: %w", tool, err)
	}
	return nil
}

func archiveObjects(objFiles []string, config *Args) error {
	archiveDir, err := os.MkdirTemp("", "qk-archive-*")
	if err != nil {
		return fmt.Errorf("could not prepare archive members: %w", err)
	}
	defer os.RemoveAll(archiveDir)
	members := make([]string, 0, len(objFiles))
	for index, object := range objFiles {
		data, err := os.ReadFile(object)
		if err != nil {
			return fmt.Errorf("could not read archive member %s: %w", object, err)
		}
		extension := filepath.Ext(object)
		if extension == "" {
			extension = ".o"
		}
		member := filepath.Join(archiveDir, fmt.Sprintf("module-%04d%s", index, extension))
		if err := os.WriteFile(member, data, 0o644); err != nil {
			return fmt.Errorf("could not prepare archive member %s: %w", object, err)
		}
		members = append(members, member)
	}
	if targetIsWindowsMSVC(config.target) {
		if runtime.GOOS == "windows" {
			args := []string{"/NOLOGO", "/OUT:" + config.output}
			args = append(args, members...)
			args = append(args, config.linkArgs...)
			return runExternalTool("lib.exe", args, config.verbose, config.quietLink)
		}
		args := []string{"rcs", config.output}
		args = append(args, members...)
		return runExternalTool("llvm-ar", args, config.verbose, config.quietLink)
	}
	args := []string{"rcs", config.output}
	args = append(args, members...)
	return runExternalTool("ar", args, config.verbose, config.quietLink)
}

func partiallyLinkObjects(objFiles, roots []string, config *Args) error {
	switch {
	case targetIsWindows(config.target):
		return fmt.Errorf("relocatable object output for Windows targets is unavailable; use -t lib")
	case targetIsApple(config.target):
		if runtime.GOOS != "darwin" {
			return fmt.Errorf("Mach-O relocatable object output requires Apple's ld on a Darwin host; use -t lib")
		}
		args := []string{"ld", "-r"}
		if config.sysroot != "" {
			args = append(args, "-syslibroot", config.sysroot)
		}
		for _, root := range roots {
			args = append(args, "-u", "_"+root)
		}
		args = append(args, objFiles...)
		args = append(args, "-o", config.output)
		return runExternalTool("xcrun", args, config.verbose, config.quietLink)
	case targetIsWebAssembly(config.target):
		args := []string{"-r"}
		args = append(args, objFiles...)
		args = append(args, "-o", config.output)
		return runExternalTool("wasm-ld", args, config.verbose, config.quietLink)
	default:
		args := []string{"-r"}
		for _, root := range roots {
			args = append(args, "-u", root)
		}
		args = append(args, config.linkArgs...)
		args = append(args, objFiles...)
		args = append(args, "-o", config.output)
		return runExternalTool("ld.lld", args, config.verbose, config.quietLink)
	}
}

func moduleLinksContainLibc(moduleLinks []attributes.Link) bool {
	for _, link := range moduleLinks {
		if link.Kind == attributes.LinkSystem && isCLibrary(link.Value) {
			return true
		}
	}
	return false
}

func withoutLibcLinks(moduleLinks []attributes.Link) []attributes.Link {
	result := make([]attributes.Link, 0, len(moduleLinks))
	for _, link := range moduleLinks {
		if link.Kind == attributes.LinkSystem && isCLibrary(link.Value) {
			continue
		}
		result = append(result, link)
	}
	return result
}

func isCLibrary(library string) bool {
	switch strings.ToLower(library) {
	case "c", "system", "msvcrt", "ucrt":
		return true
	default:
		return false
	}
}

func validateExternalLinkTarget(target, sysroot string) error {
	switch {
	case isWindowsGNUTarget(target):
		if sysroot == "" {
			return fmt.Errorf("Windows GNU target requires an explicit MinGW sysroot; pass -sysroot <path>")
		}
		return nil
	case targetIsWindows(target):
		return nil
	case targetIsApple(target):
		if runtime.GOOS != "darwin" {
			return fmt.Errorf("Darwin linking requires Apple's toolchain on a Darwin host; emit a static library or target object instead")
		}
	case strings.Contains(effectiveTargetName(target), "linux"):
		if runtime.GOOS != "linux" {
			return fmt.Errorf("Linux linking requires a Linux host toolchain; emit a static library or target object instead")
		}
	case strings.Contains(effectiveTargetName(target), "freebsd"):
		if runtime.GOOS != "freebsd" {
			return fmt.Errorf("FreeBSD linking requires a FreeBSD host toolchain; emit a static library or target object instead")
		}
	case strings.Contains(effectiveTargetName(target), "openbsd"):
		if runtime.GOOS != "openbsd" {
			return fmt.Errorf("OpenBSD linking requires an OpenBSD host toolchain; emit a static library or target object instead")
		}
	case strings.Contains(effectiveTargetName(target), "netbsd"):
		if runtime.GOOS != "netbsd" {
			return fmt.Errorf("NetBSD linking requires a NetBSD host toolchain; emit a static library or target object instead")
		}
	}
	return nil
}

func buildWasmLinkArgs(objFiles []string, moduleLinks []attributes.Link, roots []string, config *Args) ([]string, error) {
	args := []string{"--no-entry", "--gc-sections"}
	for _, root := range roots {
		args = append(args, "--export="+root)
	}
	args = append(args, unwrapLinkerArgs(config.linkArgs)...)
	moduleLinks = orderedModuleLinks(moduleLinks)
	for _, link := range moduleLinks {
		if link.Kind == attributes.LinkSearchPath {
			args = append(args, "-L"+link.Value)
		}
	}
	for _, path := range config.libraryPaths {
		args = append(args, "-L"+path)
	}
	args = append(args, objFiles...)
	for _, link := range moduleLinks {
		switch link.Kind {
		case attributes.LinkSystem:
			args = append(args, "-l"+link.Value)
		case attributes.LinkPath:
			args = append(args, link.Value)
		case attributes.LinkSearchPath:
			continue
		case attributes.LinkFramework:
			return nil, fmt.Errorf("WebAssembly does not support framework link %q", link.Value)
		default:
			return nil, fmt.Errorf("unknown module link kind %d", link.Kind)
		}
	}
	for _, library := range config.libs {
		args = append(args, "-l"+library)
	}
	args = append(args, "-o", config.output)
	return args, nil
}

// Static linkers resolve archives from left to right. Link attributes are
// declarative, so put explicit archives before the system libraries and
// frameworks that satisfy their unresolved symbols. Preserve declaration
// order within each category for dependencies between explicit archives.
func orderedModuleLinks(links []attributes.Link) []attributes.Link {
	ordered := make([]attributes.Link, 0, len(links))
	for _, kind := range []attributes.LinkKind{
		attributes.LinkSearchPath,
		attributes.LinkPath,
		attributes.LinkSystem,
		attributes.LinkFramework,
	} {
		for _, link := range links {
			if link.Kind == kind {
				ordered = append(ordered, link)
			}
		}
	}
	for _, link := range links {
		if link.Kind > attributes.LinkFramework {
			ordered = append(ordered, link)
		}
	}
	return ordered
}

func unwrapLinkerArgs(args []string) []string {
	result := make([]string, 0, len(args))
	for _, argument := range args {
		if strings.HasPrefix(argument, "-Wl,") {
			result = append(result, strings.Split(strings.TrimPrefix(argument, "-Wl,"), ",")...)
		} else {
			result = append(result, argument)
		}
	}
	return result
}

func buildMSVCLinkArgs(objFiles []string, moduleLinks []attributes.Link, config *Args) ([]string, error) {
	args := []string{"/NOLOGO", "/INCREMENTAL:NO", "/OPT:REF", "/OUT:" + config.output}
	if config.outputType == OutputSharedLib {
		if config.static {
			return nil, fmt.Errorf("cannot use -static with shared lib output")
		}
		args = append(args, "/DLL")
	}
	if config.outputType == OutputExecutable {
		args = append(args, "/ENTRY:mainCRTStartup")
	}
	// QK exposes a C-compatible main function on Windows. The MSVC startup
	// object in libcmt supplies mainCRTStartup and the argc/argv/envp setup.
	args = append(args, "/DEFAULTLIB:libcmt")
	for _, directory := range filepath.SplitList(os.Getenv("LIB")) {
		if directory != "" {
			args = append(args, "/LIBPATH:"+directory)
		}
	}
	args = append(args, config.linkArgs...)
	moduleLinks = orderedModuleLinks(moduleLinks)
	for _, link := range moduleLinks {
		switch link.Kind {
		case attributes.LinkSystem:
			if !isCLibrary(link.Value) {
				args = append(args, windowsLibraryName(link.Value))
			}
		case attributes.LinkPath:
			args = append(args, link.Value)
		case attributes.LinkSearchPath:
			args = append(args, "/LIBPATH:"+link.Value)
		case attributes.LinkFramework:
			return nil, fmt.Errorf("Windows MSVC does not support framework link %q", link.Value)
		default:
			return nil, fmt.Errorf("unknown module link kind %d", link.Kind)
		}
	}
	for _, library := range config.libs {
		if !isCLibrary(library) {
			args = append(args, windowsLibraryName(library))
		}
	}
	for _, path := range config.libraryPaths {
		args = append(args, "/LIBPATH:"+path)
	}
	args = append(args, "kernel32.lib")
	args = append(args, objFiles...)
	return args, nil
}

func windowsLibraryName(library string) string {
	if filepath.Ext(library) == "" {
		return library + ".lib"
	}
	return library
}

func buildLinkArgs(objFiles []string, moduleLinks []attributes.Link, config *Args) ([]string, error) {
	args := append([]string{}, objFiles...)

	switch config.outputType {
	case OutputExecutable:
	case OutputSharedLib:
		if config.static {
			return nil, fmt.Errorf("cannot use --static with shared lib output")
		}
		args = append(args, "-shared")
	default:
		return nil, fmt.Errorf("unknown output type")
	}
	args = append(args, deadStripLinkerFlag(config.target), unusedDynamicLibrariesFlag(config.target))

	if config.static {
		args = append(args, "-static")
	}
	linksLibc := moduleLinksContainLibc(moduleLinks)
	if !linksLibc {
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

	moduleLinks = orderedModuleLinks(moduleLinks)
	for _, link := range moduleLinks {
		if link.Kind == attributes.LinkSearchPath {
			args = append(args, "-L"+link.Value)
		}
	}
	for _, path := range config.libraryPaths {
		args = append(args, "-L"+path)
	}
	for _, link := range moduleLinks {
		switch link.Kind {
		case attributes.LinkSystem:
			// The MSVC driver selects the appropriate static or dynamic CRT from
			// its default libraries. Adding ucrt.lib explicitly mixes the import
			// and static CRTs and produces duplicate runtime symbols.
			if targetIsWindowsMSVC(config.target) && isCLibrary(link.Value) {
				continue
			}
			args = append(args, "-l"+targetSystemLibrary(config.target, link.Value))
		case attributes.LinkPath:
			args = append(args, link.Value)
		case attributes.LinkSearchPath:
			continue
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
		// Panic reporting uses the stable Win32 kernel ABI directly.
		args = append(args, "-lkernel32")
	}
	args = append(args, "-o", config.output)
	return args, nil
}

func targetSystemLibrary(target, library string) string {
	if library == "c" && targetIsWindows(target) {
		if targetIsWindowsMSVC(target) {
			return "ucrt"
		}
		return "msvcrt"
	}
	return library
}

func targetIsWindowsMSVC(target string) bool {
	return strings.Contains(strings.ToLower(target), "msvc")
}

func hostWindowsToolchainArgs(target, sysroot string) ([]string, error) {
	if !targetIsWindows(target) {
		return nil, nil
	}
	if !targetIsWindowsMSVC(target) {
		if sysroot == "" {
			return nil, fmt.Errorf("Windows GNU target requires an explicit MinGW sysroot; pass -sysroot <path>")
		}
		return nil, nil
	}
	if runtime.GOOS != "windows" {
		return nil, nil
	}
	libraryEnvironment := os.Getenv("LIB")
	if libraryEnvironment == "" {
		return nil, fmt.Errorf("MSVC target requires an initialized Visual Studio developer environment (LIB is not set)")
	}
	var args []string
	for _, directory := range filepath.SplitList(libraryEnvironment) {
		if directory != "" {
			args = append(args, "-L"+directory)
		}
	}
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
		// MinGW's CRT supplies the executable entry point and process argument
		// setup for QK's C-compatible main function.
		return nil
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
	case targetIsWindowsMSVC(target):
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
	case targetIsWindowsMSVC(target):
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
