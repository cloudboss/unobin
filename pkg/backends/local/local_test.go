package local

import (
	"testing"

	"github.com/stretchr/testify/require"

	localencrypt "github.com/cloudboss/unobin/pkg/encrypters/local"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestTypeCreatesLocalState(t *testing.T) {
	registered := Type()
	require.Equal(t, "local", registered.Name)
	config, err := cfg.Decode(registered.Configuration, map[string]any{"path": t.TempDir()})
	require.NoError(t, err)
	backend, err := registered.New(config, "factory", "stack", localencrypt.Noop{})
	require.NoError(t, err)
	require.Equal(t, "stack", backend.Stack())
	_, err = backend.Current()
	require.ErrorIs(t, err, state.ErrNoCurrent)
}

func TestTypeRejectsMissingPath(t *testing.T) {
	registered := Type()
	_, err := registered.New(&Config{}, "factory", "stack", nil)
	require.EqualError(t, err, "local backend: path is required")
	_, err = registered.New(nil, "factory", "stack", nil)
	require.EqualError(t, err, "local backend: missing or wrong configuration (got <nil>)")
}
