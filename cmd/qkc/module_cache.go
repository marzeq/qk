package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	moduleCacheMaxAge  = 30 * 24 * time.Hour
	moduleCacheMaxSize = int64(512 << 20)
)

type moduleCacheEntry struct {
	path    string
	modTime time.Time
	size    int64
}

func cleanupModuleObjectCache(verbose bool) {
	cacheHome, err := os.UserCacheDir()
	if err != nil {
		return
	}
	cacheDir := filepath.Join(cacheHome, "qk", "modules")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-moduleCacheMaxAge)
	var cached []moduleCacheEntry
	var totalSize int64
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".o") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(cacheDir, entry.Name())
		if info.ModTime().Before(cutoff) {
			if os.Remove(path) == nil {
				removed++
			}
			continue
		}
		cached = append(cached, moduleCacheEntry{path: path, modTime: info.ModTime(), size: info.Size()})
		totalSize += info.Size()
	}

	sort.Slice(cached, func(i, j int) bool {
		return cached[i].modTime.Before(cached[j].modTime)
	})
	for _, entry := range cached {
		if totalSize <= moduleCacheMaxSize {
			break
		}
		if os.Remove(entry.path) == nil {
			totalSize -= entry.size
			removed++
		}
	}
	if verbose && removed > 0 {
		fmt.Printf("removed %d stale cached modules\n", removed)
	}
}

func moduleObjectCachePath(llvmOutput string, args *Args) string {
	cacheHome, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	hash := sha256.New()
	for _, value := range append([]string{
		"qk-module-object-v6-libllvm22",
		llvmOutput,
		string(args.optLevel),
		args.target,
		args.cpu,
		args.features,
		args.targetABI,
		args.relocation,
		args.codeModel,
	}) {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	key := hex.EncodeToString(hash.Sum(nil))
	return filepath.Join(cacheHome, "qk", "modules", key+".o")
}

func storeModuleObject(cachePath, objPath string) {
	if cachePath == "" {
		return
	}
	data, err := os.ReadFile(objPath)
	if err != nil {
		return
	}
	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}
	temp, err := os.CreateTemp(cacheDir, "module-*.o")
	if err != nil {
		return
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return
	}
	if err := temp.Close(); err != nil {
		return
	}
	_ = os.Rename(tempPath, cachePath)
}
