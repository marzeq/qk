package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/marzeq/qk/attributes"
)

func TestArtifactCacheUsesRequestedLayoutAndBinaryFormat(t *testing.T) {
	t.Setenv("QK_CACHE_DIR", t.TempDir())
	cache, err := newArtifactCache(&Args{target: "aarch64-test", release: true})
	if err != nil {
		t.Fatal(err)
	}
	implementation := implementationHash("std.io", "; llvm")
	path := cache.modulePath(implementation)
	artifact := &cachedArtifact{ImplementationHash: implementation, Objects: map[string][]byte{"native": {1, 2, 3}}}
	if err := storeCachedArtifact(path, artifact); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "blob" || filepath.Base(filepath.Dir(path)) != implementation {
		t.Fatalf("unexpected module artifact path %q", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte(artifactMagic)) || bytes.HasPrefix(raw, []byte("{")) {
		t.Fatalf("artifact is not the custom binary format: %q", raw[:min(len(raw), 16)])
	}
	if bytes.Contains(raw, []byte("; llvm")) {
		t.Fatal("artifact contains LLVM IR")
	}
	loaded, ok := loadCachedArtifact(path, implementation)
	if !ok || !bytes.Equal(loaded.Objects["native"], []byte{1, 2, 3}) {
		t.Fatalf("artifact did not round trip: %#v", loaded)
	}
}

func TestSpecializationsAccumulateWithoutRewritingModuleBlob(t *testing.T) {
	t.Setenv("QK_CACHE_DIR", t.TempDir())
	cache, err := newArtifactCache(&Args{target: "x86_64-test"})
	if err != nil {
		t.Fatal(err)
	}
	implementation := implementationHash("std.io", "; base")
	modulePath := cache.modulePath(implementation)
	if err := storeCachedArtifact(modulePath, &cachedArtifact{ImplementationHash: implementation}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(modulePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []string{"request-a", "request-b"} {
		path := cache.specializationPath(implementation, request)
		requestHash := implementationHash("specialization", request)
		if err := storeCachedArtifact(path, &cachedArtifact{ImplementationHash: requestHash}); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(modulePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("adding specializations rewrote the owning module blob")
	}
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(modulePath), "specializations"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("specializations did not accumulate: entries=%d err=%v", len(entries), err)
	}
}

func TestCorruptArtifactIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blob")
	if err := os.WriteFile(path, []byte(artifactMagic+"broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadCachedArtifact(path, implementationHash("x", "y")); ok {
		t.Fatal("corrupt artifact was accepted")
	}
}

func TestConfigurationSeparatesIncompatibleBuilds(t *testing.T) {
	t.Setenv("QK_CACHE_DIR", t.TempDir())
	debugCache, err := newArtifactCache(&Args{target: "x86_64-test"})
	if err != nil {
		t.Fatal(err)
	}
	releaseCache, err := newArtifactCache(&Args{target: "x86_64-test", release: true})
	if err != nil {
		t.Fatal(err)
	}
	if debugCache.root == releaseCache.root {
		t.Fatal("release mode did not change the configuration namespace")
	}
}

func TestBuildSnapshotRoundTrip(t *testing.T) {
	t.Setenv("QK_CACHE_DIR", t.TempDir())
	cache, err := newArtifactCache(&Args{target: "x86_64-test"})
	if err != nil {
		t.Fatal(err)
	}
	implementation := implementationHash("main", "; module")
	modulePath := cache.modulePath(implementation)
	if err := storeCachedArtifact(modulePath, &cachedArtifact{
		ImplementationHash: implementation, Objects: map[string][]byte{cachedNativeObjectKey: {1}},
	}); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cache.root, modulePath)
	if err != nil {
		t.Fatal(err)
	}
	want := &cachedBuildSnapshot{
		Modules:   []cachedBuildEntry{{Name: "main", Path: relative}},
		Links:     []attributes.Link{{Kind: attributes.LinkSystem, Value: "c"}},
		LinkRoots: []string{"entry"}, Warnings: []string{"warning"},
	}
	if err := cache.storeBuildSnapshot("build", want); err != nil {
		t.Fatal(err)
	}
	got, ok := cache.loadBuildSnapshot("build")
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot did not round trip: got %#v, want %#v", got, want)
	}
}
