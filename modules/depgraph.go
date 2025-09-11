package modules

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/marzeq/quokka/parser"
	"github.com/marzeq/quokka/tokeniser"
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
	"/usr/share/quokka/lib",
	"/usr/local/share/quokka/lib",
	filepath.Join(os.Getenv("HOME"), ".local/share/quokka/lib"),
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

	modules := getImports(rn)
	for _, moduleName := range modules {
		files, err := findFilesForModule(moduleName)
		if err != nil {
			return err
		}

		modules := getImports(rn)
		for _, moduleName := range modules {
			files, err := findFilesForModule(moduleName)
			if err != nil {
				return err
			}

			if len(files) == 0 {
				return fmt.Errorf("module not found: %s", moduleName)
			}

			for _, f := range files {
				childNode, ok := g.Files[f]
				if !ok || childNode == nil {
					continue
				}
				node.Imports = append(node.Imports, childNode)
			}
		}

		for _, f := range files {
			t, err := tokeniser.NewTokeniserFromFile(f)
			if err != nil {
				return err
			}
			toks, err := t.Tokenise()
			if err != nil {
				return err
			}
			p := parser.NewParser(toks)
			ast, err := p.Parse()
			if err != nil {
				return err
			}

			firstModule := ""
			if len(ast.Body) > 0 {
				if mn, ok := ast.Body[0].(*parser.ModuleNode); ok {
					firstModule = mn.Name
				}
			}
			if firstModule != moduleName {
				continue
			}

			if err := g.Construct(f, ast); err != nil {
				return err
			}
			node.Imports = append(node.Imports, g.Files[f])
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

func findFilesForModule(moduleName string) ([]string, error) {
	var result []string
	seen := map[string]struct{}{}

	for _, dir := range moduleSearchPaths {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if _, ok := seen[path]; ok {
				return nil
			}

			t, err := tokeniser.NewTokeniserFromFile(path)
			if err != nil {
				return nil
			}
			toks, err := t.Tokenise()
			if err != nil {
				return nil
			}
			p := parser.NewParser(toks)
			ast, err := p.Parse()
			if err != nil || len(ast.Body) == 0 {
				return nil
			}
			if mn, ok := ast.Body[0].(*parser.ModuleNode); ok && mn.Name == moduleName {
				result = append(result, path)
				seen[path] = struct{}{}
			}
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
