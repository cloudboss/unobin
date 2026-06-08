package runtime

import (
	"context"
	"errors"
	"maps"
	"sync"
	"time"
)

// ErrInterrupted is returned by ApplyPlan when the executor's Drain
// channel was closed before all steps could be dispatched. The
// returned snapshot still reflects every step that completed before
// the drain, so re-plan plus apply will pick up the remainder.
var ErrInterrupted = errors.New("apply: interrupted")

// stepResult is what a worker hands back to the scheduler when it
// finishes a step. A nil err means the step completed successfully and
// its dependents may be promoted.
type stepResult struct {
	step *PlanStep
	err  error
}

// runApplySchedule executes pf.Steps against rs. Independent branches
// of the step graph run concurrently up to Executor.Parallelism (or
// DefaultParallelism). On the first step error, no new steps are
// dispatched, in-flight steps run to completion (so their cloud calls
// finish and their state writes commit), and the first error is
// returned wrapped with its step address.
func (e *Executor) runApplySchedule(ctx context.Context, rs *runState, pf *PlanFile) error {
	if len(pf.Steps) == 0 {
		return nil
	}
	graph := buildStepGraph(pf, e.DAG)
	rs.dependsOn = persistedDependsOn(graph, pf.Steps)
	parallelism := min(e.effectiveParallelism(), len(pf.Steps))

	pending := make([]*PlanStep, len(pf.Steps))
	for i := range pf.Steps {
		pending[i] = &pf.Steps[i]
	}
	indegree := make(map[string]int, len(graph.indegree))
	maps.Copy(indegree, graph.indegree)
	dispatched := make(map[string]bool, len(pf.Steps))

	ready := make(chan *PlanStep)
	results := make(chan stepResult)

	var wg sync.WaitGroup
	for range parallelism {
		wg.Go(func() {
			for step := range ready {
				// Library calls recover their own panics; this guard is the
				// backstop for a panic in the runtime's own step handling,
				// so a defect there fails the step instead of the process.
				err := guardErr("applying this step", true, func() error {
					return e.applyStep(ctx, rs, step)
				})
				results <- stepResult{step: step, err: err}
			}
		})
	}

	var firstErr error
	var firstFail *ApplyError
	halted := false
	drained := false
	inFlight := 0
	heldLocks := map[string]bool{}
	startedAt := make(map[string]time.Time, len(pf.Steps))
	failedAddrs := map[string]bool{}

	emit := func(ev ApplyEvent) {
		if e.Events == nil {
			return
		}
		ev.Time = time.Now()
		e.Events <- ev
	}

	handleResult := func(r stepResult) {
		inFlight--
		if lock := graph.locks[r.step.Address]; lock != "" {
			delete(heldLocks, lock)
		}
		elapsed := time.Since(startedAt[r.step.Address])
		if r.err != nil {
			library := ""
			if n, ok := e.DAG.Nodes[templateAddress(r.step.Address)]; ok {
				library = n.Alias
			}
			// A panic recovered at a CRUD boundary cannot know its own
			// import alias; name it here, where the failing node is known.
			blameLibrary(r.err, library)
			emit(ApplyEvent{
				Address: r.step.Address, Kind: r.step.Kind, Composite: r.step.Composite,
				Decision: r.step.Decision,
				Stage:    StageFail, Elapsed: elapsed, Err: r.err,
			})
			failedAddrs[r.step.Address] = true
			if firstErr == nil {
				firstFail = &ApplyError{
					Address:  r.step.Address,
					Kind:     r.step.Kind,
					Decision: r.step.Decision,
					Library:  library,
					Elapsed:  elapsed,
					Err:      r.err,
				}
				firstErr = firstFail
			}
			halted = true
			return
		}
		emit(ApplyEvent{
			Address: r.step.Address, Kind: r.step.Kind, Composite: r.step.Composite,
			Decision: r.step.Decision,
			Stage:    StageDone, Elapsed: elapsed,
		})
		for _, dep := range graph.dependents[r.step.Address] {
			indegree[dep]--
		}
	}

	pickReady := func() *PlanStep {
		for _, step := range pending {
			if dispatched[step.Address] {
				continue
			}
			if indegree[step.Address] != 0 {
				continue
			}
			if lock := graph.locks[step.Address]; lock != "" && heldLocks[lock] {
				continue
			}
			return step
		}
		return nil
	}

	for {
		if !halted {
			select {
			case <-e.Drain:
				halted = true
				drained = true
			default:
			}
		}
		var next *PlanStep
		if !halted {
			next = pickReady()
		}
		if next != nil {
			select {
			case ready <- next:
				dispatched[next.Address] = true
				inFlight++
				if lock := graph.locks[next.Address]; lock != "" {
					heldLocks[lock] = true
				}
				startedAt[next.Address] = time.Now()
				emit(ApplyEvent{
					Address: next.Address, Kind: next.Kind, Composite: next.Composite,
					Decision: next.Decision, Stage: StageStart,
				})
			case r := <-results:
				handleResult(r)
			case <-e.Drain:
				halted = true
				drained = true
			}
			continue
		}
		if inFlight == 0 {
			break
		}
		select {
		case r := <-results:
			handleResult(r)
		case <-e.Drain:
			halted = true
			drained = true
		}
	}

	close(ready)
	wg.Wait()

	if firstErr != nil {
		if firstFail != nil {
			firstFail.SkippedCount = countTransitiveSkipped(
				graph, firstFail.Address, dispatched, failedAddrs)
			firstFail.SucceededCount = countSucceeded(
				pending, dispatched, failedAddrs)
		}
		return firstErr
	}
	if drained {
		return ErrInterrupted
	}
	if !allDispatched(pending, dispatched) {
		return errors.New("apply: scheduler exited with steps left unread")
	}
	return nil
}

// countTransitiveSkipped counts the steps that were not dispatched
// because they transitively depended on a failed step. The walk starts
// at addr and follows dependents, skipping anything already dispatched
// or already counted.
func countTransitiveSkipped(
	g *stepGraph, addr string, dispatched, failed map[string]bool,
) int {
	seen := map[string]bool{}
	queue := []string{addr}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, dep := range g.dependents[cur] {
			if seen[dep] || dispatched[dep] || failed[dep] {
				continue
			}
			seen[dep] = true
			queue = append(queue, dep)
		}
	}
	return len(seen)
}

// countSucceeded counts steps that were dispatched and did not fail.
// In drain or fail modes some steps may still be in flight when the
// scheduler exits; this counter treats only the ones that recorded
// no failure as successes.
func countSucceeded(
	pending []*PlanStep, dispatched, failed map[string]bool,
) int {
	n := 0
	for _, s := range pending {
		if dispatched[s.Address] && !failed[s.Address] {
			n++
		}
	}
	return n
}

func allDispatched(steps []*PlanStep, dispatched map[string]bool) bool {
	for _, s := range steps {
		if !dispatched[s.Address] {
			return false
		}
	}
	return true
}
