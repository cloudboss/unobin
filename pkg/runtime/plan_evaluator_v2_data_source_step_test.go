package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestPlanEvaluationV2DataSourceStepPublishesOutputs(t *testing.T) {
	for _, name := range []string{"read", "pending inputs", "pending configuration", "destroy"} {
		t.Run(name, func(t *testing.T) {
			desired := validPlannedDataSourceTarget(t)
			prior := operationDataSourceState(t)
			request := dataSourcePlanningRequest{
				Address: "data-source.image", DependsOn: []string{}, Desired: &desired, Prior: &prior,
			}
			snapshot := newPlanEvaluationV2Snapshot(t)
			addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
				Address: request.Address, Kind: state.StateDataSource,
				Payload: state.StatePayload{Kind: state.StateDataSource, DataSource: &prior},
			})
			original, err := snapshot.Clone()
			require.NoError(t, err)
			executor := &Executor{DAG: newDAG(nil)}
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), snapshot, pass,
			)
			require.NoError(t, err)
			switch name {
			case "pending inputs":
				pending, err := PendingEncodedValue([]string{"resource.network.id"})
				require.NoError(t, err)
				desired.Inputs = operationObject(t, map[string]EncodedValue{"name": pending})
			case "pending configuration":
				desired.Configuration = pendingOperationConfiguration()
			case "destroy":
				request.Desired = nil
			}
			observed := operationObject(t, map[string]EncodedValue{"id": StringValue("fresh")})
			reads := 0
			step, err := executor.planEvaluationV2DataSourceStep(
				context.Background(), evaluation, pass, request,
				dataSourcePlanningCallbacks{Read: func(context.Context) (EncodedValue, error) {
					reads++
					return observed, nil
				}},
			)
			require.NoError(t, err)
			require.NoError(t, step.Validate())
			require.Equal(t, &prior, step.Operation.DataSource.Prior)
			if name == "read" {
				require.Equal(t, 1, reads)
				require.Equal(t, &observed, step.Operation.DataSource.ObservedOutputs)
				require.Equal(t, map[string]any{
					"image": map[string]any{"id": "fresh"},
				}, evaluation.run.eval.Data)
				evaluation.run.eval.Data["image"].(map[string]any)["id"] = "changed"
				require.Equal(t, &observed, step.Operation.DataSource.ObservedOutputs)
			} else {
				require.Zero(t, reads)
				require.Nil(t, step.Operation.DataSource.ObservedOutputs)
				require.Empty(t, evaluation.run.eval.Data)
				for _, expression := range []string{"data-source.image", "data-source.image.id"} {
					_, err := Eval(parseValue(t, expression), evaluation.run.eval)
					require.ErrorIs(t, err, ErrEvalNotFound)
				}
			}
			decision := DecisionRead
			if name == "destroy" {
				decision = DecisionDestroy
			}
			require.Equal(t, decision, step.Operation.DataSource.Decision)
			require.Equal(t, original, snapshot)
			require.Equal(t, original, evaluation.prior)
		})
	}
}

