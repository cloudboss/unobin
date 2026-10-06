package factorycli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	localbackend "github.com/cloudboss/unobin/pkg/backends/local"
	localencrypt "github.com/cloudboss/unobin/pkg/encrypters/local"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestRegistryDefaultsAndExplicitEmpty(t *testing.T) {
	defaults, err := (*Registries)(nil).build()
	require.NoError(t, err)
	backend, err := defaults.backends["local"].New(
		&localbackend.Config{Path: t.TempDir()}, "factory", "stack", localencrypt.Noop{})
	require.NoError(t, err)
	require.Equal(t, "stack", backend.Stack())
	for _, typ := range localencrypt.Types() {
		require.Equal(t, typ.Name, defaults.encrypters[typ.Name].Name)
		require.Equal(t, typ.Description, defaults.encrypters[typ.Name].Description)
	}
	empty, err := (&Registries{}).build()
	require.NoError(t, err)
	require.Equal(t, map[string]sdkstate.BackendType{}, empty.backends)
	require.Equal(t, map[string]sdkencrypt.EncrypterType{}, empty.encrypters)
}

func TestRegistryRejectsInvalidRegistrations(t *testing.T) {
	backend := localbackend.Type()
	encrypter := localencrypt.Types()[1]
	for _, test := range []struct {
		name       string
		registries Registries
		message    string
	}{
		{name: "duplicate backend", registries: Registries{
			Backends: []sdkstate.BackendType{backend, backend}},
			message: `state: duplicate backend "local"`},
		{name: "duplicate encrypter", registries: Registries{
			Encrypters: []sdkencrypt.EncrypterType{encrypter, encrypter}},
			message: `encryption: duplicate key-source "noop"`},
		{name: "unnamed backend", registries: Registries{
			Backends: []sdkstate.BackendType{{}}}, message: "state: backend name is empty"},
		{name: "unnamed encrypter", registries: Registries{
			Encrypters: []sdkencrypt.EncrypterType{{}}}, message: "encryption: key-source name is empty"},
		{name: "missing backend constructor", registries: Registries{
			Backends: []sdkstate.BackendType{{Name: "custom"}}},
			message: `state: backend "custom" has no constructor`},
		{name: "missing encrypter constructor", registries: Registries{
			Encrypters: []sdkencrypt.EncrypterType{{Name: "custom"}}},
			message: `encryption: key-source "custom" has no constructor`},
	} {
		t.Run(test.name, func(t *testing.T) {
			registered, err := test.registries.build()
			require.EqualError(t, err, test.message)
			require.Nil(t, registered)
		})
	}
}

func TestRegistryPreservesCustomConstructors(t *testing.T) {
	want := errors.New("custom backend failure")
	var calls int
	registries := &Registries{Backends: []sdkstate.BackendType{{
		Name: "custom", New: func(_ any, factory, stack string, _ sdkencrypt.Encrypter) (
			sdkstate.Backend, error,
		) {
			require.Equal(t, "factory", factory)
			require.Equal(t, "stack", stack)
			calls++
			return nil, want
		},
	}}}
	registered, err := registries.build()
	require.NoError(t, err)
	registries.Backends[0].Name = "changed"
	_, err = registered.backends["custom"].New(nil, "factory", "stack", nil)
	require.ErrorIs(t, err, want)
	require.Equal(t, 1, calls)
	require.NotContains(t, registered.backends, "changed")
}
