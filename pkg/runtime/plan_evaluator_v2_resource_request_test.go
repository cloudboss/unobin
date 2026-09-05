package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func planEvaluationV2ResourceRequestResolver(
	t *testing.T,
	base registeredResourcePlanningRequest,
) planEvaluationV2ResourceResolver {
	t.Helper()
	return func(binding Binding) (
		*resourceDefinitionRegistration, *resolvedConfigurationDefinition, error,
	) {
		if binding != base.Desired.Binding {
			return nil, nil, fmt.Errorf("unavailable resource %v", binding)
		}
		return base.DesiredRegistration, base.DesiredConfigType, nil
	}
}

func newPlanEvaluationV2ResourceRequestTest(
	t *testing.T,
	inputs EncodedValue,
	prior *ResourceTarget,
) (*Executor, *planEvaluationV2, *planningPassState) {
	t.Helper()
	libraries := map[string]*Library{
		"cloud": {LibraryPath: "example.com/cloud", Configuration: configurationRegistration(1, nil)},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-evaluator-v2/resource-request", "targets")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	snapshot := newPlanEvaluationV2Snapshot(t)
	if prior != nil {
		addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
			Address: "resource.main", Kind: state.StateResource,
			Payload: state.StatePayload{
				Kind: state.StateResource, Resource: &state.ResourceStatePayload{Target: *prior},
			},
		})
	}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(inputs, snapshot, pass)
	require.NoError(t, err)
	return executor, evaluation, pass
}

func TestPlanEvaluationV2ResourceRequestSelectsOperation(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    string
		size     int64
		decision Decision
		reasons  []string
	}{
		{"create", "server", 1, DecisionCreate, []string{}},
		{"unchanged", "server", 1, DecisionNoOp, []string{}},
		{"update", "server", 2, DecisionUpdate, []string{}},
		{"replace", "renamed", 1, DecisionReplace, []string{"address:name"}},
		{"remote missing", "server", 1, DecisionCreate, []string{"remote-missing"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			capture := &registeredPlanningCapture{}
			base := planEvaluationV2ResourceStepRequest(t, capture)
			prior := base.Prior
			if test.name == "create" {
				prior = nil
			}
			if test.name == "remote missing" {
				capture.read = func(string, *recordedConfiguration, *registeredPlanningOutput) (
					*registeredPlanningOutput, error,
				) {
					return nil, ErrNotFound
				}
			}
			executor, evaluation, pass := newPlanEvaluationV2ResourceRequestTest(t,
				operationObject(t, map[string]EncodedValue{
					"name": StringValue(test.input), "size": IntegerValue(test.size),
				}), prior,
			)
			original, err := evaluation.prior.Clone()
			require.NoError(t, err)
			node := executor.DAG.Nodes["resource.main"]
			request, err := executor.planEvaluationV2ResourceRequest(
				evaluation, node, planEvaluationV2ResourceRequestResolver(t, base),
			)
			require.NoError(t, err)
			require.Equal(t, "resource.main", request.Address)
			require.Equal(t, NodeResource, request.Kind)
			require.Equal(t, []string{"library-config.cloud"}, request.DependsOn)
			require.Empty(t, capture.reads)
			configuration, err := executor.planEvaluationV2LibraryConfigurationRequest(
				evaluation, executor.DAG.Nodes["library-config.cloud"],
			)
			require.NoError(t, err)
			_, err = configuration.Plan(context.Background(), pass)
			require.NoError(t, err)
			step, err := request.Plan(context.Background(), pass)
			require.NoError(t, err)
			require.NoError(t, validatePlannedStepV2(request, step))
			require.Equal(t, test.decision, step.Operation.Resource.Decision)
			require.Equal(t, test.reasons, step.Operation.Resource.Reasons)
			wantOutputs := map[string]any{}
			if test.decision == DecisionNoOp {
				wantOutputs["main"] = map[string]any{"id": "server-1", "value": "server"}
			}
			require.Equal(t, wantOutputs, evaluation.run.eval.Resources)
			if prior == nil {
				require.Nil(t, step.Operation.Resource.Prior)
				require.Empty(t, capture.reads)
			} else {
				require.Equal(t, prior, step.Operation.Resource.Prior)
				require.Equal(t, []string{"server:same"}, capture.reads)
			}
			require.Equal(t, original, evaluation.prior)
		})
	}
}

