package runtime

import (
	"context"
	"fmt"
)

func (e *Executor) planEvaluationV2ActionStep(
	ctx context.Context,
	evaluation *planEvaluationV2,
	request actionPlanningRequest,
) (*PlanStepV2, error) {
	if ctx == nil {
		return nil, fmt.Errorf("action planning context is required")
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	step, err := planActionStep(request)
	if err != nil || step == nil {
		return nil, err
	}
	scope, err := e.scopeForAddress(evaluation.run, request.Address)
	if err != nil {
		return nil, err
	}
	if scope == nil {
		return step, nil
	}
	operation := step.Operation.Action
	var outputs *EncodedValue
	if operation.Decision == DecisionSkip {
		outputs = &operation.Prior.Outputs
	}
	if err := publishPlanEvaluationV2Outputs(scope.Actions, step.Address, outputs); err != nil {
		return nil, err
	}
	return step, nil
}
