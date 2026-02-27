package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/tokeniser"
)

func main() {
	base := flag.String("base", "", "base directory to search for source files")
	exclude := flag.String("exclude", "", "comma-separated list of directories to exclude from search")
	verbose := flag.Bool("verbose", false, "enable verbose output")
	debug := flag.Bool("debug", false, "enable debug checks after semantic analysis")

	flag.Parse()

	if *base == "" {
		*base = resolveBaseDir()
	}
	var excludeDirs []string
	if *exclude != "" {
		excludeDirs = strings.Split(*exclude, ",")
	}

	searchPaths := buildSearchPaths(*base)

	files, err := collectSourceFiles(searchPaths, excludeDirs)
	check(err)

	if len(files) == 0 {
		fmt.Println("no source files found")
		os.Exit(1)
	}

	var partials []*loader.PartialModuleInfo

	for _, file := range files {
		ast, err := parseFile(file)
		check(err)

		info, err := loader.CollectModuleInfo(ast)
		check(err)

		partials = append(partials, info)
	}

	if *verbose {
		fmt.Println("parsed and collected modules")
	}

	modules, err := loader.BuildModules(partials)
	check(err)

	analyser := sema.NewAnalyser()

	errs := loader.RunSemanticPipeline(modules, analyser, *verbose, *debug)
	checkErrs(errs)

	if *verbose {
		fmt.Println("semantic analysis completed successfully")
	}
}

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

func resolveBaseDir() string {
	dir, err := os.Getwd()
	check(err)
	return dir
}

func buildSearchPaths(base string) []string {
	var paths []string

	paths = append(paths, base)

	home, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths,
			filepath.Join(home, ".local", "share", "qk", "std"),
		)
	}

	paths = append(paths, filepath.Join("/usr", "local", "lib", "qk"))

	paths = append(paths, filepath.Join("/usr", "lib", "qk"))

	return paths
}

func check(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func checkErrs(errs []error) {
	if len(errs) > 0 {
		for _, err := range errs {
			fmt.Println(err)
		}
		os.Exit(1)
	}
}
