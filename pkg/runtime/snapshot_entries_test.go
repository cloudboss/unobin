package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestSnapshotEntriesFindExactAddresses(t *testing.T) {
	first := &state.Entry{Address: "resource.nodes['a/b']"}
	second := &state.Entry{Address: "resource.nodes['a\\'b']"}
	index := indexSnapshotEntries(&state.Snapshot{Entries: []*state.Entry{first, second}})
	require.Same(t, first, index.find(first.Address))
	require.Same(t, second, index.find(second.Address))
	require.Nil(t, index.find("resource.nodes"))
	require.Nil(t, indexSnapshotEntries(nil).find("resource.missing"))
}

func TestRunStateEntriesAreLocalToOperation(t *testing.T) {
	first := &state.Entry{Address: "resource.first"}
	prior := &state.Snapshot{Entries: []*state.Entry{first}}
	one := &runState{prior: prior}
	require.Same(t, first, one.priorEntry(first.Address))
	second := &state.Entry{Address: "resource.second"}
	prior.Entries = append(prior.Entries, second)
	two := &runState{prior: prior}
	require.Same(t, second, two.priorEntry(second.Address))
}