func TestPlanEvaluationV2DataSourceStepFeedsDependentPlans(t *testing.T) {
	for _, updated := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged resource", true: "updated resource"}[updated],
			func(t *testing.T) {
				capture := &registeredPlanningCapture{}
				resource := planEvaluationV2ResourceStepRequest(t, capture)
				if updated {
					resource.Desired.Inputs = operationObject(t, map[string]EncodedValue{
						"name": StringValue("server"), "size": IntegerValue(2),
					})
				}
				reads := 0
				libraries := map[string]*Library{"cloud": dataSourceTargetLibrary(t, &reads)}
				src := ubtest.ReadValidFixture(t,
					"testdata/ub/plan-evaluator-v2/data-source-step", "chain",
				)
				dag, source := syntaxDAGAndBody(t, src, libraries)
				executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
				node := executor.DAG.Nodes["data-source.main"]
				output := executor.DAG.Nodes["output.id"]
				planRequest := validPlanFileV2PlanningRequest(t)
				passes := 0
				planRequest.Evaluate = func(pass *planningPassState) ([]planStepV2Request, error) {
					passes++
					evaluation, err := executor.preparePlanEvaluationV2(
						planRequest.Inputs, newPlanEvaluationV2Snapshot(t), pass,
					)
					require.NoError(t, err)
					evaluation.run.eval.Data["main"] = map[string]any{"id": "stale"}
					outputRequest, err := executor.planEvaluationV2OutputRequest(evaluation, output)
					require.NoError(t, err)
					return []planStepV2Request{
						{
							Address: resource.Address, Kind: NodeResource, DependsOn: resource.DependsOn,
							Plan: func(ctx context.Context, pass *planningPassState) (*PlanStepV2, error) {
								return executor.planEvaluationV2ResourceStep(ctx, evaluation, pass, resource)
							},
						},
						dataSourceChainPlanningRequest(t, executor, evaluation, node, &reads),
						outputRequest,
					}, nil
				}
				plan, err := planPlanFileV2(context.Background(), planRequest)
				require.NoError(t, err)
				require.NoError(t, plan.Validate())
				require.Len(t, plan.Steps, 3)
				want := StringValue("server")
				if updated {
					want, err = PendingEncodedValue([]string{"data-source.main.id"})
					require.NoError(t, err)
					require.Equal(t, 2, passes)
					require.Zero(t, reads)
					require.Nil(t, plan.Steps[1].Operation.DataSource.ObservedOutputs)
				} else {
					require.Equal(t, 1, passes)
					require.Equal(t, 1, reads)
				}
				require.Equal(t, want, plan.Steps[2].Operation.Output.Value)
				require.True(t, plan.Steps[2].Operation.Output.Sensitive)
				encoded, err := EncodePlanV2(plan)
				require.NoError(t, err)
				decoded, err := DecodePlanV2(encoded)
				require.NoError(t, err)
				require.Equal(t, plan, decoded)
			})
	}
}

func dataSourceChainPlanningRequest(
	t *testing.T,
	executor *Executor,
	evaluation *planEvaluationV2,
	node *Node,
	reads *int,
) planStepV2Request {
	t.Helper()
	dependencies := planEvaluationV2Dependencies(executor.DAG.Edges[node.Address])
	return planStepV2Request{
		Address: node.Address, Kind: NodeDataSource, DependsOn: dependencies,
		Plan: func(ctx context.Context, pass *planningPassState) (*PlanStepV2, error) {
			target, err := executor.planEvaluationV2DataSourceTarget(evaluation, node)
			require.NoError(t, err)
			return executor.planEvaluationV2DataSourceStep(ctx, evaluation, pass,
				dataSourcePlanningRequest{
					Address: node.Address, DependsOn: dependencies, Desired: target,
				}, dataSourcePlanningCallbacks{
					Read: func(ctx context.Context) (EncodedValue, error) {
						inputs, err := decodeResourceInputs[dataSourceTargetInput](target.Inputs)
						require.NoError(t, err)
						require.Equal(t, uint8(2), inputs.Size)
						inputs.reads = reads
						result, err := inputs.Read(ctx, NoConfig{})
						require.NoError(t, err)
						return encodeResourceOutputs(result)
					},
				})
		},
	}
}

func TestPlanEvaluationV2DataSourceStepStopsOnReadFailure(t *testing.T) {
	wantErr := errors.New("read failed")
	for _, name := range []string{"error", "not found", "panic", "invalid outputs", "missing read"} {
		t.Run(name, func(t *testing.T) {
			desired := validPlannedDataSourceTarget(t)
			executor := &Executor{DAG: newDAG(nil)}
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
			)
			require.NoError(t, err)
			evaluation.run.eval.Data["main"] = map[string]any{"id": "seeded"}
			callbacks := dataSourcePlanningCallbacks{Read: func(context.Context) (EncodedValue, error) {
				switch name {
				case "not found":
					return EncodedValue{}, ErrNotFound
				case "panic":
					panic("provider panic")
				case "invalid outputs":
					return StringValue("invalid"), nil
				default:
					return EncodedValue{}, wantErr
				}
			}}
			if name == "missing read" {
				callbacks.Read = nil
			}
			step, err := executor.planEvaluationV2DataSourceStep(
				context.Background(), evaluation, pass,
				dataSourcePlanningRequest{
					Address: "data-source.main", DependsOn: []string{}, Desired: &desired,
				}, callbacks,
			)
			require.Error(t, err)
			require.Nil(t, step)
			switch name {
			case "error":
				require.ErrorIs(t, err, wantErr)
			case "not found":
				require.ErrorIs(t, err, ErrNotFound)
			}
			require.Equal(t, map[string]any{
				"main": map[string]any{"id": "seeded"},
			}, evaluation.run.eval.Data)
		})
	}
}

