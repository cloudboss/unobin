package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

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
	parallelism := e.effectiveParallelism()
	if parallelism > len(pf.Steps) {
		parallelism = len(pf.Steps)
	}

	pending := make([]*PlanStep, len(pf.Steps))
	for i := range pf.Steps {
		pending[i] = &pf.Steps[i]
	}
	indegree := make(map[string]int, len(graph.indegree))
	for k, v := range graph.indegree {
		indegree[k] = v
	}
	dispatched := make(map[string]bool, len(pf.Steps))

	ready := make(chan *PlanStep)
	results := make(chan stepResult)

	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for step := range ready {
				err := e.applyStep(ctx, rs, step)
				results <- stepResult{step: step, err: err}
			}
		}()
	}

	var firstErr error
	halted := false
	inFlight := 0
	heldLocks := map[string]bool{}

	handleResult := func(r stepResult) {
		inFlight--
		if lock := graph.locks[r.step.Address]; lock != "" {
			delete(heldLocks, lock)
		}
		if r.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", r.step.Address, r.err)
			}
			halted = true
			return
		}
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
			case r := <-results:
				handleResult(r)
			}
			continue
		}
		if inFlight == 0 {
			break
		}
		r := <-results
		handleResult(r)
	}

	close(ready)
	wg.Wait()

	if firstErr == nil && !halted && !allDispatched(pending, dispatched) {
		return errors.New("apply: scheduler exited with steps left unread")
	}
	return firstErr
}

func allDispatched(steps []*PlanStep, dispatched map[string]bool) bool {
	for _, s := range steps {
		if !dispatched[s.Address] {
			return false
		}
	}
	return true
}
