//go:build windows && cgo && !qk_static_llvm

package ir

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/marzeq/qk/attributes"
)

func openForeignLibrary(path string) (unsafe.Pointer, error) {
	library, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, err
	}
	// LoadDLL keeps the module loaded until Release is called. The evaluator's
	// handle cache deliberately retains it for the compiler process lifetime.
	return unsafe.Pointer(uintptr(library.Handle)), nil
}

func findForeignSymbol(handle unsafe.Pointer, name string) unsafe.Pointer {
	address, err := syscall.GetProcAddress(syscall.Handle(uintptr(handle)), name)
	if err != nil {
		return nil
	}
	return unsafe.Pointer(address)
}

func findProcessForeignSymbol(name string) unsafe.Pointer {
	getModuleHandle := syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
	if err := getModuleHandle.Find(); err == nil {
		if module, _, _ := getModuleHandle.Call(0); module != 0 {
			if address := findForeignSymbol(unsafe.Pointer(module), name); address != nil {
				return address
			}
		}
	}
	for _, libraryName := range []string{"ucrtbase.dll", "msvcrt.dll"} {
		handle := foreignLibraries.handles[libraryName]
		if handle == nil {
			var err error
			handle, err = openForeignLibrary(libraryName)
			if err != nil {
				continue
			}
			foreignLibraries.handles[libraryName] = handle
		}
		if address := findForeignSymbol(handle, name); address != nil {
			return address
		}
	}
	return nil
}

func foreignLibraryCandidates(link attributes.Link, searchPaths []string) ([]string, error) {
	switch link.Kind {
	case attributes.LinkPath:
		if strings.HasSuffix(link.Value, ".a") || strings.HasSuffix(link.Value, ".lib") {
			return nil, fmt.Errorf("compile-time @link path %q is a static or import library; use a loadable DLL", link.Value)
		}
		return []string{link.Value}, nil
	case attributes.LinkSystem:
		name := link.Value
		switch strings.ToLower(name) {
		case "ucrt":
			name = "ucrtbase"
		case "msvcrt":
			name = "msvcrt"
		}
		if !strings.HasSuffix(strings.ToLower(name), ".dll") {
			name += ".dll"
		}
		names := []string{name}
		if !strings.HasPrefix(strings.ToLower(name), "lib") {
			names = append(names, "lib"+name)
		}
		candidates := make([]string, 0, (len(searchPaths)+1)*len(names))
		for _, directory := range searchPaths {
			for _, candidate := range names {
				candidates = append(candidates, filepath.Join(directory, candidate))
			}
		}
		return append(candidates, names...), nil
	case attributes.LinkFramework:
		return nil, fmt.Errorf("compile-time @link framework %q cannot be loaded on Windows", link.Value)
	}
	return nil, nil
}
