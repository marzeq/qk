package modules

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/tokeniser"
)

type FileNode struct {
	Path    string
	Imports []*FileNode
}

type DepGraph struct {
	Files map[string]*FileNode
	ASTs  map[string]*parser.RootNode
}

func NewDepGraph(path string, rn *parser.RootNode) (*DepGraph, error) {
	dg := &DepGraph{
		Files: make(map[string]*FileNode),
		ASTs:  make(map[string]*parser.RootNode),
	}

	err := dg.Construct(path, rn)
	if err != nil {
		return nil, err
	}

	return dg, nil
}

var moduleSearchPaths = []string{
	".",
	"/usr/share/qk/lib",
	"/usr/local/share/qk/lib",
	filepath.Join(os.Getenv("HOME"), ".local/share/qk/lib"),
}

func (g *DepGraph) Construct(path string, rn *parser.RootNode) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, ok := g.ASTs[path]; ok {
		return nil
	}

	node := &FileNode{
		Path:    path,
		Imports: []*FileNode{},
	}
	g.Files[path] = node
	g.ASTs[path] = rn

	allFiles, err := findSrcFiles()
	if err != nil {
		return err
	}

	for _, moduleName := range getImports(rn) {
		moduleFilesFound := false

		for _, f := range allFiles {
			isCwd := false
			absF, _ := filepath.Abs(f)
			cwd, _ := filepath.Abs(".")
			if filepath.Dir(absF) == cwd {
				isCwd = true
			}

			t, err := tokeniser.NewTokeniserFromFile(f)
			if err != nil {
				if isCwd {
					return err
				}
				continue
			}
			toks, err := t.Tokenise()
			if err != nil {
				if isCwd {
					return err
				}
				continue
			}
			p := parser.NewParser(toks)
			ast, err := p.Parse()
			if err != nil || len(ast.Body) == 0 {
				if isCwd {
					return err
				}
				continue
			}

			if mn, ok := ast.Body[0].(*parser.ModuleNode); !ok || mn.Name != moduleName {
				continue
			}

			moduleFilesFound = true
			if err := g.Construct(f, ast); err != nil {
				return err
			}
			node.Imports = append(node.Imports, g.Files[f])
		}

		if !moduleFilesFound {
			return fmt.Errorf("module not found: %s", moduleName)
		}
	}

	return nil
}

func getImports(rn *parser.RootNode) []string {
	modules := []string{}
	for _, n := range rn.Body {
		if imp, ok := n.(*parser.ImportNode); ok {
			modules = append(modules, imp.Modules...)
		}
	}
	return modules
}

func findSrcFiles() ([]string, error) {
	var result []string
	seen := map[string]struct{}{}

	for _, dir := range moduleSearchPaths {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".qk" {
				return nil
			}
			if _, ok := seen[path]; ok {
				return nil
			}
			seen[path] = struct{}{}
			result = append(result, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (g *DepGraph) TopoSort() ([]string, error) {
	visited := map[string]bool{}
	temp := map[string]bool{}
	result := []string{}

	var visit func(n *FileNode) error
	visit = func(n *FileNode) error {
		if n == nil {
			return nil
		}
		if temp[n.Path] {
			return fmt.Errorf("cycle detected at file: %s", n.Path)
		}
		if visited[n.Path] {
			return nil
		}

		temp[n.Path] = true
		for _, child := range n.Imports {
			if err := visit(child); err != nil {
				return err
			}
		}
		temp[n.Path] = false

		visited[n.Path] = true
		result = append(result, n.Path)
		return nil
	}

	for _, n := range g.Files {
		if !visited[n.Path] {
			if err := visit(n); err != nil {
				return nil, err
			}
		}
	}

	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result, nil
}
