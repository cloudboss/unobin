package codegen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteSourceLaysOutFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stack-out")

	err := WriteSource(dir, Input{
		Body:      "description: 'x'\n",
		StackName: "demo",
		Version:   "v0",
		Commit:    "c",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/modules/core",
		},
	}, "1.26", "v0.10.0", map[string]string{
		"github.com/cloudboss/unobin/pkg/modules/core": "v0.10.0",
	}, nil)
	require.NoError(t, err)

	mainBytes, err := os.ReadFile(filepath.Join(dir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(mainBytes), `stackName       = "demo"`)

	modBytes, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	mod := string(modBytes)
	require.Contains(t, mod, "module demo")
	require.Contains(t, mod, "go 1.26")
	require.Contains(t, mod, "github.com/cloudboss/unobin v0.10.0")
}

func TestWriteSourceSkipsInternalUnobinImports(t *testing.T) {
	dir := t.TempDir()
	err := WriteSource(dir, Input{
		Body:      "description: 'x'\n",
		StackName: "demo",
		Version:   "v0",
		Commit:    "c",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/modules/core",
		},
	}, "1.26", "v0.10.0", map[string]string{
		"github.com/cloudboss/unobin/pkg/modules/core": "v0.10.0",
	}, nil)
	require.NoError(t, err)

	modBytes, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	mod := string(modBytes)
	require.NotContains(t, mod, "\tgithub.com/cloudboss/unobin/pkg/modules/core",
		"internal unobin packages should not get their own require line")
}

func TestWriteSourceIncludesExternalImports(t *testing.T) {
	dir := t.TempDir()
	err := WriteSource(dir, Input{
		Body:      "description: 'x'\n",
		StackName: "demo",
		Version:   "v0",
		Commit:    "c",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/modules/core",
			"aws":  "github.com/cloudboss/unobin-modules/aws",
		},
	}, "1.26", "v0.10.0", map[string]string{
		"github.com/cloudboss/unobin/pkg/modules/core": "v0.10.0",
		"github.com/cloudboss/unobin-modules/aws":      "v0.5.0",
	}, nil)
	require.NoError(t, err)

	modBytes, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	mod := string(modBytes)
	require.Contains(t, mod, "github.com/cloudboss/unobin-modules/aws v0.5.0")
}

func TestWriteSourceRejectsMissingVersion(t *testing.T) {
	dir := t.TempDir()
	err := WriteSource(dir, Input{
		Body:      "description: 'x'\n",
		StackName: "demo",
		Version:   "v0",
		Commit:    "c",
		GoImports: map[string]string{
			"aws": "github.com/cloudboss/unobin-modules/aws",
		},
	}, "1.26", "v0.10.0", map[string]string{}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no version")
}

func TestWriteSourceRequiresGoVersion(t *testing.T) {
	dir := t.TempDir()
	err := WriteSource(dir, Input{
		Body:      "description: 'x'",
		StackName: "demo",
		Version:   "v0",
		Commit:    "c",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/modules/core",
		},
	}, "", "v0.10.0", map[string]string{
		"github.com/cloudboss/unobin/pkg/modules/core": "v0.10.0",
	}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "goVersion")
}

func TestWriteSourceWritesReplaceDirectives(t *testing.T) {
	dir := t.TempDir()
	err := WriteSource(dir, Input{
		Body:      "description: 'x'\n",
		StackName: "demo",
		Version:   "v0",
		Commit:    "c",
		GoImports: map[string]string{
			"core": "github.com/cloudboss/unobin/pkg/modules/core",
		},
	}, "1.26", "v0.10.0", map[string]string{
		"github.com/cloudboss/unobin/pkg/modules/core": "v0.10.0",
	}, Replaces{
		"github.com/cloudboss/unobin": "/local/checkout/unobin",
	})
	require.NoError(t, err)

	modBytes, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(modBytes),
		"github.com/cloudboss/unobin => /local/checkout/unobin")
}
