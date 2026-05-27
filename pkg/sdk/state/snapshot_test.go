package state

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func sampleSnapshot() *Snapshot {
	return &Snapshot{
		FormatVersion: CurrentFormatVersion,
		Stack: StackInfo{
			Name:            "cluster-deploy",
			Version:         "v2.0.3",
			ContentRevision: "abc123def456",
		},
		DeploymentID: "prod-east-alpha",
		GeneratedAt:  time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		Entries: []*Entry{
			{
				Address:       "resource.aws.vpc.main",
				Type:          EntryLeaf,
				Kind:          "vpc",
				SchemaVersion: 1,
				Inputs:        map[string]any{"cidr-block": "10.0.0.0/16"},
				Outputs:       map[string]any{"id": "vpc-abc"},
			},
			{
				Address:    "resource.net.cluster.web",
				Type:       EntryModuleCall,
				Module:     "net",
				ModuleType: "cluster",
				Inputs:     map[string]any{"name": "web", "size": float64(5)},
				Outputs:    map[string]any{"arn": "arn:..."},
				DependsOn:  []string{"resource.aws.vpc.main"},
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
	e := s.Find("resource.aws.vpc.main")
	require.NotNil(t, e)
	require.Equal(t, "vpc", e.Kind)

	require.Nil(t, s.Find("resource.no.such.thing"))
}

func TestSnapshotRejectsBadFormatVersion(t *testing.T) {
	b := []byte(`{"format-version": 99, "stack": {"name": "x"}, "entries": []}`)
	_, err := DecodeSnapshot(b)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported format-version")
}

func TestSnapshotRejectsLeafWithoutKind(t *testing.T) {
	s := sampleSnapshot()
	s.Entries[0].Kind = ""
	_, err := EncodeSnapshot(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing kind")
}

func TestSnapshotRejectsModuleCallWithoutModule(t *testing.T) {
	s := sampleSnapshot()
	s.Entries[1].Module = ""
	_, err := EncodeSnapshot(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing module")
}

func TestSnapshotRejectsUnknownType(t *testing.T) {
	s := sampleSnapshot()
	s.Entries[0].Type = "weird"
	_, err := EncodeSnapshot(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown type")
}

func TestSnapshotRejectsDuplicateAddresses(t *testing.T) {
	s := sampleSnapshot()
	s.Entries = append(s.Entries, &Entry{
		Address: "resource.aws.vpc.main",
		Type:    EntryLeaf,
		Kind:    "vpc",
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
	s := NewSnapshot(StackInfo{Name: "x"}, "prod")
	require.Equal(t, CurrentFormatVersion, s.FormatVersion)
	require.Equal(t, "prod", s.DeploymentID)
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
	require.Contains(t, out, `"address": "resource.aws.vpc.main"`)
	require.Contains(t, out, `"module-type": "cluster"`)
}

func TestSnapshotActionEntry(t *testing.T) {
	snap := &Snapshot{
		FormatVersion: CurrentFormatVersion,
		Stack:         StackInfo{Name: "x", Version: "v1", ContentRevision: "abc"},
		DeploymentID:  "prod",
		GeneratedAt:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Entries: []*Entry{
			{
				Address:     "action.core.command.smoke-test",
				Type:        EntryAction,
				Kind:        "command",
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
		Stack:         StackInfo{Name: "x"},
		DeploymentID:  "prod",
		GeneratedAt:   time.Now().UTC(),
		Entries: []*Entry{
			{Address: "action.core.command.x", Type: EntryAction},
		},
	}
	_, err := EncodeSnapshot(snap)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing kind")
}