func TestPlanEvaluationV2DataSourceStepReusesReadsAcrossPasses(t *testing.T) {
	executor := &Executor{DAG: newDAG(nil)}
	snapshot := newPlanEvaluationV2Snapshot(t)
	desired := validPlannedDataSourceTarget(t)
	observed := operationObject(t, map[string]EncodedValue{"id": StringValue("fresh")})
	reads, passes := 0, 0
	steps, err := planStepsV2(context.Background(), func(pass *planningPassState) (
		[]planStepV2Request, error,
	) {
		passes++
		evaluation, err := executor.preparePlanEvaluationV2(
			operationObject(t, map[string]EncodedValue{}), snapshot, pass,
		)
		require.NoError(t, err)
		require.NoError(t, pass.invalidateOutputs("resource.changed"))
		return []planStepV2Request{{
			Address: "data-source.image", Kind: NodeDataSource, DependsOn: []string{},
			Plan: func(ctx context.Context, pass *planningPassState) (*PlanStepV2, error) {
				step, err := executor.planEvaluationV2DataSourceStep(ctx, evaluation, pass,
					dataSourcePlanningRequest{
						Address: "data-source.image", DependsOn: []string{}, Desired: &desired,
					}, dataSourcePlanningCallbacks{Read: func(context.Context) (EncodedValue, error) {
						reads++
						return observed, nil
					}})
				require.NoError(t, err)
				require.Equal(t, map[string]any{
					"image": map[string]any{"id": "fresh"},
				}, evaluation.run.eval.Data)
				evaluation.run.eval.Data["image"].(map[string]any)["id"] = "changed"
				return step, nil
			},
		}}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, passes)
	require.Equal(t, 1, reads)
	require.Len(t, steps, 1)
	require.Equal(t, &observed, steps[0].Operation.DataSource.ObservedOutputs)
}

func TestPlanEvaluationV2DataSourceStepReadsChangedRequests(t *testing.T) {
	for _, name := range []string{"address", "binding", "inputs", "configuration", "metadata"} {
		t.Run(name, func(t *testing.T) {
			executor := &Executor{DAG: newDAG(nil)}
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
			)
			require.NoError(t, err)
			desired := validPlannedDataSourceTarget(t)
			request := dataSourcePlanningRequest{
				Address: "data-source.image", DependsOn: []string{}, Desired: &desired,
			}
			reads := 0
			callbacks := dataSourcePlanningCallbacks{Read: func(context.Context) (EncodedValue, error) {
				reads++
				return operationObject(t, map[string]EncodedValue{"read": IntegerValue(int64(reads))}), nil
			}}
			_, err = executor.planEvaluationV2DataSourceStep(
				context.Background(), evaluation, pass, request, callbacks,
			)
			require.NoError(t, err)
			switch name {
			case "address":
				request.Address = "data-source.other"
			case "binding":
				desired.Binding.Export = "other"
			case "inputs":
				desired.Inputs = operationObject(t, map[string]EncodedValue{"name": StringValue("other")})
			case "configuration":
				desired.Configuration.Record.Address = "library-config.other"
			case "metadata":
				request.DependsOn = []string{"resource.other"}
				desired.SensitiveInputPaths = []string{"/name"}
			}
			step, err := executor.planEvaluationV2DataSourceStep(
				context.Background(), evaluation, pass, request, callbacks,
			)
			require.NoError(t, err)
			wantReads := 2
			if name == "metadata" {
				wantReads = 1
			}
			require.Equal(t, wantReads, reads)
			observed := operationObject(t, map[string]EncodedValue{"read": IntegerValue(int64(wantReads))})
			require.Equal(t, &observed, step.Operation.DataSource.ObservedOutputs)
			require.Equal(t, request.DependsOn, step.DependsOn)
			require.Equal(t, request.Desired, step.Operation.DataSource.Desired)
		})
	}
}

