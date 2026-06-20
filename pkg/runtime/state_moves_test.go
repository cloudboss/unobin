package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustEntryRef(t *testing.T, s string) EntryRef {
	t.Helper()
	ref, err := ParseEntryRef(s)
	require.NoError(t, err)
	return ref
}

func moveSpec(t *testing.T, from, to string) EntryMoveSpec {
	t.Helper()
	return EntryMoveSpec{From: mustEntryRef(t, from), To: mustEntryRef(t, to)}
}

func moveSnapshot(entries ...*state.Entry) *state.Snapshot {
	snap := state.NewSnapshot(
		state.FactoryInfo{Name: "test", Version: "v0", ContentRevision: "c0"},
		"default",
	)
	snap.Entries = entries
	return snap
}

func moveEntry(t *testing.T, ref string, typ state.EntryType, kind string) *state.Entry {
	t.Helper()
	r := mustEntryRef(t, ref)
	return &state.Entry{
		Address:       r.Address,
		Type:          typ,
		Kind:          kind,
		Selector:      &state.Selector{Alias: r.Selector.Alias, Export: r.Selector.Export},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": r.Address},
		Outputs:       map[string]any{"id": r.String()},
	}
}

func moveDAG(nodes ...*Node) *DAG {
	dag := &DAG{Nodes: map[string]*Node{}, Edges: map[string][]string{}}
	for _, n := range nodes {
		dag.Nodes[n.Address] = n
	}
	return dag
}

func moveNode(ref string, kind NodeKind) *Node {
	r, err := ParseEntryRef(ref)
	if err != nil {
		panic(err)
	}
	return &Node{
		Address: r.Address,
		Kind:    kind,
		Alias:   r.Selector.Alias,
		Type:    r.Selector.Export,
	}
}

func moveCompositeNode(ref string, kind NodeKind) *Node {
	n := moveNode(ref, kind)
	n.CompositeSyntaxBody = &syntax.FactoryBody{}
	return n
}

func stateMovesLibs() map[string]*Library {
	reg := MakeResourceWith[countingResource, any, any](func() *countingResource {
		return &countingResource{counters: &resourceCounters{}}
	})
	return map[string]*Library{
		"aws": {
			Name: "aws",
			Resources: map[string]ResourceRegistration{
				"instance":       reg,
				"security-group": reg,
				"subnet":         reg,
			},
		},
		"core": {
			Name:      "core",
			Resources: map[string]ResourceRegistration{"thing": reg, "other": reg},
		},
	}
}

func entryRefStrings(t *testing.T, snap *state.Snapshot) []string {
	t.Helper()
	out := make([]string, 0, len(snap.Entries))
	for _, ent := range snap.Entries {
		ref, ok := EntryRefFromEntry(ent)
		require.True(t, ok)
		out = append(out, ref.String())
	}
	return out
}

func TestApplyEntryMovesStrictResourceMoves(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
	}{
		{
			name: "address changes",
			from: "core.thing@resource.old",
			to:   "core.thing@resource.new",
		},
		{
			name: "selector changes",
			from: "core.other@resource.same",
			to:   "core.thing@resource.same",
		},
		{
			name: "selector and address change",
			from: "core.other@resource.before",
			to:   "core.thing@resource.after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 2 {
				snap := moveSnapshot(moveEntry(t, tt.from, state.EntryLeaf, "resource"))
				dag := moveDAG(moveNode(tt.to, NodeResource))
				got, results, err := ApplyEntryMoves(
					snap, dag, stateMovesLibs(), []EntryMoveSpec{moveSpec(t, tt.from, tt.to)},
					EntryMoveStrict,
				)

				require.NoError(t, err)
				assert.Equal(t, []string{tt.to}, entryRefStrings(t, got))
				require.Len(t, results, 1)
				assert.Equal(t, tt.from, results[0].From.String())
				assert.Equal(t, tt.to, results[0].To.String())
				assert.Equal(t, []string{tt.from}, entryRefStrings(t, snap))
			}
		})
	}
}

