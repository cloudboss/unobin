package e2etest

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Option configures e2e case execution.
type Option func(*config)

type config struct {
	repoRoot      string
	e2eLibraryDir string
}

// CompiledCase describes a compiled-factory e2e case.
type CompiledCase struct {
	Name          string      `json:"name"`
	Dir           string      `json:"-"`
	FactoryPath   string      `json:"factoryPath"`
	LibraryPath   string      `json:"libraryPath"`
	Build         bool        `json:"build"`
	Commands      []Command   `json:"commands"`
	Files         []FileCheck `json:"files"`
	StateSummary  string      `json:"stateSummary"`
	Deterministic bool        `json:"deterministic"`
}

// SourceCase describes a source-root CLI e2e case.
type SourceCase struct {
	Name     string      `json:"name"`
	Dir      string      `json:"-"`
	RootPath string      `json:"rootPath"`
	Commands []Command   `json:"commands"`
	Files    []FileCheck `json:"files"`
}

// Command describes one subprocess invocation.
type Command struct {
	Name     string            `json:"name"`
	Args     []string          `json:"args"`
	Dir      string            `json:"dir"`
	Env      map[string]string `json:"env"`
	Stdout   string            `json:"stdout"`
	Stderr   string            `json:"stderr"`
	ExitCode int               `json:"exitCode"`
}

// FileCheck describes a file compared to a golden.
type FileCheck struct {
	Path string `json:"path"`
	Want string `json:"want"`
}

// RunCompiledCases runs compiled-factory cases found under dir.
func RunCompiledCases(t *testing.T, dir string, opts ...Option) {
	t.Helper()
	cases, err := DiscoverCompiledCases(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := newConfig(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			runCompiledCase(t, cfg, c)
		})
	}
}

// RunSourceCases runs source-root cases found under dir.
func RunSourceCases(t *testing.T, dir string, opts ...Option) {
	t.Helper()
	_, err := DiscoverSourceCases(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("unimplemented")
}

// DiscoverCompiledCases reads compiled-factory case.json files under dir.
func DiscoverCompiledCases(dir string) ([]CompiledCase, error) {
	caseDirs, err := discoverCaseDirs(dir)
	if err != nil {
		return nil, err
	}
	cases := make([]CompiledCase, 0, len(caseDirs))
	for _, caseDir := range caseDirs {
		var c CompiledCase
		if err := decodeCase(caseDir, &c); err != nil {
			return nil, err
		}
		c.Dir = caseDir
		if err := validateCompiledCase(c); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(caseDir, "case.json"), err)
		}
		cases = append(cases, c)
	}
	sort.Slice(cases, func(i, j int) bool {
		return cases[i].Name < cases[j].Name
	})
	if err := rejectDuplicateNames(compiledNames(cases)); err != nil {
		return nil, err
	}
	return cases, nil
}

// DiscoverSourceCases reads source-root case.json files under dir.
func DiscoverSourceCases(dir string) ([]SourceCase, error) {
	caseDirs, err := discoverCaseDirs(dir)
	if err != nil {
		return nil, err
	}
	cases := make([]SourceCase, 0, len(caseDirs))
	for _, caseDir := range caseDirs {
		var c SourceCase
		if err := decodeCase(caseDir, &c); err != nil {
			return nil, err
		}
		c.Dir = caseDir
		if err := validateSourceCase(c); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(caseDir, "case.json"), err)
		}
		cases = append(cases, c)
	}
	sort.Slice(cases, func(i, j int) bool {
		return cases[i].Name < cases[j].Name
	})
	if err := rejectDuplicateNames(sourceNames(cases)); err != nil {
		return nil, err
	}
	return cases, nil
}

func discoverCaseDirs(dir string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "case.json" {
			return nil
		}
		dirs = append(dirs, filepath.Dir(path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no cases under %s", dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

func decodeCase(caseDir string, v any) error {
	path := filepath.Join(caseDir, "case.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func validateCompiledCase(c CompiledCase) error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if err := checkRelPath("factoryPath", c.FactoryPath); err != nil {
		return err
	}
	if err := validateCommands(c.Commands); err != nil {
		return err
	}
	if err := validateFiles(c.Files); err != nil {
		return err
	}
	return checkRelPath("stateSummary", c.StateSummary)
}

func validateSourceCase(c SourceCase) error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if err := checkRelPath("rootPath", c.RootPath); err != nil {
		return err
	}
	if err := validateCommands(c.Commands); err != nil {
		return err
	}
	return validateFiles(c.Files)
}

func validateCommands(commands []Command) error {
	for i, cmd := range commands {
		prefix := fmt.Sprintf("commands[%d]", i)
		if cmd.Name == "" {
			return fmt.Errorf("%s.name is required", prefix)
		}
		if err := checkRelPath(prefix+".dir", cmd.Dir); err != nil {
			return err
		}
		if err := checkRelPath(prefix+".stdout", cmd.Stdout); err != nil {
			return err
		}
		if err := checkRelPath(prefix+".stderr", cmd.Stderr); err != nil {
			return err
		}
	}
	return nil
}

func validateFiles(files []FileCheck) error {
	for i, file := range files {
		prefix := fmt.Sprintf("files[%d]", i)
		if err := checkRelPath(prefix+".path", file.Path); err != nil {
			return err
		}
		if err := checkRelPath(prefix+".want", file.Want); err != nil {
			return err
		}
	}
	return nil
}

func checkRelPath(field, path string) error {
	if path == "" {
		return nil
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("%s must be relative", field)
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s must stay under the case directory", field)
	}
	return nil
}

func rejectDuplicateNames(names []string) error {
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			return fmt.Errorf("duplicate case name %s", names[i])
		}
	}
	return nil
}

func compiledNames(cases []CompiledCase) []string {
	names := make([]string, 0, len(cases))
	for _, c := range cases {
		names = append(names, c.Name)
	}
	return names
}

func sourceNames(cases []SourceCase) []string {
	names := make([]string, 0, len(cases))
	for _, c := range cases {
		names = append(names, c.Name)
	}
	return names
}
