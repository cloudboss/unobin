package e2etest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"
)

func TestSeedStateWritesCurrentSnapshot(t *testing.T) {
	caseDir := t.TempDir()
	body, err := os.ReadFile("testdata/state-seed.json")
	require.NoError(t, err)
	writeText(t, filepath.Join(caseDir, "seed/state.json"), string(body))
	workspace := t.TempDir()
	c := CompiledCase{Name: "seeded", Dir: caseDir, StateSeed: "seed/state.json"}

	require.NoError(t, seedState(workspace, c))

	store, err := local.NewStore(
		filepath.Join(workspace, ".unobin", "state"),
		"seeded",
		"dev",
		encrypters.Noop{},
	)
	require.NoError(t, err)
	revision, err := store.CurrentRev()
	require.NoError(t, err)
	snap, err := store.GetV2(revision)
	require.NoError(t, err)
	require.Equal(t, sdkstate.FactoryInfo{
		Name:            "seeded",
		Version:         "v0.0.0",
		ContentRevision: "old",
	}, snap.Factory)
	require.Len(t, snap.Entries, 1)
	require.Equal(t, "resource.old", snap.Entries[0].Address)
}
