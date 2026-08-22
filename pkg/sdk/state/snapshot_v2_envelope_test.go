package state

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSealOpenSnapshotV2PreservesSnapshot(t *testing.T) {
	snapshot := validSnapshotV2(t)

	sealed, err := SealSnapshotV2(snapshot, reversingEncrypter{})
	require.NoError(t, err)

	var envelope Envelope
	require.NoError(t, json.Unmarshal(sealed, &envelope))
	assert.Equal(t, PayloadTypeState, envelope.PayloadType)
	require.NotNil(t, envelope.Encrypter)
	assert.Equal(t, "reversing", envelope.Encrypter.Name)
	assert.Equal(t, map[string]any{"direction": "backward"}, envelope.Encrypter.Body)

	opened, err := OpenSnapshotV2(sealed, reversingEncrypter{})
	require.NoError(t, err)
	assert.Equal(t, snapshot, opened)
}

func TestSealSnapshotV2RejectsInvalidSnapshot(t *testing.T) {
	snapshot := validSnapshotV2(t)
	snapshot.Stack = ""

	sealed, err := SealSnapshotV2(snapshot, reversingEncrypter{})
	require.ErrorContains(t, err, "stack is required")
	assert.Nil(t, sealed)
}

func TestOpenSnapshotV2RejectsObsoleteSnapshot(t *testing.T) {
	sealed, err := Seal(
		[]byte(`{"format-version":1}`),
		PayloadTypeState,
		reversingEncrypter{},
	)
	require.NoError(t, err)

	snapshot, err := OpenSnapshotV2(sealed, reversingEncrypter{})
	require.ErrorContains(t, err, "obsolete alpha format")
	assert.Equal(t, SnapshotV2{}, snapshot)
}

func TestOpenSnapshotV2UsesConfiguredEncrypter(t *testing.T) {
	snapshot := validSnapshotV2(t)
	sealed, err := SealSnapshotV2(snapshot, reversingEncrypter{})
	require.NoError(t, err)

	var envelope Envelope
	require.NoError(t, json.Unmarshal(sealed, &envelope))
	envelope.Encrypter = &Ref{
		Name: "untrusted-file-reference",
		Body: map[string]any{"key-id": "different-key"},
	}
	sealed, err = json.Marshal(envelope)
	require.NoError(t, err)

	opened, err := OpenSnapshotV2(sealed, reversingEncrypter{})
	require.NoError(t, err)
	assert.Equal(t, snapshot, opened)
}

func TestOpenSnapshotV2RejectsPlanEnvelope(t *testing.T) {
	body, err := encodeSnapshotV2(validSnapshotV2(t))
	require.NoError(t, err)
	sealed, err := Seal(body, PayloadTypePlan, reversingEncrypter{})
	require.NoError(t, err)

	snapshot, err := OpenSnapshotV2(sealed, reversingEncrypter{})
	require.ErrorContains(t, err, "payload-type plan, expected state")
	assert.Equal(t, SnapshotV2{}, snapshot)
}
