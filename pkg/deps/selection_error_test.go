package deps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/diagnostic"
)

func TestSourceSelectionError(t *testing.T) {
	failure := &SourceSelectionError{
		Dependency: "example.com/libraries//nested", Package: "example.com/libraries//nested/config",
		Version: "v1.0.0", Commit: "selected-commit", Message: "selected package is unavailable",
	}
	assert.Equal(t, failure.Message, failure.Error())
	ds := diagnostic.FromError(diagnostic.Context("read source", failure), diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, "unobin.library-api.module-source", ds[0].Code)
	assert.Equal(t, "read source: selected package is unavailable", ds[0].Message)
	assert.Equal(t, &diagnostic.LibraryCompatibilityDetails{
		Dependency: failure.Dependency, Package: failure.Package,
		Version: failure.Version, Commit: failure.Commit,
	}, ds[0].LibraryCompatibility)
}
