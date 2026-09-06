package state

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnapshotV2Codec(t *testing.T) {
	snapshot := validSnapshotV2(t)

	encoded, err := EncodeSnapshotV2(snapshot)
	require.NoError(t, err)
	require.True(t, bytes.HasSuffix(encoded, []byte{'\n'}))

	decoded, err := DecodeSnapshotV2(encoded)
	require.NoError(t, err)
	require.Equal(t, snapshot, decoded)
}

func TestSnapshotV2CodecPreservesEveryEntryKind(t *testing.T) {
	resource := ResourceStatePayload{Target: validV2ResourceTarget(t)}
	action := validV2ActionPayload(t)
	dataSource := validV2DataSourcePayload(t)
	composite := validV2CompositePayload(t)
	tests := []struct {
		name  string
		entry StateEntryV2
	}{
		{
			name: "resource",
			entry: StateEntryV2{
				Address: "resource.api",
				Kind:    StateResource,
				Payload: StatePayload{Kind: StateResource, Resource: &resource},
			},
		},
		{
			name: "action",
			entry: StateEntryV2{
				Address: "action.notify",
				Kind:    StateAction,
				Payload: StatePayload{Kind: StateAction, Action: &action},
			},
		},
		{
			name: "data source",
			entry: StateEntryV2{
				Address: "data-source.image",
				Kind:    StateDataSource,
				Payload: StatePayload{Kind: StateDataSource, DataSource: &dataSource},
			},
		},
		{
			name: "composite",
			entry: StateEntryV2{
				Address: "resource.application",
				Kind:    StateComposite,
				Payload: StatePayload{Kind: StateComposite, Composite: &composite},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validSnapshotV2(t)
			snapshot.Entries = []StateEntryV2{tt.entry}

			encoded, err := EncodeSnapshotV2(snapshot)
			require.NoError(t, err)
			decoded, err := DecodeSnapshotV2(encoded)
			require.NoError(t, err)
			require.Equal(t, snapshot, decoded)
		})
	}
}

func TestEncodeSnapshotV2RejectsInvalidSnapshot(t *testing.T) {
	snapshot := validSnapshotV2(t)
	snapshot.Stack = ""

	encoded, err := EncodeSnapshotV2(snapshot)
	require.ErrorContains(t, err, "stack is required")
	require.Nil(t, encoded)
}

func TestDecodeSnapshotV2RejectsInvalidJSONContract(t *testing.T) {
	valid := marshalSnapshotV2(t, validSnapshotV2(t))
	tests := []struct {
		name    string
		old     string
		new     string
		message string
	}{
		{
			name:    "duplicate version member",
			old:     `"format-version":2,`,
			new:     `"format-version":2,"format-version":1,`,
			message: `$.format-version: duplicate member`,
		},
		{
			name:    "duplicate nested member",
			old:     `"factory":{"name":"deploy",`,
			new:     `"factory":{"name":"deploy","name":"again",`,
			message: `$.factory.name: duplicate member`,
		},
		{
			name:    "unknown nested member",
			old:     `"payload":{"kind":"action",`,
			new:     `"payload":{"kind":"action","unexpected":true,`,
			message: `$.entries[0].payload.unexpected: unknown member`,
		},
		{
			name:    "unknown encoded value member",
			old:     `"value":"ok"`,
			new:     `"value":"ok","unexpected":true`,
			message: `$.entries[0].payload.action.inputs.fields[0].value.unexpected`,
		},
		{
			name:    "missing required member",
			old:     `"stack":"production",`,
			new:     "",
			message: `$.stack: member is required`,
		},
		{
			name:    "missing nested required member",
			old:     `"depends-on":["resource.api"],`,
			new:     "",
			message: `$.entries[0].payload.action.depends-on: member is required`,
		},
		{
			name:    "null optional member",
			old:     `"stable-id":"server-123"`,
			new:     `"stable-id":null`,
			message: `$.entries[1].payload.resource.target.identity.stable-id: null is not allowed`,
		},
		{
			name:    "noncanonical integer",
			old:     `"schema-version":2,`,
			new:     `"schema-version":-0,`,
			message: `$.entries[1].payload.resource.target.schema-version: noncanonical integer "-0"`,
		},
		{
			name:    "invalid version type",
			old:     `"format-version":2,`,
			new:     `"format-version":"2",`,
			message: `$.format-version: invalid value`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := replaceSnapshotV2JSON(t, valid, tt.old, tt.new)
			snapshot, err := DecodeSnapshotV2(input)
			require.ErrorContains(t, err, tt.message)
			require.Equal(t, SnapshotV2{}, snapshot)
		})
	}
}

func TestDecodeSnapshotV2RejectsTrailingValue(t *testing.T) {
	input := append(marshalSnapshotV2(t, validSnapshotV2(t)), []byte(` {}`)...)

	snapshot, err := DecodeSnapshotV2(input)
	require.ErrorContains(t, err, "$: unexpected value after JSON value")
	require.Equal(t, SnapshotV2{}, snapshot)
}

func TestDecodeSnapshotV2RejectsObsoleteAlphaFormat(t *testing.T) {
	snapshot, err := DecodeSnapshotV2([]byte(`{"format-version":1}`))
	require.ErrorContains(t, err, "obsolete alpha format; create a new plan or state")
	require.Equal(t, SnapshotV2{}, snapshot)
}

func TestDecodeSnapshotV2VerifiesNestedDigest(t *testing.T) {
	valid := marshalSnapshotV2(t, validSnapshotV2(t))
	input := replaceSnapshotV2JSON(
		t,
		valid,
		`"value":"us-east-1"`,
		`"value":"us-west-2"`,
	)

	snapshot, err := DecodeSnapshotV2(input)
	require.ErrorContains(t, err, "configuration digest does not match record contents")
	require.Equal(t, SnapshotV2{}, snapshot)
}

func marshalSnapshotV2(t *testing.T, snapshot SnapshotV2) []byte {
	t.Helper()
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	return encoded
}

func replaceSnapshotV2JSON(t *testing.T, input []byte, old, new string) []byte {
	t.Helper()
	require.Contains(t, string(input), old)
	return bytes.Replace(input, []byte(old), []byte(new), 1)
}
