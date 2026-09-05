package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type dataSourceRequestRegistration struct {
	DataSourceRegistration
	read func(context.Context, any, any) (any, error)
}

func (r dataSourceRequestRegistration) Read(ctx context.Context, receiver, cfg any) (any, error) {
	return r.read(ctx, receiver, cfg)
}

func newDataSourceRequestEvaluation(
	t *testing.T,
	read func(context.Context, any, any) (any, error),
) (*Executor, *planEvaluationV2, *planningPassState) {
	t.Helper()
	reads := 0
	library := dataSourceTargetLibrary(t, &reads)
	library.Configuration = planEvaluationV2Libraries()["cloud"].Configuration
	library.DataSources["lookup"] = dataSourceRequestRegistration{
		DataSourceRegistration: library.DataSources["lookup"], read: read,
	}
	libraries := map[string]*Library{"cloud": library}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-evaluator-v2/data-source-request", "targets")
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

func TestPlanEvaluationV2DataSourceRequestReadsDesiredTarget(t *testing.T) {
	for _, hasPrior := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "recorded"}[hasPrior], func(t *testing.T) {
			var calls []string
			executor, evaluation, pass := newDataSourceRequestEvaluation(t,
				func(_ context.Context, receiver, config any) (any, error) {
					input := receiver.(*dataSourceTargetInput)
					require.Equal(t, uint8(2), input.Size)
					require.Nil(t, input.Nullable)
					require.Nil(t, input.Zones)
					endpoint := config.(*planEvaluationV2CloudConfiguration).Endpoint.Value
					calls = append(calls, input.Name+":"+endpoint)
					input.Name = "provider mutation"
					return &dataSourceTargetOutput{ID: "fresh"}, nil
				},
			)
			var prior *DataSourceStatePayload
			if hasPrior {
				value := operationDataSourceState(t)
				prior = &value
				addPlanEvaluationV2Entry(t, evaluation.prior, state.StateEntryV2{
					Address: "data-source.main", Kind: state.StateDataSource,
					Payload: state.StatePayload{Kind: state.StateDataSource, DataSource: prior},
				})
			}
			original, err := evaluation.prior.Clone()
			require.NoError(t, err)
			request, err := executor.planEvaluationV2DataSourceRequest(
				evaluation, executor.DAG.Nodes["data-source.main"],
			)
			require.NoError(t, err)
			require.Equal(t, "data-source.main", request.Address)
			require.Equal(t, NodeDataSource, request.Kind)
			require.Equal(t, []string{"library-config.cloud"}, request.DependsOn)
			require.Empty(t, calls)
			config, err := executor.planEvaluationV2LibraryConfigurationRequest(
				evaluation, executor.DAG.Nodes["library-config.cloud"],
			)
			require.NoError(t, err)
			_, err = config.Plan(context.Background(), pass)
			require.NoError(t, err)
			step, err := request.Plan(context.Background(), pass)
			require.NoError(t, err)
			require.NoError(t, validatePlannedStepV2(request, step))
			operation := step.Operation.DataSource
			require.Equal(t, DecisionRead, operation.Decision)
			require.Equal(t, prior, operation.Prior)
			require.Equal(t, []string{"image:desired"}, calls)
			require.Equal(t, map[string]any{"main": map[string]any{"id": "fresh"}},
				evaluation.run.eval.Data)
			require.Equal(t, original, evaluation.prior)
			fields, _ := operation.Desired.Inputs.ObjectFields()
			require.Equal(t, StringValue("image"), fields["name"])
			require.Equal(t, []string{"/size"}, operation.Desired.SensitiveInputPaths)
			require.Equal(t, []string{"/id"}, operation.Desired.SensitiveOutputPaths)
			_, err = request.Plan(context.Background(), pass)
			require.NoError(t, err)
			require.Equal(t, []string{"image:desired"}, calls)
		})
	}
}

