package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"sort"
	"strings"
	"sync"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/types"
)

const qkmFormatVersion = 4
const qkmFrontendABI = "qk-frontend-interface-v8"
const qkmBackendABI = "qk-libllvm-22-v1"

// qkmFile is a compiler-owned whole-build manifest. Its module payloads live
// in content-addressed qmm files so manifests from unrelated projects can
// reference the same dependency and standard-library data.
type qkmFile struct {
	Format         int                   `json:"format"`
	FrontendABI    string                `json:"frontend_abi"`
	InputHash      string                `json:"input_hash"`
	VariantHash    string                `json:"variant_hash"`
	Primary        string                `json:"primary"`
	Order          []string              `json:"order"`
	Modules        map[string]*qkmModule `json:"modules,omitempty"`
	ModuleRefs     map[string]string     `json:"module_refs,omitempty"`
	RuntimeLLVM    string                `json:"runtime_llvm,omitempty"`
	RuntimeObjects map[string][]byte     `json:"runtime_objects,omitempty"`
	Links          []attributes.Link     `json:"links,omitempty"`
	LinkRoots      []string              `json:"link_roots,omitempty"`
	Warnings       []string              `json:"warnings,omitempty"`
}

type qkmModule struct {
	SourceHash       string                `json:"source_hash,omitempty"`
	ComptimeHash     string                `json:"comptime_hash,omitempty"`
	Imports          []string              `json:"imports,omitempty"`
	ImportInterfaces map[string]string     `json:"import_interfaces,omitempty"`
	Interface        *sema.ModuleInterface `json:"interface,omitempty"`
	Templates        []byte                `json:"generic_templates,omitempty"`
	IR               []byte                `json:"ir,omitempty"`
	LLVM             string                `json:"llvm"`
	LinkProviders    []qkmLinkProvider     `json:"link_providers,omitempty"`
	Warnings         []string              `json:"warnings,omitempty"`
	Objects          map[string][]byte     `json:"objects,omitempty"`
	cacheRef         string
	cacheDirty       bool
}

type qkmLinkProvider struct {
	Links   []attributes.Link `json:"links"`
	Symbols []string          `json:"symbols"`
}

var qkmGobRegistration sync.Once

func registerQKMGobTypes() {
	qkmGobRegistration.Do(func() {
		for _, value := range []any{
			types.SelfType{}, types.TypeParameter{}, types.NoInitializerType{}, types.DefinedType{}, &types.AliasRef{},
			types.PrimitiveType(""), types.StructType{}, types.TraitType{}, types.TraitPointerType{}, types.OpaqueType{},
			types.EnumType{}, types.FlagsType{}, types.UnionType{}, types.PointerType{}, types.SliceType{}, types.ArrayType{},
			types.SequenceType{}, types.FunctionType{}, types.MultipleReturnType{}, types.ErrorType{}, types.UntypedInt{},
			types.UntypedFloat{}, types.UnresolvedEnum{},
			attributes.AttributeNoReturn{}, attributes.AttributeInline{}, attributes.AttributeNoInline{}, attributes.AttributePacked{},
			attributes.FunctionAttributeForeign{}, attributes.FunctionAttributeExport{},
			ir.Add{}, ir.Sub{}, ir.Mul{}, ir.Div{}, ir.CmpEq{}, ir.CmpNe{}, ir.CmpLt{}, ir.CmpLe{}, ir.CmpGt{}, ir.CmpGe{},
			ir.LogicalAnd{}, ir.LogicalOr{}, ir.Mod{}, ir.BitwiseAnd{}, ir.BitwiseOr{}, ir.BitwiseXor{}, ir.ShiftLeft{}, ir.ShiftRight{},
			ir.Negate{}, ir.LogicalNot{}, ir.BitwiseNot{}, ir.Alloca{}, ir.AllocaArray{}, ir.Load{}, ir.LoadGlobal{}, ir.Store{},
			ir.StoreGlobal{}, ir.LoadPtr{}, ir.StorePtr{}, ir.InsertValue{}, ir.ExtractValue{}, ir.AddressOf{}, ir.AddressOfGlobal{},
			ir.FieldAddress{}, ir.ElementAddress{}, ir.Call{}, ir.InlineAsm{}, ir.Jump{}, ir.Branch{}, ir.Return{}, ir.Unreachable{},
			ir.Cast{}, ir.Sizeof{}, ir.Alignof{}, ir.Offsetof{}, ir.StringConst{},
		} {
			gob.Register(value)
		}
	})
}

