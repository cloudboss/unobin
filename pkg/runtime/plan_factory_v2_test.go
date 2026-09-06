package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type factoryV2Lookup struct {
	Name string `ub:"name"`

	reads *int
}

type factoryV2LookupOutput struct {
	Sizes map[string]int `ub:"sizes"`
}

func (r *factoryV2Lookup) Read(
	context.Context, any,
) (*factoryV2LookupOutput, error) {
	*r.reads++
	return &factoryV2LookupOutput{Sizes: map[string]int{"": 2, "blue": 1}}, nil
}

func (*factoryV2Lookup) Run(
	context.Context, any,
) (*factoryV2LookupOutput, error) {
	panic("unexpected action execution during planning")
}

func newFactoryV2TestExecutor(
	t *testing.T, capture *registeredPlanningCapture, reads *int,
) *Executor {
	t.Helper()
	library := newCatalogPlanningLibrary(capture, "current")
	library.DataSources = map[string]DataSourceRegistration{
		"lookup": MakeDataSourceWith[
			factoryV2Lookup, *factoryV2LookupOutput, any,
		](func() *factoryV2Lookup { return &factoryV2Lookup{reads: reads} }),
	}
	library.Actions = map[string]ActionRegistration{
		"lookup": MakeAction[factoryV2Lookup, *factoryV2LookupOutput, any](),
	}
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/cloud", New: func() *Library { return library }},
	})
	require.NoError(t, err)
	libraries, err := catalog.Libraries(map[string]string{"cloud": "example.com/cloud"})
	require.NoError(t, err)
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "targets")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	return &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries, LibraryCatalog: catalog}
}

func TestPlanFactoryV2TraversesCompleteGraph(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    string
		size     int64
		prior    bool
		decision Decision
	}{
		{"create", "server", 1, false, DecisionCreate},
		{"unchanged", "server", 1, true, DecisionNoOp},
		{"update", "server", 2, true, DecisionUpdate},
		{"replace", "renamed", 1, true, DecisionReplace},
	} {
		t.Run(test.name, func(t *testing.T) {
			capture := &registeredPlanningCapture{}
			reads := 0
			executor := newFactoryV2TestExecutor(t, capture, &reads)
			snapshot := newPlanEvaluationV2Snapshot(t)
			if test.prior {
				prior := planEvaluationV2ResourceStepRequest(t, capture).Prior
				addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
					Address: "resource.main", Kind: state.StateResource,
					Payload: state.StatePayload{
						Kind: state.StateResource, Resource: &state.ResourceStatePayload{Target: *prior},
					},
				})
			}
			original, err := snapshot.Clone()
			require.NoError(t, err)
			steps, err := executor.planFactoryStepsV2(context.Background(),
				operationObject(t, map[string]EncodedValue{
					"name": StringValue(test.input), "size": IntegerValue(test.size),
				}), snapshot,
			)
			require.NoError(t, err)
			byAddress := map[string]PlanStepV2{}
			for _, step := range steps {
				require.NoError(t, step.Validate())
				byAddress[step.Address] = step
			}
			require.ElementsMatch(t, []string{
				"library-config.cloud", "data-source.settings", "resource.main",
				"resource.many['']", "resource.many['blue']", "resource.child",
				"action.notify", "output.value",
			}, factoryV2StepAddresses(steps))
			require.Equal(t, test.decision, byAddress["resource.main"].Operation.Resource.Decision)
			require.Equal(t, DecisionRead, byAddress["data-source.settings"].Operation.DataSource.Decision)
			require.Equal(t, DecisionRerun, byAddress["action.notify"].Operation.Action.Decision)
			require.Equal(t, []string{"data-source.settings", "library-config.cloud"},
				byAddress["resource.many['blue']"].DependsOn)
			child := byAddress["resource.child"].Operation.Resource.Desired.Inputs
			require.Equal(t, test.decision != DecisionNoOp, child.HasPending())
			require.Equal(t, 1, reads)
			if test.prior {
				require.Equal(t, []string{"current:same"}, capture.reads)
			} else {
				require.Empty(t, capture.reads)
			}
			require.Equal(t, original, snapshot)
		})
	}
}

