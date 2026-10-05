package runtime

import "github.com/cloudboss/unobin/pkg/sdk/state"

func (rs *runState) priorEntry(address string) *state.Entry {
	if rs.prior == nil {
		return nil
	}
	return rs.prior.Find(address)
}

func (rs *runState) nextEntry(address string) *state.Entry {
	if rs.next == nil {
		return nil
	}
	return rs.next.Find(address)
}

func (rs *runState) upsertNext(entry *state.Entry) {
	upsertEntry(rs.next, entry)
}

func (rs *runState) removeNext(address string) {
	removeEntry(rs.next, address)
}

func (rs *runState) pruneNext(steps []PlanStep) {
	pruneStateEntries(rs.next, steps)
}