func encodeQKMTemplates(templates []ir.GenericTemplate) ([]byte, error) {
	if len(templates) == 0 {
		return nil, nil
	}
	registerQKMGobTypes()
	for templateIndex := range templates {
		for parameterIndex := range templates[templateIndex].Parameters {
			parameter := templates[templateIndex].Parameters[parameterIndex]
			parameter.Constraint = types.DetachForSerialization(parameter.Constraint)
			templates[templateIndex].Parameters[parameterIndex] = parameter
		}
		detachIRTypes(reflect.ValueOf(templates[templateIndex].Functions))
	}
	var output bytes.Buffer
	if err := gob.NewEncoder(&output).Encode(templates); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

var cachedTypeReflection = reflect.TypeFor[types.Type]()

func detachIRTypes(value reflect.Value) {
	if !value.IsValid() {
		return
	}
	if value.Type() == cachedTypeReflection {
		if value.CanSet() && !value.IsNil() {
			value.Set(reflect.ValueOf(types.DetachForSerialization(value.Interface().(types.Type))))
		}
		return
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return
		}
		copy := reflect.New(value.Elem().Type()).Elem()
		copy.Set(value.Elem())
		detachIRTypes(copy)
		if value.CanSet() {
			value.Set(copy)
		}
	case reflect.Pointer:
		if !value.IsNil() {
			detachIRTypes(value.Elem())
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if value.Field(index).CanSet() {
				detachIRTypes(value.Field(index))
			}
		}
	case reflect.Slice:
		for index := 0; index < value.Len(); index++ {
			detachIRTypes(value.Index(index))
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			item := reflect.New(iterator.Value().Type()).Elem()
			item.Set(iterator.Value())
			detachIRTypes(item)
			value.SetMapIndex(iterator.Key(), item)
		}
	}
}

func decodeQKMTemplates(encoded []byte) ([]ir.GenericTemplate, error) {
	if len(encoded) == 0 {
		return nil, nil
	}
	registerQKMGobTypes()
	var templates []ir.GenericTemplate
	err := gob.NewDecoder(bytes.NewReader(encoded)).Decode(&templates)
	return templates, err
}

func encodeQKMIR(module *ir.Module) ([]byte, error) {
	if module == nil {
		return nil, nil
	}
	registerQKMGobTypes()
	detachIRTypes(reflect.ValueOf(module))
	var output bytes.Buffer
	if err := gob.NewEncoder(&output).Encode(module); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func decodeQKMIR(encoded []byte) (*ir.Module, error) {
	if len(encoded) == 0 {
		return nil, nil
	}
	registerQKMGobTypes()
	var module ir.Module
	if err := gob.NewDecoder(bytes.NewReader(encoded)).Decode(&module); err != nil {
		return nil, err
	}
	return &module, nil
}

type qkmInputs struct {
	Hash        string
	VariantHash string
	Path        string
	CacheRoot   string
}

func makeQKMInputs(args *Args, sources, sourcePackages map[string]string) (qkmInputs, error) {
	hash := sha256.New()
	writeHashPart := func(value string) {
		_, _ = io.WriteString(hash, value)
		_, _ = hash.Write([]byte{0})
	}
	variant := sha256.New()
	writeVariantPart := func(value string) {
		_, _ = io.WriteString(variant, value)
		_, _ = variant.Write([]byte{0})
		writeHashPart(value)
	}
	for _, value := range []string{
		qkmFrontendABI,
		args.target,
		args.sysroot,
		args.cpu,
		args.features,
		args.targetABI,
		fmt.Sprint(args.release),
		fmt.Sprint(args.debug),
		fmt.Sprint(args.outputType),
		string(args.warningMode),
	} {
		writeVariantPart(value)
	}
	writeHashPart(compilerQKMIdentity())
	writeHashPart(args.mainModule)
	warningKinds := make([]string, 0, len(args.warningModes))
	for kind := range args.warningModes {
		warningKinds = append(warningKinds, kind)
	}
	sort.Strings(warningKinds)
	for _, kind := range warningKinds {
		writeVariantPart(kind)
		writeVariantPart(string(args.warningModes[kind]))
	}
	origins := make([]string, 0, len(sources))
	for origin := range sources {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	for _, origin := range origins {
		writeHashPart(origin)
		writeHashPart(sourcePackages[origin])
		writeHashPart(sources[origin])
	}
	key := hex.EncodeToString(hash.Sum(nil))
	cacheHome := os.Getenv("QK_CACHE_DIR")
	if cacheHome == "" {
		var err error
		cacheHome, err = os.UserCacheDir()
		if err != nil {
			return qkmInputs{}, err
		}
		cacheHome = filepath.Join(cacheHome, "qk")
	}
	name := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(args.mainModule)
	if name == "" || name == "." {
		name = "root"
	}
	root := filepath.Join(cacheHome, "modules")
	return qkmInputs{
		Hash: key, VariantHash: hex.EncodeToString(variant.Sum(nil)), CacheRoot: root,
		Path: filepath.Join(root, name, key+".qkm"),
	}, nil
}

var qkmCompilerIdentity struct {
	sync.Once
	value string
}

func compilerQKMIdentity() string {
	qkmCompilerIdentity.Do(func() {
		var dirtyRevision string
		if info, ok := debug.ReadBuildInfo(); ok {
			var revision string
			modified := false
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					revision = setting.Value
				case "vcs.modified":
					modified = setting.Value == "true"
				}
			}
			if revision != "" && !modified {
				qkmCompilerIdentity.value = "vcs:" + revision
				return
			}
			dirtyRevision = revision
		}
		executable, err := os.Executable()
		if err == nil {
			if stat, statErr := os.Stat(executable); statErr == nil {
				// A locally rebuilt compiler gets a new size and/or nanosecond
				// timestamp. This avoids hashing the entire executable on every
				// short compiler invocation while still invalidating dirty builds.
				qkmCompilerIdentity.value = fmt.Sprintf("binary:%s:%d:%d", dirtyRevision, stat.Size(), stat.ModTime().UnixNano())
				return
			}
		}
		qkmCompilerIdentity.value = "abi:" + qkmFrontendABI
	})
	return qkmCompilerIdentity.value
}

