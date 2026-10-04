package resolve

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/diagnostic"
)

func TestModulePathError(t *testing.T) {
	failure := &ModulePathError{
		Dependency: "example.com/libraries//nested", ModulePath: "example.com/libraries/nested",
		Version: "v2.0.0", Message: "module path must end in /v2",
	}
	assert.Equal(t, failure.Message, failure.Error())
	ds := diagnostic.FromError(failure, diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, "unobin.library-api.module-source", ds[0].Code)
	assert.Equal(t, &diagnostic.LibraryCompatibilityDetails{
		Dependency: failure.Dependency, ModulePath: failure.ModulePath, Version: failure.Version,
	}, ds[0].LibraryCompatibility)
}

func TestValidateGoModulePathReturnsTypedFailure(t *testing.T) {
	err := ValidateGoModulePath(&RemoteImport{
		URL: "example.com/libraries", Subdir: "nested", Version: "nested/v2.0.0-rc.1",
	}, "example.com/libraries/nested")
	var failure *ModulePathError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, "example.com/libraries//nested", failure.Dependency)
	assert.Equal(t, "v2.0.0-rc.1", failure.Version)
	assert.Equal(t, "example.com/libraries/nested", failure.ModulePath)
}
