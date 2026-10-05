package runtime

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func BenchmarkStateLookupDuringPlanning(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("entries=%d", count), func(b *testing.B) {
			benchmarkStateLookup(b, count, false)
		})
	}
}

func BenchmarkStateLookupWithChanges(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("entries=%d", count), func(b *testing.B) {
			benchmarkStateLookup(b, count, true)
		})
	}
}

func benchmarkStateLookup(b *testing.B, count int, changes bool) {
	b.Helper()
	prior := state.NewSnapshot(state.FactoryInfo{Name: "lookup", Version: "v1"}, "test")
	addresses := make([]string, count)
	prior.Entries = make([]*state.Entry, count)
	binding := &state.Binding{Alias: "test", LibraryPath: "example.com/test", Export: "thing"}
	for i := range count {
		addresses[i] = instanceAddress("resource.nodes", fmt.Sprintf("node-%05d", i))
		prior.Entries[i] = &state.Entry{
			Address: addresses[i], Type: state.EntryLeaf, Category: "resource", Binding: binding,
			SchemaVersion: 1, Outputs: map[string]any{"id": i},
		}
	}
	require.NoError(b, prior.Validate())
	dag := &DAG{Nodes: map[string]*Node{
		"resource.moved": {
			Address: "resource.moved", Kind: NodeResource,
			Alias: binding.Alias, LibraryPath: binding.LibraryPath, Type: binding.Export,
		},
	}}
	libs := map[string]*Library{"test": {Resources: map[string]ResourceRegistration{
		"thing": MakeResource[plainResource, *plainResourceOutput, any](
			testResourceDefinition[plainResource, *plainResourceOutput, any](),
		),
	}}}
	movedAddress := "resource.moved"
	moves := []EntryMoveSpec{{From: EntryRef{Address: addresses[count-1]},
		To: EntryRef{Address: movedAddress}}}
	updated := cloneEntry(prior.Entries[count/3])
	updated.Outputs = map[string]any{"id": count + 1}
	added := cloneEntry(prior.Entries[0])
	added.Address = "resource.added"
	added.Outputs = map[string]any{"id": count + 2}
	priorResults := make([]*state.Entry, count)
	nextResults := make([]*state.Entry, count)
	var current *runState
	var primed, old, removed *state.Entry
	for b.Loop() {
		current = &runState{prior: prior}
		if changes {
			primed = current.priorEntry(addresses[count-1])
			moved, _, err := ApplyEntryMoves(prior, dag, libs, moves, EntryMoveStrict)
			if err != nil {
				b.Fatal(err)
			}
			current.prior = moved
			current.next = cloneSnapshot(moved)
			current.upsertNext(updated)
			current.upsertNext(added)
			current.removeNext(addresses[count/2])
			old = current.priorEntry(addresses[count-1])
			removed = current.nextEntry(addresses[count/2])
		}
		for i, address := range addresses {
			if changes && i == count-1 {
				address = movedAddress
			}
			priorResults[i] = current.priorEntry(address)
		}
		if changes {
			for i, entry := range current.next.Entries {
				nextResults[i] = current.nextEntry(entry.Address)
			}
		}
	}
	for i, entry := range priorResults {
		require.NotNil(b, entry)
		expectedAddress := addresses[i]
		if changes && i == count-1 {
			expectedAddress = movedAddress
		}
		require.Equal(b, expectedAddress, entry.Address)
		require.Equal(b, map[string]any{"id": i}, entry.Outputs)
	}
	lookups := count
	if changes {
		require.Same(b, prior.Entries[count-1], primed)
		require.Nil(b, old)
		require.Nil(b, removed)
		expected := append([]*state.Entry(nil), priorResults...)
		expected[count/3] = updated
		expected = append(expected[:count/2], expected[count/2+1:]...)
		expected = append(expected, added)
		require.Equal(b, expected, current.next.Entries)
		require.Equal(b, expected, nextResults)
		require.NoError(b, current.next.Validate())
		lookups = count*2 + 3
	}
	b.ReportMetric(float64(count), "entries")
	b.ReportMetric(float64(lookups), "lookups")
}
