package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanActionStepBuildsCompleteStep(t *testing.T) {
	desired := validPlannedActionTarget(t)
	dependencies := []string{"resource.network"}

	step, err := planActionStep(actionPlanningRequest{
		Address:   "action.notify",
		DependsOn: dependencies,
		Desired:   &desired,
	})
	require.NoError(t, err)
	require.Equal(t, &PlanStepV2{
		Address:   "action.notify",
		Kind:      NodeAction,
		DependsOn: []string{"resource.network"},
		Operation: StepOperation{
			Kind: StepAction,
			Action: &ActionPlanOperation{
				Decision: DecisionRerun,
				Desired:  &desired,
			},
		},
	}, step)

	dependencies[0] = "resource.changed"
	require.Equal(t, []string{"resource.network"}, step.DependsOn)
}

func TestPlanDataSourceStepBuildsCompleteStep(t *testing.T) {
	desired := validPlannedDataSourceTarget(t)
	outputs := operationObject(t, map[string]EncodedValue{
		"id": StringValue("ami-1"),
	})
	dependencies := []string{"resource.network"}

	step, err := planDataSourceStep(
		context.Background(),
		dataSourcePlanningRequest{
			Address:   "data-source.image",
			DependsOn: dependencies,
			Desired:   &desired,
		},
		dataSourcePlanningCallbacks{
			Read: func(context.Context) (EncodedValue, error) {
				return outputs, nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, &PlanStepV2{
		Address:   "data-source.image",
		Kind:      NodeDataSource,
		DependsOn: []string{"resource.network"},
		Operation: StepOperation{
			Kind: StepDataSource,
			DataSource: &DataSourcePlanOperation{
				Decision:        DecisionRead,
				Desired:         &desired,
				ObservedOutputs: &outputs,
			},
		},
	}, step)

	dependencies[0] = "resource.changed"
	require.Equal(t, []string{"resource.network"}, step.DependsOn)
}

func TestPlanLibraryConfigurationStepBuildsCompleteStep(t *testing.T) {
	inputs := operationObject(t, map[string]EncodedValue{
		"region": StringValue("east"),
	})
	result := validConfigurationRecord(t)
	dependencies := []string{"resource.network"}

	step, err := planLibraryConfigurationStep(
		context.Background(),
		libraryConfigurationPlanningRequest{
			Address:   "library-config.cloud",
			DependsOn: dependencies,
			Inputs:    inputs,
		},
		libraryConfigurationPlanningCallbacks{
			Eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
				return result, nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, &PlanStepV2{
		Address:   "library-config.cloud",
		Kind:      NodeLibraryConfiguration,
		DependsOn: []string{"resource.network"},
		Operation: StepOperation{
			Kind: StepLibraryConfiguration,
			LibraryConfiguration: &LibraryConfigurationPlanOperation{
				Decision: DecisionEval,
				Inputs:   inputs,
				Result: PlannedConfiguration{
					Kind:   PlannedConfigurationConcrete,
					Record: &result,
				},
			},
		},
	}, step)

	dependencies[0] = "resource.changed"
	require.Equal(t, []string{"resource.network"}, step.DependsOn)
}

func TestPlanCompositeStepBuildsEveryCategory(t *testing.T) {
	tests := []struct {
		category NodeKind
		address  string
	}{
		{category: NodeResource, address: "resource.application"},
		{category: NodeAction, address: "action.application"},
		{category: NodeDataSource, address: "data-source.application"},
	}

	for _, test := range tests {
		t.Run(string(test.category), func(t *testing.T) {
			desired := validPlannedCompositeTarget(t, test.category)
			dependencies := []string{"resource.network"}

			step, err := planCompositeStep(compositePlanningRequest{
				Address:   test.address,
				DependsOn: dependencies,
				Category:  test.category,
				Desired:   &desired,
			})
			require.NoError(t, err)
			require.Equal(t, &PlanStepV2{
				Address:   test.address,
				Kind:      test.category,
				DependsOn: []string{"resource.network"},
				Operation: StepOperation{
					Kind: StepComposite,
					Composite: &CompositePlanOperation{
						Decision: DecisionEval,
						Desired:  &desired,
					},
				},
			}, step)

			dependencies[0] = "resource.changed"
			require.Equal(t, []string{"resource.network"}, step.DependsOn)
		})
	}
}

func TestPlanOutputStepBuildsCompleteStep(t *testing.T) {
	value := operationObject(t, map[string]EncodedValue{
		"endpoint": StringValue("https://example.com"),
	})
	dependencies := []string{"resource.application"}

	step, err := planOutputStep(outputPlanningRequest{
		Address:   "output.endpoint",
		DependsOn: dependencies,
		Value:     value,
		Sensitive: true,
	})
	require.NoError(t, err)
	require.Equal(t, &PlanStepV2{
		Address:   "output.endpoint",
		Kind:      NodeOutput,
		DependsOn: []string{"resource.application"},
		Operation: StepOperation{
			Kind: StepOutput,
			Output: &OutputPlanOperation{
				Decision:  DecisionEval,
				Value:     value,
				Sensitive: true,
			},
		},
	}, step)

	dependencies[0] = "resource.changed"
	require.Equal(t, []string{"resource.application"}, step.DependsOn)
}

func TestPlanStatefulStepsOmitAbsentNodes(t *testing.T) {
	action, err := planActionStep(actionPlanningRequest{})
	require.NoError(t, err)
	require.Nil(t, action)

	dataSource, err := planDataSourceStep(
		context.Background(),
		dataSourcePlanningRequest{},
		dataSourcePlanningCallbacks{},
	)
	require.NoError(t, err)
	require.Nil(t, dataSource)

	composite, err := planCompositeStep(compositePlanningRequest{})
	require.NoError(t, err)
	require.Nil(t, composite)
}

func TestPlanStatefulStepsBuildDestroyOperations(t *testing.T) {
	actionPrior := operationActionState(t)
	action, err := planActionStep(actionPlanningRequest{
		Address:   "action.notify",
		DependsOn: []string{},
		Prior:     &actionPrior,
	})
	require.NoError(t, err)
	require.Equal(t, DecisionDestroy, action.Operation.Action.Decision)
	require.Equal(t, &actionPrior, action.Operation.Action.Prior)

	dataSourcePrior := operationDataSourceState(t)
	dataSource, err := planDataSourceStep(
		context.Background(),
		dataSourcePlanningRequest{
			Address:   "data-source.image",
			DependsOn: []string{},
			Prior:     &dataSourcePrior,
		},
		dataSourcePlanningCallbacks{
			Read: func(context.Context) (EncodedValue, error) {
				require.FailNow(t, "destroy must not read the data source")
				return EncodedValue{}, nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, DecisionDestroy, dataSource.Operation.DataSource.Decision)
	require.Equal(t, &dataSourcePrior, dataSource.Operation.DataSource.Prior)

	compositePrior := operationCompositeState(t, NodeAction)
	composite, err := planCompositeStep(compositePlanningRequest{
		Address:   "action.application",
		DependsOn: []string{},
		Category:  NodeAction,
		Prior:     &compositePrior,
	})
	require.NoError(t, err)
	require.Equal(t, DecisionDestroy, composite.Operation.Composite.Decision)
	require.Equal(t, &compositePrior, composite.Operation.Composite.Prior)
}

func TestPlanStepsRejectInvalidMetadataBeforeCallbacks(t *testing.T) {
	actionDesired := validPlannedActionTarget(t)
	dataSourceDesired := validPlannedDataSourceTarget(t)
	compositeDesired := validPlannedCompositeTarget(t, NodeResource)
	inputs := operationObject(t, map[string]EncodedValue{
		"region": StringValue("east"),
	})
	output := operationObject(t, map[string]EncodedValue{
		"endpoint": StringValue("https://example.com"),
	})

	tests := []struct {
		name    string
		plan    func(*bool) (*PlanStepV2, error)
		message string
	}{
		{
			name: "action address",
			plan: func(*bool) (*PlanStepV2, error) {
				return planActionStep(actionPlanningRequest{
					Address:   "resource.notify",
					DependsOn: []string{},
					Desired:   &actionDesired,
				})
			},
			message: "address category resource does not match action",
		},
		{
			name: "data-source dependencies",
			plan: func(called *bool) (*PlanStepV2, error) {
				return planDataSourceStep(
					context.Background(),
					dataSourcePlanningRequest{
						Address: "data-source.image",
						DependsOn: []string{
							"resource.z",
							"resource.a",
						},
						Desired: &dataSourceDesired,
					},
					dataSourcePlanningCallbacks{
						Read: func(context.Context) (EncodedValue, error) {
							*called = true
							return output, nil
						},
					},
				)
			},
			message: "dependencies must be unique and sorted",
		},
		{
			name: "library-configuration dependencies",
			plan: func(called *bool) (*PlanStepV2, error) {
				return planLibraryConfigurationStep(
					context.Background(),
					libraryConfigurationPlanningRequest{
						Address:   "library-config.cloud",
						DependsOn: nil,
						Inputs:    inputs,
					},
					libraryConfigurationPlanningCallbacks{
						Eval: func(
							context.Context,
							EncodedValue,
						) (ConfigurationRecord, error) {
							*called = true
							return validConfigurationRecord(t), nil
						},
					},
				)
			},
			message: "dependencies are required",
		},
		{
			name: "composite address",
			plan: func(*bool) (*PlanStepV2, error) {
				return planCompositeStep(compositePlanningRequest{
					Address:   "action.application",
					DependsOn: []string{},
					Category:  NodeResource,
					Desired:   &compositeDesired,
				})
			},
			message: "address category action does not match resource",
		},
		{
			name: "output address",
			plan: func(*bool) (*PlanStepV2, error) {
				return planOutputStep(outputPlanningRequest{
					Address:   "resource.endpoint",
					DependsOn: []string{},
					Value:     output,
				})
			},
			message: "output address is invalid",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			step, err := test.plan(&called)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, step)
			require.False(t, called)
		})
	}
}
