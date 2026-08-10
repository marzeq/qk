package stdlib

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:generate go run ../cmd/qkstdlibcheck -libs ../libs

// ReadSources loads library sources from an external directory tree. The .qks
// suffix keeps installed library sources out of ordinary project discovery.
func ReadSources(root string) (map[string]string, error) {
	result := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		extension := strings.ToLower(filepath.Ext(path))
		if extension != ".qks" && extension != ".qk" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		origin, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		result[origin] = string(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
