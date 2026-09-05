package runtime

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

const compositeStepFixtureDir = "testdata/ub/plan-evaluator-v2/composite-step"

func compositeStepLibrary(t *testing.T) *Library {
	t.Helper()
	body := ubtest.ReadValidFixture(t, compositeStepFixtureDir, "body")
	library := &Library{LibraryPath: "example.com/app"}
	for _, kind := range []NodeKind{NodeResource, NodeDataSource, NodeAction} {
		library.AddComposite(syntaxComposite(t, "box", kind, body))
	}
	return library
}

func newCompositeStepEvaluation(t *testing.T) (*Executor, *planEvaluationV2) {
	t.Helper()
	libraries := map[string]*Library{"app": compositeStepLibrary(t)}
	src := ubtest.ReadValidFixture(t, compositeStepFixtureDir, "chain")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
		newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
	)
	require.NoError(t, err)
	return executor, evaluation
}

func TestPlanEvaluationV2CompositeStepPublishesOutputs(t *testing.T) {
	for _, kind := range []NodeKind{NodeResource, NodeDataSource, NodeAction} {
		t.Run(string(kind), func(t *testing.T) {
			executor, evaluation := newCompositeStepEvaluation(t)
			address := string(kind) + ".main"
			node := executor.DAG.Nodes[address]
			desired, err := executor.planEvaluationV2CompositeTarget(evaluation, node)
			require.NoError(t, err)
			prior := operationCompositeState(t, kind)
			request := compositePlanningRequest{
				Address: address, Category: kind, DependsOn: []string{}, Desired: desired, Prior: &prior,
			}
			scope, err := executor.ensureCompositeScope(evaluation.run, address)
			require.NoError(t, err)
			scope.Resources["child"] = map[string]any{"id": "current"}
			parent := scopeMapForKind(evaluation.run.eval, kind)
			parent["main"] = map[string]any{"id": "stale", "removed": true}
			step, err := executor.planEvaluationV2CompositeStep(context.Background(), evaluation, request)
			require.NoError(t, err)
			require.NoError(t, step.Validate())
			require.Equal(t, &CompositePlanOperation{
				Decision: DecisionEval, Desired: desired, Prior: &prior,
			}, step.Operation.Composite)
			fields, _ := desired.Inputs.ObjectFields()
			name, _ := fields["name"].String()
			require.Equal(t, map[string]any{"main": map[string]any{
				"label": name + "-box", "id": "current", "empty": nil,
				"details": map[string]any{"name": name + "-box", "id": "current"},
			}}, parent)
			delete(scope.Resources, "child")
			_, err = executor.planEvaluationV2CompositeStep(context.Background(), evaluation, request)
			require.NoError(t, err)
			require.Equal(t, map[string]any{"main": map[string]any{
				"label": name + "-box", "empty": nil,
			}}, parent)
			_, err = Eval(parseValue(t, address+".id"), evaluation.run.eval)
			require.ErrorIs(t, err, ErrEvalNotFound)
			require.Equal(t, operationCompositeState(t, kind), prior)
		})
	}
}

func TestPlanEvaluationV2CompositeStepUsesDesiredInputs(t *testing.T) {
	executor, evaluation := newCompositeStepEvaluation(t)
	node := executor.DAG.Nodes["resource.main"]
	desired, err := executor.planEvaluationV2CompositeTarget(evaluation, node)
	require.NoError(t, err)
	request := compositePlanningRequest{
		Address: node.Address, Category: node.Kind, DependsOn: []string{}, Desired: desired,
	}
	for _, name := range []string{"first", "second"} {
		desired.Inputs = operationObject(t, map[string]EncodedValue{"name": StringValue(name)})
		_, err := executor.planEvaluationV2CompositeStep(context.Background(), evaluation, request)
		require.NoError(t, err)
		require.Equal(t, map[string]any{"main": map[string]any{
			"label": name + "-box", "empty": nil,
		}}, evaluation.run.eval.Resources)
	}
	prior := operationCompositeState(t, NodeResource)
	request.Prior = &prior
	pending, err := PendingEncodedValue([]string{"resource.upstream.name"})
	require.NoError(t, err)
	desired.Inputs = operationObject(t, map[string]EncodedValue{"name": pending})
	step, err := executor.planEvaluationV2CompositeStep(context.Background(), evaluation, request)
	require.NoError(t, err)
	require.Equal(t, DecisionEval, step.Operation.Composite.Decision)
	require.Empty(t, evaluation.run.eval.Resources)
	request.Desired = nil
	evaluation.run.eval.Resources["main"] = map[string]any{"id": "stale"}
	delete(executor.DAG.Nodes, node.Address)
	step, err = executor.planEvaluationV2CompositeStep(context.Background(), evaluation, request)
	require.NoError(t, err)
	require.Equal(t, DecisionDestroy, step.Operation.Composite.Decision)
	require.Empty(t, evaluation.run.eval.Resources)
}

