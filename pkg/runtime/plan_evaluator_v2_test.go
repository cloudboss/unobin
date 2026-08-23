package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type planEvaluationV2CloudConfiguration struct {
	Endpoint cfg.String
	Region   *cfg.String
}

func planEvaluationV2Libraries() map[string]*Library {
	return map[string]*Library{
		"cloud": {
			LibraryPath: "example.com/cloud",
			Configuration: &cfg.ConfigurationType[*planEvaluationV2CloudConfiguration]{
				SchemaVersion: 1,
				New: func() *planEvaluationV2CloudConfiguration {
					return &planEvaluationV2CloudConfiguration{
						Region: &cfg.String{Default: "us-east-1"},
					}
				},
			},
		},
	}
}

func newPlanEvaluationV2Snapshot(t *testing.T) *state.SnapshotV2 {
	t.Helper()
	snapshot, err := state.NewSnapshotV2(
		state.FactoryInfo{
			Name:            "factory",
			Version:         "1.0.0",
			ContentRevision: strings.Repeat("f", 64),
		},
		"production",
	)
	require.NoError(t, err)
	snapshot.GeneratedAt = time.Unix(1, 0).UTC()
	return snapshot
}

func newPlanEvaluationV2Facts() *planningFacts {
	return &planningFacts{
		replacements: map[string]map[string]bool{},
		invalidated:  map[string]bool{},
	}
}

func newPlanEvaluationV2Pass(facts *planningFacts) *planningPassState {
	return &planningPassState{
		facts:   facts,
		reads:   &planningReadCache{results: map[string]*resourceReadResult{}},
		changes: map[string]bool{},
	}
}

func planEvaluationV2ResourceEntry(
	t *testing.T,
	address string,
	outputs EncodedValue,
) state.StateEntryV2 {
	t.Helper()
	target := validOperationResourceTarget(t)
	target.Outputs = outputs
	target.DependsOn = []string{}
	target.SensitiveOutputPaths = []string{}
	require.NoError(t, target.Validate())
	return state.StateEntryV2{
		Address: address,
		Kind:    state.StateResource,
		Payload: state.StatePayload{
			Kind:     state.StateResource,
			Resource: &state.ResourceStatePayload{Target: target},
		},
	}
}

func addPlanEvaluationV2Entry(
	t *testing.T,
	snapshot *state.SnapshotV2,
	entry state.StateEntryV2,
) {
	t.Helper()
	require.NoError(t, snapshot.SetEntry(entry))
}

func TestPreparePlanEvaluationV2SeedsSnapshot(t *testing.T) {
	ratio, err := NumberValue(1.5)
	require.NoError(t, err)
	items, err := ListValue([]EncodedValue{StringValue("one"), IntegerValue(2)})
	require.NoError(t, err)
	labels, err := MapValue(map[string]EncodedValue{"team": StringValue("runtime")})
	require.NoError(t, err)
	inputs := operationObject(t, map[string]EncodedValue{
		"count":       IntegerValue(3),
		"enabled":     BooleanValue(true),
		"environment": StringValue("production"),
		"items":       items,
		"labels":      labels,
		"nested": operationObject(t, map[string]EncodedValue{
			"region": StringValue("east"),
		}),
		"nullable": NullValue(),
		"omitted":  AbsentValue(),
		"ratio":    ratio,
	})

	snapshot := newPlanEvaluationV2Snapshot(t)
	addPlanEvaluationV2Entry(t, snapshot, planEvaluationV2ResourceEntry(
		t,
		"resource.server",
		operationObject(t, map[string]EncodedValue{"id": StringValue("server-1")}),
	))
	action := operationActionState(t)
	addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
		Address: "action.notify",
		Kind:    state.StateAction,
		Payload: state.StatePayload{
			Kind:   state.StateAction,
			Action: &action,
		},
	})
	dataSource := operationDataSourceState(t)
	addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
		Address: "data-source.image",
		Kind:    state.StateDataSource,
		Payload: state.StatePayload{
			Kind:       state.StateDataSource,
			DataSource: &dataSource,
		},
	})
	composite := operationCompositeState(t, NodeResource)
	addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
		Address: "resource.application",
		Kind:    state.StateComposite,
		Payload: state.StatePayload{
			Kind:      state.StateComposite,
			Composite: &composite,
		},
	})

	executor := &Executor{DAG: newDAG(map[string][]string{
		"action.notify":        {"resource.server"},
		"data-source.image":    nil,
		"resource.application": {"action.notify", "data-source.image"},
		"resource.server":      nil,
	})}
	evaluation, err := executor.preparePlanEvaluationV2(
		inputs,
		snapshot,
		newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
	)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"count":       int64(3),
		"enabled":     true,
		"environment": "production",
		"items":       []any{"one", int64(2)},
		"labels":      map[string]any{"team": "runtime"},
		"nested":      map[string]any{"region": "east"},
		"nullable":    nil,
		"ratio":       1.5,
	}, evaluation.run.eval.Inputs)
	require.Equal(t, map[string]any{
		"application": map[string]any{"url": "https://example.com"},
		"server":      map[string]any{"id": "server-1"},
	}, evaluation.run.eval.Resources)
	require.Equal(t, map[string]any{
		"notify": map[string]any{"sent": true},
	}, evaluation.run.eval.Actions)
	require.Equal(t, map[string]any{
		"image": map[string]any{"id": "ami-1"},
	}, evaluation.run.eval.Data)
	require.Equal(t, []string{
		"data-source.image",
		"resource.server",
		"action.notify",
		"resource.application",
	}, evaluation.run.order)
	require.Equal(t, snapshot, evaluation.prior)
	require.NotSame(t, snapshot, evaluation.prior)
}