func TestPlanEvaluationV2DataSourceStepPublishesInstanceOutputs(t *testing.T) {
	address := "resource.apps['prod']/data-source.image['blue']"
	boundary := &Node{
		Address: "resource.apps", Kind: NodeResource,
		Body: parseValue(t, "{}"), ForEach: parseValue(t, "{ prod: 'production' }"),
	}
	executor := &Executor{DAG: &DAG{
		Nodes: map[string]*Node{boundary.Address: boundary}, Edges: map[string][]string{},
	}}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
	)
	require.NoError(t, err)
	scope, err := executor.scopeForAddress(evaluation.run, address)
	require.NoError(t, err)
	require.NotNil(t, scope)
	scope.Data["image"] = map[string]any{
		"blue": map[string]any{"id": "stale"}, "green": map[string]any{"id": "sibling"},
	}
	desired := validPlannedDataSourceTarget(t)
	request := dataSourcePlanningRequest{Address: address, DependsOn: []string{}, Desired: &desired}
	_, err = executor.planEvaluationV2DataSourceStep(context.Background(), evaluation, pass, request,
		dataSourcePlanningCallbacks{Read: func(context.Context) (EncodedValue, error) {
			return operationObject(t, map[string]EncodedValue{"id": StringValue("fresh")}), nil
		}})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"image": map[string]any{
		"blue": map[string]any{"id": "fresh"}, "green": map[string]any{"id": "sibling"},
	}}, scope.Data)
	desired.Configuration = pendingOperationConfiguration()
	_, err = executor.planEvaluationV2DataSourceStep(
		context.Background(), evaluation, pass, request, dataSourcePlanningCallbacks{},
	)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"image": map[string]any{
		"green": map[string]any{"id": "sibling"},
	}}, scope.Data)
	require.Empty(t, evaluation.run.eval.Data)
}

func TestPlanEvaluationV2DataSourceStepRejectsInvalidSetup(t *testing.T) {
	for _, name := range []string{
		"context", "canceled context", "executor", "graph", "evaluation", "run", "scope",
		"pass", "facts", "cache", "address", "dependencies", "desired", "prior",
	} {
		t.Run(name, func(t *testing.T) {
			executor := &Executor{DAG: newDAG(nil)}
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
			)
			require.NoError(t, err)
			desired := validPlannedDataSourceTarget(t)
			request := dataSourcePlanningRequest{
				Address: "data-source.main", DependsOn: []string{}, Desired: &desired,
			}
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
			case "cache":
				pass.reads = nil
			case "address":
				request.Address = "action.main"
			case "dependencies":
				request.DependsOn = []string{"invalid"}
			case "desired":
				desired.Binding = Binding{}
			case "prior":
				request.Prior = &DataSourceStatePayload{}
			}
			reads := 0
			step, err := executor.planEvaluationV2DataSourceStep(ctx, evaluation, pass, request,
				dataSourcePlanningCallbacks{Read: func(context.Context) (EncodedValue, error) {
					reads++
					return operationObject(t, map[string]EncodedValue{}), nil
				}})
			require.Error(t, err)
			require.Nil(t, step)
			require.Zero(t, reads)
		})
	}
}

func TestPlanEvaluationV2DataSourceStepAllowsAbsentTargets(t *testing.T) {
	executor := &Executor{DAG: newDAG(nil)}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
	)
	require.NoError(t, err)
	step, err := executor.planEvaluationV2DataSourceStep(
		context.Background(), evaluation, pass,
		dataSourcePlanningRequest{}, dataSourcePlanningCallbacks{},
	)
	require.NoError(t, err)
	require.Nil(t, step)
	prior := operationDataSourceState(t)
	step, err = executor.planEvaluationV2DataSourceStep(context.Background(), evaluation, pass,
		dataSourcePlanningRequest{
			Address: "resource.removed/data-source.image", DependsOn: []string{}, Prior: &prior,
		}, dataSourcePlanningCallbacks{})
	require.NoError(t, err)
	require.Equal(t, DecisionDestroy, step.Operation.DataSource.Decision)
	require.Equal(t, &prior, step.Operation.DataSource.Prior)
	require.Empty(t, evaluation.run.eval.Data)
}
