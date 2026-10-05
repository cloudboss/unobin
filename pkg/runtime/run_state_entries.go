package runtime

import "github.com/cloudboss/unobin/pkg/sdk/state"

func (rs *runState) priorEntry(address string) *state.Entry {
	if rs.prior == nil {
		return nil
	}
	if rs.priorEntries == nil || rs.priorEntries.snapshot != rs.prior {
		rs.priorEntries = indexSnapshotEntries(rs.prior)
	}
	return rs.priorEntries.find(address)
}

func (rs *runState) nextEntry(address string) *state.Entry {
	if rs.next == nil {
		return nil
	}
	return rs.indexNext().find(address)
}

func (rs *runState) indexNext() *snapshotEntries {
	if rs.nextEntries == nil || rs.nextEntries.snapshot != rs.next {
		rs.nextEntries = indexSnapshotEntries(rs.next)
	}
	return rs.nextEntries
}

func (rs *runState) upsertNext(entry *state.Entry) {
	rs.indexNext().upsert(entry)
}

func (rs *runState) removeNext(address string) {
	rs.indexNext().remove(address)
}

func (rs *runState) pruneNext(steps []PlanStep) {
	pruneStateEntries(rs.next, steps)
	rs.nextEntries = nil
}
