package runtime

import (
	"context"
	"fmt"
	"slices"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
)

type dataSourceApplyRequest struct {
	Address   string
	Operation DataSourcePlanOperation
	Desired   *PlannedDataSourceTarget
	Prior     *DataSourceStatePayload
	DependsOn []string
}

type dataSourceApplyCallbacks struct {
	Read    func(context.Context) (EncodedValue, error)
	Persist func(context.Context, *DataSourceStatePayload) error
}

func applyDataSourceOperation(
	ctx context.Context,
	request dataSourceApplyRequest,
	callbacks dataSourceApplyCallbacks,
) (*DataSourceStatePayload, error) {
	if ctx == nil {
		return nil, fmt.Errorf("apply context is required")
	}
	if err := validateNodeAddress(request.Address, NodeDataSource); err != nil {
		return nil, err
	}
	if err := request.Operation.Validate(); err != nil {
		return nil, fmt.Errorf("saved data-source operation: %w", err)
	}
	if err := validateDataSourceApplyPremises(request); err != nil {
		return nil, err
	}
	if err := validateDataSourceApplyCallbacks(request.Operation, callbacks); err != nil {
		return nil, err
	}

	switch request.Operation.Decision {
	case DecisionRead:
		outputs, err := guard(
			"reading this data source",
			false,
			func() (EncodedValue, error) {
				return callbacks.Read(ctx)
			},
		)
		if err != nil {
			return nil, fmt.Errorf("read data source: %w", err)
		}
		if err := validatePlanObject(outputs, "data-source outputs", false); err != nil {
			return nil, err
		}
		if request.Operation.ObservedOutputs != nil &&
			!encodedValuesEqual(*request.Operation.ObservedOutputs, outputs) {
			return nil, fmt.Errorf(
				"data source outputs changed since the plan was computed",
			)
		}
		return persistNewDataSourceApplyTarget(
			ctx,
			request,
			callbacks.Persist,
			outputs,
		)
	case DecisionDestroy:
		if err := persistDataSourceApplyTarget(ctx, callbacks.Persist, nil); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, fmt.Errorf(
			"unsupported data-source decision %q",
			request.Operation.Decision,
		)
	}
}

func validateDataSourceApplyPremises(request dataSourceApplyRequest) error {
	if err := validatePlanDependencies(request.DependsOn); err != nil {
		return err
	}
	if request.Operation.Desired == nil {
		if request.Desired != nil {
			if request.Operation.Decision == DecisionDestroy {
				return fmt.Errorf("saved destroy requires the data source to remain absent")
			}
			return fmt.Errorf("saved operation forbids a desired data source")
		}
	} else {
		if request.Desired == nil {
			return fmt.Errorf("saved operation requires a desired data source")
		}
		if err := validateDataSourceApplyDesired(
			*request.Operation.Desired,
			*request.Desired,
		); err != nil {
			return err
		}
	}

	if request.Operation.Prior == nil {
		if request.Prior != nil {
			return fmt.Errorf("saved operation forbids prior data-source state")
		}
		return nil
	}
	if request.Prior == nil {
		return fmt.Errorf("saved operation requires prior data-source state")
	}
	if !sameDataSourceStatePayload(*request.Operation.Prior, *request.Prior) {
		return fmt.Errorf("prior data-source state does not match the saved plan")
	}
	return nil
}

func validateDataSourceApplyDesired(
	planned PlannedDataSourceTarget,
	current PlannedDataSourceTarget,
) error {
	if err := current.Validate(); err != nil {
		return fmt.Errorf("current desired data source: %w", err)
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

func validateDataSourceApplyCallbacks(
	operation DataSourcePlanOperation,
	callbacks dataSourceApplyCallbacks,
) error {
	if callbacks.Persist == nil {
		return fmt.Errorf("data-source state persistence callback is required")
	}
	if operation.Decision == DecisionRead && callbacks.Read == nil {
		return fmt.Errorf("data-source read callback is required")
	}
	return nil
}

func persistNewDataSourceApplyTarget(
	ctx context.Context,
	request dataSourceApplyRequest,
	persist func(context.Context, *DataSourceStatePayload) error,
	outputs EncodedValue,
) (*DataSourceStatePayload, error) {
	target := DataSourceStatePayload{
		Binding:              request.Desired.Binding,
		Inputs:               request.Desired.Inputs,
		Outputs:              outputs,
		Configuration:        internalconfig.Clone(*request.Desired.Configuration.Record),
		DependsOn:            slices.Clone(request.DependsOn),
		SensitiveInputPaths:  slices.Clone(request.Desired.SensitiveInputPaths),
		SensitiveOutputPaths: slices.Clone(request.Desired.SensitiveOutputPaths),
	}
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("next data-source state: %w", err)
	}
	if err := persistDataSourceApplyTarget(ctx, persist, &target); err != nil {
		return nil, err
	}
	return &target, nil
}

func persistDataSourceApplyTarget(
	ctx context.Context,
	persist func(context.Context, *DataSourceStatePayload) error,
	target *DataSourceStatePayload,
) error {
	if err := persist(ctx, target); err != nil {
		return fmt.Errorf("persist data-source state: %w", err)
	}
	return nil
}

func sameDataSourceStatePayload(a, b DataSourceStatePayload) bool {
	return a.Binding == b.Binding &&
		encodedValuesEqual(a.Inputs, b.Inputs) &&
		encodedValuesEqual(a.Outputs, b.Outputs) &&
		sameConfigurationRecord(a.Configuration, b.Configuration) &&
		slices.Equal(a.DependsOn, b.DependsOn) &&
		slices.Equal(a.SensitiveInputPaths, b.SensitiveInputPaths) &&
		slices.Equal(a.SensitiveOutputPaths, b.SensitiveOutputPaths)
}
