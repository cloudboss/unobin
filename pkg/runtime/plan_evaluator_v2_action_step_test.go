package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestPlanEvaluationV2ActionStepPublishesOutputs(t *testing.T) {
	for _, name := range []string{
		"skip", "new action", "changed trigger", "always", "pending inputs",
		"pending configuration", "destroy",
	} {
		t.Run(name, func(t *testing.T) {
			desired := validPlannedActionTarget(t)
			prior := operationActionState(t)
			request := actionPlanningRequest{
				Address: "action.main", DependsOn: []string{}, Desired: &desired, Prior: &prior,
			}
			snapshot := newPlanEvaluationV2Snapshot(t)
			addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
				Address: request.Address, Kind: state.StateAction,
				Payload: state.StatePayload{Kind: state.StateAction, Action: &prior},
			})
			original, err := snapshot.Clone()
			require.NoError(t, err)
			executor := &Executor{DAG: newDAG(nil)}
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), snapshot, pass,
			)
			require.NoError(t, err)
			evaluation.run.eval.Actions["main"] = map[string]any{"sent": false, "stale": true}
			switch name {
			case "new action":
				request.Prior = nil
			case "changed trigger":
				desired.TriggerHash = "changed"
			case "always":
				desired.TriggerHash = ""
			case "pending inputs":
				pending, err := PendingEncodedValue([]string{"resource.network.id"})
				require.NoError(t, err)
				desired.Inputs = operationObject(t, map[string]EncodedValue{"message": pending})
			case "pending configuration":
				desired.Configuration = pendingOperationConfiguration()
			case "destroy":
				request.Desired = nil
			}
			step, err := executor.planEvaluationV2ActionStep(context.Background(), evaluation, request)
			require.NoError(t, err)
			require.NoError(t, step.Validate())
			require.Equal(t, request.Prior, step.Operation.Action.Prior)
			require.Equal(t, request.Desired, step.Operation.Action.Desired)
			decision := DecisionRerun
			if name == "skip" {
				decision = DecisionSkip
				require.Equal(t, map[string]any{
					"main": map[string]any{"sent": true},
				}, evaluation.run.eval.Actions)
				evaluation.run.eval.Actions["main"].(map[string]any)["sent"] = false
				require.Equal(t, prior.Outputs, step.Operation.Action.Prior.Outputs)
			} else {
				require.Empty(t, evaluation.run.eval.Actions)
				for _, expression := range []string{"action.main", "action.main.sent"} {
					_, err := Eval(parseValue(t, expression), evaluation.run.eval)
					require.ErrorIs(t, err, ErrEvalNotFound)
				}
			}
			if name == "destroy" {
				decision = DecisionDestroy
			}
			require.Equal(t, decision, step.Operation.Action.Decision)
			require.Equal(t, original, snapshot)
			require.Equal(t, original, evaluation.prior)
		})
	}
}

func TestPlanEvaluationV2ActionStepFeedsDependentPlans(t *testing.T) {
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
				runs := 0
				libraries := map[string]*Library{"cloud": actionTargetLibrary(t, &runs)}
				src := ubtest.ReadValidFixture(t,
					"testdata/ub/plan-evaluator-v2/action-step", "chain",
				)
				dag, source := syntaxDAGAndBody(t, src, libraries)
				executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
				planRequest := validPlanFileV2PlanningRequest(t)
				snapshot := actionStepChainSnapshot(t, executor, planRequest.Inputs)
				passes := 0
				planRequest.Evaluate = func(pass *planningPassState) ([]planStepV2Request, error) {
					passes++
					evaluation, err := executor.preparePlanEvaluationV2(planRequest.Inputs, snapshot, pass)
					require.NoError(t, err)
					output, err := executor.planEvaluationV2OutputRequest(
						evaluation, dag.Nodes["output.id"],
					)
					require.NoError(t, err)
					requests := []planStepV2Request{{
						Address: resource.Address, Kind: NodeResource, DependsOn: resource.DependsOn,
						Plan: func(ctx context.Context, pass *planningPassState) (*PlanStepV2, error) {
							return executor.planEvaluationV2ResourceStep(ctx, evaluation, pass, resource)
						},
					}}
					for _, entry := range evaluation.prior.Entries {
						node := dag.Nodes[entry.Address]
						dependencies := planEvaluationV2Dependencies(dag.Edges[node.Address])
						requests = append(requests, planStepV2Request{
							Address: node.Address, Kind: NodeAction, DependsOn: dependencies,
							Plan: func(ctx context.Context, _ *planningPassState) (*PlanStepV2, error) {
								target, err := executor.planEvaluationV2ActionTarget(evaluation, node)
								require.NoError(t, err)
								return executor.planEvaluationV2ActionStep(ctx, evaluation, actionPlanningRequest{
									Address: node.Address, DependsOn: dependencies,
									Desired: target, Prior: entry.Payload.Action,
								})
							},
						})
					}
					return append(requests, output), nil
				}
				plan, err := planPlanFileV2(context.Background(), planRequest)
				require.NoError(t, err)
				require.NoError(t, plan.Validate())
				require.Len(t, plan.Steps, 4)
				want := StringValue("next-saved")
				decision := DecisionSkip
				if updated {
					want, err = PendingEncodedValue([]string{"action.next.id"})
					require.NoError(t, err)
					decision = DecisionRerun
					require.Equal(t, 2, passes)
				} else {
					require.Equal(t, 1, passes)
				}
				for _, index := range []int{1, 2} {
					operation := plan.Steps[index].Operation.Action
					require.Equal(t, decision, operation.Decision)
					require.Equal(t, updated, operation.Desired.Inputs.HasPending())
				}
				require.Equal(t, want, plan.Steps[3].Operation.Output.Value)
				require.True(t, plan.Steps[3].Operation.Output.Sensitive)
				require.Zero(t, runs)
				encoded, err := encodePlanFileV2(plan)
				require.NoError(t, err)
				decoded, err := decodePlanFileV2(encoded)
				require.NoError(t, err)
				require.Equal(t, plan, decoded)
			})
	}
}

