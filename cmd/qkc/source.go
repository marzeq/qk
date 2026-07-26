package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/marzeq/qk/comptime"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/tokeniser"
)

func parseFile(path string, config comptime.Config) (*parser.RootNode, error) {
	t, err := tokeniser.NewTokeniserFromFile(path)
	if err != nil {
		return nil, err
	}

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

func samePath(first, second string) bool {
	return pathKey(first) == pathKey(second)
}

func pathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func buildSearchPaths(baseDir string) []string {
	paths := []string{baseDir}
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
