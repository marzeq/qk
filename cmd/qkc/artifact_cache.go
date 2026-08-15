package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/marzeq/qk/attributes"
)

const artifactCacheVersion = 5
const artifactCompilerABI = "qk-staged-bindings-v4"
const artifactMagic = "QKARTF01"
const buildSnapshotMagic = "QKBUILD1"
const maxArtifactField = 1 << 30

type artifactCache struct {
	root string
}

type cachedArtifact struct {
	ImplementationHash string
	Objects            map[string][]byte
}

type cachedBuildEntry struct {
	Name string
	Path string
}

type cachedBuildSnapshot struct {
	Modules   []cachedBuildEntry
	Runtime   string
	Links     []attributes.Link
	LinkRoots []string
	Warnings  []string
}

func newArtifactCache(args *Args) (*artifactCache, error) {
	root := os.Getenv("QK_CACHE_DIR")
	if root == "" {
		userRoot, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(userRoot, "qk")
	}
	hash := sha256.New()
	for _, value := range []string{
		fmt.Sprint(artifactCacheVersion), artifactCompilerABI, args.target, args.sysroot, args.cpu, args.features,
		args.targetABI, fmt.Sprint(args.release), fmt.Sprint(args.debug), fmt.Sprint(args.outputType),
		string(args.optLevel), args.relocation, args.codeModel,
	} {
		writeHashString(hash, value)
	}
	return &artifactCache{root: filepath.Join(root, "artifacts-v4", hex.EncodeToString(hash.Sum(nil)))}, nil
}

func writeHashString(writer io.Writer, value string) {
	_, _ = io.WriteString(writer, value)
	_, _ = writer.Write([]byte{0})
}

func implementationHash(module, llvm string) string {
	hash := sha256.New()
	writeHashString(hash, module)
	writeHashString(hash, llvm)
	return hex.EncodeToString(hash.Sum(nil))
}

