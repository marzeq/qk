package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/marzeq/qk/formatter"
)

func runFormat(args []string) error {
	if len(args) == 0 {
		args = []string{"."}
	}
	seen := make(map[string]bool)
	var files []string
	add := func(path string) {
		if !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
	}
	for _, arg := range args {
		recursive := strings.HasSuffix(arg, string(filepath.Separator)+"...")
		path := arg
		if recursive {
			path = strings.TrimSuffix(arg, string(filepath.Separator)+"...")
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if recursive || filepath.Ext(path) != ".qk" {
				return fmt.Errorf("%s is not a QK source file", arg)
			}
			add(path)
			continue
		}
		if recursive {
			err = filepath.WalkDir(path, func(found string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					if found != path && (entry.Name() == ".git" || entry.Name() == "build" || entry.Name() == "node_modules" || entry.Name() == "testdata") {
						return filepath.SkipDir
					}
					return nil
				}
				if filepath.Ext(found) == ".qk" {
					add(found)
				}
				return nil
			})
		} else {
			var entries []os.DirEntry
			entries, err = os.ReadDir(path)
			if err == nil {
				for _, entry := range entries {
					if !entry.IsDir() && filepath.Ext(entry.Name()) == ".qk" {
						add(filepath.Join(path, entry.Name()))
					}
				}
			}
		}
		if err != nil {
			return err
		}
	}
	sort.Strings(files)
	type change struct {
		path, text string
		mode       os.FileMode
	}
	var changes []change
	for _, path := range files {
		original, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted, err := formatter.Format(string(original), path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if string(original) == formatted {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		changes = append(changes, change{path, formatted, info.Mode().Perm()})
	}
	for _, item := range changes {
		if err := os.WriteFile(item.path, []byte(item.text), item.mode); err != nil {
			return err
		}
		fmt.Println(item.path)
	}
	return nil
}
