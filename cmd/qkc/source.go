package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/marzeq/qk/comptime"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/tokeniser"
)

type sourcePackage struct {
	Path    string
	Name    string
	Files   []string
	Imports []string
}

type packageRequest struct {
	ImportPath   string
	SemanticPath string
	Importer     string
	Origin       *sourceMount
}

type packageLocation struct {
	Directory    string
	SemanticPath string
	Mount        *sourceMount
}

func packagePathFromDirectory(root, dir string) string {
	relative, err := filepath.Rel(root, dir)
	if err != nil || relative == "." {
		return "."
	}
	return strings.Join(strings.Split(relative, string(filepath.Separator)), ".")
}

func resolvePackageDirectory(root, path string) (string, bool, error) {
	current := root
	for _, component := range strings.Split(path, ".") {
		entries, err := os.ReadDir(current)
		if err != nil {
			if os.IsNotExist(err) {
				return "", false, nil
			}
			return "", false, err
		}
		found := false
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() == component {
				current = filepath.Join(current, entry.Name())
				found = true
				break
			}
		}
		if !found {
			return "", false, nil
		}
	}
	return current, true, nil
}

func parseSource(path, source string, config comptime.Config) (*parser.RootNode, error) {
	t := tokeniser.NewTokeniser(source, path)
	toks, err := t.Tokenise()
	if err != nil {
		return nil, err
	}
	toks, err = comptime.Expand(toks, config)
	if err != nil {
		return nil, err
	}

	p := parser.NewParser(toks)
	return p.Parse()
}

func collectPackageFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !strings.EqualFold(filepath.Ext(entry.Name()), ".qk") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func inspectPackage(path string, files []string, libraryRoot string, sources, sourcePackages map[string]string, trustedSources map[string]bool) (*sourcePackage, error) {
	pkg := &sourcePackage{Path: path, Files: append([]string(nil), files...)}
	seenImports := map[string]bool{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		source := string(data)
		tokens, err := tokeniser.NewTokeniser(source, file).Tokenise()
		if err != nil {
			return nil, err
		}
		header, err := parser.ScanSourceHeader(tokens)
		if err != nil {
			return nil, err
		}
		if header.Module == "" {
			return nil, fmt.Errorf("%s: module declaration is missing or empty", file)
		}
		if pkg.Name == "" {
			pkg.Name = header.Module
		} else if pkg.Name != header.Module {
			return nil, fmt.Errorf("package %q contains both module %q and module %q in %s", path, pkg.Name, header.Module, file)
		}
		for _, imported := range header.Imports {
			if !seenImports[imported] {
				seenImports[imported] = true
				pkg.Imports = append(pkg.Imports, imported)
			}
		}
		sources[file] = source
		sourcePackages[file] = path
		trustedSources[file] = trustedStandardLibrarySource(file, libraryRoot)
	}
	return pkg, nil
}