func actionStepChainSnapshot(
	t *testing.T,
	executor *Executor,
	inputs EncodedValue,
) *state.SnapshotV2 {
	t.Helper()
	snapshot := newPlanEvaluationV2Snapshot(t)
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(inputs, snapshot, pass)
	require.NoError(t, err)
	evaluation.run.eval.Resources["main"] = map[string]any{"value": "server"}
	evaluation.run.eval.Actions["main"] = map[string]any{"id": "main-saved"}
	for _, name := range []string{"main", "next"} {
		node := executor.DAG.Nodes["action."+name]
		target, err := executor.planEvaluationV2ActionTarget(evaluation, node)
		require.NoError(t, err)
		prior := ActionStatePayload{
			Binding: target.Binding, Inputs: target.Inputs,
			Outputs:       operationObject(t, map[string]EncodedValue{"id": StringValue(name + "-saved")}),
			Configuration: *target.Configuration.Record, TriggerHash: target.TriggerHash,
			DependsOn:            planEvaluationV2Dependencies(executor.DAG.Edges[node.Address]),
			SensitiveInputPaths:  target.SensitiveInputPaths,
			SensitiveOutputPaths: target.SensitiveOutputPaths,
		}
		addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
			Address: node.Address, Kind: state.StateAction,
			Payload: state.StatePayload{Kind: state.StateAction, Action: &prior},
		})
	}
	return snapshot
}

func TestPlanEvaluationV2ActionStepPublishesInstanceOutputs(t *testing.T) {
	address := "resource.apps['prod']/action.main['blue']"
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
	scope.Actions["main"] = map[string]any{
		"blue": map[string]any{"sent": false}, "green": map[string]any{"sent": false},
	}
	desired := validPlannedActionTarget(t)
	prior := operationActionState(t)
	request := actionPlanningRequest{
		Address: address, DependsOn: []string{}, Desired: &desired, Prior: &prior,
	}
	_, err = executor.planEvaluationV2ActionStep(context.Background(), evaluation, request)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"main": map[string]any{
		"blue": map[string]any{"sent": true}, "green": map[string]any{"sent": false},
	}}, scope.Actions)
	desired.TriggerHash = "changed"
	_, err = executor.planEvaluationV2ActionStep(context.Background(), evaluation, request)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"main": map[string]any{
		"green": map[string]any{"sent": false},
	}}, scope.Actions)
	require.Empty(t, evaluation.run.eval.Actions)
}

func TestPlanEvaluationV2ActionStepRejectsInvalidSetup(t *testing.T) {
	for _, name := range []string{
		"context", "canceled context", "executor", "graph", "evaluation", "run", "scope",
		"address", "dependencies", "desired", "prior",
	} {
		t.Run(name, func(t *testing.T) {
			executor := &Executor{DAG: newDAG(nil)}
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
			)
			require.NoError(t, err)
			seeded := map[string]any{"main": map[string]any{"sent": true}}
			evaluation.run.eval.Actions = seeded
			desired := validPlannedActionTarget(t)
			request := actionPlanningRequest{
				Address: "action.main", DependsOn: []string{}, Desired: &desired,
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
			case "address":
				request.Address = "resource.main"
			case "dependencies":
				request.DependsOn = []string{"invalid"}
			case "desired":
				desired.Binding = Binding{}
			case "prior":
				request.Prior = &ActionStatePayload{}
			}
			step, err := executor.planEvaluationV2ActionStep(ctx, evaluation, request)
			require.Error(t, err)
			require.Nil(t, step)
			if name == "canceled context" {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.Equal(t, map[string]any{"main": map[string]any{"sent": true}}, seeded)
		})
	}
}

func TestPlanEvaluationV2ActionStepAllowsAbsentTargets(t *testing.T) {
	executor := &Executor{DAG: newDAG(nil)}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
	)
	require.NoError(t, err)
	step, err := executor.planEvaluationV2ActionStep(
		context.Background(), evaluation, actionPlanningRequest{},
	)
	require.NoError(t, err)
	require.Nil(t, step)
	prior := operationActionState(t)
	step, err = executor.planEvaluationV2ActionStep(context.Background(), evaluation,
		actionPlanningRequest{
			Address: "resource.removed/action.main", DependsOn: []string{}, Prior: &prior,
		})
	require.NoError(t, err)
	require.Equal(t, DecisionDestroy, step.Operation.Action.Decision)
	require.Equal(t, &prior, step.Operation.Action.Prior)
	require.Empty(t, evaluation.run.eval.Actions)
}
