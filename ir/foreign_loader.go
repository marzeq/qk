//go:build (linux || darwin || windows) && cgo && !qk_static_llvm

package ir

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/marzeq/qk/attributes"
)

var foreignLibraries = struct {
	sync.Mutex
	handles map[string]unsafe.Pointer
}{handles: make(map[string]unsafe.Pointer)}

func foreignSymbol(name string, links []attributes.Link) (unsafe.Pointer, error) {
	foreignLibraries.Lock()
	defer foreignLibraries.Unlock()
	if len(links) == 0 {
		if address := findProcessForeignSymbol(name); address != nil {
			return address, nil
		}
		return nil, fmt.Errorf("foreign symbol %q is not present in the compiler process", name)
	}
	var searchPaths []string
	var failures []string
	for _, link := range links {
		if link.Kind == attributes.LinkSearchPath {
			searchPaths = append(searchPaths, link.Value)
		}
	}
	for _, link := range links {
		candidates, err := foreignLibraryCandidates(link, searchPaths)
		if err != nil {
			return nil, err
		}
		for _, path := range candidates {
			handle := foreignLibraries.handles[path]
			if handle == nil {
				handle, err = openForeignLibrary(path)
				if err != nil {
					failures = append(failures, path+": "+err.Error())
					continue
				}
				foreignLibraries.handles[path] = handle
			}
			if address := findForeignSymbol(handle, name); address != nil {
				return address, nil
			}
			failures = append(failures, path+": symbol is not exported")
		}
	}
	return nil, fmt.Errorf("could not load foreign symbol %q from compile-time @link libraries: %s", name, strings.Join(failures, "; "))
}
