package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestRefreshPreservesNonResourceEntries(t *testing.T) {
	snapshot := moveSnapshot(
		moveEntry(t, "resource.group", true, "resource"),
		moveEntry(t, "data-source.group", true, "data-source"),
		moveEntry(t, "action.group", true, "action"),
		moveEntry(t, "data-source.observation", false, "data-source"),
		moveEntry(t, "action.completed", false, "action"),
	)
	snapshot.Outputs = map[string]any{"status": "complete"}
	store := newStateStore(t)
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	executor := &Executor{DAG: moveDAG(), Store: store, Factory: snapshot.Factory}
	result, err := executor.Refresh(context.Background())
	require.NoError(t, err)
	require.Zero(t, result.Refreshed)
	require.Zero(t, result.Dropped)
	current, err := store.Current()
	require.NoError(t, err)
	require.Equal(t, snapshot.Entries, current.Entries)
	require.Equal(t, snapshot.Outputs, current.Outputs)
}

func TestStateCompositeMovesByCategory(t *testing.T) {
	for _, category := range []string{"resource", "data-source", "action"} {
		for _, populated := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/populated=%t", category, populated), func(t *testing.T) {
				from, to := category+".old", category+".new"
				boundary := moveEntryWithBinding(t, from, true, category, "core", "box")
				snapshot := moveSnapshot(boundary)
				nodes := []*Node{moveCompositeNodeWithBinding(to, NodeKind(category), "core", "box")}
				var child *state.Entry
				if populated {
					child = moveEntry(t, from+"/"+category+".child", false, category)
					child.DependsOn = []string{from}
					snapshot.Entries = append(snapshot.Entries, child)
					nodes = append(nodes, moveNode(to+"/"+category+".child", NodeKind(category)))
				}
				moved, _, err := ApplyEntryMoves(snapshot, moveDAG(nodes...), stateMovesLibs(),
					[]EntryMoveSpec{moveSpec(t, from, to)}, EntryMoveStrict)
				require.NoError(t, err)
				wantBoundary := *boundary
				wantBoundary.Address = to
				require.Equal(t, &wantBoundary, moved.Find(to))
				require.Nil(t, moved.Find(from))
				require.Equal(t, boundary, snapshot.Find(from))
				if populated {
					wantChild := *child
					wantChild.Address = to + "/" + category + ".child"
					wantChild.DependsOn = []string{to}
					require.Equal(t, &wantChild, moved.Find(wantChild.Address))
					require.Equal(t, []*state.Entry{&wantBoundary, &wantChild}, moved.Entries)
				} else {
					require.Equal(t, []*state.Entry{&wantBoundary}, moved.Entries)
				}
			})
		}
	}
}
