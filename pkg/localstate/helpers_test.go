package localstate

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
		Stack: sdkstate.StackInfo{
			Name:    "cluster-deploy",
			Version: "v2.0.3",
			Commit:  "abc123def456",
		},
		DeploymentID: "prod-east-alpha",
		GeneratedAt:  time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		Entries: []*sdkstate.Entry{
			{
				Address:       "resource.aws.vpc.main",
				Type:          sdkstate.EntryLeaf,
				Kind:          "vpc",
				SchemaVersion: 1,
				Inputs:        map[string]any{"cidr-block": "10.0.0.0/16"},
				Outputs:       map[string]any{"id": "vpc-abc"},
			},
		},
	}
}

func setKey(t *testing.T, envVar string) {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	t.Setenv(envVar, base64.StdEncoding.EncodeToString(key))
}
