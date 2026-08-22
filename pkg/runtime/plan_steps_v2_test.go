package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanStepsV2ReevaluatesCompleteSet(t *testing.T) {
	definition := registeredPlanningDefinition(1, IdentityConfiguration)
	capture := &registeredPlanningCapture{}
	registration := newRegisteredPlanningResource(t, definition, capture, "current")
	binding := Binding{LibraryPath: "example.com/current", Export: "bucket"}
	configurationDefinition, configuration := registeredOperationConfiguration(
		t,
		binding.LibraryPath,
		"current",
	)
	prior := registeredPlanningTarget(
		t,
		definition,
		registration,
		binding,
		configuration,
		"logs",
		1,
		&registeredPlanningOutput{ID: "logs-1", Value: "logs"},
	)
	existing := registeredPlanningDesired(
		t,
		registration,
		binding,
		configuration,
		"logs",
		1,
	)
	created := registeredPlanningDesired(
		t,
		registration,
		binding,
		configuration,
		"archive",
		1,
	)
	existingRequest := registeredResourcePlanningRequest{
		Address:             "resource.logs",
		DependsOn:           []string{"library-config.cloud"},
		Desired:             &existing,
		DesiredConfigType:   configurationDefinition,
		DesiredRegistration: registration,
		Prior:               &prior,
		PriorConfigType:     configurationDefinition,
		PriorRegistration:   registration,
	}
	createdRequest := registeredResourcePlanningRequest{
		Address:             "resource.archive",
		DependsOn:           []string{"resource.logs"},
		Desired:             &created,
		DesiredConfigType:   configurationDefinition,
		DesiredRegistration: registration,
	}
	actionDesired := validPlannedActionTarget(t)
	dataSourceDesired := validPlannedDataSourceTarget(t)
	compositeDesired := validPlannedCompositeTarget(t, NodeResource)
	configurationInputs := operationObject(t, map[string]EncodedValue{
		"region": StringValue("east"),
	})
	configurationResult := validConfigurationRecord(t)
	outputDependencies := []string{"resource.application"}
	evaluations := 0
	dataSourceReads := 0
	configurationEvals := 0

	steps, err := planStepsV2(
		context.Background(),
		func(*planningPassState) ([]planStepV2Request, error) {
			evaluations++
			var observedDataSourceOutputs EncodedValue
			return []planStepV2Request{
				{
					Address:   "library-config.cloud",
					Kind:      NodeLibraryConfiguration,
					DependsOn: []string{},
					Plan: func(ctx context.Context, _ *planningPassState) (*PlanStepV2, error) {
						return planLibraryConfigurationStep(
							ctx,
							libraryConfigurationPlanningRequest{
								Address:   "library-config.cloud",
								DependsOn: []string{},
								Inputs:    configurationInputs,
							},
							libraryConfigurationPlanningCallbacks{
								Eval: func(
									context.Context,
									EncodedValue,
								) (ConfigurationRecord, error) {
									configurationEvals++
									return configurationResult, nil
								},
							},
						)
					},
				},
				{
					Address:   existingRequest.Address,
					Kind:      NodeResource,
					DependsOn: existingRequest.DependsOn,
					Plan: func(
						ctx context.Context,
						pass *planningPassState,
					) (*PlanStepV2, error) {
						return planRegisteredResourceStep(ctx, pass, existingRequest)
					},
				},
				{
					Address:   createdRequest.Address,
					Kind:      NodeResource,
					DependsOn: createdRequest.DependsOn,
					Plan: func(
						ctx context.Context,
						pass *planningPassState,
					) (*PlanStepV2, error) {
						return planRegisteredResourceStep(ctx, pass, createdRequest)
					},
				},
				{
					Address:   "data-source.image",
					Kind:      NodeDataSource,
					DependsOn: []string{"resource.archive"},
					Plan: func(ctx context.Context, _ *planningPassState) (*PlanStepV2, error) {
						outputs := operationObject(t, map[string]EncodedValue{
							"pass": IntegerValue(int64(evaluations)),
						})
						step, err := planDataSourceStep(
							ctx,
							dataSourcePlanningRequest{
								Address:   "data-source.image",
								DependsOn: []string{"resource.archive"},
								Desired:   &dataSourceDesired,
							},
							dataSourcePlanningCallbacks{
								Read: func(context.Context) (EncodedValue, error) {
									dataSourceReads++
									return outputs, nil
								},
							},
						)
						if err == nil {
							observedDataSourceOutputs = *step.Operation.DataSource.ObservedOutputs
						}
						return step, err
					},
				},
				{
					Address:   "action.notify",
					Kind:      NodeAction,
					DependsOn: []string{"data-source.image"},
					Plan: func(context.Context, *planningPassState) (*PlanStepV2, error) {
						return planActionStep(actionPlanningRequest{
							Address:   "action.notify",
							DependsOn: []string{"data-source.image"},
							Desired:   &actionDesired,
						})
					},
				},
				{
					Address:   "resource.application",
					Kind:      NodeResource,
					DependsOn: []string{"action.notify"},
					Plan: func(context.Context, *planningPassState) (*PlanStepV2, error) {
						return planCompositeStep(compositePlanningRequest{
							Address:   "resource.application",
							DependsOn: []string{"action.notify"},
							Category:  NodeResource,
							Desired:   &compositeDesired,
						})
					},
				},
				{
					Address:   "output.result",
					Kind:      NodeOutput,
					DependsOn: outputDependencies,
					Plan: func(context.Context, *planningPassState) (*PlanStepV2, error) {
						return planOutputStep(outputPlanningRequest{
							Address:   "output.result",
							DependsOn: outputDependencies,
							Value:     observedDataSourceOutputs,
						})
					},
				},
			}, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 2, evaluations)
	require.Equal(t, []string{"current:current"}, capture.reads)
	require.Equal(t, 2, dataSourceReads)
	require.Equal(t, 2, configurationEvals)
	require.Equal(t, []string{
		"library-config.cloud",
		"resource.logs",
		"resource.archive",
		"data-source.image",
		"action.notify",
		"resource.application",
		"output.result",
	}, planStepV2Addresses(steps))
	require.Equal(t, []StepOperationKind{
		StepLibraryConfiguration,
		StepResource,
		StepResource,
		StepDataSource,
		StepAction,
		StepComposite,
		StepOutput,
	}, planStepV2OperationKinds(steps))
	require.Equal(t, DecisionNoOp, steps[1].Operation.Resource.Decision)
	require.Equal(t, DecisionCreate, steps[2].Operation.Resource.Decision)
	require.Equal(t, operationObject(t, map[string]EncodedValue{
		"pass": IntegerValue(2),
	}), steps[6].Operation.Output.Value)

	outputDependencies[0] = "resource.changed"
	require.Equal(t, []string{"resource.application"}, steps[6].DependsOn)
}

func TestPlanStepsV2ReturnsRequiredEmptySet(t *testing.T) {
	evaluations := 0
	steps, err := planStepsV2(
		context.Background(),
		func(*planningPassState) ([]planStepV2Request, error) {
			evaluations++
			return nil, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, evaluations)
	require.NotNil(t, steps)
	require.Empty(t, steps)
}

func TestPlanStepsV2RejectsInvalidRequestSetBeforePlanners(t *testing.T) {
	dataSourceDesired := validPlannedDataSourceTarget(t)
	actionDesired := validPlannedActionTarget(t)
	compositeDesired := validPlannedCompositeTarget(t, NodeAction)

	tests := []struct {
		name     string
		requests func(*bool) []planStepV2Request
		message  string
	}{
		{
			name: "invalid dependencies",
			requests: func(called *bool) []planStepV2Request {
				return []planStepV2Request{
					dataSourcePlanStepV2Request(t, &dataSourceDesired, called),
					{
						Address: "output.result",
						Kind:    NodeOutput,
						Plan: func(context.Context, *planningPassState) (*PlanStepV2, error) {
							return planOutputStep(outputPlanningRequest{
								Address: "output.result",
								Value:   StringValue("ok"),
							})
						},
					},
				}
			},
			message: "dependencies are required",
		},
		{
			name: "duplicate address",
			requests: func(called *bool) []planStepV2Request {
				return []planStepV2Request{
					dataSourcePlanStepV2Request(t, &dataSourceDesired, called),
					{
						Address:   "action.notify",
						Kind:      NodeAction,
						DependsOn: []string{},
						Plan: func(context.Context, *planningPassState) (*PlanStepV2, error) {
							return planActionStep(actionPlanningRequest{
								Address:   "action.notify",
								DependsOn: []string{},
								Desired:   &actionDesired,
							})
						},
					},
					{
						Address:   "action.notify",
						Kind:      NodeAction,
						DependsOn: []string{},
						Plan: func(context.Context, *planningPassState) (*PlanStepV2, error) {
							return planCompositeStep(compositePlanningRequest{
								Address:   "action.notify",
								DependsOn: []string{},
								Category:  NodeAction,
								Desired:   &compositeDesired,
							})
						},
					},
				}
			},
			message: `duplicate planning address "action.notify"`,
		},
		{
			name: "dependency cycle",
			requests: func(called *bool) []planStepV2Request {
				return []planStepV2Request{
					dataSourcePlanStepV2Request(t, &dataSourceDesired, called),
					actionPlanStepV2Request(
						"action.first",
						[]string{"action.second"},
						&actionDesired,
					),
					actionPlanStepV2Request(
						"action.second",
						[]string{"action.first"},
						&actionDesired,
					),
				}
			},
			message: "dependency cycle",
		},
		{
			name: "missing planner",
			requests: func(called *bool) []planStepV2Request {
				return []planStepV2Request{
					dataSourcePlanStepV2Request(t, &dataSourceDesired, called),
					{
						Address:   "action.notify",
						Kind:      NodeAction,
						DependsOn: []string{},
					},
				}
			},
			message: "step planner is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			steps, err := planStepsV2(
				context.Background(),
				func(*planningPassState) ([]planStepV2Request, error) {
					return test.requests(&called), nil
				},
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, steps)
			require.False(t, called)
		})
	}
}

