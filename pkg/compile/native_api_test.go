package compile

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sourcecheck"
)

func TestNativeConstraintAPICompatibilityBeforeExtraction(t *testing.T) {
	old := libraryapi.Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.0"}, GeneratorAPI: "1.0",
	}
	for _, tc := range []struct {
		name       string
		descriptor libraryapi.Descriptor
		accepted   bool
	}{
		{name: "API 1.0", descriptor: old},
		{name: "current", descriptor: libraryapi.Current(), accepted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compatibility, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
				Descriptor: &tc.descriptor,
			})
			require.NoError(t, err)
			reads := 0
			cache := sourcecheck.NewSchemaCacheWithReadersAndCompatibility(
				compatibility,
				func(dir string) (*ubruntime.LibrarySchema, []string, error) {
					reads++
					return goschema.Read(dir)
				}, nil,
			)
			schema, warnings, err := cache.Read(filepath.Join("testdata", "go", "native-api-gate"))
			if !tc.accepted {
				var newer *libraryapi.NewerMinorError
				require.ErrorAs(t, err, &newer)
				require.Equal(t, libraryapi.Version{Major: 1, Minor: 1}, newer.Required)
				require.Equal(t, libraryapi.Version{Major: 1}, newer.Implemented)
				require.Nil(t, schema)
				require.Empty(t, warnings)
				require.Zero(t, reads)
				require.Empty(t, compatibility.Manifest())
				return
			}
			require.NoError(t, err)
			require.Empty(t, warnings)
			require.Equal(t, 1, reads)
			require.Contains(t, schema.Actions, "echo")
			require.Len(t, schema.Actions["echo"].Constraints, 2)
			require.Equal(t, "(@core.is-match(input.name, '^[a-z]+$'))",
				schema.Actions["echo"].Constraints[0].Require)
			require.Equal(t, "(@core.length(input.name) <= 8)",
				schema.Actions["echo"].Constraints[1].Require)
			require.Equal(t, "1.1", compatibility.Manifest()[0].Declaration.RequiredAPI)
		})
	}
}