func factoryV2StepAddresses(steps []PlanStepV2) []string {
	addresses := make([]string, len(steps))
	for i := range steps {
		addresses[i] = steps[i].Address
	}
	return addresses
}

func TestPlanFactoryV2RemovesRecordedEntries(t *testing.T) {
	for _, destroy := range []bool{false, true} {
		t.Run(map[bool]string{false: "removed", true: "destroy"}[destroy], func(t *testing.T) {
			capture := &registeredPlanningCapture{}
			reads := 0
			executor := newFactoryV2TestExecutor(t, capture, &reads)
			executor.Destroy = destroy
			snapshot := newPlanEvaluationV2Snapshot(t)
			prior := planEvaluationV2ResourceStepRequest(t, capture).Prior
			data := operationDataSourceState(t)
			action := operationActionState(t)
			composite := operationCompositeState(t, NodeResource)
			for _, entry := range []state.StateEntryV2{
				{Address: "resource.removed", Kind: state.StateComposite,
					Payload: state.StatePayload{Kind: state.StateComposite, Composite: &composite}},
				{Address: "resource.removed/resource.child", Kind: state.StateResource,
					Payload: state.StatePayload{
						Kind: state.StateResource, Resource: &state.ResourceStatePayload{Target: *prior},
					}},
				{Address: "action.removed", Kind: state.StateAction,
					Payload: state.StatePayload{Kind: state.StateAction, Action: &action}},
				{Address: "data-source.removed", Kind: state.StateDataSource,
					Payload: state.StatePayload{Kind: state.StateDataSource, DataSource: &data}},
			} {
				addPlanEvaluationV2Entry(t, snapshot, entry)
			}
			inputs := map[string]EncodedValue{}
			if !destroy {
				inputs = map[string]EncodedValue{"name": StringValue("server"), "size": IntegerValue(1)}
			}
			steps, err := executor.planFactoryStepsV2(
				context.Background(), operationObject(t, inputs), snapshot,
			)
			require.NoError(t, err)
			removed := map[string]Decision{}
			for _, step := range steps {
				require.NoError(t, step.Validate())
				if snapshot.Find(step.Address) == nil {
					continue
				}
				switch step.Operation.Kind {
				case StepResource:
					removed[step.Address] = step.Operation.Resource.Decision
				case StepDataSource:
					removed[step.Address] = step.Operation.DataSource.Decision
				case StepAction:
					removed[step.Address] = step.Operation.Action.Decision
				case StepComposite:
					removed[step.Address] = step.Operation.Composite.Decision
				}
			}
			require.Equal(t, map[string]Decision{
				"resource.removed": DecisionDestroy, "resource.removed/resource.child": DecisionDestroy,
				"data-source.removed": DecisionDestroy, "action.removed": DecisionDestroy,
			}, removed)
			require.Equal(t, []string{"current:same"}, capture.reads)
			if destroy {
				require.Len(t, steps, len(removed))
				require.Zero(t, reads)
			}
		})
	}
}

