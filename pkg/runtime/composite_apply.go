package runtime

import (
	"context"
	"fmt"
	"slices"
)

type compositeApplyRequest struct {
	Address   string
	Category  NodeKind
	Operation CompositePlanOperation
	Desired   *PlannedCompositeTarget
	Prior     *CompositeStatePayload
	DependsOn []string
}

type compositeApplyCallbacks struct {
	Eval    func(context.Context) (EncodedValue, error)
	Persist func(context.Context, *CompositeStatePayload) error
}

func applyCompositeOperation(
	ctx context.Context,
	request compositeApplyRequest,
	callbacks compositeApplyCallbacks,
) (*CompositeStatePayload, error) {
	if ctx == nil {
		return nil, fmt.Errorf("apply context is required")
	}
	if err := validateNodeAddress(request.Address, request.Category); err != nil {
		return nil, err
	}
	if err := request.Operation.Validate(request.Category); err != nil {
		return nil, fmt.Errorf("saved composite operation: %w", err)
	}
	if err := validateCompositeApplyPremises(request); err != nil {
		return nil, err
	}
	if err := validateCompositeApplyCallbacks(request.Operation, callbacks); err != nil {
		return nil, err
	}

	switch request.Operation.Decision {
	case DecisionEval:
		outputs, err := guard(
			"evaluating this composite",
			false,
			func() (EncodedValue, error) {
				return callbacks.Eval(ctx)
			},
		)
		if err != nil {
			return nil, fmt.Errorf("evaluate composite: %w", err)
		}
		if err := validatePlanObject(outputs, "composite outputs", false); err != nil {
			return nil, err
		}
		return persistNewCompositeApplyTarget(ctx, request, callbacks.Persist, outputs)
	case DecisionDestroy:
		if err := persistCompositeApplyTarget(ctx, callbacks.Persist, nil); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, fmt.Errorf(
			"unsupported composite decision %q",
			request.Operation.Decision,
		)
	}
}

func validateCompositeApplyPremises(request compositeApplyRequest) error {
	if err := validatePlanDependencies(request.DependsOn); err != nil {
		return err
	}
	if request.Operation.Desired == nil {
		if request.Desired != nil {
			if request.Operation.Decision == DecisionDestroy {
				return fmt.Errorf("saved destroy requires the composite to remain absent")
			}
			return fmt.Errorf("saved operation forbids a desired composite")
		}
	} else {
		if request.Desired == nil {
			return fmt.Errorf("saved operation requires a desired composite")
		}
		if err := validateCompositeApplyDesired(
			*request.Operation.Desired,
			*request.Desired,
		); err != nil {
			return err
		}
	}

	if request.Operation.Prior == nil {
		if request.Prior != nil {
			return fmt.Errorf("saved operation forbids prior composite state")
		}
		return nil
	}
	if request.Prior == nil {
		return fmt.Errorf("saved operation requires prior composite state")
	}
	if !sameCompositeStatePayload(*request.Operation.Prior, *request.Prior) {
		return fmt.Errorf("prior composite state does not match the saved plan")
	}
	return nil
}

func validateCompositeApplyDesired(
	planned PlannedCompositeTarget,
	current PlannedCompositeTarget,
) error {
	if err := current.Validate(); err != nil {
		return fmt.Errorf("current desired composite: %w", err)
	}
	if current.Inputs.HasPending() {
		return fmt.Errorf("desired inputs did not resolve before apply")
	}
	if planned.Category != current.Category {
		return fmt.Errorf("desired category does not match the saved plan")
	}
	if planned.Binding != current.Binding {
		return fmt.Errorf("desired binding does not match the saved plan")
	}
	if !plannedEncodedValueMatches(planned.Inputs, current.Inputs) {
		return fmt.Errorf("desired inputs do not match the saved plan")
	}
	if !slices.Equal(planned.SensitiveInputPaths, current.SensitiveInputPaths) {
		return fmt.Errorf("sensitive input paths do not match the saved plan")
	}
	if !slices.Equal(planned.SensitiveOutputPaths, current.SensitiveOutputPaths) {
		return fmt.Errorf("sensitive output paths do not match the saved plan")
	}
	return nil
}

func validateCompositeApplyCallbacks(
	operation CompositePlanOperation,
	callbacks compositeApplyCallbacks,
) error {
	if callbacks.Persist == nil {
		return fmt.Errorf("composite state persistence callback is required")
	}
	if operation.Decision == DecisionEval && callbacks.Eval == nil {
		return fmt.Errorf("composite eval callback is required")
	}
	return nil
}

func persistNewCompositeApplyTarget(
	ctx context.Context,
	request compositeApplyRequest,
	persist func(context.Context, *CompositeStatePayload) error,
	outputs EncodedValue,
) (*CompositeStatePayload, error) {
	target := CompositeStatePayload{
		Category:             string(request.Desired.Category),
		Binding:              request.Desired.Binding,
		Inputs:               request.Desired.Inputs,
		Outputs:              outputs,
		DependsOn:            slices.Clone(request.DependsOn),
		SensitiveInputPaths:  slices.Clone(request.Desired.SensitiveInputPaths),
		SensitiveOutputPaths: slices.Clone(request.Desired.SensitiveOutputPaths),
	}
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("next composite state: %w", err)
	}
	if err := persistCompositeApplyTarget(ctx, persist, &target); err != nil {
		return nil, err
	}
	return &target, nil
}

func persistCompositeApplyTarget(
	ctx context.Context,
	persist func(context.Context, *CompositeStatePayload) error,
	target *CompositeStatePayload,
) error {
	if err := persist(ctx, target); err != nil {
		return fmt.Errorf("persist composite state: %w", err)
	}
	return nil
}

func sameCompositeStatePayload(a, b CompositeStatePayload) bool {
	return a.Category == b.Category &&
		a.Binding == b.Binding &&
		encodedValuesEqual(a.Inputs, b.Inputs) &&
		encodedValuesEqual(a.Outputs, b.Outputs) &&
		slices.Equal(a.DependsOn, b.DependsOn) &&
		slices.Equal(a.SensitiveInputPaths, b.SensitiveInputPaths) &&
		slices.Equal(a.SensitiveOutputPaths, b.SensitiveOutputPaths)
}
