package runner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestRunnerConstructsCatalogBeforeCommands(t *testing.T) {
	var calls int
	info := Info{
		LibraryRegistrations: []runtime.LibraryRegistration{
			{LibraryPath: "example.com/shared", New: func() *runtime.Library {
				calls++
				return &runtime.Library{}
			}},
		},
		LibraryBindings: map[string]string{
			"first": "example.com/shared", "second": "example.com/shared",
		},
	}
	linked, err := linkRunnerLibraries(info)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.NotNil(t, linked.libraryCatalog)
	require.Same(t, linked.Libraries["first"], linked.Libraries["second"])
	require.Equal(t, "example.com/shared", linked.Libraries["first"].LibraryPath)
}

func TestRunnerRejectsDuplicateCatalogBeforeConstructors(t *testing.T) {
	var calls int
	registration := runtime.LibraryRegistration{
		LibraryPath: "example.com/shared", New: func() *runtime.Library {
			calls++
			return &runtime.Library{}
		},
	}
	command := newRootCmd(Info{
		FactoryName:          "invalid-catalog",
		LibraryRegistrations: []runtime.LibraryRegistration{registration, registration},
	})
	command.SetArgs([]string{"version"})
	require.ErrorContains(t, command.Execute(),
		`library path "example.com/shared" has multiple registrations`)
	require.Zero(t, calls)
}