func newFactoryV2CompositeExecutor(t *testing.T, fixture string) *Executor {
	t.Helper()
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/cloud", New: func() *Library {
			return &Library{Resources: map[string]ResourceRegistration{
				"plain": MakeResource[plainResource, *plainResourceOutput, any](plainResourceDefinition()),
			}}
		}},
		{LibraryPath: "example.com/inner", New: func() *Library {
			body := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "inner")
			composite := syntaxResourceComposite(t, "box", body)
			composite.LibraryBindings = map[string]string{"cloud": "example.com/cloud"}
			return &Library{ResourceComposites: map[string]*CompositeType{"box": composite}}
		}},
		{LibraryPath: "example.com/app", New: func() *Library {
			body := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "outer")
			library := &Library{}
			for _, kind := range []NodeKind{NodeResource, NodeDataSource, NodeAction} {
				composite := syntaxComposite(t, "box", kind, body)
				composite.LibraryBindings = map[string]string{"inner": "example.com/inner"}
				library.AddComposite(composite)
			}
			return library
		}},
	})
	require.NoError(t, err)
	libraries, err := catalog.Libraries(map[string]string{
		"app": "example.com/app", "cloud": "example.com/cloud",
	})
	require.NoError(t, err)
	source := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", fixture)
	dag, body := syntaxDAGAndBody(t, source, libraries)
	return &Executor{DAG: dag, SyntaxSource: body, Libraries: libraries, LibraryCatalog: catalog}
}

func TestPlanFactoryV2ExpandsNestedComposites(t *testing.T) {
	executor := newFactoryV2CompositeExecutor(t, "composites")
	steps, err := executor.planFactoryStepsV2(context.Background(),
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
	)
	require.NoError(t, err)
	want := map[string]string{
		"resource.apps['a/b']": "first", "resource.apps['z']": "second",
		"data-source.single": "data", "action.single": "action",
	}
	actual := map[string]string{}
	for _, step := range steps {
		require.NoError(t, step.Validate())
		switch step.Operation.Kind {
		case StepResource:
			require.Equal(t, DecisionCreate, step.Operation.Resource.Decision)
			target := step.Operation.Resource.Desired
			require.Equal(t, Binding{LibraryPath: "example.com/cloud", Export: "plain"}, target.Binding)
			fields, _ := target.Inputs.ObjectFields()
			name, _ := fields["name"].String()
			actual[DirectParent(DirectParent(step.Address))] = name
		case StepComposite:
			require.Equal(t, DecisionEval, step.Operation.Composite.Decision)
		case StepOutput:
			require.Equal(t, StringValue("first"), step.Operation.Output.Value)
		default:
			t.Fatalf("unexpected step %s", step.Address)
		}
	}
	require.Equal(t, want, actual)
	require.Len(t, steps, 13)
}

func TestPlanFactoryV2DefersCompositeInputs(t *testing.T) {
	executor := newFactoryV2CompositeExecutor(t, "pending-composite")
	steps, err := executor.planFactoryStepsV2(context.Background(),
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
	)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"resource.upstream", "resource.app/resource.boxes['']/resource.leaf",
		"resource.app/resource.boxes['']", "resource.app", "output.value",
	}, factoryV2StepAddresses(steps))
	for _, step := range steps {
		require.NoError(t, step.Validate())
		switch step.Operation.Kind {
		case StepResource:
			require.Equal(t, step.Address != "resource.upstream",
				step.Operation.Resource.Desired.Inputs.HasPending())
		case StepComposite:
			require.True(t, step.Operation.Composite.Desired.Inputs.HasPending())
		case StepOutput:
			require.True(t, step.Operation.Output.Value.HasPending())
		}
	}
}

