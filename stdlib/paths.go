package stdlib

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/tokeniser"
)

// SourcePackagePaths derives canonical package identities from source
// directories and verifies that semantic module declarations agree. The root
// is the directory containing the top-level std directory; for compiler-
// embedded sources it is the stable virtual path "<stdlib>".
func SourcePackagePaths(sources map[string]string, root string) (map[string]string, map[string]bool, error) {
	paths := make(map[string]string, len(sources))
	available := make(map[string]bool)
	origins := make([]string, 0, len(sources))
	for origin := range sources {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	for _, origin := range origins {
		directory := filepath.Dir(origin)
		relative, err := filepath.Rel(root, directory)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, nil, fmt.Errorf("standard-library source %q is outside package root %q", origin, root)
		}
		packagePath := "."
		if relative != "." {
			packagePath = strings.Join(strings.Split(filepath.Clean(relative), string(filepath.Separator)), ".")
		}

		tokens, err := tokeniser.NewTokeniser(sources[origin], origin).Tokenise()
		if err != nil {
			return nil, nil, err
		}
		header, err := parser.ScanSourceHeader(tokens)
		if err != nil {
			return nil, nil, err
		}
		if header.Module == "" {
			return nil, nil, fmt.Errorf("%s: module declaration is missing or empty", origin)
		}
		if header.Module != packagePath {
			return nil, nil, fmt.Errorf("package directory %q must declare module %q, found %q in %s", directory, packagePath, header.Module, origin)
		}
		paths[origin] = packagePath
		available[packagePath] = true
	}
	return paths, available, nil
}