func TestPlanStepsV2RejectsMismatchedPlannedStep(t *testing.T) {
	actionDesired := validPlannedActionTarget(t)
	steps, err := planStepsV2(
		context.Background(),
		func(*planningPassState) ([]planStepV2Request, error) {
			return []planStepV2Request{
				{
					Address:   "action.notify",
					Kind:      NodeAction,
					DependsOn: []string{},
					Plan: func(context.Context, *planningPassState) (*PlanStepV2, error) {
						return planActionStep(actionPlanningRequest{
							Address:   "action.other",
							DependsOn: []string{},
							Desired:   &actionDesired,
						})
					},
				},
			}, nil
		},
	)
	require.ErrorContains(t, err, `planned address "action.other" does not match request`)
	require.Nil(t, steps)
}

func TestPlanStepsV2RejectsInvalidSetup(t *testing.T) {
	called := false
	var missingContext context.Context
	steps, err := planStepsV2(
		missingContext,
		func(*planningPassState) ([]planStepV2Request, error) {
			called = true
			return nil, nil
		},
	)
	require.ErrorContains(t, err, "planning context is required")
	require.Nil(t, steps)
	require.False(t, called)

	steps, err = planStepsV2(context.Background(), nil)
	require.ErrorContains(t, err, "plan step evaluator is required")
	require.Nil(t, steps)

	expectedErr := errors.New("evaluation failed")
	steps, err = planStepsV2(
		context.Background(),
		func(*planningPassState) ([]planStepV2Request, error) {
			return nil, expectedErr
		},
	)
	require.ErrorIs(t, err, expectedErr)
	require.Nil(t, steps)
}

