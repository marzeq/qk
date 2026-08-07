package stdlib

import (
	"embed"
	"io/fs"
	"strings"
)

//go:generate go run ../cmd/qkstdlibcheck

// Sources contains the compiler-authorized standard library. The .qks suffix
// keeps these files out of ordinary project source discovery.
//
//go:embed sources
var Sources embed.FS

func ReadSources() (map[string]string, error) {
	result := make(map[string]string)
	err := fs.WalkDir(Sources, "sources", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".qks") {
			return nil
		}

		data, err := Sources.ReadFile(path)
		if err != nil {
			return err
		}
		origin := strings.TrimPrefix(path, "sources/")
		result["<stdlib>/"+origin] = string(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
