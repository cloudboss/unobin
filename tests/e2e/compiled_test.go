package e2e

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/libraryapi"
)

func TestCompiledCases(t *testing.T) {
	e2etest.RunCompiledCases(t, "testdata/compiled-cases",
		e2etest.WithUnobinDir(filepath.Join("..", "..")),
		e2etest.WithGoModule("example.com/unobin/e2elib", "testdata/modules/e2elib"),
	)
}

func TestLifecycleLibraryAPIBaseline(t *testing.T) {
	dir := filepath.Join("testdata", "modules", "e2elib")
	declaration, err := golibrary.ReadCompatibility(dir, dir)
	require.NoError(t, err)
	assert.Equal(t, "1.0", declaration.RequiredAPI)
	for _, api := range []string{"1.0", "1.1"} {
		t.Run(api, func(t *testing.T) {
			descriptor := libraryapi.Descriptor{
				FormatVersion: 1, ImplementedAPIs: []string{api}, GeneratorAPI: api,
			}
			context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
				Descriptor: &descriptor, UnobinVersion: "v0.1.0",
			})
			require.NoError(t, err)
			require.NoError(t, context.CheckDirectory(dir, true))
			manifest := context.Manifest()
			require.Len(t, manifest, 1)
			assert.Equal(t, "example.com/unobin/e2elib", manifest[0].Package)
			assert.Equal(t, "1.0", manifest[0].Declaration.RequiredAPI)
		})
	}
}

func TestConditionalCompiledPlan(t *testing.T) {
	e2etest.RunCompiledCases(t, "testdata/ub/valid/conditional-plan",
		e2etest.WithUnobinDir(filepath.Join("..", "..")),
		e2etest.WithGoModule("example.com/unobin/e2elib", "testdata/modules/e2elib"),
	)
}

func TestSensitiveDestroyPlan(t *testing.T) {
	e2etest.RunCompiledCases(t, "testdata/ub/valid/sensitive-destroy",
		e2etest.WithUnobinDir(filepath.Join("..", "..")),
		e2etest.WithGoModule("example.com/unobin/e2elib", "testdata/modules/e2elib"),
	)
}
