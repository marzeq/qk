//go:build (linux || darwin) && cgo && !qk_static_llvm

package ir

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>
static void *qk_open_library(const char *path) { return dlopen(path, RTLD_NOW | RTLD_LOCAL); }
static void *qk_find_symbol(void *handle, const char *name) { return dlsym(handle, name); }
static void *qk_find_process_symbol(const char *name) { return dlsym(RTLD_DEFAULT, name); }
static const char *qk_loader_error(void) { return dlerror(); }
*/
import "C"

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"github.com/marzeq/qk/attributes"
)

func openForeignLibrary(path string) (unsafe.Pointer, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	if handle := C.qk_open_library(cPath); handle != nil {
		return handle, nil
	}
	if detail := C.qk_loader_error(); detail != nil {
		return nil, fmt.Errorf("%s", C.GoString(detail))
	}
	return nil, fmt.Errorf("dynamic loader rejected library")
}

func findForeignSymbol(handle unsafe.Pointer, name string) unsafe.Pointer {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	return C.qk_find_symbol(handle, cName)
}

func findProcessForeignSymbol(name string) unsafe.Pointer {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	return C.qk_find_process_symbol(cName)
}

func foreignLibraryCandidates(link attributes.Link, searchPaths []string) ([]string, error) {
	switch link.Kind {
	case attributes.LinkPath:
		if strings.HasSuffix(link.Value, ".a") {
			return nil, fmt.Errorf("compile-time @link path %q is a static archive; use a loadable shared library", link.Value)
		}
		return []string{link.Value}, nil
	case attributes.LinkSystem:
		if runtime.GOOS == "darwin" {
			if link.Value == "c" {
				return []string{"/usr/lib/libSystem.B.dylib"}, nil
			}
			candidates := make([]string, 0, len(searchPaths)+4)
			for _, directory := range searchPaths {
				candidates = append(candidates, filepath.Join(directory, "lib"+link.Value+".dylib"))
			}
			return append(candidates,
				"lib"+link.Value+".dylib",
				filepath.Join("/opt/homebrew/lib", "lib"+link.Value+".dylib"),
				filepath.Join("/usr/local/lib", "lib"+link.Value+".dylib"),
				link.Value,
			), nil
		}
		candidates := make([]string, 0, len(searchPaths)+3)
		for _, directory := range searchPaths {
			candidates = append(candidates, filepath.Join(directory, "lib"+link.Value+".so"))
		}
		candidates = append(candidates, "lib"+link.Value+".so")
		if link.Value == "c" {
			candidates = append(candidates, "libc.so.6")
		}
		return append(candidates, link.Value), nil
	case attributes.LinkFramework:
		if runtime.GOOS != "darwin" {
			return nil, fmt.Errorf("compile-time @link framework %q cannot be loaded on Linux", link.Value)
		}
		name := link.Value
		return []string{
			filepath.Join("/System/Library/Frameworks", name+".framework", name),
			filepath.Join("/Library/Frameworks", name+".framework", name),
		}, nil
	}
	return nil, nil
}
