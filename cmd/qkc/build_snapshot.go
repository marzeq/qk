package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/marzeq/qk/attributes"
)

func storeSuccessfulBuildSnapshot(
	cache *artifactCache,
	buildHash string,
	order []string,
	artifactPaths map[string]string,
	links []attributes.Link,
	linkRoots []string,
	warnings []string,
	verbose bool,
) {
	snapshot := &cachedBuildSnapshot{
		Links:     append([]attributes.Link(nil), links...),
		LinkRoots: append([]string(nil), linkRoots...),
		Warnings:  append([]string(nil), warnings...),
	}
	for _, moduleName := range order {
		path := artifactPaths[moduleName]
		relative, err := filepath.Rel(cache.root, path)
		if path == "" || err != nil {
			return
		}
		snapshot.Modules = append(snapshot.Modules, cachedBuildEntry{Name: moduleName, Path: relative})
	}
	if path := artifactPaths["__qk.runtime"]; path != "" {
		if relative, err := filepath.Rel(cache.root, path); err == nil {
			snapshot.Runtime = relative
		} else {
			return
		}
	}
	if err := cache.storeBuildSnapshot(buildHash, snapshot); err != nil && verbose {
		fmt.Fprintf(os.Stderr, "warning: failed to store lowered build snapshot: %v\n", err)
	}
}