func dataSourcePlanStepV2Request(
	t *testing.T,
	desired *PlannedDataSourceTarget,
	called *bool,
) planStepV2Request {
	t.Helper()
	return planStepV2Request{
		Address:   "data-source.image",
		Kind:      NodeDataSource,
		DependsOn: []string{},
		Plan: func(ctx context.Context, _ *planningPassState) (*PlanStepV2, error) {
			return planDataSourceStep(
				ctx,
				dataSourcePlanningRequest{
					Address:   "data-source.image",
					DependsOn: []string{},
					Desired:   desired,
				},
				dataSourcePlanningCallbacks{
					Read: func(context.Context) (EncodedValue, error) {
						*called = true
						return operationObject(t, map[string]EncodedValue{
							"id": StringValue("ami-1"),
						}), nil
					},
				},
			)
		},
	}
}

func actionPlanStepV2Request(
	address string,
	dependencies []string,
	desired *PlannedActionTarget,
) planStepV2Request {
	return planStepV2Request{
		Address:   address,
		Kind:      NodeAction,
		DependsOn: dependencies,
		Plan: func(context.Context, *planningPassState) (*PlanStepV2, error) {
			return planActionStep(actionPlanningRequest{
				Address:   address,
				DependsOn: dependencies,
				Desired:   desired,
			})
		},
	}
}

func planStepV2Addresses(steps []PlanStepV2) []string {
	addresses := make([]string, len(steps))
	for i := range steps {
		addresses[i] = steps[i].Address
	}
	return addresses
}

func planStepV2OperationKinds(steps []PlanStepV2) []StepOperationKind {
	kinds := make([]StepOperationKind, len(steps))
	for i := range steps {
		kinds[i] = steps[i].Operation.Kind
	}
	return kinds
}
