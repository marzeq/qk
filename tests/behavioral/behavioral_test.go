package behavioral_test

import (
	"bytes"
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const updateEnvironment = "UPDATE_BEHAVIORAL"

var (
	repositoryRoot string
	fixturesRoot   string
	compilerPath   string
	testMainDir    string
)

type testSpec struct {
	Mode         string            `json:"mode"`
	Package      string            `json:"package,omitempty"`
	CompilerArgs []string          `json:"compilerArgs,omitempty"`
	Args         []string          `json:"args,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	GOOS         []string          `json:"goos,omitempty"`
	GOARCH       []string          `json:"goarch,omitempty"`
	Timeout      string            `json:"timeout,omitempty"`
	Serial       bool              `json:"serial,omitempty"`
	StdoutRegex  bool              `json:"stdoutRegex,omitempty"`
	StderrRegex  bool              `json:"stderrRegex,omitempty"`
}

type featureManifest struct {
	Features []featureEntry `json:"features"`
}

type featureEntry struct {
	Name                  string   `json:"name"`
	Positive              []string `json:"positive"`
	Negative              []string `json:"negative,omitempty"`
	NegativeNotApplicable bool     `json:"negativeNotApplicable,omitempty"`
}

func TestMain(m *testing.M) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "behavioral tests: cannot locate test source")
		os.Exit(2)
	}
	fixturesRoot = filepath.Dir(file)
	repositoryRoot = filepath.Clean(filepath.Join(fixturesRoot, "..", ".."))

	var err error
	testMainDir, err = os.MkdirTemp("", "qk-behavioral-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "behavioral tests: create temporary directory: %v\n", err)
		os.Exit(2)
	}
	compilerPath = filepath.Join(testMainDir, "qkc"+executableSuffix())
	build := exec.Command("go", "build", "-o", compilerPath, "./cmd/qkc")
	build.Dir = repositoryRoot
	build.Env = append(os.Environ(), "GOCACHE="+filepath.Join(testMainDir, "go-cache"))
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		fmt.Fprintf(os.Stderr, "behavioral tests: build qkc: %v\n%s", buildErr, output)
		_ = os.RemoveAll(testMainDir)
		os.Exit(2)
	}

	code := m.Run()
	_ = os.RemoveAll(testMainDir)
	os.Exit(code)
}

func TestBehavioral(t *testing.T) {
	cases, err := discoverCases(fixturesRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no behavioral fixtures found")
	}

	for _, caseName := range cases {
		t.Run(caseName, func(t *testing.T) {
			spec := readSpec(t, filepath.Join(fixturesRoot, filepath.FromSlash(caseName)))
			if !spec.Serial {
				t.Parallel()
			}
			runCase(t, caseName, spec)
		})
	}
}

func TestFeatureManifest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(fixturesRoot, "features.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest featureManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode features.json: %v", err)
	}
	if len(manifest.Features) == 0 {
		t.Fatal("features.json contains no features")
	}

	knownCases, err := discoverCases(fixturesRoot)
	if err != nil {
		t.Fatal(err)
	}
	known := make(map[string]bool, len(knownCases))
	modes := make(map[string]string, len(knownCases))
	for _, name := range knownCases {
		known[name] = true
		modes[name] = readSpec(t, filepath.Join(fixturesRoot, filepath.FromSlash(name))).Mode
	}
	seen := make(map[string]bool, len(manifest.Features))
	referenced := make(map[string]bool, len(knownCases))
	for index, feature := range manifest.Features {
		if feature.Name == "" {
			t.Errorf("features[%d] has an empty name", index)
		} else if seen[feature.Name] {
			t.Errorf("duplicate feature %q", feature.Name)
		}
		seen[feature.Name] = true
		if len(feature.Positive) == 0 {
			t.Errorf("feature %q has no positive fixture", feature.Name)
		}
		if len(feature.Negative) == 0 && !feature.NegativeNotApplicable {
			t.Errorf("feature %q has no negative fixture and is not marked negativeNotApplicable", feature.Name)
		}
		if len(feature.Negative) != 0 && feature.NegativeNotApplicable {
			t.Errorf("feature %q has negative fixtures and is also marked negativeNotApplicable", feature.Name)
		}
		for _, fixture := range append(append([]string(nil), feature.Positive...), feature.Negative...) {
			referenced[fixture] = true
			if !known[fixture] {
				t.Errorf("feature %q references missing fixture %q", feature.Name, fixture)
			}
		}
		for _, fixture := range feature.Positive {
			if known[fixture] && modes[fixture] != "run-pass" && modes[fixture] != "compile-pass" {
				t.Errorf("feature %q positive fixture %q has mode %q", feature.Name, fixture, modes[fixture])
			}
		}
		for _, fixture := range feature.Negative {
			if known[fixture] && modes[fixture] != "run-fail" && modes[fixture] != "compile-fail" {
				t.Errorf("feature %q negative fixture %q has mode %q", feature.Name, fixture, modes[fixture])
			}
		}
	}
	for _, fixture := range knownCases {
		if !referenced[fixture] {
			t.Errorf("fixture %q is not referenced by features.json", fixture)
		}
	}
}

func discoverCases(root string) ([]string, error) {
	var cases []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "test.json" {
			return nil
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		cases = append(cases, filepath.ToSlash(relative))
		return nil
	})
	sort.Strings(cases)
	return cases, err
}

func readSpec(t *testing.T, caseDir string) testSpec {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(caseDir, "test.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec testSpec
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		t.Fatalf("decode test.json: %v", err)
	}
	switch spec.Mode {
	case "run-pass", "compile-pass", "compile-fail", "run-fail":
	default:
		t.Fatalf("unknown behavioral mode %q", spec.Mode)
	}
	return spec
}

func runCase(t *testing.T, caseName string, spec testSpec) {
	t.Helper()
	if !matches(runtime.GOOS, spec.GOOS) || !matches(runtime.GOARCH, spec.GOARCH) {
		t.Skipf("fixture is gated to GOOS=%v GOARCH=%v", spec.GOOS, spec.GOARCH)
	}

	fixtureDir := filepath.Join(fixturesRoot, filepath.FromSlash(caseName))
	workDir := t.TempDir()
	if err := copyFixture(fixtureDir, workDir); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode := invoke(t, workDir, spec, spec.Mode, spec.CompilerArgs)
	assertResult(t, fixtureDir, workDir, spec, stdout, stderr, exitCode)
}

func invoke(t *testing.T, workDir string, spec testSpec, mode string, compilerArgs []string) (string, string, int) {
	t.Helper()
	timeout := 30 * time.Second
	if spec.Timeout != "" {
		parsed, err := time.ParseDuration(spec.Timeout)
		if err != nil {
			t.Fatalf("invalid timeout %q: %v", spec.Timeout, err)
		}
		timeout = parsed
	}

	command := "build"
	args := append([]string(nil), compilerArgs...)
	switch mode {
	case "run-pass", "run-fail":
		command = "run"
	case "compile-pass", "compile-fail":
		args = append(args, "-no-emit")
	case "build-pass":
	default:
		t.Fatalf("cannot invoke mode %q", mode)
	}
	args = append([]string{command}, args...)
	packageArg := spec.Package
	if packageArg == "" {
		packageArg = "."
	}
	args = append(args, packageArg)
	programArgs := append([]string(nil), spec.Args...)
	if fileArgs, ok := optionalFile(filepath.Join(workDir, "args.txt")); ok {
		programArgs = append(programArgs, splitLines(fileArgs)...)
	}
	if command == "run" {
		args = append(args, programArgs...)
	}

	cmd := exec.Command(compilerPath, args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	for key, value := range spec.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	// Diagnostics omit caret marker lines when rendered with ANSI colours.
	// Force the compiler's plain format so snapshots are identical whether the
	// parent go test command is attached to a terminal or captured by CI.
	cmd.Env = append(cmd.Env, "NO_COLOR=1", "QK_LIB_DIR="+filepath.Join(repositoryRoot, "libs"))
	if stdin, ok := optionalFile(filepath.Join(workDir, "stdin.txt")); ok {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start qkc: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("qkc exceeded timeout %s", timeout)
	}
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("run qkc: %v", runErr)
		}
		exitCode = exitErr.ExitCode()
	}
	stdoutText := stdout.String()
	if command == "run" {
		stdoutText = strings.TrimSuffix(stdoutText, fmt.Sprintf("\nExit code: %d\n", exitCode))
	}
	return normalize(stdoutText, workDir), normalize(stderr.String(), workDir), exitCode
}

func assertResult(t *testing.T, fixtureDir, workDir string, spec testSpec, stdout, stderr string, exitCode int) {
	t.Helper()
	expectedExit, hasExpectedExit := readExitCode(t, fixtureDir)
	if hasExpectedExit {
		if exitCode != expectedExit {
			t.Errorf("exit code: got %d, want %d", exitCode, expectedExit)
		}
	} else if strings.HasSuffix(spec.Mode, "-pass") && exitCode != 0 {
		t.Errorf("exit code: got %d, want 0", exitCode)
	} else if strings.HasSuffix(spec.Mode, "-fail") && exitCode == 0 {
		t.Error("exit code: got 0, want non-zero")
	}

	assertOutput(t, fixtureDir, workDir, "stdout.txt", stdout, spec.StdoutRegex)
	assertOutput(t, fixtureDir, workDir, "stderr.txt", stderr, spec.StderrRegex)
}

func assertOutput(t *testing.T, fixtureDir, workDir, name, actual string, regex bool) {
	t.Helper()
	path := filepath.Join(fixtureDir, name)
	expected, exists := optionalFile(path)
	if !exists {
		expected = ""
	}
	expected = normalizeExpected(expected, fixtureDir, workDir)
	if regex {
		matched, err := regexp.MatchString("(?s)\\A(?:"+expected+")\\z", actual)
		if err != nil {
			t.Fatalf("invalid %s regular expression: %v", name, err)
		}
		if !matched {
			t.Errorf("%s does not match\npattern:\n%s\nactual:\n%s", name, expected, actual)
		}
		return
	}
	if actual == expected {
		return
	}
	if os.Getenv(updateEnvironment) == "1" {
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatalf("update %s: %v", name, err)
		}
		return
	}
	t.Errorf("%s mismatch\n%s", name, lineDiff(expected, actual))
}

func assertNoDynamicLibraries(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		return
	}
	file, err := elf.Open(path)
	if err != nil {
		t.Fatalf("open output ELF %s: %v", path, err)
	}
	defer file.Close()
	libraries, err := file.ImportedLibraries()
	if err != nil {
		t.Fatalf("read imported libraries from %s: %v", path, err)
	}
	if len(libraries) != 0 {
		t.Errorf("output unexpectedly depends on dynamic libraries: %s", strings.Join(libraries, ", "))
	}
}

func copyFixture(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func normalize(value, workDir string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	workDirs := []string{workDir}
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil && resolved != workDir {
		workDirs = append(workDirs, resolved)
	}
	// macOS exposes /var through /private/var. MkdirTemp may return the former
	// while diagnostics contain the latter even when no symlink component was
	// present for EvalSymlinks to rewrite.
	if runtime.GOOS == "darwin" && strings.HasPrefix(workDir, "/var/") {
		workDirs = append(workDirs, "/private"+workDir)
	}
	slices.SortFunc(workDirs, func(left, right string) int { return len(right) - len(left) })
	for _, directory := range workDirs {
		value = strings.ReplaceAll(value, directory, "<CASE>")
		value = strings.ReplaceAll(value, filepath.ToSlash(directory), "<CASE>")
	}
	value = strings.ReplaceAll(value, compilerPath, "<QKC>")
	// Some Homebrew LLVM builds inject /usr/local/lib as a driver search path.
	// Newer ld64.lld versions warn when it is absent; this host-toolchain noise
	// is unrelated to the compiled program and varies between installations.
	value = strings.ReplaceAll(value, "ld64.lld: warning: directory not found for option -L/usr/local/lib\n", "")
	return value
}

func normalizeExpected(value, fixtureDir, workDir string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, fixtureDir, "<CASE>")
	value = strings.ReplaceAll(value, filepath.ToSlash(fixtureDir), "<CASE>")
	value = strings.ReplaceAll(value, "<FIXTURE>", "<CASE>")
	return normalize(value, workDir)
}

func readExitCode(t *testing.T, fixtureDir string) (int, bool) {
	t.Helper()
	text, ok := optionalFile(filepath.Join(fixtureDir, "exit-code.txt"))
	if !ok {
		return 0, false
	}
	value, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		t.Fatalf("invalid exit-code.txt: %v", err)
	}
	return value, true
}

func optionalFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return string(data), true
}

func splitLines(value string) []string {
	value = strings.TrimSuffix(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	if value == "" {
		return nil
	}
	return strings.Split(value, "\n")
}

func matches(value string, allowed []string) bool {
	return len(allowed) == 0 || contains(allowed, value)
}

func contains(values []string, wanted string) bool {
	return slices.Contains(values, wanted)
}

func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func lineDiff(expected, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")
	limit := max(len(actualLines), len(expectedLines))
	for index := range limit {
		var want, got string
		if index < len(expectedLines) {
			want = expectedLines[index]
		}
		if index < len(actualLines) {
			got = actualLines[index]
		}
		if want != got {
			return fmt.Sprintf("first difference at line %d\nwant: %q\n got: %q\n\nwant full:\n%s\nactual full:\n%s", index+1, want, got, expected, actual)
		}
	}
	return "outputs differ"
}
