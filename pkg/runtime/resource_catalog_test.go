package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type resourceCatalogRegistration struct {
	ResourceRegistration
}

func newResourceCatalogRegistration() *resourceCatalogRegistration {
	return &resourceCatalogRegistration{
		ResourceRegistration: MakeResource[plainResource, *plainResourceOutput, any](
			plainResourceDefinition(),
		),
	}
}

func TestResourceCatalogResolvesCanonicalBindingAcrossAliases(t *testing.T) {
	registration := newResourceCatalogRegistration()
	library := &Library{
		LibraryPath: "example.com/shared",
		Resources: map[string]ResourceRegistration{
			"bucket": registration,
		},
	}

	catalog, err := newResourceCatalog(
		map[string]*Library{"first": library},
		map[string]*Library{"second": library},
	)
	require.NoError(t, err)

	got, err := catalog.resource(Binding{
		LibraryPath: "example.com/shared",
		Export:      "bucket",
	})
	require.NoError(t, err)
	require.Same(t, registration, got)

	library.Resources["bucket"] = newResourceCatalogRegistration()
	got, err = catalog.resource(Binding{
		LibraryPath: "example.com/shared",
		Export:      "bucket",
	})
	require.NoError(t, err)
	require.Same(t, registration, got)
}

func TestResourceCatalogRejectsDuplicateLibraryPaths(t *testing.T) {
	_, err := newResourceCatalog(map[string]*Library{
		"first":  {LibraryPath: "example.com/shared"},
		"second": {LibraryPath: "example.com/shared"},
	})
	require.ErrorContains(
		t,
		err,
		`library path "example.com/shared" has multiple registrations`,
	)
}

func TestResourceCatalogRejectsInvalidRegistrations(t *testing.T) {
	tests := []struct {
		name      string
		libraries map[string]*Library
		message   string
	}{
		{
			name:      "nil library",
			libraries: map[string]*Library{"cloud": nil},
			message:   `library "cloud" is nil`,
		},
		{
			name:      "missing canonical path",
			libraries: map[string]*Library{"cloud": {}},
			message:   `library "cloud": library path is required`,
		},
		{
			name: "invalid resource export",
			libraries: map[string]*Library{
				"cloud": {
					LibraryPath: "example.com/cloud",
					Resources: map[string]ResourceRegistration{
						"bad export": newResourceCatalogRegistration(),
					},
				},
			},
			message: `library "example.com/cloud" resource export is invalid: "bad export"`,
		},
		{
			name: "nil resource registration",
			libraries: map[string]*Library{
				"cloud": {
					LibraryPath: "example.com/cloud",
					Resources: map[string]ResourceRegistration{
						"bucket": nil,
					},
				},
			},
			message: `library "example.com/cloud" resource "bucket" is nil`,
		},
		{
			name: "typed nil resource registration",
			libraries: map[string]*Library{
				"cloud": {
					LibraryPath: "example.com/cloud",
					Resources: map[string]ResourceRegistration{
						"bucket": (*resourceCatalogRegistration)(nil),
					},
				},
			},
			message: `library "example.com/cloud" resource "bucket" is nil`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			catalog, err := newResourceCatalog(test.libraries)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, catalog)
		})
	}
}

func TestResourceCatalogRejectsUnavailableBindings(t *testing.T) {
	catalog, err := newResourceCatalog(map[string]*Library{
		"cloud": {
			LibraryPath: "example.com/cloud",
			Resources: map[string]ResourceRegistration{
				"bucket": newResourceCatalogRegistration(),
			},
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		binding Binding
		message string
	}{
		{
			name:    "invalid binding",
			binding: Binding{LibraryPath: "example.com/cloud"},
			message: "resource binding: export is required",
		},
		{
			name: "missing library",
			binding: Binding{
				LibraryPath: "example.com/removed",
				Export:      "bucket",
			},
			message: `resource library "example.com/removed" is unavailable`,
		},
		{
			name: "missing export",
			binding: Binding{
				LibraryPath: "example.com/cloud",
				Export:      "archive",
			},
			message: `resource library "example.com/cloud" has no export "archive"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registration, err := catalog.resource(test.binding)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, registration)
		})
	}
}
