package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func planEvaluationV2ResourceStepRequest(
	t *testing.T,
	capture *registeredPlanningCapture,
) registeredResourcePlanningRequest {
	t.Helper()
	definition := registeredPlanningDefinition(1, IdentityConfiguration)
	registration := newRegisteredPlanningResource(t, definition, capture, "server")
	binding := Binding{LibraryPath: "example.com/cloud", Export: "server"}
	configurationType, configuration := registeredOperationConfiguration(
		t, binding.LibraryPath, "same",
	)
	prior := registeredPlanningTarget(
		t, definition, registration, binding, configuration, "server", 1,
		&registeredPlanningOutput{ID: "server-1", Value: "server"},
	)
	desired := registeredPlanningDesired(t, registration, binding, configuration, "server", 1)
	return registeredResourcePlanningRequest{
		Address: "resource.main", DependsOn: []string{},
		Desired: &desired, DesiredConfigType: configurationType, DesiredRegistration: registration,
		Prior: &prior, PriorConfigType: configurationType, PriorRegistration: registration,
	}
}

func TestPlanEvaluationV2ResourceStepPublishesOutputs(t *testing.T) {
	tests := []struct {
		name        string
		decision    Decision
		change      func(*testing.T, *registeredResourcePlanningRequest, *registeredPlanningCapture)
		invalidated bool
	}{
		{name: "unchanged", decision: DecisionNoOp},
		{name: "previously invalidated", decision: DecisionNoOp, invalidated: true},
		{
			name: "create", decision: DecisionCreate,
			change: func(
				_ *testing.T, request *registeredResourcePlanningRequest, _ *registeredPlanningCapture,
			) {
				request.Prior = nil
			},
		},
		{
			name: "update", decision: DecisionUpdate,
			change: func(
				t *testing.T, request *registeredResourcePlanningRequest, _ *registeredPlanningCapture,
			) {
				request.Desired.Inputs = operationObject(t, map[string]EncodedValue{
					"name": StringValue("server"), "size": IntegerValue(2),
				})
			},
		},
		{
			name: "replace", decision: DecisionReplace,
			change: func(
				t *testing.T, request *registeredResourcePlanningRequest, _ *registeredPlanningCapture,
			) {
				request.Desired.Inputs = operationObject(t, map[string]EncodedValue{
					"name": StringValue("renamed"), "size": IntegerValue(1),
				})
			},
		},
		{
			name: "destroy", decision: DecisionDestroy,
			change: func(
				_ *testing.T, request *registeredResourcePlanningRequest, _ *registeredPlanningCapture,
			) {
				request.Desired = nil
			},
		},
		{
			name: "remote missing", decision: DecisionCreate,
			change: func(
				_ *testing.T, _ *registeredResourcePlanningRequest, capture *registeredPlanningCapture,
			) {
				capture.read = func(string, *recordedConfiguration, *registeredPlanningOutput) (
					*registeredPlanningOutput, error,
				) {
					return nil, ErrNotFound
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capture := &registeredPlanningCapture{}
			request := planEvaluationV2ResourceStepRequest(t, capture)
			snapshot := newPlanEvaluationV2Snapshot(t)
			addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
				Address: request.Address, Kind: state.StateResource,
				Payload: state.StatePayload{
					Kind: state.StateResource, Resource: &state.ResourceStatePayload{Target: *request.Prior},
				},
			})
			original, err := snapshot.Clone()
			require.NoError(t, err)
			executor := &Executor{DAG: newDAG(map[string][]string{request.Address: nil})}
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			if test.invalidated {
				require.NoError(t, pass.invalidateOutputs(request.Address))
			}
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), snapshot, pass,
			)
			require.NoError(t, err)
			evaluation.run.eval.Resources["main"] = map[string]any{"stale": "discard"}
			if test.change != nil {
				test.change(t, &request, capture)
			}

			step, err := executor.planEvaluationV2ResourceStep(
				context.Background(), evaluation, pass, request,
			)
			require.NoError(t, err)
			require.NoError(t, step.Validate())
			require.Equal(t, test.decision, step.Operation.Resource.Decision)
			want := map[string]any{}
			if test.decision == DecisionNoOp && !test.invalidated {
				want["main"] = map[string]any{"id": "server-1", "value": "server"}
			} else {
				_, err := Eval(parseValue(t, "resource.main.id"), evaluation.run.eval)
				require.ErrorIs(t, err, ErrEvalNotFound)
				_, err = Eval(parseValue(t, "resource.main"), evaluation.run.eval)
				require.ErrorIs(t, err, ErrEvalNotFound)
			}
			require.Equal(t, want, evaluation.run.eval.Resources)
			require.Equal(t, original, snapshot)
			require.Equal(t, original, evaluation.prior)
			if request.Prior == nil {
				require.Empty(t, capture.reads)
			} else {
				require.Equal(t, []string{"server:same"}, capture.reads)
			}
			if outputs, ok := evaluation.run.eval.Resources["main"].(map[string]any); ok {
				outputs["id"] = "changed"
			}
			if step.Operation.Resource.Observation != nil &&
				step.Operation.Resource.Observation.Outputs != nil {
				fields, _ := step.Operation.Resource.Observation.Outputs.ObjectFields()
				require.Equal(t, StringValue("server-1"), fields["id"])
			}
		})
	}
}

