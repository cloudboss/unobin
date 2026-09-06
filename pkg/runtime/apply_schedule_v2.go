package runtime

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"
)

type applyScheduleV2Graph struct {
	indegree   map[string]int
	dependents map[string][]string
}

type applyScheduleV2Result struct {
	step PlanStepV2
	err  error
}

type applyReadyV2Item struct {
	step  PlanStepV2
	index int
}

type applyReadyV2Queue []applyReadyV2Item

func (q applyReadyV2Queue) Len() int { return len(q) }

func (q applyReadyV2Queue) Less(i, j int) bool { return q[i].index < q[j].index }

func (q applyReadyV2Queue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

func (q *applyReadyV2Queue) Push(value any) {
	*q = append(*q, value.(applyReadyV2Item))
}

func (q *applyReadyV2Queue) Pop() any {
	old := *q
	last := len(old) - 1
	item := old[last]
	*q = old[:last]
	return item
}

type applyScheduleV2Options struct {
	Parallelism int
	Nodes       map[string]*Node
	Drain       <-chan struct{}
	Events      chan<- ApplyEvent
}

func runApplyScheduleV2(
	ctx context.Context,
	steps []PlanStepV2,
	parallelism int,
	apply func(context.Context, PlanStepV2) error,
) error {
	return runApplyScheduleV2WithOptions(ctx, steps, parallelism, applyScheduleV2Options{}, apply)
}

func runApplyScheduleV2WithOptions(
	ctx context.Context,
	steps []PlanStepV2,
	parallelism int,
	options applyScheduleV2Options,
	apply func(context.Context, PlanStepV2) error,
) error {
	if ctx == nil {
		return fmt.Errorf("apply context is required")
	}
	if apply == nil {
		return fmt.Errorf("step apply callback is required")
	}
	graph, indexByAddress, err := buildApplyScheduleV2Graph(steps)
	if err != nil {
		return err
	}
	if len(steps) == 0 {
		return nil
	}
	if options.Parallelism > 0 {
		parallelism = options.Parallelism
	}
	if parallelism <= 0 {
		parallelism = DefaultParallelism
	}
	parallelism = min(parallelism, len(steps))
	var readySteps applyReadyV2Queue
	for i := range steps {
		if graph.indegree[steps[i].Address] == 0 {
			heap.Push(&readySteps, applyReadyV2Item{step: steps[i], index: i})
		}
	}
	heldLocks := map[string]bool{}
	waitingByLock := map[string]*applyReadyV2Queue{}
	nextReady := func() (applyReadyV2Item, bool) {
		for readySteps.Len() > 0 {
			item := heap.Pop(&readySteps).(applyReadyV2Item)
			lock := options.lockName(item.step)
			if lock != "" && heldLocks[lock] {
				if waitingByLock[lock] == nil {
					waitingByLock[lock] = &applyReadyV2Queue{}
				}
				heap.Push(waitingByLock[lock], item)
				continue
			}
			return item, true
		}
		return applyReadyV2Item{}, false
	}
	ready := make(chan PlanStepV2)
	results := make(chan applyScheduleV2Result)
	var wait sync.WaitGroup
	for range parallelism {
		wait.Go(func() {
			for step := range ready {
				err := options.apply(ctx, step, apply)
				results <- applyScheduleV2Result{step: step, err: err}
			}
		})
	}
	indegree := maps.Clone(graph.indegree)
	dispatched := make(map[string]bool, len(steps))
	failed := map[string]bool{}
	startedAt := make(map[string]time.Time, len(steps))
	inFlight, completed, succeeded := 0, 0, 0
	var firstErr error
	var firstFailure *ApplyError
	halted, drained := false, false
	handleResult := func(result applyScheduleV2Result) {
		inFlight--
		completed++
		if lock := options.lockName(result.step); lock != "" {
			delete(heldLocks, lock)
			if waiting := waitingByLock[lock]; waiting != nil {
				for waiting.Len() > 0 {
					heap.Push(&readySteps, heap.Pop(waiting).(applyReadyV2Item))
				}
				delete(waitingByLock, lock)
			}
		}
		elapsed := time.Since(startedAt[result.step.Address])
		if result.err != nil {
			alias := ""
			if node := options.Nodes[templateAddress(result.step.Address)]; node != nil {
				alias = node.Alias
			}
			blameLibrary(result.err, alias)
			options.emit(result.step, StageFail, elapsed, result.err)
			failed[result.step.Address] = true
			if firstErr == nil {
				firstFailure = &ApplyError{
					Address: result.step.Address, Kind: result.step.Kind,
					Decision: planStepV2Decision(result.step), Alias: alias,
					LibraryPath: planStepV2LibraryPath(result.step),
					Elapsed:     elapsed, Err: result.err,
				}
				firstErr = firstFailure
			}
			halted = true
			return
		}
		succeeded++
		options.emit(result.step, StageDone, elapsed, nil)
		for _, address := range graph.dependents[result.step.Address] {
			indegree[address]--
			if indegree[address] == 0 {
				index := indexByAddress[address]
				heap.Push(&readySteps, applyReadyV2Item{step: steps[index], index: index})
			}
		}
	}
	for {
		if !halted && ctx.Err() != nil {
			firstErr, halted = ctx.Err(), true
		}
		if !halted {
			select {
			case <-options.Drain:
				halted, drained = true, true
			default:
			}
		}
		var next applyReadyV2Item
		hasNext := false
		if !halted && inFlight < parallelism {
			next, hasNext = nextReady()
		}
		if hasNext {
			select {
			case ready <- next.step:
				dispatched[next.step.Address] = true
				inFlight++
				if lock := options.lockName(next.step); lock != "" {
					heldLocks[lock] = true
				}
				startedAt[next.step.Address] = time.Now()
				options.emit(next.step, StageStart, 0, nil)
			case result := <-results:
				heap.Push(&readySteps, next)
				handleResult(result)
			case <-options.Drain:
				heap.Push(&readySteps, next)
				halted, drained = true, true
			case <-ctx.Done():
				heap.Push(&readySteps, next)
				firstErr, halted = ctx.Err(), true
			}
			continue
		}
		if inFlight == 0 {
			break
		}
		if halted {
			handleResult(<-results)
			continue
		}
		select {
		case result := <-results:
			handleResult(result)
		case <-options.Drain:
			halted, drained = true, true
		case <-ctx.Done():
			firstErr, halted = ctx.Err(), true
		}
	}
	close(ready)
	wait.Wait()
	if firstErr != nil {
		if firstFailure != nil {
			firstFailure.SkippedCount = countUndispatchedDependents(
				graph.dependents, firstFailure.Address, dispatched, failed,
			)
			firstFailure.SucceededCount = succeeded
		}
		return firstErr
	}
	if drained {
		return ErrInterrupted
	}
	if completed != len(steps) {
		return errors.New("apply: version 2 step dependency cycle")
	}
	if len(dispatched) != len(steps) {
		return errors.New("apply: version 2 scheduler stopped with steps remaining")
	}
	return nil
}

func (o applyScheduleV2Options) lockName(step PlanStepV2) string {
	if node := o.Nodes[templateAddress(step.Address)]; node != nil {
		return node.LockName
	}
	return ""
}

func (o applyScheduleV2Options) apply(
	ctx context.Context, step PlanStepV2, apply func(context.Context, PlanStepV2) error,
) error {
	if node := o.Nodes[templateAddress(step.Address)]; node != nil && node.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, node.Timeout)
		defer cancel()
	}
	return guardErr("applying a version 2 plan step", true, func() error {
		return apply(ctx, step)
	})
}