func TestPreparePlanEvaluationV2OmitsInvalidatedResourceOutputs(t *testing.T) {
	snapshot := newPlanEvaluationV2Snapshot(t)
	addPlanEvaluationV2Entry(t, snapshot, planEvaluationV2ResourceEntry(
		t,
		"resource.stable",
		operationObject(t, map[string]EncodedValue{"id": StringValue("stable-1")}),
	))
	addPlanEvaluationV2Entry(t, snapshot, planEvaluationV2ResourceEntry(
		t,
		"resource.upstream",
		operationObject(t, map[string]EncodedValue{"id": StringValue("old-1")}),
	))
	facts := newPlanEvaluationV2Facts()
	pass := newPlanEvaluationV2Pass(facts)
	require.NoError(t, pass.invalidateOutputs("resource.upstream"))
	executor := &Executor{DAG: newDAG(map[string][]string{
		"resource.stable":   nil,
		"resource.upstream": nil,
	})}

	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}),
		snapshot,
		pass,
	)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"stable": map[string]any{"id": "stable-1"},
	}, evaluation.run.eval.Resources)

	_, err = Eval(parseValue(t, "resource.upstream.id"), evaluation.run.eval)
	require.ErrorIs(t, err, ErrEvalNotFound)
}

func TestPreparePlanEvaluationV2SeedsNestedInstances(t *testing.T) {
	instances, err := MapValue(map[string]EncodedValue{
		"blue": StringValue("primary"),
	})
	require.NoError(t, err)
	inputs := operationObject(t, map[string]EncodedValue{"instances": instances})
	boundary := &Node{
		Address: "resource.apps",
		Kind:    NodeResource,
		Body:    parseValue(t, "{ name: @each.value }"),
		ForEach: parseValue(t, "input.instances"),
	}
	child := &Node{
		Address:   "resource.apps/resource.child",
		Kind:      NodeResource,
		Composite: "resource.apps",
	}
	executor := &Executor{DAG: &DAG{
		Nodes: map[string]*Node{
			boundary.Address: boundary,
			child.Address:    child,
		},
		Edges: map[string][]string{
			boundary.Address: {child.Address},
			child.Address:    nil,
		},
	}}
	snapshot := newPlanEvaluationV2Snapshot(t)
	composite := operationCompositeState(t, NodeResource)
	addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
		Address: "resource.apps['blue']",
		Kind:    state.StateComposite,
		Payload: state.StatePayload{
			Kind:      state.StateComposite,
			Composite: &composite,
		},
	})
	addPlanEvaluationV2Entry(t, snapshot, planEvaluationV2ResourceEntry(
		t,
		"resource.apps['blue']/resource.child",
		operationObject(t, map[string]EncodedValue{"id": StringValue("child-1")}),
	))

	evaluation, err := executor.preparePlanEvaluationV2(
		inputs,
		snapshot,
		newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
	)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"apps": map[string]any{
			"blue": map[string]any{"url": "https://example.com"},
		},
	}, evaluation.run.eval.Resources)
	scope := evaluation.run.composites["resource.apps['blue']"]
	require.NotNil(t, scope)
	require.Equal(t, map[string]any{"name": "primary"}, scope.Inputs)
	require.Equal(t, map[string]any{
		"child": map[string]any{"id": "child-1"},
	}, scope.Resources)
	require.Equal(t, map[string]map[string]any{
		"resource.apps": {"blue": "primary"},
	}, evaluation.run.forEachInstances)
}

