package runtime

import (
	"context"
	"fmt"
)

type planStepV2ApplyCallback func(
	context.Context,
	*applyStateV2,
	PlanStepV2,
) error

type applyPlanStepsV2Callbacks struct {
	Resource             planStepV2ApplyCallback
	Action               planStepV2ApplyCallback
	DataSource           planStepV2ApplyCallback
	LibraryConfiguration planStepV2ApplyCallback
	Composite            planStepV2ApplyCallback
	Output               outputStepEvaluator
}

func applyPlanStepsV2(
	ctx context.Context,
	applyState *applyStateV2,
	steps []PlanStepV2,
	parallelism int,
	callbacks applyPlanStepsV2Callbacks,
) error {
	if ctx == nil {
		return fmt.Errorf("apply context is required")
	}
	if applyState == nil {
		return fmt.Errorf("version 2 apply state is required")
	}
	if err := validateApplyPlanStepsV2(steps, callbacks); err != nil {
		return err
	}

	outputSteps := make([]PlanStepV2, 0)
	for i := range steps {
		if steps[i].Operation.Kind == StepOutput {
			outputSteps = append(outputSteps, steps[i])
		}
	}

	if err := runApplyScheduleV2(
		ctx,
		steps,
		parallelism,
		func(ctx context.Context, step PlanStepV2) error {
			if step.Operation.Kind == StepOutput {
				return nil
			}
			callback, name := callbacks.callback(step.Operation.Kind)
			if err := callback(ctx, applyState, step); err != nil {
				return fmt.Errorf("apply %s step: %w", name, err)
			}
			return nil
		},
	); err != nil {
		return err
	}

	return applyOutputSteps(ctx, applyState, outputSteps, callbacks.Output)
}

func validateApplyPlanStepsV2(
	steps []PlanStepV2,
	callbacks applyPlanStepsV2Callbacks,
) error {
	if callbacks.Output == nil {
		return fmt.Errorf("output evaluator is required")
	}

	for i := range steps {
		step := steps[i]
		if err := step.Validate(); err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
		if step.Operation.Kind == StepOutput {
			continue
		}
		callback, name := callbacks.callback(step.Operation.Kind)
		if callback == nil {
			return fmt.Errorf("%s apply callback is required", name)
		}
	}
	if _, _, err := buildApplyScheduleV2Graph(steps); err != nil {
		return err
	}
	return nil
}

func (c applyPlanStepsV2Callbacks) callback(
	kind StepOperationKind,
) (planStepV2ApplyCallback, string) {
	switch kind {
	case StepResource:
		return c.Resource, "resource"
	case StepAction:
		return c.Action, "action"
	case StepDataSource:
		return c.DataSource, "data-source"
	case StepLibraryConfiguration:
		return c.LibraryConfiguration, "library-configuration"
	case StepComposite:
		return c.Composite, "composite"
	default:
		return nil, string(kind)
	}
}