func (o applyScheduleV2Options) emit(
	step PlanStepV2, stage ApplyStage, elapsed time.Duration, err error,
) {
	if o.Events == nil {
		return
	}
	o.Events <- ApplyEvent{
		Address: step.Address, Kind: step.Kind, Decision: planStepV2Decision(step),
		Composite: step.Operation.Kind == StepComposite,
		Stage:     stage, Time: time.Now(), Elapsed: elapsed, Err: err,
	}
}

func planStepV2LibraryPath(step PlanStepV2) string {
	switch operation := step.Operation; operation.Kind {
	case StepResource:
		if operation.Resource.Desired != nil {
			return operation.Resource.Desired.Binding.LibraryPath
		}
		return operation.Resource.Prior.Binding.LibraryPath
	case StepAction:
		if operation.Action.Desired != nil {
			return operation.Action.Desired.Binding.LibraryPath
		}
		return operation.Action.Prior.Binding.LibraryPath
	case StepDataSource:
		if operation.DataSource.Desired != nil {
			return operation.DataSource.Desired.Binding.LibraryPath
		}
		return operation.DataSource.Prior.Binding.LibraryPath
	case StepComposite:
		if operation.Composite.Desired != nil {
			return operation.Composite.Desired.Binding.LibraryPath
		}
		return operation.Composite.Prior.Binding.LibraryPath
	case StepLibraryConfiguration:
		if operation.LibraryConfiguration.Result.Record != nil {
			return operation.LibraryConfiguration.Result.Record.LibraryPath
		}
	}
	return ""
}

