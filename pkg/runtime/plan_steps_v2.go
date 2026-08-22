package runtime

import (
	"context"
	"fmt"
	"slices"
)

type planStepV2Planner func(
	context.Context,
	*planningPassState,
) (*PlanStepV2, error)

type planStepV2Request struct {
	Address   string
	Kind      NodeKind
	DependsOn []string
	Plan      planStepV2Planner
}

func planStepsV2(
	ctx context.Context,
	evaluate func(*planningPassState) ([]planStepV2Request, error),
) ([]PlanStepV2, error) {
	if ctx == nil {
		return nil, fmt.Errorf("planning context is required")
	}
	if evaluate == nil {
		return nil, fmt.Errorf("plan step evaluator is required")
	}
	return runFixedPointPlanning(
		ctx,
		func(pass *planningPassState) ([]PlanStepV2, error) {
			requests, err := evaluate(pass)
			if err != nil {
				return nil, err
			}
			requests, err = preparePlanStepV2Requests(requests)
			if err != nil {
				return nil, err
			}

			steps := make([]PlanStepV2, 0, len(requests))
			for i := range requests {
				step, err := requests[i].Plan(ctx, pass)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", requests[i].Address, err)
				}
				if err := validatePlannedStepV2(requests[i], step); err != nil {
					return nil, err
				}
				steps = append(steps, *step)
			}
			return steps, nil
		},
	)
}

func preparePlanStepV2Requests(
	requests []planStepV2Request,
) ([]planStepV2Request, error) {
	prepared := make([]planStepV2Request, len(requests))
	addresses := make(map[string]bool, len(requests))
	for i := range requests {
		request := requests[i]
		if err := validateNodeAddress(request.Address, request.Kind); err != nil {
			return nil, fmt.Errorf("planning request %d: %w", i, err)
		}
		if err := validatePlanDependencies(request.DependsOn); err != nil {
			return nil, fmt.Errorf("%s: %w", request.Address, err)
		}
		if request.Plan == nil {
			return nil, fmt.Errorf("%s: step planner is required", request.Address)
		}
		if addresses[request.Address] {
			return nil, fmt.Errorf("duplicate planning address %q", request.Address)
		}
		addresses[request.Address] = true
		request.DependsOn = slices.Clone(request.DependsOn)
		prepared[i] = request
	}
	if err := validatePlanStepV2RequestGraph(prepared); err != nil {
		return nil, err
	}
	return prepared, nil
}

func validatePlannedStepV2(request planStepV2Request, step *PlanStepV2) error {
	if step == nil {
		return fmt.Errorf("%s: step planner returned no step", request.Address)
	}
	if err := step.Validate(); err != nil {
		return fmt.Errorf("%s: %w", request.Address, err)
	}
	if step.Address != request.Address {
		return fmt.Errorf(
			"%s: planned address %q does not match request",
			request.Address,
			step.Address,
		)
	}
	if step.Kind != request.Kind {
		return fmt.Errorf(
			"%s: planned kind %q does not match request kind %q",
			request.Address,
			step.Kind,
			request.Kind,
		)
	}
	if !slices.Equal(step.DependsOn, request.DependsOn) {
		return fmt.Errorf("%s: planned dependencies do not match request", request.Address)
	}
	return nil
}

func validatePlanStepV2RequestGraph(requests []planStepV2Request) error {
	active := make(map[string]bool, len(requests))
	indegree := make(map[string]int, len(requests))
	dependents := make(map[string][]string, len(requests))
	for i := range requests {
		active[requests[i].Address] = true
		indegree[requests[i].Address] = 0
	}
	for i := range requests {
		request := requests[i]
		for _, dependency := range request.DependsOn {
			if !active[dependency] {
				continue
			}
			dependents[dependency] = append(dependents[dependency], request.Address)
			indegree[request.Address]++
		}
	}

	ready := make([]string, 0, len(requests))
	for i := range requests {
		if indegree[requests[i].Address] == 0 {
			ready = append(ready, requests[i].Address)
		}
	}
	completed := 0
	for next := 0; next < len(ready); next++ {
		address := ready[next]
		completed++
		for _, dependent := range dependents[address] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}
	if completed != len(requests) {
		return fmt.Errorf("planning: version 2 step dependency cycle")
	}
	return nil
}