func TestPlanEvaluationV2ResourceRequestResolvesRecordedBinding(t *testing.T) {
	for _, binding := range []Binding{
		{LibraryPath: "example.com/old", Export: "server"},
		{LibraryPath: "example.com/cloud", Export: "old-server"},
	} {
		t.Run(binding.LibraryPath+"/"+binding.Export, func(t *testing.T) {
			capture := &registeredPlanningCapture{}
			base := planEvaluationV2ResourceStepRequest(t, capture)
			definition := registeredPlanningDefinition(1, IdentityConfiguration)
			priorRegistration := newRegisteredPlanningResource(t, definition, capture, "old")
			priorConfigType, configuration := registeredOperationConfiguration(
				t, binding.LibraryPath, "recorded",
			)
			prior := registeredPlanningTarget(t, definition, priorRegistration, binding, configuration,
				"server", 1, &registeredPlanningOutput{ID: "server-1", Value: "server"},
			)
			executor, evaluation, pass := newPlanEvaluationV2ResourceRequestTest(t,
				base.Desired.Inputs, &prior,
			)
			var resolved []Binding
			resolve := func(binding Binding) (
				*resourceDefinitionRegistration, *resolvedConfigurationDefinition, error,
			) {
				resolved = append(resolved, binding)
				if binding == prior.Binding {
					return priorRegistration, priorConfigType, nil
				}
				return planEvaluationV2ResourceRequestResolver(t, base)(binding)
			}
			request, err := executor.planEvaluationV2ResourceRequest(
				evaluation, executor.DAG.Nodes["resource.main"], resolve,
			)
			require.NoError(t, err)
			require.Equal(t, []Binding{base.Desired.Binding, prior.Binding}, resolved)
			require.Empty(t, capture.reads)
			config, err := executor.planEvaluationV2LibraryConfigurationRequest(
				evaluation, executor.DAG.Nodes["library-config.cloud"],
			)
			require.NoError(t, err)
			_, err = config.Plan(context.Background(), pass)
			require.NoError(t, err)
			step, err := request.Plan(context.Background(), pass)
			require.NoError(t, err)
			require.Equal(t, DecisionReplace, step.Operation.Resource.Decision)
			require.Equal(t, []string{"binding"}, step.Operation.Resource.Reasons)
			require.Equal(t, prior, *step.Operation.Resource.Prior)
			require.Equal(t, base.Desired.Binding, step.Operation.Resource.Desired.Binding)
			require.Equal(t, []string{"old:recorded"}, capture.reads)
		})
	}
}

func TestPlanEvaluationV2ResourceRequestUsesInstancesAndDetachedMetadata(t *testing.T) {
	base := planEvaluationV2ResourceStepRequest(t, &registeredPlanningCapture{})
	executor, evaluation, pass := newPlanEvaluationV2ResourceRequestTest(t,
		base.Desired.Inputs, base.Prior,
	)
	resolve := planEvaluationV2ResourceRequestResolver(t, base)
	nodes, err := executor.expandPlanEvaluationV2Node(evaluation, executor.DAG.Nodes["resource.many"])
	require.NoError(t, err)
	require.Equal(t, []string{"resource.many['']", "resource.many['blue']"},
		planEvaluationV2NodeAddresses(nodes))
	for _, node := range nodes {
		entry := *evaluation.prior.Find("resource.main")
		entry.Address = node.Address
		require.NoError(t, evaluation.prior.SetEntry(entry))
	}
	config, err := executor.planEvaluationV2LibraryConfigurationRequest(
		evaluation, executor.DAG.Nodes["library-config.cloud"],
	)
	require.NoError(t, err)
	_, err = config.Plan(context.Background(), pass)
	require.NoError(t, err)
	for i, node := range nodes {
		request, err := executor.planEvaluationV2ResourceRequest(evaluation, node, resolve)
		require.NoError(t, err)
		address := node.Address
		node.Address = "resource.changed"
		request.DependsOn[0] = "resource.changed"
		evaluation.prior.Find(address).Payload.Resource.Target.Inputs = operationObject(
			t, map[string]EncodedValue{"name": StringValue("changed"), "size": IntegerValue(4)},
		)
		step, err := request.Plan(context.Background(), pass)
		require.NoError(t, err)
		require.Equal(t, address, step.Address)
		require.Equal(t, []string{"library-config.cloud"}, step.DependsOn)
		require.Equal(t, base.Prior.Inputs, step.Operation.Resource.Prior.Inputs)
		require.Equal(t, []Decision{DecisionUpdate, DecisionNoOp}[i], step.Operation.Resource.Decision)
	}
	request, err := executor.planEvaluationV2ResourceRequest(
		evaluation, executor.DAG.Nodes["resource.child"], resolve,
	)
	require.NoError(t, err)
	require.Equal(t, []string{
		"library-config.cloud", "resource.many['']", "resource.many['blue']",
	}, request.DependsOn)
	step, err := request.Plan(context.Background(), pass)
	require.NoError(t, err)
	require.Equal(t, base.Desired.Inputs, step.Operation.Resource.Desired.Inputs)
}