func loadQKM(inputs qkmInputs) (*qkmFile, bool) {
	file, err := os.Open(inputs.Path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return nil, false
	}
	defer compressed.Close()
	var cached qkmFile
	if err := json.NewDecoder(compressed).Decode(&cached); err != nil ||
		cached.Format != qkmFormatVersion || cached.FrontendABI != qkmFrontendABI ||
		cached.InputHash != inputs.Hash || len(cached.Order) == 0 || !hydrateQKMModules(&cached, inputs.CacheRoot) {
		return nil, false
	}
	for _, module := range cached.Modules {
		if _, err := decodeQKMTemplates(module.Templates); err != nil {
			return nil, false
		}
		if _, err := decodeQKMIR(module.IR); err != nil {
			return nil, false
		}
	}
	return &cached, true
}

// findQKMModuleCandidates discovers reusable module variants from prior build
// manifests. Candidates are keyed locally by source and compile-time variant;
// their imported interface hashes are validated later in dependency order.
func findQKMModuleCandidates(inputs qkmInputs, sourceHashes map[string]string) map[string][]*qkmModule {
	result := make(map[string][]*qkmModule)
	_ = filepath.WalkDir(inputs.CacheRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".qkm" || path == inputs.Path {
			return nil
		}
		cached, ok := loadQKMPath(path)
		if !ok || cached.VariantHash != inputs.VariantHash {
			return nil
		}
		for name, module := range cached.Modules {
			if module == nil || module.SourceHash == "" || module.SourceHash != sourceHashes[name] ||
				module.Interface == nil || len(module.IR) == 0 || module.LLVM == "" {
				continue
			}
			result[name] = append(result[name], module)
		}
		return nil
	})
	return result
}

func loadQKMPath(path string) (*qkmFile, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return nil, false
	}
	defer compressed.Close()
	var cached qkmFile
	if json.NewDecoder(compressed).Decode(&cached) != nil || cached.Format != qkmFormatVersion ||
		cached.FrontendABI != qkmFrontendABI || !hydrateQKMModules(&cached, filepath.Dir(filepath.Dir(path))) {
		return nil, false
	}
	return &cached, true
}

func hydrateQKMModules(cached *qkmFile, cacheRoot string) bool {
	if len(cached.ModuleRefs) == 0 {
		return len(cached.Modules) != 0
	}
	modules := make(map[string]*qkmModule, len(cached.ModuleRefs))
	for name, ref := range cached.ModuleRefs {
		module, ok := loadQKMModule(cacheRoot, ref)
		if !ok {
			return false
		}
		modules[name] = module
	}
	cached.Modules = modules
	return len(modules) != 0
}

