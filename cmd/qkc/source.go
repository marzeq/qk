package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/tokeniser"
)

func parseFile(path string) (*parser.RootNode, error) {
	t, err := tokeniser.NewTokeniserFromFile(path)
	if err != nil {
		return nil, err
	}

	toks, err := t.Tokenise()
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
		abs, _ := filepath.Abs(e)
		excluded[abs] = struct{}{}
	}

	var files []string
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}

		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			abs, _ := filepath.Abs(path)
			for ex := range excluded {
				if strings.HasPrefix(abs, ex) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if !d.IsDir() && filepath.Ext(path) == ".qk" {
				if _, ok := seen[abs]; !ok {
					seen[abs] = struct{}{}
					files = append(files, abs)
				}
			}
			return nil
		})
	}
	return files, nil
}

func buildSearchPaths(baseDir string) []string {
	paths := []string{baseDir}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".local", "share", "qk"))
	}
	paths = append(paths,
		filepath.Join("/usr", "local", "lib", "qk"),
		filepath.Join("/usr", "lib", "qk"),
	)
	return paths
}
