package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func newActionRequestEvaluation(
	t *testing.T,
	runs *int,
) (*Executor, *planEvaluationV2, *planningPassState) {
	t.Helper()
	library := actionTargetLibrary(t, runs)
	library.Configuration = planEvaluationV2Libraries()["cloud"].Configuration
	libraries := map[string]*Library{"cloud": library}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-evaluator-v2/action-request", "targets")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{"endpoint": StringValue("desired")}),
		newPlanEvaluationV2Snapshot(t), pass,
	)
	require.NoError(t, err)
	return executor, evaluation, pass
}

func TestPlanEvaluationV2ActionRequestPlansAfterConfiguration(t *testing.T) {
	for _, name := range []string{"new", "unchanged", "changed trigger"} {
		t.Run(name, func(t *testing.T) {
			runs := 0
			executor, evaluation, pass := newActionRequestEvaluation(t, &runs)
			node := executor.DAG.Nodes["action.main"]
			config, err := executor.planEvaluationV2LibraryConfigurationRequest(
				evaluation, executor.DAG.Nodes["library-config.cloud"],
			)
			require.NoError(t, err)
			_, err = config.Plan(context.Background(), pass)
			require.NoError(t, err)
			target, err := executor.planEvaluationV2ActionTarget(evaluation, node)
			require.NoError(t, err)
			var prior *ActionStatePayload
			if name != "new" {
				value := operationActionState(t)
				value.Binding, value.Inputs = target.Binding, target.Inputs
				value.Configuration, value.TriggerHash = *target.Configuration.Record, target.TriggerHash
				if name == "changed trigger" {
					value.TriggerHash = "old"
				}
				prior = &value
				addPlanEvaluationV2Entry(t, evaluation.prior, state.StateEntryV2{
					Address: node.Address, Kind: state.StateAction,
					Payload: state.StatePayload{Kind: state.StateAction, Action: prior},
				})
			}
			original, err := evaluation.prior.Clone()
			require.NoError(t, err)
			clear(evaluation.configurations)
			request, err := executor.planEvaluationV2ActionRequest(evaluation, node)
			require.NoError(t, err)
			require.Equal(t, "action.main", request.Address)
			require.Equal(t, NodeAction, request.Kind)
			require.Equal(t, []string{"library-config.cloud"}, request.DependsOn)
			_, err = config.Plan(context.Background(), pass)
			require.NoError(t, err)
			step, err := request.Plan(context.Background(), pass)
			require.NoError(t, err)
			require.NoError(t, validatePlannedStepV2(request, step))
			require.Equal(t, target, step.Operation.Action.Desired)
			require.Equal(t, prior, step.Operation.Action.Prior)
			require.Equal(t, []string{"/size"}, target.SensitiveInputPaths)
			require.Equal(t, []string{"/id"}, target.SensitiveOutputPaths)
			if name == "unchanged" {
				require.Equal(t, DecisionSkip, step.Operation.Action.Decision)
				require.Equal(t, map[string]any{"main": map[string]any{"sent": true}},
					evaluation.run.eval.Actions)
			} else {
				require.Equal(t, DecisionRerun, step.Operation.Action.Decision)
				require.Empty(t, evaluation.run.eval.Actions)
			}
			require.Equal(t, original, evaluation.prior)
			require.Zero(t, runs)
		})
	}
}

func TestPlanEvaluationV2ActionRequestRetainsDeferredTargets(t *testing.T) {
	for _, name := range []string{"pending", "triggered", "always", "pending configuration"} {
		t.Run(name, func(t *testing.T) {
			runs := 0
			executor, evaluation, pass := newActionRequestEvaluation(t, &runs)
			address := "action." + name
			if name == "pending configuration" {
				address = "action.main"
				evaluation.run.eval.Inputs["endpoint"] = PendingValue{
					Refs: []string{"resource.network.id"},
				}
			}
			prior := operationActionState(t)
			addPlanEvaluationV2Entry(t, evaluation.prior, state.StateEntryV2{
				Address: address, Kind: state.StateAction,
				Payload: state.StatePayload{Kind: state.StateAction, Action: &prior},
			})
			node := executor.DAG.Nodes[address]
			request, err := executor.planEvaluationV2ActionRequest(evaluation, node)
			require.NoError(t, err)
			config, err := executor.planEvaluationV2LibraryConfigurationRequest(
				evaluation, executor.DAG.Nodes["library-config.cloud"],
			)
			require.NoError(t, err)
			_, err = config.Plan(context.Background(), pass)
			require.NoError(t, err)
			evaluation.run.eval.Actions[node.Name] = map[string]any{"sent": true}
			step, err := request.Plan(context.Background(), pass)
			require.NoError(t, err)
			require.NoError(t, validatePlannedStepV2(request, step))
			require.Equal(t, DecisionRerun, step.Operation.Action.Decision)
			target := step.Operation.Action.Desired
			require.Equal(t, name == "pending", target.Inputs.HasPending())
			if name == "pending configuration" {
				require.Equal(t, pendingOperationConfiguration(), target.Configuration)
			} else {
				require.Empty(t, target.TriggerHash)
			}
			require.Empty(t, evaluation.run.eval.Actions)
			if name == "pending" || name == "triggered" {
				evaluation.run.eval.Resources["upstream"] = map[string]any{"id": "resolved"}
				resolved, err := request.Plan(context.Background(), pass)
				require.NoError(t, err)
				require.False(t, resolved.Operation.Action.Desired.Inputs.HasPending())
				require.NotEmpty(t, resolved.Operation.Action.Desired.TriggerHash)
				require.Empty(t, target.TriggerHash)
			}
			require.Zero(t, runs)
		})
	}
}