func discoverSourcePackages(primaryPath, rootDir, selectedFile string, searchRoots []string, mounts []sourceMount, libraryRoot string, implicitStd bool) ([]*sourcePackage, map[string]string, map[string]string, map[string]bool, map[string]map[string]string, error) {
	rootFiles := []string{selectedFile}
	if selectedFile == "" {
		var err error
		rootFiles, err = collectPackageFiles(rootDir)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
	}
	if len(rootFiles) == 0 {
		return nil, nil, nil, nil, nil, fmt.Errorf("no source files found in package directory %s", rootDir)
	}

	sources := map[string]string{}
	sourcePackages := map[string]string{}
	trustedSources := map[string]bool{}
	importResolutions := map[string]map[string]string{}
	root, err := inspectPackage(primaryPath, rootFiles, libraryRoot, sources, sourcePackages, trustedSources)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if selectedFile == "" && primaryPath != "." && root.Name != "main" && root.Name != primaryPath {
		return nil, nil, nil, nil, nil, fmt.Errorf("package directory %q must declare module %q or module main, found %q", rootDir, primaryPath, root.Name)
	}
	if libraryRoot != "" {
		vendorRoot := filepath.Join(libraryRoot, "vendor")
		if info, statErr := os.Stat(vendorRoot); statErr == nil && info.IsDir() {
			mounts = append([]sourceMount{{Kind: sourceMountCollection, Prefix: "vendor", Directory: vendorRoot, PreservePrefix: true}}, mounts...)
		}
	}
	packages := []*sourcePackage{root}
	seen := map[string]bool{primaryPath: true}
	resolvedDirectories := map[string]string{primaryPath: rootDir}
	queue := make([]packageRequest, 0, len(root.Imports)+1)
	for _, imported := range root.Imports {
		queue = append(queue, packageRequest{ImportPath: imported, Importer: primaryPath})
	}
	if implicitStd && root.Name != "std" && !strings.HasPrefix(root.Name, "std.") {
		queue = append(queue, packageRequest{ImportPath: "std", SemanticPath: "std", Importer: primaryPath})
	}
	for len(queue) != 0 {
		request := queue[0]
		queue = queue[1:]
		location, resolveErr := resolvePackageLocation(request, mounts, searchRoots)
		if resolveErr != nil {
			return nil, nil, nil, nil, nil, resolveErr
		}
		path := request.SemanticPath
		if location != nil {
			path = location.SemanticPath
		}
		if path == "" {
			path = request.ImportPath
		}
		if importResolutions[request.Importer] == nil {
			importResolutions[request.Importer] = make(map[string]string)
		}
		importResolutions[request.Importer][request.ImportPath] = path
		if location != nil {
			if previous := resolvedDirectories[path]; previous != "" && pathKey(previous) != pathKey(location.Directory) {
				return nil, nil, nil, nil, nil, fmt.Errorf(
					"semantic module %q is provided by both %s and %s",
					path, previous, location.Directory,
				)
			}
			resolvedDirectories[path] = location.Directory
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		var files []string
		var packageDir string
		if location != nil {
			packageDir = location.Directory
			files, err = collectPackageFiles(packageDir)
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}
		}
		if len(files) == 0 {
			continue
		}
		pkg, err := inspectPackage(path, files, libraryRoot, sources, sourcePackages, trustedSources)
		if err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("loading %s: %w", packageDir, err)
		}
		if pkg.Name == "main" {
			return nil, nil, nil, nil, nil, fmt.Errorf("imported package %q cannot declare module main", request.ImportPath)
		}
		if pkg.Name != path {
			return nil, nil, nil, nil, nil, fmt.Errorf("package directory %q must declare module %q, found %q", packageDir, path, pkg.Name)
		}
		packages = append(packages, pkg)
		importResolutions[path] = make(map[string]string)
		for _, imported := range pkg.Imports {
			childRequest := packageRequest{ImportPath: imported, Importer: path, Origin: location.Mount}
			child, childErr := resolvePackageLocation(childRequest, mounts, searchRoots)
			if childErr != nil {
				return nil, nil, nil, nil, nil, childErr
			}
			if child != nil {
				childRequest.SemanticPath = child.SemanticPath
			}
			if childRequest.SemanticPath == "" {
				childRequest.SemanticPath = imported
			}
			importResolutions[path][imported] = childRequest.SemanticPath
			queue = append(queue, childRequest)
		}
	}
	return packages, sources, sourcePackages, trustedSources, importResolutions, nil
}

func resolvePackageLocation(request packageRequest, mounts []sourceMount, searchRoots []string) (*packageLocation, error) {
	if request.Origin != nil {
		origin := *request.Origin
		if origin.Kind == sourceMountCollection && !origin.PreservePrefix {
			var peers []sourceMount
			for _, mount := range mounts {
				if mount.Kind == sourceMountCollection && !mount.PreservePrefix && mount.Prefix == origin.Prefix {
					peers = append(peers, mount)
				}
			}
			if location, found, err := resolveWithinMounts(peers, request.ImportPath); err != nil || found {
				return location, err
			}
		} else if location, ok, err := resolveWithinMount(origin, request.ImportPath); err != nil || ok {
			return location, err
		}
	}
	var candidates []sourceMount
	for _, mount := range mounts {
		if request.ImportPath != mount.Prefix && !strings.HasPrefix(request.ImportPath, mount.Prefix+".") {
			continue
		}
		candidates = append(candidates, mount)
	}
	locations := make(map[string]*packageLocation)
	for _, mount := range candidates {
		location, ok, err := resolveMountedImport(mount, request.ImportPath)
		if err != nil {
			return nil, err
		}
		if ok {
			key := pathKey(location.Directory) + "\x00" + location.SemanticPath
			locations[key] = location
		}
	}
	if len(locations) > 1 {
		paths := make([]string, 0, len(locations))
		for _, location := range locations {
			paths = append(paths, fmt.Sprintf("%s (module %s)", location.Directory, location.SemanticPath))
		}
		sort.Strings(paths)
		return nil, fmt.Errorf("source import %q is ambiguous between %s", request.ImportPath, strings.Join(paths, " and "))
	}
	for _, location := range locations {
		return location, nil
	}
	path := request.SemanticPath
	if path == "" {
		path = request.ImportPath
	}
	for _, root := range searchRoots {
		directory, exists, err := resolvePackageDirectory(root, path)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		files, err := collectPackageFiles(directory)
		if err != nil {
			return nil, err
		}
		if len(files) != 0 {
			return &packageLocation{Directory: directory, SemanticPath: path}, nil
		}
	}
	return nil, nil
}

