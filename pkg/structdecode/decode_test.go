package structdecode

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testConfig struct {
	Region     string
	RoleARN    string `ub:"role-arn"`
	Profile    *string
	Tags       map[string]string
	Subnets    []string
	AssumeRole testAssumeRole `ub:"assume-role"`
	Timeout    time.Duration
}

type testAssumeRole struct {
	ExternalID *string
}

func TestDecodeStructFields(t *testing.T) {
	profile := "dev"
	externalID := "ext"
	var got testConfig
	err := Decode(&got, map[string]any{
		"region":   "us-west-2",
		"role-arn": "arn:aws:iam::1:role/deployer",
		"profile":  profile,
		"tags": map[string]any{
			"env": "test",
		},
		"subnets": []any{"subnet-a", "subnet-b"},
		"assume-role": map[string]any{
			"external-id": externalID,
		},
		"timeout": "250ms",
	})
	require.NoError(t, err)
	require.Equal(t, "us-west-2", got.Region)
	require.Equal(t, "arn:aws:iam::1:role/deployer", got.RoleARN)
	require.Equal(t, "dev", *got.Profile)
	require.Equal(t, map[string]string{"env": "test"}, got.Tags)
	require.Equal(t, []string{"subnet-a", "subnet-b"}, got.Subnets)
	require.Equal(t, "ext", *got.AssumeRole.ExternalID)
	require.Equal(t, 250*time.Millisecond, got.Timeout)
}

func TestDecodeRejectsUnknownKey(t *testing.T) {
	var got testConfig
	err := Decode(&got, map[string]any{"region": "us-west-2", "bogus": true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "bogus")
}

func TestDecodeAcceptsMissingAndNullPointers(t *testing.T) {
	var got testConfig
	err := Decode(&got, map[string]any{"profile": nil})
	require.NoError(t, err)
	require.Nil(t, got.Profile)
	require.Nil(t, got.AssumeRole.ExternalID)
}

func TestDecodeRejectsNonStructDestination(t *testing.T) {
	var got string
	err := Decode(&got, map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "pointer to a struct")
}