func TestPlanEvaluationV2ActionRequestUsesInstancesAndDetachedMetadata(t *testing.T) {
	runs := 0
	executor, evaluation, pass := newActionRequestEvaluation(t, &runs)
	config, err := executor.planEvaluationV2LibraryConfigurationRequest(
		evaluation, executor.DAG.Nodes["library-config.cloud"],
	)
	require.NoError(t, err)
	_, err = config.Plan(context.Background(), pass)
	require.NoError(t, err)
	nodes, err := executor.expandPlanEvaluationV2Node(
		evaluation, executor.DAG.Nodes["action.many"],
	)
	require.NoError(t, err)
	require.Equal(t, []string{"action.many['']", "action.many['blue']"},
		planEvaluationV2NodeAddresses(nodes))
	for i, node := range nodes {
		prior := operationActionState(t)
		addPlanEvaluationV2Entry(t, evaluation.prior, state.StateEntryV2{
			Address: node.Address, Kind: state.StateAction,
			Payload: state.StatePayload{Kind: state.StateAction, Action: &prior},
		})
		request, err := executor.planEvaluationV2ActionRequest(evaluation, node)
		require.NoError(t, err)
		address := node.Address
		node.Address = "action.changed"
		request.DependsOn[0] = "resource.changed"
		evaluation.prior.Find(address).Payload.Action.DependsOn = []string{"resource.changed"}
		step, err := request.Plan(context.Background(), pass)
		require.NoError(t, err)
		require.Equal(t, address, step.Address)
		require.Equal(t, []string{"library-config.cloud"}, step.DependsOn)
		require.Equal(t, &prior, step.Operation.Action.Prior)
		fields, _ := step.Operation.Action.Desired.Inputs.ObjectFields()
		require.Equal(t, StringValue([]string{"empty", "blue"}[i]), fields["name"])
		step.Operation.Action.Prior.DependsOn = []string{"resource.other"}
		again, err := request.Plan(context.Background(), pass)
		require.NoError(t, err)
		require.Equal(t, &prior, again.Operation.Action.Prior)
	}
	request, err := executor.planEvaluationV2ActionRequest(
		evaluation, executor.DAG.Nodes["action.dependent"],
	)
	require.NoError(t, err)
	require.Equal(t, []string{
		"action.many['']", "action.many['blue']", "library-config.cloud",
	}, request.DependsOn)
	require.Zero(t, runs)
}

func TestPlanEvaluationV2ActionRequestRejectsInvalidSetup(t *testing.T) {
	for _, name := range []string{
		"executor", "graph", "evaluation", "run", "scope", "prior", "configurations",
		"node", "kind", "composite", "address", "for-each", "import", "library path",
		"binding", "registration", "lookup", "prior kind", "prior payload", "dependencies",
	} {
		t.Run(name, func(t *testing.T) {
			runs := 0
			executor, evaluation, _ := newActionRequestEvaluation(t, &runs)
			node := executor.DAG.Nodes["action.main"]
			switch name {
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
			case "prior":
				evaluation.prior = nil
			case "configurations":
				evaluation.configurations = nil
			case "node":
				node = nil
			case "kind":
				node.Kind = NodeDataSource
			case "composite":
				node.CompositeSyntaxBody = &syntax.FactoryBody{}
			case "address":
				node.Address = "invalid"
			case "for-each":
				node = executor.DAG.Nodes["action.many"]
			case "import":
				node.Alias = "missing"
			case "library path":
				node.LibraryPath = "example.com/wrong"
			case "binding":
				executor.Libraries["cloud"].LibraryPath = ""
			case "registration":
				executor.Libraries["cloud"].Actions["notify"] = nil
			case "lookup":
				node.Type = "missing"
			case "prior kind", "prior payload":
				prior := operationActionState(t)
				addPlanEvaluationV2Entry(t, evaluation.prior, state.StateEntryV2{
					Address: node.Address, Kind: state.StateAction,
					Payload: state.StatePayload{Kind: state.StateAction, Action: &prior},
				})
				if name == "prior kind" {
					evaluation.prior.Find(node.Address).Kind = state.StateComposite
				} else {
					evaluation.prior.Find(node.Address).Payload.Action = nil
				}
			case "dependencies":
				evaluation.run.forEachInstances = nil
			}
			request, err := executor.planEvaluationV2ActionRequest(evaluation, node)
			require.Error(t, err)
			require.Equal(t, planStepV2Request{}, request)
			require.Zero(t, runs)
		})
	}
}

func TestPlanEvaluationV2ActionRequestStopsOnFailure(t *testing.T) {
	for _, name := range []string{
		"unevaluated configuration", "context", "canceled context", "body",
	} {
		t.Run(name, func(t *testing.T) {
			runs := 0
			executor, evaluation, pass := newActionRequestEvaluation(t, &runs)
			node := executor.DAG.Nodes["action.main"]
			if name == "body" {
				node.Body = nil
			}
			request, err := executor.planEvaluationV2ActionRequest(evaluation, node)
			require.NoError(t, err)
			if name != "unevaluated configuration" {
				config, err := executor.planEvaluationV2LibraryConfigurationRequest(
					evaluation, executor.DAG.Nodes["library-config.cloud"],
				)
				require.NoError(t, err)
				_, err = config.Plan(context.Background(), pass)
				require.NoError(t, err)
			}
			ctx := context.Background()
			switch name {
			case "context":
				ctx = nil
			case "canceled context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			evaluation.run.eval.Actions["main"] = map[string]any{"sent": true}
			step, err := request.Plan(ctx, pass)
			require.Error(t, err)
			require.Nil(t, step)
			if name == "canceled context" {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.Equal(t, map[string]any{"main": map[string]any{"sent": true}},
				evaluation.run.eval.Actions)
			require.Zero(t, runs)
		})
	}
}
