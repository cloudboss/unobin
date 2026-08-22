package local

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

func sampleSnapshot() *sdkstate.Snapshot {
	return &sdkstate.Snapshot{
		FormatVersion: sdkstate.CurrentFormatVersion,
		Factory: sdkstate.FactoryInfo{
			Name:            "cluster-deploy",
			Version:         "v2.0.3",
			ContentRevision: "abc123def456",
		},
		Stack:       "prod-east-alpha",
		GeneratedAt: time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		Entries: []*sdkstate.Entry{
			{
				Address:       "resource.main",
				Type:          sdkstate.EntryLeaf,
				Category:      "resource",
				Binding:       &sdkstate.Binding{Alias: "aws", Export: "vpc"},
				SchemaVersion: 1,
				Inputs:        map[string]any{"cidr-block": "10.0.0.0/16"},
				Outputs:       map[string]any{"id": "vpc-abc"},
			},
		},
	}
}

func sampleSnapshotV2(t *testing.T) *sdkstate.SnapshotV2 {
	t.Helper()
	snapshot, err := sdkstate.NewSnapshotV2(
		sdkstate.FactoryInfo{
			Name:            "cluster-deploy",
			Version:         "v2.0.3",
			ContentRevision: "abc123def456",
		},
		"prod-east-alpha",
	)
	require.NoError(t, err)
	snapshot.GeneratedAt = time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	require.NoError(t, snapshot.Validate())
	return snapshot
}

func setKey(t *testing.T, envVar string) {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	t.Setenv(envVar, base64.StdEncoding.EncodeToString(key))
}
