package runtime

import (
	"context"
	"fmt"
	"slices"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
)

type dataSourcePlanningRequest struct {
	Address   string
	DependsOn []string
	Desired   *PlannedDataSourceTarget
	Prior     *DataSourceStatePayload
}

type dataSourcePlanningCallbacks struct {
	Read func(context.Context) (EncodedValue, error)
}

func planDataSourceOperation(
	ctx context.Context,
	request dataSourcePlanningRequest,
	callbacks dataSourcePlanningCallbacks,
) (*DataSourcePlanOperation, error) {
	if ctx == nil {
		return nil, fmt.Errorf("data-source planning context is required")
	}
	if request.Desired == nil && request.Prior == nil {
		return nil, nil
	}

	operation := DataSourcePlanOperation{}
	if request.Desired != nil {
		desired := clonePlannedDataSourceTarget(*request.Desired)
		if err := desired.Validate(); err != nil {
			return nil, fmt.Errorf("desired target: %w", err)
		}
		operation.Desired = &desired
	}
	if request.Prior != nil {
		prior := cloneDataSourceStatePayload(*request.Prior)
		if err := prior.Validate(); err != nil {
			return nil, fmt.Errorf("prior state: %w", err)
		}
		operation.Prior = &prior
	}

	if operation.Desired == nil {
		operation.Decision = DecisionDestroy
	} else {
		operation.Decision = DecisionRead
		concrete := !operation.Desired.Inputs.HasPending() &&
			operation.Desired.Configuration.Kind == PlannedConfigurationConcrete
		if concrete {
			if callbacks.Read == nil {
				return nil, fmt.Errorf("data-source read callback is required")
			}
			outputs, err := guard(
				"reading this data source during planning",
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
			operation.ObservedOutputs = &outputs
		}
	}
	if err := operation.Validate(); err != nil {
		return nil, fmt.Errorf("data-source operation: %w", err)
	}
	return &operation, nil
}

func clonePlannedDataSourceTarget(target PlannedDataSourceTarget) PlannedDataSourceTarget {
	result := target
	result.Configuration.PendingRefs = slices.Clone(target.Configuration.PendingRefs)
	if target.Configuration.Record != nil {
		record := internalconfig.Clone(*target.Configuration.Record)
		result.Configuration.Record = &record
	}
	result.SensitiveInputPaths = slices.Clone(target.SensitiveInputPaths)
	result.SensitiveOutputPaths = slices.Clone(target.SensitiveOutputPaths)
	return result
}

func cloneDataSourceStatePayload(target DataSourceStatePayload) DataSourceStatePayload {
	result := target
	result.Configuration = internalconfig.Clone(target.Configuration)
	result.DependsOn = slices.Clone(target.DependsOn)
	result.SensitiveInputPaths = slices.Clone(target.SensitiveInputPaths)
	result.SensitiveOutputPaths = slices.Clone(target.SensitiveOutputPaths)
	return result
}