func TestPlanFactoryV2ExpandsCompositesFromFreshData(t *testing.T) {
	executor := newFactoryV2CompositeExecutor(t, "observed-composites")
	reads := 0
	executor.Libraries["cloud"].DataSources = map[string]DataSourceRegistration{
		"lookup": MakeDataSourceWith[
			factoryV2Lookup, *factoryV2LookupOutput, any,
		](func() *factoryV2Lookup { return &factoryV2Lookup{reads: &reads} }),
	}
	snapshot := newPlanEvaluationV2Snapshot(t)
	data := operationDataSourceState(t)
	sizes, err := MapValue(map[string]EncodedValue{"stale": IntegerValue(1)})
	require.NoError(t, err)
	data.Outputs = operationObject(t, map[string]EncodedValue{"sizes": sizes})
	composite := operationCompositeState(t, NodeResource)
	action := operationActionState(t)
	for _, entry := range []state.StateEntryV2{
		{Address: "data-source.settings", Kind: state.StateDataSource,
			Payload: state.StatePayload{Kind: state.StateDataSource, DataSource: &data}},
		{Address: "resource.apps['stale']", Kind: state.StateComposite,
			Payload: state.StatePayload{Kind: state.StateComposite, Composite: &composite}},
		{Address: "resource.apps['stale']/action.old", Kind: state.StateAction,
			Payload: state.StatePayload{Kind: state.StateAction, Action: &action}},
	} {
		addPlanEvaluationV2Entry(t, snapshot, entry)
	}
	steps, err := executor.planFactoryStepsV2(context.Background(),
		operationObject(t, map[string]EncodedValue{}), snapshot,
	)
	require.NoError(t, err)
	var current []string
	for _, step := range steps {
		if step.Operation.Kind == StepComposite && step.Operation.Composite.Desired != nil {
			current = append(current, step.Address)
		}
	}
	require.ElementsMatch(t, []string{
		"resource.apps['']", "resource.apps['']/resource.boxes['']",
		"resource.apps['blue']", "resource.apps['blue']/resource.boxes['']",
	}, current)
	require.Equal(t, 1, reads)
}

func TestPlanFactoryV2ChecksLibraryConstraints(t *testing.T) {
	for _, kind := range []NodeKind{NodeResource, NodeDataSource, NodeAction} {
		t.Run(string(kind), func(t *testing.T) {
			reads := 0
			executor := newFactoryV2TestExecutor(t, &registeredPlanningCapture{}, &reads)
			export := "lookup"
			if kind == NodeResource {
				export = "server"
			}
			executor.Libraries["cloud"].Constraints = map[string][]lang.ConstraintSpec{
				string(kind) + "." + export: {{
					Kind: "predicate", When: "true", Require: "false", Message: "provider rule rejected",
				}},
			}
			steps, err := executor.planFactoryStepsV2(context.Background(),
				operationObject(t, map[string]EncodedValue{
					"name": StringValue("server"), "size": IntegerValue(1),
				}), newPlanEvaluationV2Snapshot(t),
			)
			require.ErrorContains(t, err, "provider rule rejected")
			require.Nil(t, steps)
			if kind == NodeDataSource {
				require.Zero(t, reads)
			}
		})
	}
}

func TestPlanFactoryV2ChecksKnownCompositeConstraints(t *testing.T) {
	executor := newFactoryV2CompositeExecutor(t, "pending-composite")
	body := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "constraints")
	constraints := syntaxResourceComposite(t, "constraints", body).SyntaxBody.Constraints
	executor.DAG.Nodes["resource.app"].CompositeSyntaxBody.Constraints = constraints
	steps, err := executor.planFactoryStepsV2(context.Background(),
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
	)
	require.ErrorContains(t, err, "independent rule rejected")
	require.NotContains(t, err.Error(), "name is required")
	require.Nil(t, steps)
}

func TestPlanFactoryV2ResolvesEveryDesiredBindingBeforeReads(t *testing.T) {
	for _, address := range []string{"resource.main", "data-source.settings", "action.notify"} {
		t.Run(address, func(t *testing.T) {
			reads := 0
			capture := &registeredPlanningCapture{}
			executor := newFactoryV2TestExecutor(t, capture, &reads)
			executor.DAG.Nodes[address].Type = "missing"
			steps, err := executor.planFactoryStepsV2(context.Background(),
				operationObject(t, map[string]EncodedValue{
					"name": StringValue("server"), "size": IntegerValue(1),
				}), newPlanEvaluationV2Snapshot(t),
			)
			require.ErrorContains(t, err, address)
			require.Nil(t, steps)
			require.Zero(t, reads)
			require.Empty(t, capture.reads)
		})
	}
}
