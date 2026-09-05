package runtime

import (
	"context"
	"fmt"
)

func (e *Executor) planEvaluationV2DataSourceStep(
	ctx context.Context,
	evaluation *planEvaluationV2,
	pass *planningPassState,
	request dataSourcePlanningRequest,
	callbacks dataSourcePlanningCallbacks,
) (*PlanStepV2, error) {
	if ctx == nil {
		return nil, fmt.Errorf("data-source planning context is required")
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
	if pass == nil || pass.facts == nil || pass.reads == nil {
		return nil, fmt.Errorf("planning pass state is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.Desired == nil && request.Prior == nil {
		return nil, nil
	}
	if err := validatePlanningStepMetadata(
		request.Address, NodeDataSource, request.DependsOn,
	); err != nil {
		return nil, err
	}
	scope, err := e.scopeForAddress(evaluation.run, request.Address)
	if err != nil {
		return nil, err
	}
	if read := callbacks.Read; read != nil {
		callbacks.Read = func(ctx context.Context) (EncodedValue, error) {
			return pass.readDataSource(ctx, request.Address, *request.Desired, read)
		}
	}
	step, err := planDataSourceStep(ctx, request, callbacks)
	if err != nil {
		return nil, err
	}
	if scope != nil {
		if err := publishPlanEvaluationV2Outputs(
			scope.Data, step.Address, step.Operation.DataSource.ObservedOutputs,
		); err != nil {
			return nil, err
		}
	}
	return step, nil
}
