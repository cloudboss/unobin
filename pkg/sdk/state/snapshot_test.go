package state

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func sampleSnapshot() *Snapshot {
	return &Snapshot{
		FormatVersion: CurrentFormatVersion,
		Factory: FactoryInfo{
			Name:            "cluster-deploy",
			Version:         "v2.0.3",
			ContentRevision: "abc123def456",
		},
		Stack:       "prod-east-alpha",
		GeneratedAt: time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		Entries: []*Entry{
			{
				Address:       "resource.main",
				Type:          EntryLeaf,
				Kind:          "resource",
				Selector:      &Selector{Alias: "aws", Export: "vpc"},
				SchemaVersion: 1,
				Inputs:        map[string]any{"cidr-block": "10.0.0.0/16"},
				Outputs:       map[string]any{"id": "vpc-abc"},
			},
			{
				Address:   "resource.web",
				Type:      EntryLibraryCall,
				Kind:      "resource",
				Selector:  &Selector{Alias: "net", Export: "cluster"},
				Inputs:    map[string]any{"name": "web", "size": float64(5)},
				Outputs:   map[string]any{"arn": "arn:..."},
				DependsOn: []string{"resource.main"},
			},
		},
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	in := sampleSnapshot()
	b, err := EncodeSnapshot(in)
	require.NoError(t, err)

	out, err := DecodeSnapshot(b)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestSnapshotEncodeStable(t *testing.T) {
	in := sampleSnapshot()
	a, err := EncodeSnapshot(in)
	require.NoError(t, err)
	b, err := EncodeSnapshot(in)
	require.NoError(t, err)
	require.Equal(t, string(a), string(b))
}

func TestSnapshotFind(t *testing.T) {
	s := sampleSnapshot()
	e := s.Find("resource.main")
	require.NotNil(t, e)
	require.Equal(t, "resource", e.Kind)

	require.Nil(t, s.Find("resource.no-such-thing"))
}

func TestSnapshotRejectsBadFormatVersion(t *testing.T) {
	b := []byte(`{"format-version": 99, "factory": {"name": "x"}, "entries": []}`)
	_, err := DecodeSnapshot(b)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported format-version")
}

func TestSnapshotRejectsLeafWithoutKind(t *testing.T) {
	s := sampleSnapshot()
	s.Entries[0].Kind = ""
	_, err := EncodeSnapshot(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing node-kind")
}

func TestSnapshotRejectsLibraryCallWithoutSelector(t *testing.T) {
	s := sampleSnapshot()
	s.Entries[1].Selector = nil
	_, err := EncodeSnapshot(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "selector missing")
}

func TestSnapshotRejectsUnknownType(t *testing.T) {
	s := sampleSnapshot()
	s.Entries[0].Type = "weird"
	_, err := EncodeSnapshot(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown entry-kind")
}

func TestSnapshotRejectsDuplicateAddresses(t *testing.T) {
	s := sampleSnapshot()
	s.Entries = append(s.Entries, &Entry{
		Address:  "resource.main",
		Type:     EntryLeaf,
		Kind:     "resource",
		Selector: &Selector{Alias: "aws", Export: "vpc"},
	})
	_, err := EncodeSnapshot(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate address")
}

func TestSnapshotRejectsMissingAddress(t *testing.T) {
	s := sampleSnapshot()
	s.Entries[0].Address = ""
	_, err := EncodeSnapshot(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing address")
}

func TestNewSnapshotInitializes(t *testing.T) {
	s := NewSnapshot(FactoryInfo{Name: "x"}, "prod")
	require.Equal(t, CurrentFormatVersion, s.FormatVersion)
	require.Equal(t, "prod", s.Stack)
	require.False(t, s.GeneratedAt.IsZero())
	require.Empty(t, s.Entries)
}

func TestSnapshotJSONShape(t *testing.T) {
	s := sampleSnapshot()
	b, err := EncodeSnapshot(s)
	require.NoError(t, err)
	out := string(b)
	require.True(t, strings.HasSuffix(out, "\n"))
	require.Contains(t, out, `"format-version": 1`)
	require.Contains(t, out, `"address": "resource.main"`)
	require.Contains(t, out, `"entry-kind": "leaf"`)
	require.Contains(t, out, `"node-kind": "resource"`)
	require.NotContains(t, out, `"type":`)
	require.NotContains(t, out, `"kind": "resource"`)
	require.NotContains(t, out, `"library-type"`)
}

func TestSnapshotConfigurationRefJSON(t *testing.T) {
	s := sampleSnapshot()
	s.Entries[0].Configuration = "aws.east"
	b, err := EncodeSnapshot(s)
	require.NoError(t, err)

	var raw struct {
		Entries []map[string]any `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(b, &raw))
	require.Equal(t, map[string]any{
		"kind": "named",
		"name": "east",
		"selector": map[string]any{
			"alias": "aws",
		},
	}, raw.Entries[0]["configuration"])

	got, err := DecodeSnapshot(b)
	require.NoError(t, err)
	require.Equal(t, "aws.east", got.Entries[0].Configuration)
}

func TestSnapshotActionEntry(t *testing.T) {
	snap := &Snapshot{
		FormatVersion: CurrentFormatVersion,
		Factory:       FactoryInfo{Name: "x", Version: "v1", ContentRevision: "abc"},
		Stack:         "prod",
		GeneratedAt:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Entries: []*Entry{
			{
				Address:     "action.smoke-test",
				Type:        EntryAction,
				Kind:        "action",
				Selector:    &Selector{Alias: "core", Export: "command"},
				TriggerHash: "sha256:deadbeef",
				Inputs:      map[string]any{"argv": []any{"true"}},
				Outputs:     map[string]any{"stdout": "", "exit-code": float64(0)},
			},
		},
	}
	b, err := EncodeSnapshot(snap)
	require.NoError(t, err)
	got, err := DecodeSnapshot(b)
	require.NoError(t, err)
	require.Equal(t, snap, got)
	require.Contains(t, string(b), `"trigger-hash": "sha256:deadbeef"`)
}

func TestSnapshotPersistsOutputs(t *testing.T) {
	snap := sampleSnapshot()
	snap.Outputs = map[string]any{"vpc-id": "vpc-abc", "size": float64(5)}

	b, err := EncodeSnapshot(snap)
	require.NoError(t, err)
	got, err := DecodeSnapshot(b)
	require.NoError(t, err)
	require.Equal(t, snap.Outputs, got.Outputs)
	require.Contains(t, string(b), `"outputs":`)
}

func TestSnapshotRejectsActionWithoutKind(t *testing.T) {
	snap := &Snapshot{
		FormatVersion: CurrentFormatVersion,
		Factory:       FactoryInfo{Name: "x"},
		Stack:         "prod",
		GeneratedAt:   time.Now().UTC(),
		Entries: []*Entry{
			{
				Address:  "action.x",
				Type:     EntryAction,
				Selector: &Selector{Alias: "core", Export: "command"},
			},
		},
	}
	_, err := EncodeSnapshot(snap)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing node-kind")
}
