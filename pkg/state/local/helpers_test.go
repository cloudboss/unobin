package local

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
	encodedvalue "github.com/cloudboss/unobin/pkg/encoding/value"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

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
	inputs, err := encodedvalue.Object(map[string]encodedvalue.Value{
		"cidr-block": encodedvalue.String("10.0.0.0/16"),
	})
	require.NoError(t, err)
	outputs, err := encodedvalue.Object(map[string]encodedvalue.Value{
		"id": encodedvalue.String("vpc-abc"),
	})
	require.NoError(t, err)
	configurationValue, err := encodedvalue.Object(map[string]encodedvalue.Value{
		"region": encodedvalue.String("us-east-1"),
	})
	require.NoError(t, err)
	configuration, err := internalconfig.Build(sdkstate.ConfigurationRecord{
		Address:        "library-config.aws",
		LibraryPath:    "example.com/aws",
		SchemaVersion:  1,
		SchemaDigest:   strings.Repeat("a", 64),
		Value:          configurationValue,
		SensitivePaths: []string{},
	}, nil, nil)
	require.NoError(t, err)
	stableID := "vpc-abc"
	require.NoError(t, snapshot.SetEntry(sdkstate.StateEntryV2{
		Address: "resource.main",
		Kind:    sdkstate.StateResource,
		Payload: sdkstate.StatePayload{
			Kind: sdkstate.StateResource,
			Resource: &sdkstate.ResourceStatePayload{Target: sdkstate.ResourceTarget{
				Binding: sdkstate.CanonicalBinding{
					LibraryPath: "example.com/aws", Export: "vpc",
				},
				SchemaVersion: 1,
				Inputs:        inputs,
				Outputs:       outputs,
				Configuration: configuration,
				Identity: sdkstate.IdentityRecord{
					DefinitionDigest: strings.Repeat("b", 64),
					Version:          1,
					StableID:         &stableID,
				},
				DependsOn:            []string{},
				SensitiveInputPaths:  []string{},
				SensitiveOutputPaths: []string{},
			}},
		},
	}))
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
