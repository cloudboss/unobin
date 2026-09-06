package runtime

import "errors"

// ErrInterrupted is returned by ApplyPlanV2 when the executor's Drain
// channel was closed before all steps could be dispatched. The
// returned snapshot still reflects every step that completed before
// the drain, so re-plan plus apply will pick up the remainder.
var ErrInterrupted = errors.New("apply: interrupted")

func countUndispatchedDependents(
	dependents map[string][]string, addr string, dispatched, failed map[string]bool,
) int {
	seen := map[string]bool{}
	queue := []string{addr}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, dep := range dependents[cur] {
			if seen[dep] || dispatched[dep] || failed[dep] {
				continue
			}
			seen[dep] = true
			queue = append(queue, dep)
		}
	}
	return len(seen)
}
