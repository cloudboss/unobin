package runtime

import (
	"context"
	"fmt"
)

func (e *Executor) planEvaluationV2ResourceStep(
	ctx context.Context,
	evaluation *planEvaluationV2,
	pass *planningPassState,
	request registeredResourcePlanningRequest,
) (*PlanStepV2, error) {
	if ctx == nil {
		return nil, fmt.Errorf("resource planning context is required")
	}
	if e == nil {
		return nil, fmt.Errorf("executor is required")
	}
	if e.DAG == nil {
		return nil, fmt.Errorf("dependency graph is required")
	}
	if evaluation == nil || evaluation.run == nil || evaluation.run.eval == nil {
		return nil, fmt.Errorf("version 2 plan evaluation is required")
	}
	if pass == nil || pass.facts == nil {
		return nil, fmt.Errorf("planning pass state is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.Desired == nil && request.Prior == nil {
		return nil, nil
	}
	if err := validatePlanningStepMetadata(
		request.Address, NodeResource, request.DependsOn,
	); err != nil {
		return nil, err
	}
	scope, err := e.scopeForAddress(evaluation.run, request.Address)
	if err != nil {
		return nil, err
	}
	step, err := planRegisteredResourceStep(ctx, pass, request)
	if err != nil {
		return nil, err
	}
	if scope == nil {
		return step, nil
	}
	template, key := splitInstanceAddress(step.Address)
	operation := step.Operation.Resource
	if operation.Decision == DecisionNoOp && !pass.outputsInvalidated(step.Address) {
		fields, _ := operation.Observation.Outputs.ObjectFields()
		outputs, err := decodeConcreteObjectFields(fields, "resource output")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", step.Address, err)
		}
		if key == "" {
			seedAddress(scope.Resources, template, outputs)
		} else {
			seedAddressInstance(scope.Resources, template, key, outputs)
		}
		return step, nil
	}
	path, _ := addressValuePath(template)
	if key != "" {
		path = append(path, key)
	}
	outputs := scope.Resources
	for _, name := range path[:len(path)-1] {
		nested, ok := outputs[name].(map[string]any)
		if !ok {
			return step, nil
		}
		outputs = nested
	}
	delete(outputs, path[len(path)-1])
	return step, nil
}