func resolveWithinMounts(mounts []sourceMount, semanticPath string) (*packageLocation, bool, error) {
	locations := make(map[string]*packageLocation)
	for _, mount := range mounts {
		location, ok, err := resolveWithinMount(mount, semanticPath)
		if err != nil {
			return nil, false, err
		}
		if ok {
			locations[pathKey(location.Directory)] = location
		}
	}
	if len(locations) > 1 {
		paths := make([]string, 0, len(locations))
		for _, location := range locations {
			paths = append(paths, location.Directory)
		}
		sort.Strings(paths)
		return nil, false, fmt.Errorf("semantic import %q is ambiguous between %s", semanticPath, strings.Join(paths, " and "))
	}
	for _, location := range locations {
		return location, true, nil
	}
	return nil, false, nil
}

func resolveMountedImport(mount sourceMount, importPath string) (*packageLocation, bool, error) {
	suffix := strings.TrimPrefix(importPath, mount.Prefix)
	suffix = strings.TrimPrefix(suffix, ".")
	directory := mount.Directory
	if suffix != "" {
		directory = filepath.Join(directory, filepath.FromSlash(strings.ReplaceAll(suffix, ".", "/")))
	}
	semantic := suffix
	if mount.Kind == sourceMountExact {
		semantic = mount.SemanticBase
		if suffix != "" {
			semantic += "." + suffix
		}
	} else if mount.PreservePrefix {
		semantic = mount.Prefix
		if suffix != "" {
			semantic += "." + suffix
		}
	}
	files, err := collectPackageFiles(directory)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(files) == 0 {
		return nil, false, nil
	}
	copy := mount
	return &packageLocation{Directory: directory, SemanticPath: semantic, Mount: &copy}, true, nil
}

func resolveWithinMount(mount sourceMount, semanticPath string) (*packageLocation, bool, error) {
	base := mount.SemanticBase
	if mount.Kind == sourceMountCollection && mount.PreservePrefix {
		base = mount.Prefix
	}
	if base == "" {
		parts := strings.Split(semanticPath, ".")
		if len(parts) != 0 {
			base = parts[0]
		}
	}
	if semanticPath != base && !strings.HasPrefix(semanticPath, base+".") {
		return nil, false, nil
	}
	suffix := strings.TrimPrefix(strings.TrimPrefix(semanticPath, base), ".")
	directory := mount.Directory
	if mount.Kind == sourceMountCollection && !mount.PreservePrefix {
		directory = filepath.Join(directory, filepath.FromSlash(strings.ReplaceAll(semanticPath, ".", "/")))
	} else if suffix != "" {
		directory = filepath.Join(directory, filepath.FromSlash(strings.ReplaceAll(suffix, ".", "/")))
	}
	files, err := collectPackageFiles(directory)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil || len(files) == 0 {
		return nil, false, err
	}
	copy := mount
	return &packageLocation{Directory: directory, SemanticPath: semanticPath, Mount: &copy}, true, nil
}

func trustedStandardLibrarySource(source, libraryRoot string) bool {
	if libraryRoot == "" {
		return false
	}
	standardRoot := filepath.Join(libraryRoot, "std")
	resolvedSource, sourceErr := filepath.EvalSymlinks(source)
	resolvedRoot, rootErr := filepath.EvalSymlinks(standardRoot)
	if sourceErr != nil || rootErr != nil {
		return false
	}
	return pathWithin(resolvedSource, resolvedRoot)
}

func collectSourceFiles(paths []string, exclude []string) ([]string, error) {
	seen := map[string]struct{}{}
	excluded := map[string]struct{}{}
	for _, e := range exclude {
		abs, err := filepath.Abs(e)
		if err != nil {
			return nil, err
		}
		excluded[abs] = struct{}{}
	}

	var files []string
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}

		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if path != root && errors.Is(err, fs.ErrPermission) {
					if d != nil && d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				return err
			}
			if path != root && strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			for ex := range excluded {
				if pathWithin(abs, ex) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".qk") {
				key := pathKey(abs)
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
					files = append(files, abs)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func pathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func buildSearchPaths(baseDir string, additional []string) []string {
	paths := []string{baseDir}
	paths = append(paths, additional...)
	if runtime.GOOS == "windows" {
		if dataDir, err := os.UserConfigDir(); err == nil {
			paths = append(paths, filepath.Join(dataDir, "qk"))
		}
	} else {
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, filepath.Join(home, ".local", "share", "qk"))
		}
		paths = append(paths,
			filepath.Join(string(filepath.Separator), "usr", "local", "lib", "qk"),
			filepath.Join(string(filepath.Separator), "usr", "lib", "qk"),
		)
	}
	if runtime.GOOS == "windows" {
		if programData := os.Getenv("ProgramData"); programData != "" {
			paths = append(paths, filepath.Join(programData, "qk"))
		}
	}
	return paths
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