func buildInputHash(args *Args, sources, sourcePackages map[string]string) string {
	hash := sha256.New()
	for _, value := range []string{args.mainModule, args.target, args.sysroot, args.cpu, args.features, args.targetABI,
		fmt.Sprint(args.release), fmt.Sprint(args.debug), fmt.Sprint(args.outputType), string(args.optLevel),
		args.relocation, args.codeModel, args.manifestPath, args.manifestData,
		fmt.Sprint(artifactCacheVersion), artifactCompilerABI} {
		writeHashString(hash, value)
	}
	origins := make([]string, 0, len(sources))
	for origin := range sources {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	for _, origin := range origins {
		writeHashString(hash, origin)
		writeHashString(hash, sourcePackages[origin])
		writeHashString(hash, sources[origin])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (cache *artifactCache) buildPath(hash string) string {
	return filepath.Join(cache.root, "builds", hash)
}

func specializationOwnerHash(moduleHashes map[string]string) string {
	names := make([]string, 0, len(moduleHashes))
	for name := range moduleHashes {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	writeHashString(hash, "staged-type-specializations-v3")
	for _, name := range names {
		writeHashString(hash, name)
		writeHashString(hash, moduleHashes[name])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (cache *artifactCache) modulePath(hash string) string {
	return filepath.Join(cache.root, hash, "blob")
}

func (cache *artifactCache) specializationPath(ownerHash, requestHash string) string {
	return filepath.Join(cache.root, ownerHash, "specializations", requestHash)
}

func loadCachedArtifact(path, expectedHash string) (*cachedArtifact, bool) {
	artifact, ok := loadCachedArtifactAny(path)
	return artifact, ok && artifact.ImplementationHash == expectedHash
}

func loadCachedArtifactAny(path string) (*cachedArtifact, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	magic := make([]byte, len(artifactMagic))
	if _, err := io.ReadFull(reader, magic); err != nil || string(magic) != artifactMagic {
		return nil, false
	}
	version, err := readArtifactU32(reader)
	if err != nil || version != artifactCacheVersion {
		return nil, false
	}
	implementationHash, err := readArtifactString(reader)
	if err != nil || !validArtifactHash(implementationHash) {
		return nil, false
	}
	count, err := readArtifactU32(reader)
	if err != nil || count > 1024 {
		return nil, false
	}
	artifact := &cachedArtifact{ImplementationHash: implementationHash, Objects: make(map[string][]byte, count)}
	for range count {
		key, keyErr := readArtifactString(reader)
		object, objectErr := readArtifactBytes(reader)
		if keyErr != nil || objectErr != nil || key == "" {
			return nil, false
		}
		artifact.Objects[key] = object
	}
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return artifact, true
}

func (cache *artifactCache) storeBuildSnapshot(hash string, snapshot *cachedBuildSnapshot) error {
	path := cache.buildPath(hash)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".build-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	w := bufio.NewWriter(temporary)
	_, err = io.WriteString(w, buildSnapshotMagic)
	if err == nil {
		err = writeArtifactU32(w, artifactCacheVersion)
	}
	if err == nil {
		err = writeArtifactU32(w, uint32(len(snapshot.Modules)))
	}
	for _, module := range snapshot.Modules {
		if err == nil {
			err = writeArtifactString(w, module.Name)
		}
		if err == nil {
			err = writeArtifactString(w, module.Path)
		}
	}
	if err == nil {
		err = writeArtifactString(w, snapshot.Runtime)
	}
	if err == nil {
		err = writeArtifactU32(w, uint32(len(snapshot.Links)))
	}
	for _, link := range snapshot.Links {
		if err == nil {
			err = writeArtifactU32(w, uint32(link.Kind))
		}
		if err == nil {
			err = writeArtifactString(w, link.Value)
		}
	}
	for _, values := range [][]string{snapshot.LinkRoots, snapshot.Warnings} {
		if err == nil {
			err = writeArtifactU32(w, uint32(len(values)))
		}
		for _, value := range values {
			if err == nil {
				err = writeArtifactString(w, value)
			}
		}
	}
	if err == nil {
		err = w.Flush()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func (cache *artifactCache) loadBuildSnapshot(hash string) (*cachedBuildSnapshot, bool) {
	file, err := os.Open(cache.buildPath(hash))
	if err != nil {
		return nil, false
	}
	defer file.Close()
	r := bufio.NewReader(file)
	magic := make([]byte, len(buildSnapshotMagic))
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != buildSnapshotMagic {
		return nil, false
	}
	version, err := readArtifactU32(r)
	if err != nil || version != artifactCacheVersion {
		return nil, false
	}
	readStrings := func() ([]string, bool) {
		count, readErr := readArtifactU32(r)
		if readErr != nil || count > 1<<20 {
			return nil, false
		}
		values := make([]string, count)
		for i := range values {
			values[i], readErr = readArtifactString(r)
			if readErr != nil {
				return nil, false
			}
		}
		return values, true
	}
	count, err := readArtifactU32(r)
	if err != nil || count > 1<<20 {
		return nil, false
	}
	snapshot := &cachedBuildSnapshot{Modules: make([]cachedBuildEntry, count)}
	for i := range snapshot.Modules {
		snapshot.Modules[i].Name, err = readArtifactString(r)
		if err != nil {
			return nil, false
		}
		snapshot.Modules[i].Path, err = readArtifactString(r)
		if err != nil || strings.Contains(snapshot.Modules[i].Path, "..") || filepath.IsAbs(snapshot.Modules[i].Path) {
			return nil, false
		}
		artifact, ok := loadCachedArtifactAny(filepath.Join(cache.root, snapshot.Modules[i].Path))
		if !ok || filepath.Base(filepath.Dir(filepath.Join(cache.root, snapshot.Modules[i].Path))) != artifact.ImplementationHash && filepath.Base(filepath.Join(cache.root, snapshot.Modules[i].Path)) == "blob" {
			return nil, false
		}
	}
	snapshot.Runtime, err = readArtifactString(r)
	if err != nil || strings.Contains(snapshot.Runtime, "..") || filepath.IsAbs(snapshot.Runtime) {
		return nil, false
	}
	if snapshot.Runtime != "" {
		if _, ok := loadCachedArtifactAny(filepath.Join(cache.root, snapshot.Runtime)); !ok {
			return nil, false
		}
	}
	linkCount, err := readArtifactU32(r)
	if err != nil || linkCount > 1<<20 {
		return nil, false
	}
	for range linkCount {
		kind, kindErr := readArtifactU32(r)
		value, valueErr := readArtifactString(r)
		if kindErr != nil || valueErr != nil || kind > uint32(attributes.LinkFramework) {
			return nil, false
		}
		snapshot.Links = append(snapshot.Links, attributes.Link{Kind: attributes.LinkKind(kind), Value: value})
	}
	var ok bool
	if snapshot.LinkRoots, ok = readStrings(); !ok {
		return nil, false
	}
	if snapshot.Warnings, ok = readStrings(); !ok {
		return nil, false
	}
	if _, err := r.ReadByte(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return snapshot, true
}

func storeCachedArtifact(path string, artifact *cachedArtifact) error {
	if artifact == nil || !validArtifactHash(artifact.ImplementationHash) {
		return errors.New("cannot store empty artifact")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".artifact-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	writer := bufio.NewWriter(temporary)
	_, writeErr := io.WriteString(writer, artifactMagic)
	if writeErr == nil {
		writeErr = writeArtifactU32(writer, artifactCacheVersion)
	}
	if writeErr == nil {
		writeErr = writeArtifactString(writer, artifact.ImplementationHash)
	}
	keys := make([]string, 0, len(artifact.Objects))
	for key := range artifact.Objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if writeErr == nil {
		writeErr = writeArtifactU32(writer, uint32(len(keys)))
	}
	for _, key := range keys {
		if writeErr == nil {
			writeErr = writeArtifactString(writer, key)
		}
		if writeErr == nil {
			writeErr = writeArtifactBytes(writer, artifact.Objects[key])
		}
	}
	if writeErr == nil {
		writeErr = writer.Flush()
	}
	if closeErr := temporary.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return writeErr
	}
	if err := os.Rename(temporaryPath, path); err == nil {
		return nil
	}
	// Another compiler may have installed the same content-addressed artifact.
	if existing, ok := loadCachedArtifact(path, artifact.ImplementationHash); ok {
		for _, key := range keys {
			if len(existing.Objects[key]) == 0 {
				return os.Rename(temporaryPath, path)
			}
		}
		return nil
	}
	return os.Rename(temporaryPath, path)
}

func validArtifactHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func readArtifactU32(reader io.Reader) (uint32, error) {
	var encoded [4]byte
	_, err := io.ReadFull(reader, encoded[:])
	return binary.BigEndian.Uint32(encoded[:]), err
}

func writeArtifactU32(writer io.Writer, value uint32) error {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	_, err := writer.Write(encoded[:])
	return err
}

func readArtifactBytes(reader io.Reader) ([]byte, error) {
	size, err := readArtifactU32(reader)
	if err != nil || size > maxArtifactField {
		return nil, errors.New("invalid artifact field length")
	}
	result := make([]byte, size)
	_, err = io.ReadFull(reader, result)
	return result, err
}

func readArtifactString(reader io.Reader) (string, error) {
	value, err := readArtifactBytes(reader)
	return string(value), err
}

func writeArtifactBytes(writer io.Writer, value []byte) error {
	if len(value) > maxArtifactField {
		return errors.New("artifact field is too large")
	}
	if err := writeArtifactU32(writer, uint32(len(value))); err != nil {
		return err
	}
	_, err := writer.Write(value)
	return err
}

func writeArtifactString(writer io.Writer, value string) error {
	return writeArtifactBytes(writer, []byte(value))
}
