package modules

import (
  "fmt"
  "path/filepath"

  "github.com/marzeq/quokka/parser"
  "github.com/marzeq/quokka/tokeniser"
)

type FileNode struct {
  Path string
  Imports []*FileNode
}

type DepGraph struct {
  Files map[string]*FileNode
  ASTs map[string]*parser.RootNode
}

func NewDepGraph(path string, rn *parser.RootNode) (*DepGraph, error) {
  dg := &DepGraph{
    Files: make(map[string]*FileNode),
    ASTs: make(map[string]*parser.RootNode),
  }

  err := dg.Construct(path, rn)
  if err != nil {
    return nil, err
  }

  return dg, nil
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
    Path: path,
    Imports: []*FileNode{},
  }
  g.Files[path] = node
  g.ASTs[path] = rn

  imports := getImports(rn)
  for _, imp := range imports {
    imp, err := filepath.Abs(imp)
    if err != nil {
      return err
    }
    t, err := tokeniser.NewTokeniserFromFile(imp)
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

    if err := g.Construct(imp, ast); err != nil {
      return err
    }
    node.Imports = append(node.Imports, g.Files[imp])
  }

  return nil
}

func getImports(rn *parser.RootNode) []string {
  imports := []string{}
  for _, n := range rn.Body {
    switch node := n.(type) {
    case *parser.ImportNode:
      imports = append(imports, node.Modules...)
    }
  }
  return imports
}

func (g *DepGraph) TopoSort() ([]string, error) {
  visited := map[string]bool{}
  temp := map[string]bool{}
  result := []string{}

  var visit func(n *FileNode) error
  visit = func(n *FileNode) error {
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