func buildApplyScheduleV2Graph(
	steps []PlanStepV2,
) (*applyScheduleV2Graph, map[string]int, error) {
	graph := &applyScheduleV2Graph{
		indegree:   make(map[string]int, len(steps)),
		dependents: make(map[string][]string, len(steps)),
	}
	indexByAddress := make(map[string]int, len(steps))
	for i := range steps {
		if err := steps[i].Validate(); err != nil {
			return nil, nil, fmt.Errorf("step %d: %w", i, err)
		}
		if _, ok := indexByAddress[steps[i].Address]; ok {
			return nil, nil, fmt.Errorf(
				"duplicate step address %q",
				steps[i].Address,
			)
		}
		indexByAddress[steps[i].Address] = i
		graph.indegree[steps[i].Address] = 0
	}

	for i := range steps {
		step := steps[i]
		if planStepV2Decision(step) == DecisionDestroy {
			for _, dependency := range step.DependsOn {
				index, ok := indexByAddress[dependency]
				if !ok || planStepV2Decision(steps[index]) != DecisionDestroy {
					continue
				}
				graph.dependents[step.Address] = append(
					graph.dependents[step.Address],
					dependency,
				)
				graph.indegree[dependency]++
			}
			continue
		}
		for _, dependency := range step.DependsOn {
			if _, ok := indexByAddress[dependency]; !ok {
				continue
			}
			graph.dependents[dependency] = append(
				graph.dependents[dependency],
				step.Address,
			)
			graph.indegree[step.Address]++
		}
	}
	if err := validateApplyScheduleV2Graph(graph, steps); err != nil {
		return nil, nil, err
	}
	return graph, indexByAddress, nil
}

func validateApplyScheduleV2Graph(
	graph *applyScheduleV2Graph,
	steps []PlanStepV2,
) error {
	indegree := make(map[string]int, len(graph.indegree))
	maps.Copy(indegree, graph.indegree)
	ready := make([]string, 0, len(steps))
	for i := range steps {
		if indegree[steps[i].Address] == 0 {
			ready = append(ready, steps[i].Address)
		}
	}
	completed := 0
	for len(ready) > 0 {
		address := ready[0]
		ready = ready[1:]
		completed++
		for _, dependent := range graph.dependents[address] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}
	if completed != len(steps) {
		return errors.New("apply: version 2 step dependency cycle")
	}
	return nil
}

func planStepV2Decision(step PlanStepV2) Decision {
	switch step.Operation.Kind {
	case StepResource:
		return step.Operation.Resource.Decision
	case StepAction:
		return step.Operation.Action.Decision
	case StepDataSource:
		return step.Operation.DataSource.Decision
	case StepLibraryConfiguration:
		return step.Operation.LibraryConfiguration.Decision
	case StepComposite:
		return step.Operation.Composite.Decision
	case StepOutput:
		return step.Operation.Output.Decision
	default:
		return ""
	}
}
