package stdlib

import "embed"

// Sources contains the compiler-authorized standard library. The .qks suffix
// keeps these files out of ordinary project source discovery.
//
//go:embed sources/*.qks
var Sources embed.FS

func ReadSources() (map[string]string, error) {
	entries, err := Sources.ReadDir("sources")
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := "sources/" + entry.Name()
		data, err := Sources.ReadFile(path)
		if err != nil {
			return nil, err
		}
		result["<stdlib>/"+entry.Name()] = string(data)
	}
	return result, nil
}
