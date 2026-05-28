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
	err := WriteSource(dir, Input{
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
	err := WriteSource(dir, Input{
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

func TestWriteSourceRejectsMissingVersion(t *testing.T) {
	dir := t.TempDir()
	err := WriteSource(dir, Input{
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
	err := WriteSource(dir, Input{
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
	err := WriteSource(dir, Input{
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
