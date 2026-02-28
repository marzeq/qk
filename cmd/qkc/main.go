package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marzeq/qk/loader"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
	"github.com/marzeq/qk/types"
)

func main() {
	args, err := parseArgs()
	check(err)

	searchPaths := buildSearchPaths(args.baseDir)

	files, err := collectSourceFiles(searchPaths, args.excludeDirs)
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

	if args.verbose {
		fmt.Println("parsed and collected modules")
	}

	modules, err := loader.BuildModules(partials)
	check(err)

	analyser := sema.NewAnalyser()

	errs := loader.RunSemanticPipeline(modules, analyser, args.verbose, args.debug)
	checkErrs(errs)

	if args.verbose {
		fmt.Println("semantic analysis completed successfully")
	}

	if _, ok := modules["main"]; !ok {
		fmt.Println("main module not found")
		os.Exit(1)
	}

	foundMain := false
	mainModule := modules["main"]
	for _, root := range mainModule.Roots {
		for _, stmt := range root.Body {
			switch fn := stmt.(type) {
			case *parser.FunctionDefNode:
				if fn.Name == "main" {
					if len(fn.Args) != 0 {
						fmt.Println(shared.NewError(fn.Loc, "main function must not have arguments"))
						os.Exit(1)
					}
					if fn.Body == nil {
						fmt.Println(shared.NewError(fn.Loc, "main function must have a body"))
						os.Exit(1)
					}
					if fn.Symbol.Signature.ReturnType != types.PrimitiveVoid {
						fmt.Println(shared.NewError(fn.Loc, "main function must return void"))
					}
					foundMain = true
					break
				}
			}
		}
	}
	if !foundMain {
		fmt.Println("main function not found in main module")
		os.Exit(1)
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

func buildSearchPaths(baseDir string) []string {
	var paths []string

	paths = append(paths, baseDir)

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
