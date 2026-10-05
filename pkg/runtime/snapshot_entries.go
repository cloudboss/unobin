package runtime

import (
	"slices"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type snapshotEntries struct {
	snapshot  *state.Snapshot
	positions map[string]int
}

func indexSnapshotEntries(snapshot *state.Snapshot) *snapshotEntries {
	index := &snapshotEntries{snapshot: snapshot}
	if snapshot == nil {
		return index
	}
	index.positions = make(map[string]int, len(snapshot.Entries))
	for i, entry := range snapshot.Entries {
		index.positions[entry.Address] = i
	}
	return index
}

func (index *snapshotEntries) find(address string) *state.Entry {
	if position, found := index.positions[address]; found {
		return index.snapshot.Entries[position]
	}
	return nil
}

func (index *snapshotEntries) upsert(entry *state.Entry) {
	if position, found := index.positions[entry.Address]; found {
		index.snapshot.Entries[position] = entry
		return
	}
	index.positions[entry.Address] = len(index.snapshot.Entries)
	index.snapshot.Entries = append(index.snapshot.Entries, entry)
}

func (index *snapshotEntries) remove(address string) {
	position, found := index.positions[address]
	if !found {
		return
	}
	index.snapshot.Entries = slices.Delete(index.snapshot.Entries, position, position+1)
	delete(index.positions, address)
	for i := position; i < len(index.snapshot.Entries); i++ {
		index.positions[index.snapshot.Entries[i].Address] = i
	}
}
