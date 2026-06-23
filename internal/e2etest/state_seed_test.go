package e2etest

import (
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"
)

func TestSeedStateWritesCurrentSnapshot(t *testing.T) {
	caseDir := t.TempDir()
	writeText(t, filepath.Join(caseDir, "seed/state.json"), `{
  "format-version": 1,
  "factory": {
    "name": "seeded",
    "version": "v0.0.0",
    "content-revision": "old"
  },
  "stack": "dev",
  "entries": [
    {
      "address": "resource.old",
      "entry-kind": "leaf",
      "category": "resource",
      "binding": {
        "alias": "e2e",
        "library-path": "example.com/unobin/e2elib",
        "kind": "file"
      },
      "schema-version": 1,
      "inputs": {
        "content": "old",
        "create-parents": true,
        "mode": 420,
        "path": "files/old.txt"
      },
      "outputs": {
        "content": "old",
        "exists": true,
        "path": "files/old.txt",
        "sha256": "cba06b5736faf67e54b07b561eae94395e774c517a7d910a54369e1263ccfbd4",
        "size": 3
      }
    }
  ]
}
`)
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
	snap, err := store.Current()
	require.NoError(t, err)
	require.Equal(t, sdkstate.FactoryInfo{
		Name:            "seeded",
		Version:         "v0.0.0",
		ContentRevision: "old",
	}, snap.Factory)
	require.Len(t, snap.Entries, 1)
	require.Equal(t, "resource.old", snap.Entries[0].Address)
}
