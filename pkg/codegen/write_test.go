package codegen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/stretchr/testify/require"
)

func TestWriteSourceLaysOutFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stack-out")

	_, err := WriteSource(dir, Input{
		Body:        "description: 'x'\n",
		FactoryName: "demo",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/libraries/core",
		},
	}, "1.26", "v0.10.0", map[string]string{
		"github.com/cloudboss/unobin/pkg/libraries/core": "v0.10.0",
	}, nil)
	require.NoError(t, err)

	mainBytes, err := os.ReadFile(filepath.Join(dir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(mainBytes), `factoryName        = "demo"`)

	modBytes, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	lib := string(modBytes)
	require.Contains(t, lib, "module demo")
	require.Contains(t, lib, "go 1.26")
	require.Contains(t, lib, "github.com/cloudboss/unobin v0.10.0")
}

func TestWriteSourceSkipsInternalUnobinImports(t *testing.T) {
	dir := t.TempDir()
	_, err := WriteSource(dir, Input{
		Body:        "description: 'x'\n",
		FactoryName: "demo",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/libraries/core",
		},
	}, "1.26", "v0.10.0", map[string]string{
		"github.com/cloudboss/unobin/pkg/libraries/core": "v0.10.0",
	}, nil)
	require.NoError(t, err)

	modBytes, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	lib := string(modBytes)
	require.NotContains(t, lib, "\tgithub.com/cloudboss/unobin/pkg/libraries/core",
		"internal unobin packages should not get their own require line")
}

func TestWriteSourceIncludesExternalImports(t *testing.T) {
	dir := t.TempDir()
	_, err := WriteSource(dir, Input{
		Body:        "description: 'x'\n",
		FactoryName: "demo",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/libraries/core",
			"aws":  "github.com/cloudboss/unobin-libraries/aws",
		},
	}, "1.26", "v0.10.0", map[string]string{
		"github.com/cloudboss/unobin/pkg/libraries/core": "v0.10.0",
		"github.com/cloudboss/unobin-libraries/aws":      "v0.5.0",
	}, nil)
	require.NoError(t, err)

	modBytes, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	lib := string(modBytes)
	require.Contains(t, lib, "github.com/cloudboss/unobin-libraries/aws v0.5.0")
}

func TestWriteSourceUsesGoModulesForGoMod(t *testing.T) {
	dir := t.TempDir()
	_, err := WriteSource(dir, Input{
		Body:        "description: 'x'\n",
		FactoryName: "demo",
		GoImports: map[string]string{
			"fs": "example.com/lib/fs",
		},
		GoModules: map[string]string{
			"example.com/lib": "v1.2.3",
		},
	}, "1.26", "v0.10.0", nil, nil)
	require.NoError(t, err)

	modBytes, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	want := `module demo

go 1.26

require (
	github.com/cloudboss/unobin v0.10.0
	example.com/lib v1.2.3
)
`
	require.Equal(t, want, string(modBytes))
}

func TestWriteSourceRejectsMissingVersion(t *testing.T) {
	dir := t.TempDir()
	_, err := WriteSource(dir, Input{
		Body:        "description: 'x'\n",
		FactoryName: "demo",
		GoImports: map[string]string{
			"aws": "github.com/cloudboss/unobin-libraries/aws",
		},
	}, "1.26", "v0.10.0", map[string]string{}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no version")
}

func TestWriteSourceRequiresGoVersion(t *testing.T) {
	dir := t.TempDir()
	_, err := WriteSource(dir, Input{
		Body:        "description: 'x'",
		FactoryName: "demo",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/libraries/core",
		},
	}, "", "v0.10.0", map[string]string{
		"github.com/cloudboss/unobin/pkg/libraries/core": "v0.10.0",
	}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "goVersion")
}

func TestWriteSourceWritesReplaceDirectives(t *testing.T) {
	dir := t.TempDir()
	_, err := WriteSource(dir, Input{
		Body:        "description: 'x'\n",
		FactoryName: "demo",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/libraries/core",
		},
	}, "1.26", "v0.10.0", map[string]string{
		"github.com/cloudboss/unobin/pkg/libraries/core": "v0.10.0",
	}, Replaces{
		"github.com/cloudboss/unobin": "/local/checkout/unobin",
	})
	require.NoError(t, err)

	modBytes, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(modBytes),
		"github.com/cloudboss/unobin => /local/checkout/unobin")
}

type writeSourceGolden struct {
	Cases []writeSourceCaseGolden `json:"cases"`
}

type writeSourceCaseGolden struct {
	Name    string              `json:"name"`
	Changes []filechange.Change `json:"changes"`
	Error   string              `json:"error"`
}

func TestWriteSourceChangesGolden(t *testing.T) {
	body := ubtest.ReadValidFixture(t, "testdata/ub/write-source", "minimal")
	result := writeSourceGolden{}
	dir := filepath.Join(t.TempDir(), "out")
	input := testMainInput(t, body, "demo")
	changes, err := writeSource(t, dir, input)
	result.Cases = append(result.Cases,
		writeSourceGoldenCase("created", dir, changes, err))
	changes, err = writeSource(t, dir, input)
	result.Cases = append(result.Cases,
		writeSourceGoldenCase("unchanged", dir, changes, err))
	input.FactoryName = "renamed"
	changes, err = writeSource(t, dir, input)
	result.Cases = append(result.Cases,
		writeSourceGoldenCase("updated", dir, changes, err))

	missingVersionDir := filepath.Join(t.TempDir(), "out")
	missingVersion := testMainInput(t, body, "missing-version")
	missingVersion.GoImports = map[string]string{"remote": "example.com/remote"}
	changes, err = WriteSource(
		missingVersionDir, missingVersion, "1.26", "v0.1.0", map[string]string{}, nil,
	)
	result.Cases = append(result.Cases, writeSourceGoldenCase(
		"failure after main", missingVersionDir, changes, err,
	))

	blockedDir := filepath.Join(t.TempDir(), "out")
	require.NoError(t, os.MkdirAll(filepath.Join(blockedDir, "go.mod"), 0o755))
	changes, err = WriteSource(
		blockedDir, testMainInput(t, body, "blocked"),
		"1.26", "v0.1.0", map[string]string{}, nil,
	)
	result.Cases = append(result.Cases, writeSourceGoldenCase(
		"go mod write failure", blockedDir, changes, err,
	))

	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/write-source.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func writeSource(
	t *testing.T,
	dir string,
	input Input,
) ([]filechange.Change, error) {
	t.Helper()
	return WriteSource(dir, input, "1.26", "v0.1.0", map[string]string{}, nil)
}

func writeSourceGoldenCase(
	name string,
	dir string,
	changes []filechange.Change,
	err error,
) writeSourceCaseGolden {
	for index := range changes {
		rel, relErr := filepath.Rel(dir, changes[index].Path)
		if relErr == nil {
			changes[index].Path = filepath.ToSlash(rel)
		}
	}
	message := ""
	if err != nil {
		message = strings.ReplaceAll(filepath.ToSlash(err.Error()), filepath.ToSlash(dir), "$OUT")
	}
	return writeSourceCaseGolden{Name: name, Changes: changes, Error: message}
}