func TestPreparePlanEvaluationV2ReturnsFreshState(t *testing.T) {
	inputs := operationObject(t, map[string]EncodedValue{
		"environment": StringValue("production"),
	})
	snapshot := newPlanEvaluationV2Snapshot(t)
	addPlanEvaluationV2Entry(t, snapshot, planEvaluationV2ResourceEntry(
		t,
		"resource.upstream",
		operationObject(t, map[string]EncodedValue{"id": StringValue("old-1")}),
	))
	original, err := snapshot.Clone()
	require.NoError(t, err)
	executor := &Executor{DAG: newDAG(map[string][]string{"resource.upstream": nil})}
	facts := newPlanEvaluationV2Facts()
	firstPass := newPlanEvaluationV2Pass(facts)

	first, err := executor.preparePlanEvaluationV2(inputs, snapshot, firstPass)
	require.NoError(t, err)
	value, err := Eval(parseValue(t, "resource.upstream.id"), first.run.eval)
	require.NoError(t, err)
	require.Equal(t, "old-1", value)
	first.run.eval.Inputs["environment"] = "changed"
	first.run.eval.Resources["upstream"].(map[string]any)["id"] = "changed"
	first.prior.Stack = "changed"
	require.NoError(t, firstPass.invalidateOutputs("resource.upstream"))

	second, err := executor.preparePlanEvaluationV2(
		inputs,
		snapshot,
		newPlanEvaluationV2Pass(facts),
	)
	require.NoError(t, err)
	require.NotSame(t, first.run, second.run)
	require.NotSame(t, first.run.eval, second.run.eval)
	require.Equal(t, "production", second.run.eval.Inputs["environment"])
	_, err = Eval(parseValue(t, "resource.upstream.id"), second.run.eval)
	require.ErrorIs(t, err, ErrEvalNotFound)
	require.Equal(t, original, snapshot)
}

func TestPlanEvaluationV2PlansLibraryConfiguration(t *testing.T) {
	libraries := planEvaluationV2Libraries()
	sourceText := ubtest.ReadValidFixture(
		t,
		"testdata/ub/plan-evaluator-v2",
		"library-configuration",
	)
	dag, source := syntaxDAGAndBody(t, sourceText, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	inputs := operationObject(t, map[string]EncodedValue{
		"token": StringValue("secret"),
	})
	snapshot := newPlanEvaluationV2Snapshot(t)
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(inputs, snapshot, pass)
	require.NoError(t, err)

	node := dag.Nodes["library-config.cloud"]
	request, err := executor.planEvaluationV2LibraryConfigurationRequest(evaluation, node)
	require.NoError(t, err)
	require.Equal(t, "library-config.cloud", request.Address)
	require.Equal(t, NodeLibraryConfiguration, request.Kind)
	require.NotNil(t, request.DependsOn)
	require.Empty(t, request.DependsOn)

	step, err := request.Plan(context.Background(), pass)
	require.NoError(t, err)
	require.Equal(t, "library-config.cloud", step.Address)
	require.Equal(t, NodeLibraryConfiguration, step.Kind)
	require.Equal(t, StepLibraryConfiguration, step.Operation.Kind)
	result := step.Operation.LibraryConfiguration.Result
	require.Equal(t, PlannedConfigurationConcrete, result.Kind)
	require.NotNil(t, result.Record)
	require.Equal(t, operationObject(t, map[string]EncodedValue{
		"endpoint": StringValue("secret"),
		"region":   StringValue("us-east-1"),
	}), result.Record.Value)
	require.Equal(t, []string{"/endpoint"}, result.Record.SensitivePaths)
	require.Len(t, result.Record.SensitiveValues, 1)
	require.Equal(t, "/endpoint", result.Record.SensitiveValues[0].Path)

	configuration := evaluation.configurations["library-config.cloud"]
	require.Equal(t, result, configuration.planned)
	decoded, ok := configuration.decoded.(*planEvaluationV2CloudConfiguration)
	require.True(t, ok)
	require.Equal(t, "secret", decoded.Endpoint.Value)
	require.Equal(t, "us-east-1", decoded.Region.Value)

	action := operationActionState(t)
	action.Configuration = *result.Record
	addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
		Address: "action.prior",
		Kind:    state.StateAction,
		Payload: state.StatePayload{
			Kind:   state.StateAction,
			Action: &action,
		},
	})
	secondPass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	secondEvaluation, err := executor.preparePlanEvaluationV2(inputs, snapshot, secondPass)
	require.NoError(t, err)
	secondRequest, err := executor.planEvaluationV2LibraryConfigurationRequest(
		secondEvaluation,
		node,
	)
	require.NoError(t, err)
	secondStep, err := secondRequest.Plan(context.Background(), secondPass)
	require.NoError(t, err)
	require.Equal(
		t,
		*result.Record,
		*secondStep.Operation.LibraryConfiguration.Result.Record,
	)
}