func TestPlanEvaluationV2DataSourceRequestPlansDependentTargets(t *testing.T) {
	var calls []string
	executor, evaluation, _ := newDataSourceRequestEvaluation(t,
		func(_ context.Context, receiver, config any) (any, error) {
			input := receiver.(*dataSourceTargetInput)
			endpoint := config.(*planEvaluationV2CloudConfiguration).Endpoint.Value
			calls = append(calls, input.Name+":"+endpoint)
			return &dataSourceTargetOutput{ID: input.Name + "-id"}, nil
		},
	)
	inputs := operationObject(t, map[string]EncodedValue{"endpoint": StringValue("desired")})
	passes := 0
	steps, err := planStepsV2(context.Background(), func(pass *planningPassState) (
		[]planStepV2Request, error,
	) {
		passes++
		current, err := executor.preparePlanEvaluationV2(inputs, evaluation.prior, pass)
		require.NoError(t, err)
		var requests []planStepV2Request
		config, err := executor.planEvaluationV2LibraryConfigurationRequest(
			current, executor.DAG.Nodes["library-config.cloud"],
		)
		require.NoError(t, err)
		requests = append(requests, config)
		for _, address := range []string{"data-source.main", "data-source.child"} {
			request, err := executor.planEvaluationV2DataSourceRequest(
				current, executor.DAG.Nodes[address],
			)
			require.NoError(t, err)
			requests = append(requests, request)
		}
		output, err := executor.planEvaluationV2OutputRequest(current, executor.DAG.Nodes["output.id"])
		require.NoError(t, err)
		requests = append(requests, output)
		if passes == 1 {
			require.NoError(t, pass.invalidateOutputs("resource.unrelated"))
		}
		return requests, nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, passes)
	require.Equal(t, []string{"image:desired", "image-id:desired"}, calls)
	require.Equal(t, StringValue("image-id-id"), steps[3].Operation.Output.Value)
	require.Equal(t, []string{"data-source.main", "library-config.cloud"}, steps[2].DependsOn)
	require.True(t, steps[3].Operation.Output.Sensitive)
}

func TestPlanEvaluationV2DataSourceRequestReadsWithoutConfiguration(t *testing.T) {
	reads := 0
	executor, evaluation, pass := newDataSourceTargetEvaluation(t,
		map[string]*Library{"cloud": dataSourceTargetLibrary(t, &reads)}, "targets",
	)
	request, err := executor.planEvaluationV2DataSourceRequest(
		evaluation, executor.DAG.Nodes["data-source.main"],
	)
	require.NoError(t, err)
	step, err := request.Plan(context.Background(), pass)
	require.NoError(t, err)
	require.NoError(t, validatePlannedStepV2(request, step))
	require.Equal(t, 1, reads)
	require.Equal(t, operationObject(t, map[string]EncodedValue{"id": StringValue("secret")}),
		*step.Operation.DataSource.ObservedOutputs)
	require.Empty(t, step.Operation.DataSource.Desired.Configuration.Record.Address)
	require.Equal(t, operationObject(t, map[string]EncodedValue{}),
		step.Operation.DataSource.Desired.Configuration.Record.Value)
}

func TestPlanEvaluationV2DataSourceRequestDefersPendingValues(t *testing.T) {
	for _, name := range []string{"inputs", "configuration"} {
		t.Run(name, func(t *testing.T) {
			executor, evaluation, pass := newDataSourceRequestEvaluation(t,
				func(context.Context, any, any) (any, error) {
					t.Fatal("pending request must not read")
					return nil, nil
				},
			)
			config, err := executor.planEvaluationV2LibraryConfigurationRequest(
				evaluation, executor.DAG.Nodes["library-config.cloud"],
			)
			require.NoError(t, err)
			_, err = config.Plan(context.Background(), pass)
			require.NoError(t, err)
			node := executor.DAG.Nodes["data-source.main"]
			if name == "inputs" {
				node = executor.DAG.Nodes["data-source.child"]
			} else {
				configuration := evaluation.configurations["library-config.cloud"]
				configuration.planned = pendingOperationConfiguration()
				evaluation.configurations["library-config.cloud"] = configuration
			}
			evaluation.run.eval.Data[node.Name] = map[string]any{"id": "stale"}
			request, err := executor.planEvaluationV2DataSourceRequest(evaluation, node)
			require.NoError(t, err)
			step, err := request.Plan(context.Background(), pass)
			require.NoError(t, err)
			require.NoError(t, validatePlannedStepV2(request, step))
			require.Equal(t, DecisionRead, step.Operation.DataSource.Decision)
			require.Nil(t, step.Operation.DataSource.ObservedOutputs)
			require.Empty(t, evaluation.run.eval.Data)
		})
	}
}

func TestPlanEvaluationV2DataSourceRequestUsesInstancesAndDetachedMetadata(t *testing.T) {
	var calls []string
	executor, evaluation, pass := newDataSourceRequestEvaluation(t,
		func(_ context.Context, receiver, _ any) (any, error) {
			name := receiver.(*dataSourceTargetInput).Name
			calls = append(calls, name)
			return &dataSourceTargetOutput{ID: name}, nil
		},
	)
	config, err := executor.planEvaluationV2LibraryConfigurationRequest(
		evaluation, executor.DAG.Nodes["library-config.cloud"],
	)
	require.NoError(t, err)
	_, err = config.Plan(context.Background(), pass)
	require.NoError(t, err)
	nodes, err := executor.expandPlanEvaluationV2Node(
		evaluation, executor.DAG.Nodes["data-source.many"],
	)
	require.NoError(t, err)
	require.Equal(t, []string{"data-source.many['']", "data-source.many['blue']"},
		planEvaluationV2NodeAddresses(nodes))
	for _, node := range nodes {
		prior := operationDataSourceState(t)
		addPlanEvaluationV2Entry(t, evaluation.prior, state.StateEntryV2{
			Address: node.Address, Kind: state.StateDataSource,
			Payload: state.StatePayload{Kind: state.StateDataSource, DataSource: &prior},
		})
		request, err := executor.planEvaluationV2DataSourceRequest(evaluation, node)
		require.NoError(t, err)
		address := node.Address
		node.Address = "data-source.changed"
		request.DependsOn[0] = "resource.changed"
		evaluation.prior.Find(address).Payload.DataSource.DependsOn = []string{"resource.changed"}
		step, err := request.Plan(context.Background(), pass)
		require.NoError(t, err)
		require.Equal(t, address, step.Address)
		require.Equal(t, []string{"library-config.cloud"}, step.DependsOn)
		require.Equal(t, &prior, step.Operation.DataSource.Prior)
	}
	request, err := executor.planEvaluationV2DataSourceRequest(
		evaluation, executor.DAG.Nodes["data-source.dependent"],
	)
	require.NoError(t, err)
	require.Equal(t, []string{
		"data-source.many['']", "data-source.many['blue']", "library-config.cloud",
	}, request.DependsOn)
	step, err := request.Plan(context.Background(), pass)
	require.NoError(t, err)
	require.Equal(t, []string{"empty", "blue", "blue"}, calls)
	require.Equal(t, operationObject(t, map[string]EncodedValue{"id": StringValue("blue")}),
		*step.Operation.DataSource.ObservedOutputs)
}

func TestPlanEvaluationV2DataSourceRequestRejectsInvalidSetup(t *testing.T) {
	for _, name := range []string{
		"executor", "graph", "evaluation", "run", "scope", "prior", "configurations",
		"node", "kind", "composite", "address", "for-each", "import", "library path",
		"registration", "lookup", "prior kind",
	} {
		t.Run(name, func(t *testing.T) {
			executor, evaluation, _ := newDataSourceRequestEvaluation(t,
				func(context.Context, any, any) (any, error) {
					t.Fatal("invalid request must not read")
					return nil, nil
				},
			)
			node := executor.DAG.Nodes["data-source.main"]
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
				node = executor.DAG.Nodes["data-source.many"]
			case "import":
				node.Alias = "missing"
			case "library path":
				node.LibraryPath = "example.com/wrong"
			case "registration":
				executor.Libraries["cloud"].DataSources["lookup"] = nil
			case "lookup":
				node.Type = "missing"
			case "prior kind":
				prior := operationDataSourceState(t)
				addPlanEvaluationV2Entry(t, evaluation.prior, state.StateEntryV2{
					Address: node.Address, Kind: state.StateDataSource,
					Payload: state.StatePayload{Kind: state.StateDataSource, DataSource: &prior},
				})
				evaluation.prior.Find(node.Address).Kind = state.StateComposite
			}
			request, err := executor.planEvaluationV2DataSourceRequest(evaluation, node)
			require.Error(t, err)
			require.Equal(t, planStepV2Request{}, request)
		})
	}
}

