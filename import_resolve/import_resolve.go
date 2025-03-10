package import_resolve

import (
	"path/filepath"

	"github.com/marzeq/quokka/parser"
	"github.com/marzeq/quokka/shared"
	"github.com/marzeq/quokka/tokeniser"
)

type (
	Node = parser.Node
)

func ProcessImports(root *Node, loaded map[string]*Node, recStack map[string]bool) (*Node, error) {
	filePath := root.Loc.FilePath

	if recStack[filePath] {
		return nil, shared.NewError(root.Loc, "import cycle detected for file '%s'", filePath)
	}

	if merged, ok := loaded[filePath]; ok {
		return merged, nil
	}

	recStack[filePath] = true

	merged := &Node{
		Type:     root.Type,
		Loc:      root.Loc,
		Children: []*Node{},
	}

	for _, node := range root.Children {
		if node.Type != parser.NODE_TYPE_IMPORT {
			continue
		}

		importPath := node.Right.Value.(string)

		resolvedPath := filepath.Join(filepath.Dir(filePath), importPath)

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

		merged.Children = append(merged.Children, importedMerged.Children...)
	}

	for _, node := range root.Children {
		if node.Type != parser.NODE_TYPE_IMPORT {
			merged.Children = append(merged.Children, node)
		}
	}

	loaded[filePath] = merged
	delete(recStack, filePath)
	return merged, nil
}
