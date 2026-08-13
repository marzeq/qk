package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type sourceMountKind uint8

const (
	sourceMountCollection sourceMountKind = iota
	sourceMountExact
)

type sourceMount struct {
	Kind           sourceMountKind
	Prefix         string
	Directory      string
	SemanticBase   string
	PreservePrefix bool
}

type projectManifest struct {
	Path   string
	Root   string
	Source string
	Mounts []sourceMount
}

var manifestDirective = regexp.MustCompile(`^\s*(sources|source)\s+([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s+("(?:\\.|[^"\\])*")\s*(?://.*)?$`)

func findProjectManifest(start string) (*projectManifest, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for {
		path := filepath.Join(dir, "qk.mod")
		data, readErr := os.ReadFile(path)
		if readErr == nil {
			return parseProjectManifest(path, string(data))
		}
		if !os.IsNotExist(readErr) {
			return nil, readErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}

func parseProjectManifest(path, source string) (*projectManifest, error) {
	manifest := &projectManifest{Path: path, Root: filepath.Dir(path), Source: source}
	exact := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(source))
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "//") {
			continue
		}
		matches := manifestDirective.FindStringSubmatch(scanner.Text())
		if matches == nil {
			return nil, fmt.Errorf("%s:%d: invalid qk.mod directive", path, line)
		}
		prefix := matches[2]
		if prefix == "std" || strings.HasPrefix(prefix, "std.") || prefix == "main" {
			return nil, fmt.Errorf("%s:%d: source prefix %q is reserved", path, line, prefix)
		}
		relative, err := strconv.Unquote(matches[3])
		if err != nil || relative == "" {
			return nil, fmt.Errorf("%s:%d: source path must be a non-empty quoted string", path, line)
		}
		directory := relative
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(manifest.Root, directory)
		}
		directory, err = filepath.Abs(directory)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: resolve source path: %w", path, line, err)
		}
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("%s:%d: source path is not a directory: %s", path, line, directory)
		}
		mount := sourceMount{Prefix: prefix, Directory: directory}
		if matches[1] == "source" {
			if previous := exact[prefix]; previous != "" && pathKey(previous) != pathKey(directory) {
				return nil, fmt.Errorf("%s:%d: conflicting source mount %q maps to both %s and %s", path, line, prefix, previous, directory)
			}
			exact[prefix] = directory
			mount.Kind = sourceMountExact
			parts := strings.Split(prefix, ".")
			mount.SemanticBase = parts[len(parts)-1]
		}
		manifest.Mounts = append(manifest.Mounts, mount)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return manifest, nil
}
