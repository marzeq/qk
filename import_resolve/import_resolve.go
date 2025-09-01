package import_resolve

import (
	"fmt"
	"path/filepath"

	"github.com/marzeq/quokka/parser"
	"github.com/marzeq/quokka/shared"
	"github.com/marzeq/quokka/tokeniser"
)

func ProcessImports(root *parser.RootNode, loaded map[string]*parser.RootNode, recStack map[string]bool) (*parser.RootNode, error) {
  fmt.Print()
  filePath := root.Loc.FilePath

  if recStack[filePath] {
    return nil, shared.NewError(root.Loc, "import cycle detected for file '%s'", filePath)
  }

  if merged, ok := loaded[filePath]; ok {
    return merged, nil
  }

  recStack[filePath] = true

  merged := &parser.RootNode{
    Loc: root.Loc,
    Body: []parser.Node{},
  }

  for _, n := range root.Body {
    switch node := n.(type) {
    case *parser.ImportNode:
      resolvedPath := filepath.Join(filepath.Dir(filePath), node.Module)

      t, err := tokeniser.NewTokeniserFromFile(resolvedPath)
      if err != nil {
        switch err.(type) {
        case shared.Error:
          return nil, err
        default:
          return nil, shared.NewError(node.Loc, "import failed: %v", err)
        }
      }
      toks, err := t.Tokenise()
      if err != nil {
        return nil, err
      }
      p := parser.NewParser(toks)
      ast, err := p.Parse()
      if err != nil {
        return nil, err
      }

      importedMerged, err := ProcessImports(ast, loaded, recStack)
      if err != nil {
        return nil, err
      }

      merged.Body = append(merged.Body, importedMerged.Body...)
    }
  }

  for _, node := range root.Body {
    switch node.(type) {
    case *parser.ImportNode: continue
    default:
      merged.Body = append(merged.Body, node)
    }
  }

  loaded[filePath] = merged
  delete(recStack, filePath)
  return merged, nil
}
