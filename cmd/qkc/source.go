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

func inspectPackage(path string, files []string, sources, sourcePackages map[string]string) (*sourcePackage, error) {
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
	}
	return pkg, nil
}

func discoverSourcePackages(primaryPath, rootDir, selectedFile string, searchRoots []string, available map[string]bool) ([]*sourcePackage, map[string]string, map[string]string, error) {
	rootFiles := []string{selectedFile}
	if selectedFile == "" {
		var err error
		rootFiles, err = collectPackageFiles(rootDir)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	if len(rootFiles) == 0 {
		return nil, nil, nil, fmt.Errorf("no source files found in package directory %s", rootDir)
	}

	sources := map[string]string{}
	sourcePackages := map[string]string{}
	root, err := inspectPackage(primaryPath, rootFiles, sources, sourcePackages)
	if err != nil {
		return nil, nil, nil, err
	}
	if selectedFile == "" && primaryPath != "." && root.Name != "main" && root.Name != primaryPath {
		return nil, nil, nil, fmt.Errorf("package directory %q must declare module %q or module main, found %q", rootDir, primaryPath, root.Name)
	}
	packages := []*sourcePackage{root}
	seen := map[string]bool{primaryPath: true}
	queue := append([]string(nil), root.Imports...)
	for len(queue) != 0 {
		path := queue[0]
		queue = queue[1:]
		if seen[path] || available[path] {
			continue
		}
		seen[path] = true
		var files []string
		var packageDir string
		for _, searchRoot := range searchRoots {
			candidate, exists, resolveErr := resolvePackageDirectory(searchRoot, path)
			if resolveErr != nil {
				return nil, nil, nil, resolveErr
			}
			if !exists {
				continue
			}
			candidateFiles, readErr := collectPackageFiles(candidate)
			if readErr != nil {
				if os.IsNotExist(readErr) {
					continue
				}
				return nil, nil, nil, readErr
			}
			if len(candidateFiles) == 0 {
				continue
			}
			files, packageDir = candidateFiles, candidate
			break
		}
		if len(files) == 0 {
			continue
		}
		pkg, err := inspectPackage(path, files, sources, sourcePackages)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("loading %s: %w", packageDir, err)
		}
		if pkg.Name == "main" {
			return nil, nil, nil, fmt.Errorf("imported package %q cannot declare module main", path)
		}
		if pkg.Name != path {
			return nil, nil, nil, fmt.Errorf("package directory %q must declare module %q, found %q", packageDir, path, pkg.Name)
		}
		packages = append(packages, pkg)
		queue = append(queue, pkg.Imports...)
	}
	return packages, sources, sourcePackages, nil
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