func TestPlanEvaluationV2CompositeStepReplacesEmptyOutputs(t *testing.T) {
	executor, evaluation := newCompositeStepEvaluation(t)
	node := executor.DAG.Nodes["resource.main"]
	desired, err := executor.planEvaluationV2CompositeTarget(evaluation, node)
	require.NoError(t, err)
	node.CompositeSyntaxBody.Outputs = nil
	evaluation.run.eval.Resources["main"] = map[string]any{"id": "stale"}
	_, err = executor.planEvaluationV2CompositeStep(context.Background(), evaluation,
		compositePlanningRequest{
			Address: node.Address, Category: node.Kind, DependsOn: []string{}, Desired: desired,
		},
	)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"main": map[string]any{}}, evaluation.run.eval.Resources)
}

func TestPlanEvaluationV2CompositeStepFeedsDependentPlans(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "updated"}[changed], func(t *testing.T) {
			executor, _ := newCompositeStepEvaluation(t)
			capture := &registeredPlanningCapture{}
			resource := planEvaluationV2ResourceStepRequest(t, capture)
			resource.Address = "resource.main/resource.child"
			if changed {
				resource.Desired.Inputs = operationObject(t, map[string]EncodedValue{
					"name": StringValue("server"), "size": IntegerValue(2),
				})
			}
			prior := operationCompositeState(t, NodeResource)
			prior.Outputs = operationObject(t, map[string]EncodedValue{"id": StringValue("stale")})
			snapshot := newPlanEvaluationV2Snapshot(t)
			addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
				Address: "resource.main", Kind: state.StateComposite,
				Payload: state.StatePayload{Kind: state.StateComposite, Composite: &prior},
			})
			original, err := snapshot.Clone()
			require.NoError(t, err)
			planRequest := validPlanFileV2PlanningRequest(t)
			passes := 0
			planRequest.Evaluate = func(pass *planningPassState) ([]planStepV2Request, error) {
				passes++
				evaluation, err := executor.preparePlanEvaluationV2(planRequest.Inputs, snapshot, pass)
				require.NoError(t, err)
				requests := []planStepV2Request{{
					Address: resource.Address, Kind: NodeResource, DependsOn: []string{},
					Plan: func(ctx context.Context, pass *planningPassState) (*PlanStepV2, error) {
						return executor.planEvaluationV2ResourceStep(ctx, evaluation, pass, resource)
					},
				}, {
					Address: "resource.main", Kind: NodeResource, DependsOn: []string{resource.Address},
					Plan: func(ctx context.Context, _ *planningPassState) (*PlanStepV2, error) {
						node := executor.DAG.Nodes["resource.main"]
						desired, err := executor.planEvaluationV2CompositeTarget(evaluation, node)
						require.NoError(t, err)
						return executor.planEvaluationV2CompositeStep(ctx, evaluation, compositePlanningRequest{
							Address: node.Address, Category: node.Kind, DependsOn: []string{resource.Address},
							Desired: desired, Prior: &prior,
						})
					},
				}}
				for _, address := range []string{"output.id", "output.label"} {
					output, err := executor.planEvaluationV2OutputRequest(evaluation, executor.DAG.Nodes[address])
					require.NoError(t, err)
					requests = append(requests, output)
				}
				return requests, nil
			}
			plan, err := planPlanFileV2(context.Background(), planRequest)
			require.NoError(t, err)
			require.NoError(t, plan.Validate())
			require.Len(t, plan.Steps, 4)
			want, decision := StringValue("server-1"), DecisionNoOp
			if changed {
				want, err = PendingEncodedValue([]string{"resource.main.id"})
				require.NoError(t, err)
				decision = DecisionUpdate
			}
			require.Equal(t, decision, plan.Steps[0].Operation.Resource.Decision)
			require.Equal(t, DecisionEval, plan.Steps[1].Operation.Composite.Decision)
			require.Equal(t, want, plan.Steps[2].Operation.Output.Value)
			require.Equal(t, StringValue("server-box"), plan.Steps[3].Operation.Output.Value)
			require.Equal(t, map[bool]int{false: 1, true: 2}[changed], passes)
			require.Equal(t, []string{"server:same"}, capture.reads)
			require.Equal(t, original, snapshot)
		})
	}
}

