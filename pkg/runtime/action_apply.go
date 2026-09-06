package runtime

import (
	"context"
	"fmt"
	"slices"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
)

type actionApplyRequest struct {
	Address   string
	Operation ActionPlanOperation
	Desired   *PlannedActionTarget
	Prior     *ActionStatePayload
	DependsOn []string
}

type actionApplyCallbacks struct {
	Run     func(context.Context) (EncodedValue, error)
	Persist func(context.Context, *ActionStatePayload) error
}

func applyActionOperation(
	ctx context.Context,
	request actionApplyRequest,
	callbacks actionApplyCallbacks,
) (*ActionStatePayload, error) {
	if ctx == nil {
		return nil, fmt.Errorf("apply context is required")
	}
	if err := validateNodeAddress(request.Address, NodeAction); err != nil {
		return nil, err
	}
	if err := request.Operation.Validate(); err != nil {
		return nil, fmt.Errorf("saved action operation: %w", err)
	}
	if err := validateActionApplyPremises(request); err != nil {
		return nil, err
	}
	if err := validateActionApplyCallbacks(request.Operation, callbacks); err != nil {
		return nil, err
	}

	switch request.Operation.Decision {
	case DecisionRerun:
		outputs, err := guard(
			"running this action",
			false,
			func() (EncodedValue, error) {
				return callbacks.Run(ctx)
			},
		)
		if err != nil {
			return nil, fmt.Errorf("run action: %w", err)
		}
		if err := validatePlanObject(outputs, "action outputs", false); err != nil {
			return nil, err
		}
		return persistNewActionApplyTarget(ctx, request, callbacks.Persist, outputs)
	case DecisionSkip:
		return persistNewActionApplyTarget(
			ctx,
			request,
			callbacks.Persist,
			request.Prior.Outputs,
		)
	case DecisionDestroy:
		if err := persistActionApplyTarget(ctx, callbacks.Persist, nil); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, fmt.Errorf(
			"unsupported action decision %q",
			request.Operation.Decision,
		)
	}
}

func validateActionApplyPremises(request actionApplyRequest) error {
	if err := validatePlanDependencies(request.DependsOn); err != nil {
		return err
	}
	if request.Operation.Desired == nil {
		if request.Desired != nil {
			if request.Operation.Decision == DecisionDestroy {
				return fmt.Errorf("saved destroy requires the action to remain absent")
			}
			return fmt.Errorf("saved operation forbids a desired action")
		}
	} else {
		if request.Desired == nil {
			return fmt.Errorf("saved operation requires a desired action")
		}
		if err := validateActionApplyDesired(
			*request.Operation.Desired,
			*request.Desired,
		); err != nil {
			return err
		}
	}

	if request.Operation.Prior == nil {
		if request.Prior != nil {
			return fmt.Errorf("saved operation forbids prior action state")
		}
		return nil
	}
	if request.Prior == nil {
		return fmt.Errorf("saved operation requires prior action state")
	}
	if !sameActionStatePayload(*request.Operation.Prior, *request.Prior) {
		return fmt.Errorf("prior action state does not match the saved plan")
	}
	return nil
}

func validateActionApplyDesired(
	planned PlannedActionTarget,
	current PlannedActionTarget,
) error {
	if err := current.Validate(); err != nil {
		return fmt.Errorf("current desired action: %w", err)
	}
	if current.Inputs.HasPending() {
		return fmt.Errorf("desired inputs did not resolve before apply")
	}
	if current.Configuration.Kind != PlannedConfigurationConcrete {
		return fmt.Errorf("desired configuration did not resolve before apply")
	}
	if planned.Binding != current.Binding {
		return fmt.Errorf("desired binding does not match the saved plan")
	}
	if !plannedEncodedValueMatches(planned.Inputs, current.Inputs) {
		return fmt.Errorf("desired inputs do not match the saved plan")
	}
	if planned.TriggerHash != "" && planned.TriggerHash != current.TriggerHash {
		return fmt.Errorf("trigger hash does not match the saved plan")
	}
	if !slices.Equal(planned.SensitiveInputPaths, current.SensitiveInputPaths) {
		return fmt.Errorf("sensitive input paths do not match the saved plan")
	}
	if !slices.Equal(planned.SensitiveOutputPaths, current.SensitiveOutputPaths) {
		return fmt.Errorf("sensitive output paths do not match the saved plan")
	}
	if planned.Configuration.Kind == PlannedConfigurationConcrete &&
		!sameConfigurationRecord(
			*planned.Configuration.Record,
			*current.Configuration.Record,
		) {
		return fmt.Errorf("desired configuration does not match the saved plan")
	}
	return nil
}

func validateActionApplyCallbacks(
	operation ActionPlanOperation,
	callbacks actionApplyCallbacks,
) error {
	if callbacks.Persist == nil {
		return fmt.Errorf("action state persistence callback is required")
	}
	if operation.Decision == DecisionRerun && callbacks.Run == nil {
		return fmt.Errorf("action run callback is required")
	}
	return nil
}

func persistNewActionApplyTarget(
	ctx context.Context,
	request actionApplyRequest,
	persist func(context.Context, *ActionStatePayload) error,
	outputs EncodedValue,
) (*ActionStatePayload, error) {
	target := ActionStatePayload{
		Binding:              request.Desired.Binding,
		Inputs:               request.Desired.Inputs,
		Outputs:              outputs,
		Configuration:        internalconfig.Clone(*request.Desired.Configuration.Record),
		TriggerHash:          request.Desired.TriggerHash,
		DependsOn:            slices.Clone(request.DependsOn),
		SensitiveInputPaths:  slices.Clone(request.Desired.SensitiveInputPaths),
		SensitiveOutputPaths: slices.Clone(request.Desired.SensitiveOutputPaths),
	}
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("next action state: %w", err)
	}
	if err := persistActionApplyTarget(ctx, persist, &target); err != nil {
		return nil, err
	}
	return &target, nil
}

func persistActionApplyTarget(
	ctx context.Context,
	persist func(context.Context, *ActionStatePayload) error,
	target *ActionStatePayload,
) error {
	if err := persist(ctx, target); err != nil {
		return fmt.Errorf("persist action state: %w", err)
	}
	return nil
}

func sameActionStatePayload(a, b ActionStatePayload) bool {
	return a.Binding == b.Binding &&
		encodedValuesEqual(a.Inputs, b.Inputs) &&
		encodedValuesEqual(a.Outputs, b.Outputs) &&
		sameConfigurationRecord(a.Configuration, b.Configuration) &&
		a.TriggerHash == b.TriggerHash &&
		slices.Equal(a.DependsOn, b.DependsOn) &&
		slices.Equal(a.SensitiveInputPaths, b.SensitiveInputPaths) &&
		slices.Equal(a.SensitiveOutputPaths, b.SensitiveOutputPaths)
}