func TestPlanEvaluationV2ResourceStepInvalidatesTransitiveDependents(t *testing.T) {
	capture := &registeredPlanningCapture{}
	base := planEvaluationV2ResourceStepRequest(t, capture)
	libraries := map[string]*Library{
		"cloud": {LibraryPath: "example.com/cloud", Configuration: configurationRegistration(1, nil)},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-evaluator-v2/resource-step", "chain")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	snapshot := newPlanEvaluationV2Snapshot(t)
	addresses := []string{"resource.upstream", "resource.child", "resource.grandchild"}
	for _, address := range addresses {
		addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
			Address: address, Kind: state.StateResource,
			Payload: state.StatePayload{
				Kind: state.StateResource, Resource: &state.ResourceStatePayload{Target: *base.Prior},
			},
		})
	}
	passes := 0
	steps, err := planStepsV2(context.Background(), func(pass *planningPassState) (
		[]planStepV2Request, error,
	) {
		passes++
		evaluation, err := executor.preparePlanEvaluationV2(
			operationObject(t, map[string]EncodedValue{}), snapshot, pass,
		)
		require.NoError(t, err)
		configuration, err := executor.planEvaluationV2LibraryConfigurationRequest(
			evaluation, dag.Nodes["library-config.cloud"],
		)
		require.NoError(t, err)
		requests := []planStepV2Request{configuration}
		for _, address := range addresses {
			node := dag.Nodes[address]
			dependencies := planEvaluationV2Dependencies(dag.Edges[address])
			requests = append(requests, planStepV2Request{
				Address: address, Kind: NodeResource, DependsOn: dependencies,
				Plan: func(ctx context.Context, pass *planningPassState) (*PlanStepV2, error) {
					target, err := executor.planEvaluationV2ResourceTarget(
						evaluation, node, base.DesiredRegistration,
					)
					require.NoError(t, err)
					request := base
					request.Address = address
					request.DependsOn = dependencies
					request.Desired = target
					return executor.planEvaluationV2ResourceStep(ctx, evaluation, pass, request)
				},
			})
		}
		return requests, nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, passes)
	require.Len(t, steps, 4)
	require.Equal(t, DecisionUpdate, steps[1].Operation.Resource.Decision)
	for i, address := range addresses[1:] {
		step := steps[i+2]
		require.Equal(t, address, step.Address)
		require.Equal(t, DecisionReplace, step.Operation.Resource.Decision)
		fields, _ := step.Operation.Resource.Desired.Inputs.ObjectFields()
		pending, err := PendingEncodedValue([]string{addresses[i] + ".value"})
		require.NoError(t, err)
		require.Equal(t, pending, fields["name"])
	}
	require.Equal(t, []string{"server:same", "server:same", "server:same"}, capture.reads)
}

func TestPlanEvaluationV2ResourceStepStopsOnReadError(t *testing.T) {
	wantErr := errors.New("read failed")
	capture := &registeredPlanningCapture{
		read: func(string, *recordedConfiguration, *registeredPlanningOutput) (
			*registeredPlanningOutput, error,
		) {
			return nil, wantErr
		},
	}
	request := planEvaluationV2ResourceStepRequest(t, capture)
	executor := &Executor{DAG: newDAG(map[string][]string{request.Address: nil})}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
	)
	require.NoError(t, err)
	evaluation.run.eval.Resources["main"] = map[string]any{"id": "seeded"}
	step, err := executor.planEvaluationV2ResourceStep(context.Background(), evaluation, pass, request)
	require.ErrorIs(t, err, wantErr)
	require.Nil(t, step)
	require.Equal(t, map[string]any{
		"main": map[string]any{"id": "seeded"},
	}, evaluation.run.eval.Resources)
	require.Empty(t, pass.passChanges())
}

