package runtime

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

type outputStepEvaluator func(context.Context, PlanStepV2) (EncodedValue, error)

func applyOutputSteps(
	ctx context.Context,
	applyState *applyStateV2,
	steps []PlanStepV2,
	evaluate outputStepEvaluator,
) error {
	if ctx == nil {
		return fmt.Errorf("apply context is required")
	}
	if applyState == nil {
		return fmt.Errorf("version 2 apply state is required")
	}
	if evaluate == nil {
		return fmt.Errorf("output evaluator is required")
	}

	addresses := make(map[string]bool, len(steps))
	for i := range steps {
		if err := steps[i].Validate(); err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
		if steps[i].Kind != NodeOutput || steps[i].Operation.Kind != StepOutput {
			return fmt.Errorf("step %d must be an output", i)
		}
		if addresses[steps[i].Address] {
			return fmt.Errorf("duplicate output address %q", steps[i].Address)
		}
		addresses[steps[i].Address] = true
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	fields := make(map[string]EncodedValue, len(steps))
	sensitivePaths := make([]string, 0, len(steps))
	for i := range steps {
		step := steps[i]
		current, err := guard(
			"evaluating output "+step.Address,
			true,
			func() (EncodedValue, error) {
				return evaluate(ctx, step)
			},
		)
		if err != nil {
			return fmt.Errorf("evaluate output %s: %w", step.Address, err)
		}
		if err := validatePlanValue(current, "evaluated output value", false); err != nil {
			return fmt.Errorf("output %s: %w", step.Address, err)
		}
		if !plannedEncodedValueMatches(step.Operation.Output.Value, current) {
			return fmt.Errorf(
				"output %s changed since the plan was computed",
				step.Address,
			)
		}
		name := strings.TrimPrefix(step.Address, "output.")
		fields[name] = current
		if step.Operation.Output.Sensitive {
			sensitivePaths = append(sensitivePaths, "/"+name)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	slices.Sort(sensitivePaths)
	outputs, err := ObjectValue(fields)
	if err != nil {
		return fmt.Errorf("construct outputs: %w", err)
	}
	if err := applyState.persistOutputs(ctx, outputs, sensitivePaths); err != nil {
		return fmt.Errorf("persist outputs: %w", err)
	}
	return nil
}
