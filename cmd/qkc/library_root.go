package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const qkModulePath = "github.com/marzeq/qk"

func resolveLibraryRoot() (string, error) {
	if override := os.Getenv("QK_LIB_DIR"); override != "" {
		return requireLibraryRoot(override)
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return resolveLibraryRootFor(executable, cwd, os.TempDir())
}

func resolveLibraryRootFor(executable, cwd, temporaryRoot string) (string, error) {
	if runningViaGoRun(executable, temporaryRoot) {
		if root := findQKCheckout(cwd); root != "" {
			return filepath.Join(root, "libs"), nil
		}
		return "", fmt.Errorf("qkc is running through go run but no QK checkout containing libs was found above %s", cwd)
	}

	executables := []string{executable}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil && resolved != executable {
		executables = append(executables, resolved)
	}
	for _, installedExecutable := range executables {
		executableDir := filepath.Dir(installedExecutable)
		for _, candidate := range []string{
			filepath.Join(executableDir, "..", "libs"),
			filepath.Join(executableDir, "libs"),
		} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return filepath.Abs(candidate)
			}
		}
	}
	return "", fmt.Errorf("QK libraries not found beside %s; expected ../libs relative to the installed bin directory or set QK_LIB_DIR", executable)
}

func runningViaGoRun(executable, temporaryRoot string) bool {
	relative, err := filepath.Rel(temporaryRoot, executable)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	for _, part := range parts {
		if strings.HasPrefix(part, "go-build") {
			return true
		}
	}
	return false
}

func findQKCheckout(start string) string {
	current, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for {
		moduleFile := filepath.Join(current, "go.mod")
		if data, readErr := os.ReadFile(moduleFile); readErr == nil && strings.Contains(string(data), "module "+qkModulePath) {
			if info, statErr := os.Stat(filepath.Join(current, "libs")); statErr == nil && info.IsDir() {
				return current
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func requireLibraryRoot(value string) (string, error) {
	root, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("QK library root %s is unavailable: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("QK library root %s is not a directory", root)
	}
	return root, nil
}