func qkmModulePath(cacheRoot, ref string) string {
	prefix := ref
	if len(prefix) > 2 {
		prefix = prefix[:2]
	}
	return filepath.Join(cacheRoot, ".shared", prefix, ref+".qmm")
}

func loadQKMModule(cacheRoot, ref string) (*qkmModule, bool) {
	if decoded, err := hex.DecodeString(ref); err != nil || len(decoded) != sha256.Size {
		return nil, false
	}
	file, err := os.Open(qkmModulePath(cacheRoot, ref))
	if err != nil {
		return nil, false
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return nil, false
	}
	defer compressed.Close()
	encoded, err := io.ReadAll(compressed)
	if err != nil {
		return nil, false
	}
	digest := sha256.Sum256(encoded)
	if hex.EncodeToString(digest[:]) != ref {
		return nil, false
	}
	var module qkmModule
	if json.Unmarshal(encoded, &module) != nil {
		return nil, false
	}
	module.cacheRef = ref
	return &module, true
}

func matchingQKMModule(candidates []*qkmModule, interfaceHashes map[string]string, comptimeHash string) *qkmModule {
	for _, candidate := range candidates {
		if candidate.ComptimeHash != comptimeHash {
			continue
		}
		matches := true
		for imported, expected := range candidate.ImportInterfaces {
			if interfaceHashes[imported] != expected {
				matches = false
				break
			}
		}
		if matches {
			return candidate
		}
	}
	return nil
}

func findQKMImplementationObjects(inputs qkmInputs, moduleName, llvm string) map[string][]byte {
	if llvm == "" {
		return nil
	}
	var result map[string][]byte
	_ = filepath.WalkDir(inputs.CacheRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil || result != nil {
			return nil
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".qkm" || path == inputs.Path {
			return nil
		}
		cached, ok := loadQKMPath(path)
		if !ok || cached.VariantHash != inputs.VariantHash {
			return nil
		}
		if module := cached.Modules[moduleName]; module != nil && module.LLVM == llvm && len(module.Objects) != 0 {
			result = module.Objects
		}
		return nil
	})
	return result
}

func findQKMRuntimeObjects(inputs qkmInputs, llvm string) map[string][]byte {
	if llvm == "" {
		return nil
	}
	var result map[string][]byte
	_ = filepath.WalkDir(inputs.CacheRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil || result != nil {
			return nil
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".qkm" || path == inputs.Path {
			return nil
		}
		cached, ok := loadQKMPath(path)
		if ok && cached.VariantHash == inputs.VariantHash && cached.RuntimeLLVM == llvm && len(cached.RuntimeObjects) != 0 {
			result = cached.RuntimeObjects
		}
		return nil
	})
	return result
}