func TestPlanEvaluationV2DefersPendingLibraryConfiguration(t *testing.T) {
	libraries := planEvaluationV2Libraries()
	node := &Node{
		Address: "library-config.cloud",
		Kind:    NodeLibraryConfig,
		Alias:   "cloud",
		Body:    parseValue(t, "{ endpoint: resource.endpoint.url }"),
	}
	executor := &Executor{
		DAG: &DAG{
			Nodes: map[string]*Node{
				"library-config.cloud": node,
				"resource.endpoint": {
					Address: "resource.endpoint",
					Kind:    NodeResource,
				},
			},
			Edges: map[string][]string{
				"library-config.cloud": {"resource.endpoint"},
				"resource.endpoint":    nil,
			},
		},
		Libraries: libraries,
	}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}),
		newPlanEvaluationV2Snapshot(t),
		pass,
	)
	require.NoError(t, err)

	request, err := executor.planEvaluationV2LibraryConfigurationRequest(evaluation, node)
	require.NoError(t, err)
	require.Equal(t, []string{"resource.endpoint"}, request.DependsOn)
	request.DependsOn[0] = "resource.changed"
	step, err := request.Plan(context.Background(), pass)
	require.NoError(t, err)
	require.Equal(t, []string{"resource.endpoint"}, step.DependsOn)
	result := step.Operation.LibraryConfiguration.Result
	require.Equal(t, PlannedConfigurationPending, result.Kind)
	require.Equal(t, []string{"resource.endpoint.url"}, result.PendingRefs)
	fields, ok := step.Operation.LibraryConfiguration.Inputs.ObjectFields()
	require.True(t, ok)
	refs, ok := fields["endpoint"].PendingRefs()
	require.True(t, ok)
	require.Equal(t, []string{"resource.endpoint.url"}, refs)
	require.Nil(t, evaluation.configurations["library-config.cloud"].decoded)
}