func TestPlanEvaluationV2ResourceRequestRejectsInvalidSetup(t *testing.T) {
	for _, name := range []string{
		"executor", "graph", "evaluation", "run", "scope", "prior", "configurations",
		"node", "kind", "composite", "address", "for-each", "import", "library path",
		"resolver", "registration", "configuration", "configuration path", "lookup", "prior kind",
	} {
		t.Run(name, func(t *testing.T) {
			capture := &registeredPlanningCapture{}
			base := planEvaluationV2ResourceStepRequest(t, capture)
			executor, evaluation, _ := newPlanEvaluationV2ResourceRequestTest(t,
				base.Desired.Inputs, base.Prior,
			)
			node := executor.DAG.Nodes["resource.main"]
			resolve := planEvaluationV2ResourceRequestResolver(t, base)
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
				node.Kind = NodeAction
			case "composite":
				node.CompositeSyntaxBody = &syntax.FactoryBody{}
			case "address":
				node.Address = "invalid"
			case "for-each":
				node = executor.DAG.Nodes["resource.many"]
			case "import":
				node.Alias = "missing"
			case "library path":
				node.LibraryPath = "example.com/wrong"
			case "resolver":
				resolve = nil
			case "registration":
				base.DesiredRegistration = nil
				resolve = planEvaluationV2ResourceRequestResolver(t, base)
			case "configuration":
				base.DesiredConfigType = nil
				resolve = planEvaluationV2ResourceRequestResolver(t, base)
			case "configuration path":
				base.DesiredConfigType.libraryPath = "example.com/wrong"
			case "lookup":
				node.Type = "unavailable"
			case "prior kind":
				entry := evaluation.prior.Find(node.Address)
				entry.Kind = state.StateComposite
			}
			request, err := executor.planEvaluationV2ResourceRequest(evaluation, node, resolve)
			require.Error(t, err)
			require.Equal(t, planStepV2Request{}, request)
			require.Empty(t, capture.reads)
		})
	}
}

func TestPlanEvaluationV2ResourceRequestStopsBeforeRead(t *testing.T) {
	for _, name := range []string{"prior lookup", "configuration pending evaluation", "read error",
		"context", "canceled context", "pass", "facts"} {
		t.Run(name, func(t *testing.T) {
			wantErr := errors.New("provider unavailable")
			capture := &registeredPlanningCapture{}
			base := planEvaluationV2ResourceStepRequest(t, capture)
			executor, evaluation, pass := newPlanEvaluationV2ResourceRequestTest(t,
				base.Desired.Inputs, base.Prior,
			)
			if name == "prior lookup" {
				evaluation.prior.Find("resource.main").Payload.Resource.Target.Binding.Export = "missing"
			}
			request, err := executor.planEvaluationV2ResourceRequest(evaluation,
				executor.DAG.Nodes["resource.main"], planEvaluationV2ResourceRequestResolver(t, base),
			)
			if name == "prior lookup" {
				require.ErrorContains(t, err, "resource.main: prior resource: unavailable resource")
				require.Equal(t, planStepV2Request{}, request)
				require.Empty(t, capture.reads)
				return
			}
			require.NoError(t, err)
			ctx := context.Background()
			switch name {
			case "read error":
				config, err := executor.planEvaluationV2LibraryConfigurationRequest(
					evaluation, executor.DAG.Nodes["library-config.cloud"],
				)
				require.NoError(t, err)
				_, err = config.Plan(ctx, pass)
				require.NoError(t, err)
				capture.read = func(string, *recordedConfiguration, *registeredPlanningOutput) (
					*registeredPlanningOutput, error,
				) {
					return nil, wantErr
				}
			case "context":
				ctx = nil
			case "canceled context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "pass":
				pass = nil
			case "facts":
				pass.facts = nil
			}
			step, err := request.Plan(ctx, pass)
			require.Error(t, err)
			require.Nil(t, step)
			if name == "read error" {
				require.ErrorIs(t, err, wantErr)
				require.Equal(t, []string{"server:same"}, capture.reads)
			} else {
				require.Empty(t, capture.reads)
			}
		})
	}
}

func TestPlanEvaluationV2ResourceRequestPlansDependentTargets(t *testing.T) {
	capture := &registeredPlanningCapture{}
	base := planEvaluationV2ResourceStepRequest(t, capture)
	resolve := planEvaluationV2ResourceRequestResolver(t, base)
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
	original, err := snapshot.Clone()
	require.NoError(t, err)
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
			request, err := executor.planEvaluationV2ResourceRequest(
				evaluation, dag.Nodes[address], resolve,
			)
			require.NoError(t, err)
			requests = append(requests, request)
		}
		require.Empty(t, evaluation.configurations)
		return requests, nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, passes)
	require.Len(t, steps, 4)
	require.Equal(t, "library-config.cloud", steps[0].Address)
	for i, address := range addresses {
		step := steps[i+1]
		require.Equal(t, address, step.Address)
		require.Equal(t, base.Desired.Binding, step.Operation.Resource.Desired.Binding)
		if i == 0 {
			require.Equal(t, DecisionUpdate, step.Operation.Resource.Decision)
			require.Equal(t, []string{"library-config.cloud"}, step.DependsOn)
			continue
		}
		require.Equal(t, DecisionReplace, step.Operation.Resource.Decision)
		require.Equal(t, []string{"library-config.cloud", addresses[i-1]}, step.DependsOn)
		fields, _ := step.Operation.Resource.Desired.Inputs.ObjectFields()
		pending, err := PendingEncodedValue([]string{addresses[i-1] + ".value"})
		require.NoError(t, err)
		require.Equal(t, pending, fields["name"])
	}
	require.Equal(t, []string{"server:same", "server:same", "server:same"}, capture.reads)
	require.Equal(t, original, snapshot)
}
