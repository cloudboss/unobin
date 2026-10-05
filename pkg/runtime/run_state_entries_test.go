package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestRunStateEntriesKeepSnapshotOrder(t *testing.T) {
	alpha := &state.Entry{Address: "resource.alpha", Outputs: map[string]any{"id": "alpha"}}
	beta := &state.Entry{Address: "resource.beta", Outputs: map[string]any{"id": "beta"}}
	rs := &runState{prior: &state.Snapshot{Entries: []*state.Entry{alpha, beta}},
		next: &state.Snapshot{Entries: []*state.Entry{alpha, beta}}}
	require.Same(t, alpha, rs.priorEntry(alpha.Address))
	require.Same(t, beta, rs.nextEntry(beta.Address))
	require.Nil(t, rs.priorEntry("resource.missing"))
	updated := &state.Entry{Address: beta.Address, Outputs: map[string]any{"id": "updated"}}
	rs.upsertNext(updated)
	require.Same(t, updated, rs.nextEntry(beta.Address))
	gamma := &state.Entry{Address: "resource.gamma", Outputs: map[string]any{"id": "gamma"}}
	rs.upsertNext(gamma)
	require.Equal(t, []*state.Entry{alpha, updated, gamma}, rs.next.Entries)
	require.Same(t, gamma, rs.nextEntry(gamma.Address))
	rs.removeNext(alpha.Address)
	require.Nil(t, rs.nextEntry(alpha.Address))
	require.Same(t, updated, rs.nextEntry(beta.Address))
	require.Equal(t, []*state.Entry{updated, gamma}, rs.next.Entries)
	rs.removeNext("resource.missing")
	require.Equal(t, []*state.Entry{updated, gamma}, rs.next.Entries)
	rs.pruneNext([]PlanStep{{Address: gamma.Address, Kind: NodeResource}})
	require.Nil(t, rs.nextEntry(beta.Address))
	require.Same(t, gamma, rs.nextEntry(gamma.Address))
	require.Equal(t, []*state.Entry{gamma}, rs.next.Entries)
	require.Equal(t, []*state.Entry{alpha, beta}, rs.prior.Entries)
}

func TestRunStateEntriesFollowSnapshotMoves(t *testing.T) {
	prior := moveSnapshot(moveEntry(t, "resource.old", state.EntryLeaf, "resource"))
	rs := &runState{prior: prior, next: cloneSnapshot(prior)}
	require.Same(t, prior.Entries[0], rs.priorEntry("resource.old"))
	require.Same(t, rs.next.Entries[0], rs.nextEntry("resource.old"))
	moved, _, err := ApplyEntryMoves(prior, moveDAG(moveNode("resource.new", NodeResource)),
		stateMovesLibs(), []EntryMoveSpec{moveSpec(t, "resource.old", "resource.new")},
		EntryMoveStrict)
	require.NoError(t, err)
	rs.prior = moved
	rs.next = cloneSnapshot(moved)
	require.Nil(t, rs.priorEntry("resource.old"))
	require.Nil(t, rs.nextEntry("resource.old"))
	require.Same(t, moved.Entries[0], rs.priorEntry("resource.new"))
	require.Same(t, rs.next.Entries[0], rs.nextEntry("resource.new"))
	require.Same(t, prior.Entries[0], prior.Find("resource.old"))
}

func TestRunStateEntriesWithoutSnapshots(t *testing.T) {
	rs := &runState{}
	require.Nil(t, rs.priorEntry("resource.missing"))
	require.Nil(t, rs.nextEntry("resource.missing"))
	rs.next = &state.Snapshot{}
	entry := &state.Entry{Address: "resource.first"}
	rs.upsertNext(entry)
	require.Same(t, entry, rs.nextEntry(entry.Address))
	rs.removeNext(entry.Address)
	require.Empty(t, rs.next.Entries)
	require.Nil(t, rs.nextEntry(entry.Address))
	rs.prior = &state.Snapshot{Entries: []*state.Entry{entry}}
	require.Same(t, entry, rs.priorEntry(entry.Address))
	rs.prior = nil
	require.Nil(t, rs.priorEntry(entry.Address))
}
