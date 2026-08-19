package runtime

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
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

func runApplyScheduleV2(
	ctx context.Context,
	steps []PlanStepV2,
	parallelism int,
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

	ready := make(chan PlanStepV2)
	results := make(chan applyScheduleV2Result)
	var wait sync.WaitGroup
	for range parallelism {
		wait.Go(func() {
			for step := range ready {
				err := guardErr("applying a version 2 plan step", true, func() error {
					return apply(ctx, step)
				})
				results <- applyScheduleV2Result{step: step, err: err}
			}
		})
	}

	indegree := make(map[string]int, len(graph.indegree))
	maps.Copy(indegree, graph.indegree)
	dispatched := make(map[string]bool, len(steps))
	inFlight := 0
	completed := 0
	var firstErr error
	halted := false

	handleResult := func(result applyScheduleV2Result) {
		inFlight--
		completed++
		if result.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", result.step.Address, result.err)
			}
			halted = true
			return
		}
		for _, address := range graph.dependents[result.step.Address] {
			indegree[address]--
			if indegree[address] == 0 {
				index := indexByAddress[address]
				heap.Push(
					&readySteps,
					applyReadyV2Item{step: steps[index], index: index},
				)
			}
		}
	}

	for {
		if !halted && ctx.Err() != nil {
			firstErr = ctx.Err()
			halted = true
		}
		if !halted && readySteps.Len() > 0 && inFlight < parallelism {
			next := heap.Pop(&readySteps).(applyReadyV2Item)
			select {
			case ready <- next.step:
				dispatched[next.step.Address] = true
				inFlight++
			case result := <-results:
				heap.Push(&readySteps, next)
				handleResult(result)
			case <-ctx.Done():
				heap.Push(&readySteps, next)
				firstErr = ctx.Err()
				halted = true
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
		case <-ctx.Done():
			firstErr = ctx.Err()
			halted = true
		}
	}

	close(ready)
	wait.Wait()
	if firstErr != nil {
		return firstErr
	}
	if completed != len(steps) {
		return errors.New("apply: version 2 step dependency cycle")
	}
	if len(dispatched) != len(steps) {
		return errors.New("apply: version 2 scheduler stopped with steps remaining")
	}
	return nil
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