func TestPlanEvaluationV2ResourceStepPublishesInstanceOutputs(t *testing.T) {
	for _, address := range []string{
		"resource.main['blue']",
		"resource.apps['prod']/resource.main['blue']",
	} {
		t.Run(address, func(t *testing.T) {
			request := planEvaluationV2ResourceStepRequest(t, &registeredPlanningCapture{})
			request.Address = address
			boundary := &Node{
				Address: "resource.apps", Kind: NodeResource,
				Body: parseValue(t, "{}"), ForEach: parseValue(t, "{ prod: 'production' }"),
			}
			executor := &Executor{DAG: &DAG{
				Nodes: map[string]*Node{boundary.Address: boundary},
				Edges: map[string][]string{},
			}}
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
			)
			require.NoError(t, err)
			scope, err := executor.scopeForAddress(evaluation.run, address)
			require.NoError(t, err)
			require.NotNil(t, scope)
			scope.Resources["main"] = map[string]any{
				"blue":  map[string]any{"stale": "discard"},
				"green": map[string]any{"id": "sibling"},
			}
			step, err := executor.planEvaluationV2ResourceStep(
				context.Background(), evaluation, pass, request,
			)
			require.NoError(t, err)
			require.Equal(t, DecisionNoOp, step.Operation.Resource.Decision)
			require.Equal(t, map[string]any{
				"main": map[string]any{
					"blue":  map[string]any{"id": "server-1", "value": "server"},
					"green": map[string]any{"id": "sibling"},
				},
			}, scope.Resources)

			request.Desired.Inputs = operationObject(t, map[string]EncodedValue{
				"name": StringValue("server"), "size": IntegerValue(2),
			})
			step, err = executor.planEvaluationV2ResourceStep(
				context.Background(), evaluation, pass, request,
			)
			require.NoError(t, err)
			require.Equal(t, DecisionUpdate, step.Operation.Resource.Decision)
			require.Equal(t, map[string]any{
				"main": map[string]any{
					"green": map[string]any{"id": "sibling"},
				},
			}, scope.Resources)
		})
	}
}

func TestPlanEvaluationV2ResourceStepAllowsAbsentTargets(t *testing.T) {
	for _, orphan := range []bool{false, true} {
		t.Run(map[bool]string{false: "omitted", true: "orphan destroy"}[orphan], func(t *testing.T) {
			capture := &registeredPlanningCapture{}
			request := planEvaluationV2ResourceStepRequest(t, capture)
			executor := &Executor{DAG: newDAG(nil)}
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
			)
			require.NoError(t, err)
			request.Desired = nil
			if orphan {
				request.Address = "resource.removed/resource.main"
			} else {
				request.Prior = nil
				request.DependsOn = nil
			}
			step, err := executor.planEvaluationV2ResourceStep(
				context.Background(), evaluation, pass, request,
			)
			require.NoError(t, err)
			if orphan {
				require.Equal(t, DecisionDestroy, step.Operation.Resource.Decision)
				require.Equal(t, []string{"server:same"}, capture.reads)
			} else {
				require.Nil(t, step)
				require.Empty(t, capture.reads)
			}
			require.Empty(t, evaluation.run.eval.Resources)
		})
	}
}

func TestPlanEvaluationV2ResourceStepRejectsInvalidSetup(t *testing.T) {
	for _, name := range []string{
		"context", "canceled context", "executor", "graph", "evaluation", "run", "scope",
		"pass", "facts", "address", "dependencies",
	} {
		t.Run(name, func(t *testing.T) {
			capture := &registeredPlanningCapture{}
			request := planEvaluationV2ResourceStepRequest(t, capture)
			executor := &Executor{DAG: newDAG(nil)}
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
			)
			require.NoError(t, err)
			ctx := context.Background()
			switch name {
			case "context":
				ctx = nil
			case "canceled context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "executor":
				executor = nil
			case "graph":
				executor.DAG = nil
			case "evaluation":
				evaluation = nil
			case "run":
				evaluation.run = nil
			case "scope":
				evaluation.run.eval = nil
			case "pass":
				pass = nil
			case "facts":
				pass.facts = nil
			case "address":
				request.Address = "action.main"
			case "dependencies":
				request.DependsOn = []string{"invalid"}
			}
			step, err := executor.planEvaluationV2ResourceStep(ctx, evaluation, pass, request)
			require.Error(t, err)
			require.Nil(t, step)
			require.Empty(t, capture.reads)
		})
	}
}
