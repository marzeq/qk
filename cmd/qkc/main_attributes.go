package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/tokeniser"
)

// Resolve build mode before staging, since target capabilities select stdlib code.
func applyMainModuleAttributes(args *Args, sources, packages map[string]string, parsed ...map[string]*parser.RootNode) error {
	var module *parser.ModuleNode
	paths := make([]string, 0, len(sources))
	for path := range sources {
		if packages[path] == args.mainModule {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		if packages[path] != args.mainModule {
			continue
		}
		var root *parser.RootNode
		if len(parsed) != 0 {
			root = parsed[0][path]
		}
		if root == nil {
			tokens, err := tokeniser.NewTokeniser(sources[path], path).Tokenise()
			if err != nil {
				return err
			}
			root, err = parser.NewParser(tokens).Parse()
			if err != nil {
				return err
			}
		}
		for _, node := range root.Body {
			if declaration, ok := node.(*parser.ModuleNode); ok {
				if module != nil && (declaration.Attributes.Get(attributes.AttributeTypeEntry) != nil || declaration.Attributes.Get(attributes.AttributeTypeFreestanding) != nil) {
					return fmt.Errorf("@entry and @freestanding must be on the first main module declaration")
				}
				if module == nil {
					module = declaration
				}
				break
			}
		}
	}
	if module == nil {
		return fmt.Errorf("main module declaration is missing")
	}
	if module.Attributes.Get(attributes.AttributeTypeFreestanding) != nil {
		args.freestanding = true
	}
	if args.entry == "" {
		if entry, ok := module.Attributes.Get(attributes.AttributeTypeEntry).(attributes.ModuleAttributeEntry); ok {
			args.entry = entry.Symbol
		}
	}
	if args.entry != "" && !args.freestanding {
		return fmt.Errorf("@entry and -entry require freestanding mode")
	}
	if args.freestanding && args.entry == "" {
		return fmt.Errorf("freestanding executable requires @entry(\"symbol\") or -entry <symbol>")
	}
	if strings.ContainsAny(args.entry, " \t\r\n,=") {
		return fmt.Errorf("invalid entry symbol %q", args.entry)
	}
	return nil
}