func TestPlanEvaluationV2CompositeStepPublishesNestedInstanceOutputs(t *testing.T) {
	child := compositeStepLibrary(t)
	body := ubtest.ReadValidFixture(t, compositeStepFixtureDir, "nested-body")
	outer := syntaxResourceComposite(t, "box", body)
	outer.Libraries = map[string]*Library{"app": child}
	libraries := map[string]*Library{"app": {
		LibraryPath: "example.com/outer", ResourceComposites: map[string]*CompositeType{"box": outer},
	}}
	src := ubtest.ReadValidFixture(t, compositeStepFixtureDir, "nested")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
		newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
	)
	require.NoError(t, err)
	address := "resource.apps['prod']/resource.boxes['blue']"
	scope, err := executor.ensureCompositeScope(evaluation.run, address)
	require.NoError(t, err)
	scope.Resources["child"] = map[string]any{"id": "blue-id"}
	parent, err := executor.scopeForAddress(evaluation.run, address)
	require.NoError(t, err)
	parent.Resources["boxes"] = map[string]any{
		"blue": map[string]any{"id": "stale"}, "green": map[string]any{"id": "green-id"},
	}
	other, err := executor.ensureCompositeScope(evaluation.run, "resource.apps['dev']")
	require.NoError(t, err)
	other.Resources["boxes"] = map[string]any{"blue": map[string]any{"id": "dev-id"}}
	desired := PlannedCompositeTarget{
		Category: NodeResource, Binding: Binding{LibraryPath: child.LibraryPath, Export: "box"},
		Inputs: operationObject(t, map[string]EncodedValue{
			"name": StringValue("production"),
		}),
		SensitiveInputPaths: []string{}, SensitiveOutputPaths: []string{},
	}
	prior := operationCompositeState(t, NodeResource)
	request := compositePlanningRequest{
		Address: address, Category: NodeResource, DependsOn: []string{}, Desired: &desired, Prior: &prior,
	}
	_, err = executor.planEvaluationV2CompositeStep(context.Background(), evaluation, request)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"boxes": map[string]any{
		"blue": map[string]any{
			"label": "production-box", "id": "blue-id", "empty": nil,
			"details": map[string]any{"name": "production-box", "id": "blue-id"},
		},
		"green": map[string]any{"id": "green-id"},
	}}, parent.Resources)
	request.Desired = nil
	step, err := executor.planEvaluationV2CompositeStep(context.Background(), evaluation, request)
	require.NoError(t, err)
	require.Equal(t, DecisionDestroy, step.Operation.Composite.Decision)
	require.Equal(t, map[string]any{"boxes": map[string]any{
		"green": map[string]any{"id": "green-id"},
	}}, parent.Resources)
	require.Equal(t, map[string]any{
		"boxes": map[string]any{"blue": map[string]any{"id": "dev-id"}},
	}, other.Resources)
	require.Empty(t, evaluation.run.eval.Resources)
	delete(executor.DAG.Nodes, "resource.apps")
	step, err = executor.planEvaluationV2CompositeStep(context.Background(), evaluation, request)
	require.NoError(t, err)
	require.Equal(t, DecisionDestroy, step.Operation.Composite.Decision)
}

func TestPlanEvaluationV2CompositeStepAllowsAbsentTargets(t *testing.T) {
	executor, evaluation := newCompositeStepEvaluation(t)
	step, err := executor.planEvaluationV2CompositeStep(
		context.Background(), evaluation, compositePlanningRequest{},
	)
	require.NoError(t, err)
	require.Nil(t, step)
	require.Empty(t, evaluation.run.eval.Resources)
}

func TestPlanEvaluationV2CompositeStepRejectsInvalidSetup(t *testing.T) {
	for _, name := range []string{
		"context", "canceled", "executor", "graph", "evaluation", "run", "scope",
		"address", "dependencies", "desired", "prior", "node", "primitive", "category", "binding",
		"arguments", "output", "output value",
	} {
		t.Run(name, func(t *testing.T) {
			executor, evaluation := newCompositeStepEvaluation(t)
			node := executor.DAG.Nodes["resource.main"]
			desired, err := executor.planEvaluationV2CompositeTarget(evaluation, node)
			require.NoError(t, err)
			request := compositePlanningRequest{
				Address: node.Address, Category: node.Kind, DependsOn: []string{}, Desired: desired,
			}
			seeded := map[string]any{"main": map[string]any{"id": "stale"}}
			evaluation.run.eval.Resources = seeded
			ctx := context.Background()
			switch name {
			case "context":
				ctx = nil
			case "canceled":
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
				request.Address = "action.main"
			case "dependencies":
				request.DependsOn = []string{"invalid"}
			case "desired":
				desired.Inputs = EncodedValue{}
			case "prior":
				request.Prior = &CompositeStatePayload{}
			case "node":
				delete(executor.DAG.Nodes, node.Address)
			case "primitive":
				node.CompositeSyntaxBody = nil
			case "category":
				node.Kind = NodeAction
			case "binding":
				desired.Binding.Export = "other"
			case "arguments":
				node.Body = nil
			case "output":
				node.CompositeSyntaxBody.Outputs[0].Body = parseValue(
					t, "{ value: 1 + true }",
				).(*lang.ObjectLit)
			case "output value":
				scope, err := executor.ensureCompositeScope(evaluation.run, node.Address)
				require.NoError(t, err)
				scope.Resources["child"] = map[string]any{"id": math.Inf(1)}
			}
			step, err := executor.planEvaluationV2CompositeStep(ctx, evaluation, request)
			require.Error(t, err)
			require.Nil(t, step)
			if name == "canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.Equal(t, map[string]any{"main": map[string]any{"id": "stale"}}, seeded)
		})
	}
}
