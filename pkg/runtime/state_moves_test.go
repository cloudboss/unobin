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
	return moveEntryWithBinding(t, ref, typ, kind, "core", "thing")
}

func moveEntryWithBinding(
	t *testing.T,
	ref string,
	typ state.EntryType,
	kind string,
	alias string,
	export string,
) *state.Entry {
	t.Helper()
	r := mustEntryRef(t, ref)
	return &state.Entry{
		Address: r.Address,
		Type:    typ,
		Kind:    kind,
		Binding: &state.Binding{
			Alias:       alias,
			LibraryPath: defaultMoveLibraryPath(alias),
			Export:      export,
		},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": r.Address},
		Outputs:       map[string]any{"id": r.String()},
	}
}

func defaultMoveLibraryPath(alias string) string {
	return "example.com/" + alias
}

func setMoveEntryLibraryPath(ent *state.Entry, path string) *state.Entry {
	ent.Binding.LibraryPath = path
	return ent
}

func moveDAG(nodes ...*Node) *DAG {
	dag := &DAG{Nodes: map[string]*Node{}, Edges: map[string][]string{}}
	for _, n := range nodes {
		dag.Nodes[n.Address] = n
	}
	return dag
}

func moveNode(ref string, kind NodeKind) *Node {
	return moveNodeWithBinding(ref, kind, "core", "thing")
}

func moveNodeWithBinding(ref string, kind NodeKind, alias string, export string) *Node {
	r, err := ParseEntryRef(ref)
	if err != nil {
		panic(err)
	}
	return &Node{
		Address:     r.Address,
		Kind:        kind,
		Alias:       alias,
		LibraryPath: defaultMoveLibraryPath(alias),
		Type:        export,
	}
}

func setMoveNodeLibraryPath(n *Node, path string) *Node {
	n.LibraryPath = path
	return n
}