func TestPlanEvaluationV2DataSourceRequestStopsOnFailure(t *testing.T) {
	readErr := errors.New("read failed")
	for _, name := range []string{
		"unevaluated configuration", "context", "canceled context", "pass", "facts", "cache",
		"read error", "read panic", "nil output", "wrong output",
	} {
		t.Run(name, func(t *testing.T) {
			reads := 0
			executor, evaluation, pass := newDataSourceRequestEvaluation(t,
				func(context.Context, any, any) (any, error) {
					reads++
					switch name {
					case "read error":
						return nil, readErr
					case "read panic":
						panic("read panicked")
					case "nil output":
						return (*dataSourceTargetOutput)(nil), nil
					case "wrong output":
						return &struct{ ID string }{ID: "wrong"}, nil
					default:
						t.Fatal("invalid planning setup must not read")
						return nil, nil
					}
				},
			)
			request, err := executor.planEvaluationV2DataSourceRequest(
				evaluation, executor.DAG.Nodes["data-source.main"],
			)
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
			case "pass":
				pass = nil
			case "facts":
				pass.facts = nil
			case "cache":
				pass.reads = nil
			}
			step, err := request.Plan(ctx, pass)
			require.Error(t, err)
			require.Nil(t, step)
			require.Empty(t, evaluation.run.eval.Data)
			switch name {
			case "read error":
				require.ErrorIs(t, err, readErr)
			case "canceled context":
				require.ErrorIs(t, err, context.Canceled)
			}
			if reads > 0 {
				_, err = request.Plan(ctx, pass)
				require.Error(t, err)
				require.Equal(t, 1, reads)
			}
		})
	}
}
