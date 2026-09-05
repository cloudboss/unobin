package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLibraryCatalogSharesRegistrationsAcrossCompositeAliases(t *testing.T) {
	var calls int
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/shared", New: func() *Library {
			calls++
			return &Library{Resources: map[string]ResourceRegistration{
				"plain": MakeResource[plainResource, *plainResourceOutput, any](
					plainResourceDefinition(),
				),
			}}
		}},
		{LibraryPath: "example.com/composite", New: func() *Library {
			return &Library{ResourceComposites: map[string]*CompositeType{
				"wrapper": {LibraryBindings: map[string]string{
					"nested": "example.com/shared",
				}},
			}}
		}},
	})
	require.NoError(t, err)
	libraries, err := catalog.Libraries(map[string]string{
		"first":   "example.com/shared",
		"second":  "example.com/shared",
		"wrapper": "example.com/composite",
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Same(t, libraries["first"], libraries["second"])
	require.Same(t, libraries["first"],
		libraries["wrapper"].ResourceComposites["wrapper"].Libraries["nested"])
	require.Equal(t, "example.com/shared", libraries["first"].LibraryPath)
	registration, err := catalog.resources.resource(Binding{
		LibraryPath: "example.com/shared", Export: "plain",
	})
	require.NoError(t, err)
	require.Same(t, libraries["first"].Resources["plain"].resourceDefinition(),
		registration.resourceDefinition())

	renamed, err := catalog.Libraries(map[string]string{"renamed": "example.com/shared"})
	require.NoError(t, err)
	require.Same(t, libraries["first"], renamed["renamed"])
}

func TestLibraryCatalogRejectsInvalidDeclarationsBeforeConstruction(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		new  func() *Library
		want string
	}{
		{"duplicate", "example.com/shared", func() *Library { return &Library{} },
			`library path "example.com/shared" has multiple registrations`},
		{"missing path", "", func() *Library { return &Library{} }, "library path is required"},
		{"missing constructor", "example.com/other", nil, "constructor is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			catalog, err := NewLibraryCatalog([]LibraryRegistration{
				{LibraryPath: "example.com/shared", New: func() *Library {
					calls++
					return &Library{}
				}},
				{LibraryPath: test.path, New: test.new},
			})
			require.ErrorContains(t, err, test.want)
			require.Nil(t, catalog)
			require.Zero(t, calls)
		})
	}
}

func TestLibraryCatalogRejectsUnavailableCompositeImport(t *testing.T) {
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/composite", New: func() *Library {
			return &Library{ActionComposites: map[string]*CompositeType{
				"wrapper": {LibraryBindings: map[string]string{"missing": "example.com/missing"}},
			}}
		}},
	})
	require.ErrorContains(t, err, `library "example.com/missing" is unavailable`)
	require.Nil(t, catalog)
}

func TestLibraryCatalogRejectsUnavailableRootImport(t *testing.T) {
	catalog, err := NewLibraryCatalog(nil)
	require.NoError(t, err)
	libraries, err := catalog.Libraries(map[string]string{"missing": "example.com/missing"})
	require.ErrorContains(t, err, `library "example.com/missing" is unavailable`)
	require.Nil(t, libraries)
}

func TestLibraryCatalogRejectsNilLibrary(t *testing.T) {
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/nil", New: func() *Library { return nil }},
	})
	require.ErrorContains(t, err, `library "example.com/nil" is nil`)
	require.Nil(t, catalog)
}

func TestLibraryCatalogRejectsCompositeRegistrationOutsideCatalog(t *testing.T) {
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/shared", New: func() *Library { return &Library{} }},
		{LibraryPath: "example.com/composite", New: func() *Library {
			return &Library{DataComposites: map[string]*CompositeType{
				"wrapper": {Libraries: map[string]*Library{
					"shared": {LibraryPath: "example.com/shared"},
				}},
			}}
		}},
	})
	require.ErrorContains(t, err, `import "shared" is not in the catalog`)
	require.Nil(t, catalog)
}

func TestLibraryCatalogRejectsConflictingConstructorPath(t *testing.T) {
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/shared", New: func() *Library {
			return &Library{LibraryPath: "example.com/other"}
		}},
	})
	require.ErrorContains(t, err,
		`library "example.com/shared" has conflicting path "example.com/other"`)
	require.Nil(t, catalog)
}