func moveCompositeNodeWithBinding(ref string, kind NodeKind, alias string, export string) *Node {
	n := moveNodeWithBinding(ref, kind, alias, export)
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

func TestApplyEntryMovesUpdatesDependsOn(t *testing.T) {
	parent := moveEntryWithBinding(
		t, "resource.old", state.EntryLibraryCall, "resource", "core", "box",
	)
	parent.DependsOn = []string{"resource.old/resource.child"}
	child := moveEntry(t, "resource.old/resource.child", state.EntryLeaf, "resource")
	snap := moveSnapshot(parent, child)
	dag := moveDAG(
		moveCompositeNodeWithBinding("resource.new", NodeResource, "core", "box"),
		moveNode("resource.new/resource.child", NodeResource),
	)

	out, moved, err := ApplyEntryMoves(
		snap,
		dag,
		stateMovesLibs(),
		[]EntryMoveSpec{moveSpec(t, "resource.old", "resource.new")},
		EntryMoveIdempotent,
	)

	require.NoError(t, err)
	require.Len(t, moved, 2)
	require.Equal(t, []string{"resource.new/resource.child"}, out.Find("resource.new").DependsOn)
}

func TestApplyEntryMovesStrictResourceMoves(t *testing.T) {
	for range 2 {
		snap := moveSnapshot(moveEntry(t, "resource.old", state.EntryLeaf, "resource"))
		dag := moveDAG(moveNode("resource.new", NodeResource))
		got, results, err := ApplyEntryMoves(
			snap, dag, stateMovesLibs(), []EntryMoveSpec{moveSpec(t, "resource.old", "resource.new")},
			EntryMoveStrict,
		)

		require.NoError(t, err)
		assert.Equal(t, []string{"resource.new"}, entryRefStrings(t, got))
		require.Len(t, results, 1)
		assert.Equal(t, "resource.old", results[0].From.String())
		assert.Equal(t, "resource.new", results[0].To.String())
		assert.Equal(t, []string{"resource.old"}, entryRefStrings(t, snap))
	}
}

func TestApplyEntryMovesPreservesBindingForSameImplementationKind(t *testing.T) {
	entry := setMoveEntryLibraryPath(
		moveEntryWithBinding(t, "resource.old", state.EntryLeaf, "resource", "legacy", "thing"),
		"example.com/core",
	)
	snap := moveSnapshot(entry)
	dag := moveDAG(moveNode("resource.new", NodeResource))

	got, results, err := ApplyEntryMoves(
		snap, dag, stateMovesLibs(), []EntryMoveSpec{moveSpec(t, "resource.old", "resource.new")},
		EntryMoveStrict,
	)

	require.NoError(t, err)
	require.Len(t, results, 1)
	moved := got.Find("resource.new")
	require.NotNil(t, moved)
	require.Equal(t, &state.Binding{
		Alias:       "legacy",
		LibraryPath: "example.com/core",
		Export:      "thing",
	}, moved.Binding)
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
			dag:  moveDAG(moveNode("resource.new", NodeResource)),
			specs: []EntryMoveSpec{
				moveSpec(t, "resource.old", "resource.new"),
			},
			wantErr: "no entry at resource.old",
		},
		{
			name: "destination exists",
			snap: moveSnapshot(
				moveEntry(t, "resource.old", state.EntryLeaf, "resource"),
				moveEntry(t, "resource.new", state.EntryLeaf, "resource"),
			),
			dag: moveDAG(moveNode("resource.new", NodeResource)),
			specs: []EntryMoveSpec{
				moveSpec(t, "resource.old", "resource.new"),
			},
			wantErr: "destination already exists at resource.new",
		},
		{
			name: "same ref",
			snap: moveSnapshot(moveEntry(t, "resource.old", state.EntryLeaf, "resource")),
			dag:  moveDAG(moveNode("resource.old", NodeResource)),
			specs: []EntryMoveSpec{
				moveSpec(t, "resource.old", "resource.old"),
			},
			wantErr: "source and destination are the same",
		},
		{
			name: "duplicate source",
			snap: moveSnapshot(moveEntry(t, "resource.old", state.EntryLeaf, "resource")),
			dag:  moveDAG(moveNode("resource.new", NodeResource)),
			specs: []EntryMoveSpec{
				moveSpec(t, "resource.old", "resource.new"),
				moveSpec(t, "resource.old", "resource.other"),
			},
			wantErr: "duplicate source resource.old",
		},
		{
			name: "missing final destination",
			snap: moveSnapshot(moveEntry(t, "resource.old", state.EntryLeaf, "resource")),
			dag:  moveDAG(),
			specs: []EntryMoveSpec{
				moveSpec(t, "resource.old", "resource.new"),
			},
			wantErr: "destination is not in this factory",
		},
		{
			name: "kind mismatch",
			snap: moveSnapshot(moveEntry(t, "resource.old", state.EntryLeaf, "resource")),
			dag:  moveDAG(moveNode("action.new", NodeAction)),
			specs: []EntryMoveSpec{
				moveSpec(t, "resource.old", "action.new"),
			},
			wantErr: "leaf entry cannot move to action",
		},
		{
			name: "implementation kind mismatch",
			snap: moveSnapshot(
				moveEntryWithBinding(t, "resource.old", state.EntryLeaf, "resource", "core", "other"),
			),
			dag: moveDAG(moveNode("resource.new", NodeResource)),
			specs: []EntryMoveSpec{
				moveSpec(t, "resource.old", "resource.new"),
			},
			wantErr: "resource.old cannot move to resource.new as kind core.other differs from core.thing",
		},
		{
			name: "implementation library path mismatch",
			snap: moveSnapshot(
				setMoveEntryLibraryPath(
					moveEntryWithBinding(
						t, "resource.old", state.EntryLeaf, "resource", "legacy", "thing",
					),
					"example.com/old",
				),
			),
			dag: moveDAG(setMoveNodeLibraryPath(
				moveNode("resource.new", NodeResource), "example.com/new",
			)),
			specs: []EntryMoveSpec{
				moveSpec(t, "resource.old", "resource.new"),
			},
			wantErr: "resource.old cannot move to resource.new as kind legacy.thing differs from core.thing",
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
			snap: moveSnapshot(moveEntry(t, "resource.new", state.EntryLeaf, "resource")),
			want: []string{"resource.new"},
		},
		{
			name: "source absent destination absent",
			snap: moveSnapshot(moveEntry(t, "resource.other", state.EntryLeaf, "resource")),
			want: []string{"resource.other"},
		},
		{
			name: "source and destination present",
			snap: moveSnapshot(
				moveEntry(t, "resource.old", state.EntryLeaf, "resource"),
				moveEntry(t, "resource.new", state.EntryLeaf, "resource"),
			),
			wantErr: "destination already exists at resource.new",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dag := moveDAG(moveNode("resource.new", NodeResource))
			got, results, err := ApplyEntryMoves(
				tt.snap, dag, stateMovesLibs(),
				[]EntryMoveSpec{moveSpec(t, "resource.old", "resource.new")},
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
		moveSpec(t, "resource.a", "resource.b"),
		moveSpec(t, "resource.b", "resource.c"),
	}
	dag := moveDAG(moveNode("resource.c", NodeResource))

	for _, start := range []string{"resource.a", "resource.b"} {
		t.Run(start, func(t *testing.T) {
			got, results, err := ApplyEntryMoves(
				moveSnapshot(moveEntry(t, start, state.EntryLeaf, "resource")),
				dag, stateMovesLibs(), specs, EntryMoveIdempotent,
			)
			require.NoError(t, err)
			assert.Equal(t, []string{"resource.c"}, entryRefStrings(t, got))
			require.Len(t, results, 1)
			assert.Equal(t, start, results[0].From.String())
			assert.Equal(t, "resource.c", results[0].To.String())
		})
	}
}

func TestApplyEntryMovesRejectsCycle(t *testing.T) {
	_, _, err := ApplyEntryMoves(
		moveSnapshot(moveEntry(t, "resource.a", state.EntryLeaf, "resource")),
		moveDAG(moveNode("resource.a", NodeResource)),
		stateMovesLibs(),
		[]EntryMoveSpec{
			moveSpec(t, "resource.a", "resource.b"),
			moveSpec(t, "resource.b", "resource.a"),
		},
		EntryMoveIdempotent,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestApplyEntryMovesCompositePrefix(t *testing.T) {
	snap := moveSnapshot(
		moveEntryWithBinding(t, "resource.web", state.EntryLibraryCall, "resource", "net", "cluster"),
		moveEntryWithBinding(
			t, "resource.web/resource.sg", state.EntryLeaf, "resource", "aws", "security-group",
		),
		moveEntryWithBinding(
			t, "resource.web/resource.node", state.EntryLeaf, "resource", "aws", "instance",
		),
		moveEntryWithBinding(t, "resource.other", state.EntryLeaf, "resource", "aws", "instance"),
	)
	dag := moveDAG(
		moveCompositeNodeWithBinding("resource.app", NodeResource, "net", "cluster"),
		moveNodeWithBinding("resource.app/resource.sg", NodeResource, "aws", "security-group"),
		moveNodeWithBinding("resource.app/resource.node", NodeResource, "aws", "instance"),
		moveNodeWithBinding("resource.other", NodeResource, "aws", "instance"),
	)

	got, results, err := ApplyEntryMoves(
		snap, dag, stateMovesLibs(),
		[]EntryMoveSpec{moveSpec(t, "resource.web", "resource.app")},
		EntryMoveIdempotent,
	)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"resource.app",
		"resource.app/resource.sg",
		"resource.app/resource.node",
		"resource.other",
	}, entryRefStrings(t, got))
	assert.Len(t, results, 3)
}

func TestApplyEntryMovesExactChildOverridesPrefix(t *testing.T) {
	snap := moveSnapshot(
		moveEntryWithBinding(t, "resource.web", state.EntryLibraryCall, "resource", "net", "cluster"),
		moveEntryWithBinding(
			t, "resource.web/resource.sg", state.EntryLeaf, "resource", "aws", "security-group",
		),
	)
	dag := moveDAG(
		moveCompositeNodeWithBinding("resource.app", NodeResource, "net", "cluster"),
		moveNodeWithBinding("resource.app/resource.firewall", NodeResource, "aws", "security-group"),
	)

	got, _, err := ApplyEntryMoves(
		snap, dag, stateMovesLibs(),
		[]EntryMoveSpec{
			moveSpec(t, "resource.web", "resource.app"),
			moveSpec(t, "resource.web/resource.sg", "resource.app/resource.firewall"),
		},
		EntryMoveIdempotent,
	)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"resource.app",
		"resource.app/resource.firewall",
	}, entryRefStrings(t, got))
}

func TestApplyEntryMovesPrefixRejectsMissingChildTarget(t *testing.T) {
	_, _, err := ApplyEntryMoves(
		moveSnapshot(
			moveEntryWithBinding(t, "resource.web", state.EntryLibraryCall, "resource", "net", "cluster"),
			moveEntryWithBinding(
				t, "resource.web/resource.node", state.EntryLeaf, "resource", "aws", "instance",
			),
		),
		moveDAG(moveCompositeNodeWithBinding("resource.app", NodeResource, "net", "cluster")),
		stateMovesLibs(),
		[]EntryMoveSpec{moveSpec(t, "resource.web", "resource.app")},
		EntryMoveIdempotent,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "destination is not in this factory")
}