func TestPlanEvaluationV2LibraryConfigurationRejectsInvalidSetup(t *testing.T) {
	libraries := planEvaluationV2Libraries()
	node := &Node{
		Address: "library-config.cloud",
		Kind:    NodeLibraryConfig,
		Alias:   "cloud",
		Body:    parseValue(t, "{ endpoint: 'https://api.example' }"),
	}
	executor := &Executor{
		DAG: &DAG{
			Nodes: map[string]*Node{node.Address: node},
			Edges: map[string][]string{node.Address: nil},
		},
		Libraries: libraries,
	}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}),
		newPlanEvaluationV2Snapshot(t),
		pass,
	)
	require.NoError(t, err)

	var missingExecutor *Executor
	_, err = missingExecutor.planEvaluationV2LibraryConfigurationRequest(evaluation, node)
	require.ErrorContains(t, err, "executor is required")

	_, err = (&Executor{}).planEvaluationV2LibraryConfigurationRequest(evaluation, node)
	require.ErrorContains(t, err, "dependency graph is required")

	_, err = executor.planEvaluationV2LibraryConfigurationRequest(nil, node)
	require.ErrorContains(t, err, "version 2 plan evaluation is required")

	_, err = executor.planEvaluationV2LibraryConfigurationRequest(evaluation, nil)
	require.ErrorContains(t, err, "library-configuration node is required")

	wrongKind := *node
	wrongKind.Kind = NodeResource
	_, err = executor.planEvaluationV2LibraryConfigurationRequest(evaluation, &wrongKind)
	require.ErrorContains(t, err, "is not a library configuration")

	missingLibrary := *node
	missingLibrary.Alias = "missing"
	_, err = executor.planEvaluationV2LibraryConfigurationRequest(evaluation, &missingLibrary)
	require.ErrorContains(t, err, `library "missing" is not imported`)

	badValue := *node
	badValue.Body = parseValue(t, "{ endpoint: 2 }")
	request, err := executor.planEvaluationV2LibraryConfigurationRequest(evaluation, &badValue)
	require.NoError(t, err)
	_, err = request.Plan(context.Background(), pass)
	require.ErrorContains(t, err, `field "endpoint": expected string, got an integer`)
}

func TestPreparePlanEvaluationV2RejectsInvalidSetup(t *testing.T) {
	validSnapshot := newPlanEvaluationV2Snapshot(t)
	invalidSnapshot := *validSnapshot
	invalidSnapshot.FormatVersion = 1
	pending, err := PendingEncodedValue([]string{"resource.upstream.id"})
	require.NoError(t, err)
	validInputs := operationObject(t, map[string]EncodedValue{})
	validExecutor := &Executor{DAG: newDAG(map[string][]string{"resource.main": nil})}
	validPass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())

	tests := []struct {
		name     string
		executor *Executor
		inputs   EncodedValue
		snapshot *state.SnapshotV2
		pass     *planningPassState
		message  string
	}{
		{
			name:     "executor",
			inputs:   validInputs,
			snapshot: validSnapshot,
			pass:     validPass,
			message:  "executor is required",
		},
		{
			name:     "dependency graph",
			executor: &Executor{},
			inputs:   validInputs,
			snapshot: validSnapshot,
			pass:     validPass,
			message:  "dependency graph is required",
		},
		{
			name:     "inputs object",
			executor: validExecutor,
			inputs:   StringValue("invalid"),
			snapshot: validSnapshot,
			pass:     validPass,
			message:  "planning inputs must be an object",
		},
		{
			name:     "concrete inputs",
			executor: validExecutor,
			inputs: operationObject(t, map[string]EncodedValue{
				"value": pending,
			}),
			snapshot: validSnapshot,
			pass:     validPass,
			message:  "planning inputs must be concrete",
		},
		{
			name:     "snapshot",
			executor: validExecutor,
			inputs:   validInputs,
			pass:     validPass,
			message:  "version 2 snapshot is required",
		},
		{
			name:     "invalid snapshot",
			executor: validExecutor,
			inputs:   validInputs,
			snapshot: &invalidSnapshot,
			pass:     validPass,
			message:  "version 2 snapshot: format version must be 2",
		},
		{
			name:     "planning pass",
			executor: validExecutor,
			inputs:   validInputs,
			snapshot: validSnapshot,
			message:  "planning pass state is required",
		},
		{
			name:     "planning facts",
			executor: validExecutor,
			inputs:   validInputs,
			snapshot: validSnapshot,
			pass:     &planningPassState{},
			message:  "planning pass state is required",
		},
		{
			name: "dependency cycle",
			executor: &Executor{DAG: newDAG(map[string][]string{
				"resource.main": {"resource.main"},
			})},
			inputs:   validInputs,
			snapshot: validSnapshot,
			pass:     validPass,
			message:  "cycle detected among",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.executor.preparePlanEvaluationV2(
				tt.inputs,
				tt.snapshot,
				tt.pass,
			)
			require.ErrorContains(t, err, tt.message)
		})
	}
}