func TestApplyEntryMovesStrictErrors(t *testing.T) {
	tests := []struct {
		name    string
		snap    *state.Snapshot
		dag     *DAG
		specs   []EntryMoveSpec
		wantErr string
	}{
		{
			name: "absent source",
			snap: moveSnapshot(),
			dag:  moveDAG(moveNode("core.thing@resource.new", NodeResource)),
			specs: []EntryMoveSpec{
				moveSpec(t, "core.thing@resource.old", "core.thing@resource.new"),
			},
			wantErr: "no entry at core.thing@resource.old",
		},
		{
			name: "destination exists",
			snap: moveSnapshot(
				moveEntry(t, "core.thing@resource.old", state.EntryLeaf, "resource"),
				moveEntry(t, "core.thing@resource.new", state.EntryLeaf, "resource"),
			),
			dag: moveDAG(moveNode("core.thing@resource.new", NodeResource)),
			specs: []EntryMoveSpec{
				moveSpec(t, "core.thing@resource.old", "core.thing@resource.new"),
			},
			wantErr: "destination already exists at core.thing@resource.new",
		},
		{
			name: "same ref",
			snap: moveSnapshot(moveEntry(t, "core.thing@resource.old", state.EntryLeaf, "resource")),
			dag:  moveDAG(moveNode("core.thing@resource.old", NodeResource)),
			specs: []EntryMoveSpec{
				moveSpec(t, "core.thing@resource.old", "core.thing@resource.old"),
			},
			wantErr: "source and destination are the same",
		},
		{
			name: "duplicate source",
			snap: moveSnapshot(moveEntry(t, "core.thing@resource.old", state.EntryLeaf, "resource")),
			dag:  moveDAG(moveNode("core.thing@resource.new", NodeResource)),
			specs: []EntryMoveSpec{
				moveSpec(t, "core.thing@resource.old", "core.thing@resource.new"),
				moveSpec(t, "core.thing@resource.old", "core.thing@resource.other"),
			},
			wantErr: "duplicate source core.thing@resource.old",
		},
		{
			name: "missing final destination",
			snap: moveSnapshot(moveEntry(t, "core.thing@resource.old", state.EntryLeaf, "resource")),
			dag:  moveDAG(),
			specs: []EntryMoveSpec{
				moveSpec(t, "core.thing@resource.old", "core.thing@resource.new"),
			},
			wantErr: "destination is not in this factory",
		},
		{
			name: "kind mismatch",
			snap: moveSnapshot(moveEntry(t, "core.thing@resource.old", state.EntryLeaf, "resource")),
			dag:  moveDAG(moveNode("core.thing@action.new", NodeAction)),
			specs: []EntryMoveSpec{
				moveSpec(t, "core.thing@resource.old", "core.thing@action.new"),
			},
			wantErr: "leaf entry cannot move to action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ApplyEntryMoves(tt.snap, tt.dag, stateMovesLibs(), tt.specs, EntryMoveStrict)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestApplyEntryMovesIdempotent(t *testing.T) {
	tests := []struct {
		name    string
		snap    *state.Snapshot
		want    []string
		wantErr string
	}{
		{
			name: "source absent destination present",
			snap: moveSnapshot(moveEntry(t, "core.thing@resource.new", state.EntryLeaf, "resource")),
			want: []string{"core.thing@resource.new"},
		},
		{
			name: "source absent destination absent",
			snap: moveSnapshot(moveEntry(t, "core.thing@resource.other", state.EntryLeaf, "resource")),
			want: []string{"core.thing@resource.other"},
		},
		{
			name: "source and destination present",
			snap: moveSnapshot(
				moveEntry(t, "core.thing@resource.old", state.EntryLeaf, "resource"),
				moveEntry(t, "core.thing@resource.new", state.EntryLeaf, "resource"),
			),
			wantErr: "destination already exists at core.thing@resource.new",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dag := moveDAG(moveNode("core.thing@resource.new", NodeResource))
			got, results, err := ApplyEntryMoves(
				tt.snap, dag, stateMovesLibs(),
				[]EntryMoveSpec{moveSpec(t, "core.thing@resource.old", "core.thing@resource.new")},
				EntryMoveIdempotent,
			)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Empty(t, results)
			assert.ElementsMatch(t, tt.want, entryRefStrings(t, got))
		})
	}
}

func TestApplyEntryMovesChains(t *testing.T) {
	specs := []EntryMoveSpec{
		moveSpec(t, "core.thing@resource.a", "core.thing@resource.b"),
		moveSpec(t, "core.thing@resource.b", "core.thing@resource.c"),
	}
	dag := moveDAG(moveNode("core.thing@resource.c", NodeResource))

	for _, start := range []string{"core.thing@resource.a", "core.thing@resource.b"} {
		t.Run(start, func(t *testing.T) {
			got, results, err := ApplyEntryMoves(
				moveSnapshot(moveEntry(t, start, state.EntryLeaf, "resource")),
				dag, stateMovesLibs(), specs, EntryMoveIdempotent,
			)
			require.NoError(t, err)
			assert.Equal(t, []string{"core.thing@resource.c"}, entryRefStrings(t, got))
			require.Len(t, results, 1)
			assert.Equal(t, start, results[0].From.String())
			assert.Equal(t, "core.thing@resource.c", results[0].To.String())
		})
	}
}

func TestApplyEntryMovesRejectsCycle(t *testing.T) {
	_, _, err := ApplyEntryMoves(
		moveSnapshot(moveEntry(t, "core.thing@resource.a", state.EntryLeaf, "resource")),
		moveDAG(moveNode("core.thing@resource.a", NodeResource)),
		stateMovesLibs(),
		[]EntryMoveSpec{
			moveSpec(t, "core.thing@resource.a", "core.thing@resource.b"),
			moveSpec(t, "core.thing@resource.b", "core.thing@resource.a"),
		},
		EntryMoveIdempotent,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestApplyEntryMovesCompositePrefix(t *testing.T) {
	snap := moveSnapshot(
		moveEntry(t, "net.cluster@resource.web", state.EntryLibraryCall, "resource"),
		moveEntry(t, "aws.security-group@resource.web/resource.sg", state.EntryLeaf, "resource"),
		moveEntry(t, "aws.instance@resource.web/resource.node", state.EntryLeaf, "resource"),
		moveEntry(t, "aws.instance@resource.other", state.EntryLeaf, "resource"),
	)
	dag := moveDAG(
		moveCompositeNode("net.cluster@resource.app", NodeResource),
		moveNode("aws.security-group@resource.app/resource.sg", NodeResource),
		moveNode("aws.instance@resource.app/resource.node", NodeResource),
		moveNode("aws.instance@resource.other", NodeResource),
	)

	got, results, err := ApplyEntryMoves(
		snap, dag, stateMovesLibs(),
		[]EntryMoveSpec{moveSpec(t, "net.cluster@resource.web", "net.cluster@resource.app")},
		EntryMoveIdempotent,
	)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"net.cluster@resource.app",
		"aws.security-group@resource.app/resource.sg",
		"aws.instance@resource.app/resource.node",
		"aws.instance@resource.other",
	}, entryRefStrings(t, got))
	assert.Len(t, results, 3)
}

func TestApplyEntryMovesExactChildOverridesPrefix(t *testing.T) {
	snap := moveSnapshot(
		moveEntry(t, "net.cluster@resource.web", state.EntryLibraryCall, "resource"),
		moveEntry(t, "aws.security-group@resource.web/resource.sg", state.EntryLeaf, "resource"),
	)
	dag := moveDAG(
		moveCompositeNode("net.cluster@resource.app", NodeResource),
		moveNode("aws.security-group@resource.app/resource.firewall", NodeResource),
	)

	got, _, err := ApplyEntryMoves(
		snap, dag, stateMovesLibs(),
		[]EntryMoveSpec{
			moveSpec(t, "net.cluster@resource.web", "net.cluster@resource.app"),
			moveSpec(t,
				"aws.security-group@resource.web/resource.sg",
				"aws.security-group@resource.app/resource.firewall"),
		},
		EntryMoveIdempotent,
	)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"net.cluster@resource.app",
		"aws.security-group@resource.app/resource.firewall",
	}, entryRefStrings(t, got))
}

func TestApplyEntryMovesPrefixRejectsMissingChildTarget(t *testing.T) {
	_, _, err := ApplyEntryMoves(
		moveSnapshot(
			moveEntry(t, "net.cluster@resource.web", state.EntryLibraryCall, "resource"),
			moveEntry(t, "aws.instance@resource.web/resource.node", state.EntryLeaf, "resource"),
		),
		moveDAG(moveCompositeNode("net.cluster@resource.app", NodeResource)),
		stateMovesLibs(),
		[]EntryMoveSpec{moveSpec(t, "net.cluster@resource.web", "net.cluster@resource.app")},
		EntryMoveIdempotent,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "destination is not in this factory")
}