func storeQKM(inputs qkmInputs, cached *qkmFile) error {
	if previous, ok := loadQKM(inputs); ok {
		for name, module := range cached.Modules {
			old := previous.Modules[name]
			if old == nil {
				continue
			}
			if module.Objects == nil {
				module.Objects = make(map[string][]byte)
			}
			for key, object := range old.Objects {
				if len(module.Objects[key]) == 0 {
					module.Objects[key] = object
					module.cacheDirty = true
				}
			}
		}
		if cached.RuntimeObjects == nil {
			cached.RuntimeObjects = make(map[string][]byte)
		}
		for key, object := range previous.RuntimeObjects {
			if len(cached.RuntimeObjects[key]) == 0 {
				cached.RuntimeObjects[key] = object
			}
		}
	}
	cached.Format = qkmFormatVersion
	cached.FrontendABI = qkmFrontendABI
	cached.InputHash = inputs.Hash
	cached.VariantHash = inputs.VariantHash
	refs, err := storeQKMModules(inputs.CacheRoot, cached.Modules)
	if err != nil {
		return err
	}
	cached.ModuleRefs = refs
	if err := os.MkdirAll(filepath.Dir(inputs.Path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(inputs.Path), "module-*.qkm")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	disk := *cached
	disk.Modules = nil
	compressed := gzip.NewWriter(temporary)
	encodeErr := json.NewEncoder(compressed).Encode(&disk)
	closeGzipErr := compressed.Close()
	closeFileErr := temporary.Close()
	if encodeErr != nil {
		return encodeErr
	}
	if closeGzipErr != nil {
		return closeGzipErr
	}
	if closeFileErr != nil {
		return closeFileErr
	}
	if err := os.Rename(temporaryPath, inputs.Path); err == nil {
		return nil
	}
	// Windows cannot atomically replace an existing destination with Rename.
	// Cache files are disposable, so fall back to removing only this exact key.
	if err := os.Remove(inputs.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temporaryPath, inputs.Path)
}

func storeQKMModules(cacheRoot string, modules map[string]*qkmModule) (map[string]string, error) {
	refs := make(map[string]string, len(modules))
	for name, module := range modules {
		if module.cacheRef != "" && !module.cacheDirty {
			if _, err := os.Stat(qkmModulePath(cacheRoot, module.cacheRef)); err == nil {
				refs[name] = module.cacheRef
				continue
			}
		}
		encoded, err := json.Marshal(module)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(encoded)
		ref := hex.EncodeToString(digest[:])
		refs[name] = ref
		path := qkmModulePath(cacheRoot, ref)
		if _, err := os.Stat(path); err == nil {
			module.cacheRef = ref
			module.cacheDirty = false
			continue
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		temporary, err := os.CreateTemp(filepath.Dir(path), "module-*.qmm")
		if err != nil {
			return nil, err
		}
		temporaryPath := temporary.Name()
		compressed := gzip.NewWriter(temporary)
		_, writeErr := compressed.Write(encoded)
		closeGzipErr := compressed.Close()
		closeFileErr := temporary.Close()
		if writeErr != nil || closeGzipErr != nil || closeFileErr != nil {
			_ = os.Remove(temporaryPath)
			if writeErr != nil {
				return nil, writeErr
			}
			if closeGzipErr != nil {
				return nil, closeGzipErr
			}
			return nil, closeFileErr
		}
		if err := os.Rename(temporaryPath, path); err != nil {
			_ = os.Remove(temporaryPath)
			if _, statErr := os.Stat(path); statErr != nil {
				return nil, err
			}
		}
		module.cacheRef = ref
		module.cacheDirty = false
	}
	return refs, nil
}

func qkmObjectKey(args *Args) string {
	hash := sha256.New()
	for _, value := range []string{
		qkmBackendABI,
		string(args.optLevel),
		args.target,
		args.cpu,
		args.features,
		args.targetABI,
		args.relocation,
		args.codeModel,
	} {
		_, _ = io.WriteString(hash, value)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func qkmInterfaceHashes(interfaces map[string]sema.ModuleInterface) map[string]string {
	result := make(map[string]string, len(interfaces))
	for module, iface := range interfaces {
		result[module] = interfaceHash(iface)
	}
	return result
}

func interfaceHash(iface sema.ModuleInterface) string {
	encoded, err := json.Marshal(iface)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func qkmSourceHashes(sources, sourcePackages map[string]string) map[string]string {
	origins := make([]string, 0, len(sources))
	for origin := range sources {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	hashes := make(map[string]hash.Hash)
	for _, origin := range origins {
		module := sourcePackages[origin]
		writer := hashes[module]
		if writer == nil {
			writer = sha256.New()
		}
		_, _ = io.WriteString(writer, origin)
		_, _ = writer.Write([]byte{0})
		_, _ = io.WriteString(writer, sources[origin])
		_, _ = writer.Write([]byte{0})
		hashes[module] = writer
	}
	result := make(map[string]string, len(hashes))
	for module, writer := range hashes {
		result[module] = hex.EncodeToString(writer.Sum(nil))
	}
	return result
}

func materializeQKMObject(module *qkmModule, key, path string) bool {
	if module == nil || len(module.Objects[key]) == 0 {
		return false
	}
	return os.WriteFile(path, module.Objects[key], 0o644) == nil
}

func rememberQKMObject(module *qkmModule, key, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if module.Objects == nil {
		module.Objects = make(map[string][]byte)
	}
	module.Objects[key] = data
	module.cacheDirty = true
	return nil
}

func pruneOldQKMFiles(cachePath string) {
	entries, err := os.ReadDir(filepath.Dir(cachePath))
	if err != nil || len(entries) <= 8 {
		return
	}
	type entryInfo struct {
		path string
		mod  int64
	}
	var files []entryInfo
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".qkm" {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			files = append(files, entryInfo{filepath.Join(filepath.Dir(cachePath), entry.Name()), info.ModTime().UnixNano()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod > files[j].mod })
	for _, old := range files[8:] {
		if old.path != cachePath {
			_ = os.Remove(old.path)
		}
	}
}
