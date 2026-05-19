package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/localstate"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

// memBackendConfig is the body the synthetic `mem.store` backend
// accepts. Tag is unused by the backend itself; it proves the resolver
// decodes the body against the registered configuration schema.
type memBackendConfig struct {
	Path cfg.String
	Tag  cfg.String
}

func newMemBackend(
	c any, stack, deploymentID string, enc sdkencrypt.Encrypter,
) (sdkstate.Backend, error) {
	bc, ok := c.(*memBackendConfig)
	if !ok {
		return nil, fmt.Errorf("mem.store: missing or wrong configuration (got %T)", c)
	}
	return localstate.NewLocalStore(bc.Path.Value, stack, deploymentID, enc)
}

func memProviderModule() *runtime.Module {
	return &runtime.Module{
		Name: "mem",
		StateBackends: map[string]sdkstate.BackendType{
			"store": {
				Name: "store",
				Configuration: &cfg.ConfigurationType{
					New: func() any { return &memBackendConfig{} },
				},
				New: newMemBackend,
			},
		},
	}
}

func TestApplyThroughProviderBackend(t *testing.T) {
	src := `
actions: {
  core: { echo: { hi: { echo: 'via-provider' } } }
}
outputs: {
  said: action.core.echo.hi.echo
}
`
	info := testInfo(t, src)
	info.Modules["mem"] = memProviderModule()

	stateRoot := filepath.Join(t.TempDir(), "state")
	cfgPath := filepath.Join(t.TempDir(), "prod.ub")
	cfgSrc := fmt.Sprintf(
		"state: { @backend: mem.store, path: '%s', tag: 'demo' }\n", stateRoot)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgSrc), 0o644))

	out := applyVia(t, info, cfgPath)
	require.Contains(t, out, "said: 'via-provider'")

	snapshotsDir := filepath.Join(stateRoot, info.StackName, "prod", "snapshots")
	entries, err := os.ReadDir(snapshotsDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries,
		"expected the provider backend to have written at least one snapshot to %s",
		snapshotsDir)

	listOut, err := runRoot(t, info, "state", "list", "-c", cfgPath)
	require.NoError(t, err)
	require.Contains(t, listOut, "* ")

	showOut, err := runRoot(t, info, "state", "show", "-c", cfgPath)
	require.NoError(t, err)
	require.Contains(t, showOut, `said: 'via-provider'`)
}