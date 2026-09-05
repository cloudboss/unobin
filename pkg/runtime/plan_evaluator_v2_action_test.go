package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

const actionTargetFixtureDir = "testdata/ub/plan-evaluator-v2/action-target"

type actionTargetInput struct {
	Name     string   `ub:"name"`
	Size     uint8    `ub:"size"`
	Nullable *string  `ub:"nullable"`
	Zones    []string `ub:"zones"`

	runs *int
}

type actionTargetOutput struct {
	ID string `ub:"id"`
}

func (d *actionTargetInput) Run(context.Context, any) (*actionTargetOutput, error) {
	*d.runs++
	return &actionTargetOutput{ID: d.Name}, nil
}

func actionTargetLibrary(t *testing.T, runs *int) *Library {
	t.Helper()
	return &Library{
		LibraryPath: "example.com/cloud",
		Actions: map[string]ActionRegistration{
			"notify": MakeActionWith[actionTargetInput, *actionTargetOutput, any](
				func() *actionTargetInput { return &actionTargetInput{runs: runs} },
			),
		},
		Defaults: map[string][]lang.DefaultSpec{
			"action.notify": {{Field: "input.size", Value: resourceTargetDefault(t, "size")}},
		},
		Schema: &LibrarySchema{Actions: map[string]*TypeSchema{
			"notify": {SensitiveInputs: []string{"size"}, SensitiveOutputs: []string{"id"}},
		}},
	}
}

func newActionTargetEvaluation(
	t *testing.T,
	libraries map[string]*Library,
	fixture string,
) (*Executor, *planEvaluationV2, *planningPassState) {
	t.Helper()
	src := ubtest.ReadValidFixture(t, actionTargetFixtureDir, fixture)
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{"token": StringValue("secret")}),
		newPlanEvaluationV2Snapshot(t), pass,
	)
	require.NoError(t, err)
	return executor, evaluation, pass
}

func TestPlanEvaluationV2ActionTargetPreparesTypedInputs(t *testing.T) {
	runs := 0
	library := actionTargetLibrary(t, &runs)
	executor, evaluation, _ := newActionTargetEvaluation(
		t, map[string]*Library{"cloud": library}, "targets",
	)
	target, err := executor.planEvaluationV2ActionTarget(
		evaluation, executor.DAG.Nodes["action.main"],
	)
	require.NoError(t, err)
	require.NoError(t, target.Validate())
	definition, err := resolveLibraryConfigurationDefinition(library.LibraryPath, library)
	require.NoError(t, err)
	record, err := definition.newConfigurationRecord(
		"", operationObject(t, map[string]EncodedValue{}), nil, nil,
	)
	require.NoError(t, err)
	require.Equal(t, &PlannedActionTarget{
		Binding: Binding{LibraryPath: "example.com/cloud", Export: "notify"},
		Inputs: operationObject(t, map[string]EncodedValue{
			"name": StringValue("secret"), "size": IntegerValue(2),
			"nullable": AbsentValue(), "zones": AbsentValue(),
		}),
		Configuration:       PlannedConfiguration{Kind: PlannedConfigurationConcrete, Record: &record},
		TriggerHash:         target.TriggerHash,
		SensitiveInputPaths: []string{"/name", "/size"}, SensitiveOutputPaths: []string{"/id"},
	}, target)
	require.NotEmpty(t, target.TriggerHash)
	require.Zero(t, runs)
	require.Equal(t, map[string]any{"token": "secret"}, evaluation.run.eval.Inputs)
}

func TestPlanEvaluationV2ActionTargetPreservesPendingValues(t *testing.T) {
	runs := 0
	executor, evaluation, _ := newActionTargetEvaluation(
		t, map[string]*Library{"cloud": actionTargetLibrary(t, &runs)}, "targets",
	)
	node := executor.DAG.Nodes["action.partial"]
	target, err := executor.planEvaluationV2ActionTarget(evaluation, node)
	require.NoError(t, err)
	name, err := PendingEncodedValue([]string{"resource.upstream.id"})
	require.NoError(t, err)
	zone, err := PendingEncodedValue([]string{"resource.upstream.zone"})
	require.NoError(t, err)
	size, err := PendingEncodedValue([]string{"resource.upstream.size"})
	require.NoError(t, err)
	require.Equal(t, operationObject(t, map[string]EncodedValue{
		"name": name, "size": size, "nullable": NullValue(),
		"zones": mustResourceList(t, []EncodedValue{StringValue("known"), zone}),
	}), target.Inputs)
	operation, err := planActionOperation(actionPlanningRequest{
		Address: node.Address, DependsOn: []string{}, Desired: target,
	})
	require.NoError(t, err)
	require.Equal(t, DecisionRerun, operation.Decision)
	require.Empty(t, target.TriggerHash)

	evaluation.run.eval.Resources["upstream"] = map[string]any{
		"id": "resolved", "zone": "east", "size": int64(3),
	}
	resolved, err := executor.planEvaluationV2ActionTarget(evaluation, node)
	require.NoError(t, err)
	require.False(t, resolved.Inputs.HasPending())
	fields, _ := resolved.Inputs.ObjectFields()
	require.Equal(t, StringValue("resolved"), fields["name"])
	require.Equal(t, IntegerValue(3), fields["size"])
	require.Equal(t, mustResourceList(t, []EncodedValue{
		StringValue("known"), StringValue("east"),
	}), fields["zones"])
	require.True(t, target.Inputs.HasPending())
	require.Zero(t, runs)
}

func TestPlanEvaluationV2ActionTargetUsesScopedConfigurations(t *testing.T) {
	runs := 0
	library := actionTargetLibrary(t, &runs)
	library.Configuration = planEvaluationV2Libraries()["cloud"].Configuration
	libraries := map[string]*Library{"cloud": library, "renamed": library}
	body := ubtest.ReadValidFixture(t, actionTargetFixtureDir, "composite-body")
	composite := syntaxResourceComposite(t, "application", body)
	composite.Libraries = map[string]*Library{"cloud": library}
	libraries["app"] = &Library{
		LibraryPath: "example.com/app", ResourceComposites: map[string]*CompositeType{
			"application": composite,
		},
	}
	executor, evaluation, pass := newActionTargetEvaluation(t, libraries, "configurations")
	for _, test := range []struct{ address, configuration, value string }{
		{"action.first", "library-config.cloud", "first"},
		{"action.second", "library-config.renamed", "second"},
		{"resource.nested/action.child", "resource.nested/library-config.cloud", "nested"},
	} {
		t.Run(test.address, func(t *testing.T) {
			node := executor.DAG.Nodes[test.address]
			target, err := executor.planEvaluationV2ActionTarget(evaluation, node)
			require.ErrorContains(t, err, "has not been evaluated")
			require.Nil(t, target)
			request, err := executor.planEvaluationV2LibraryConfigurationRequest(
				evaluation, executor.DAG.Nodes[test.configuration],
			)
			require.NoError(t, err)
			_, err = request.Plan(context.Background(), pass)
			require.NoError(t, err)
			target, err = executor.planEvaluationV2ActionTarget(evaluation, node)
			require.NoError(t, err)
			require.Equal(t, Binding{LibraryPath: library.LibraryPath, Export: "notify"}, target.Binding)
			require.Equal(t, evaluation.configurations[test.configuration].planned, target.Configuration)
			fields, _ := target.Inputs.ObjectFields()
			require.Equal(t, StringValue(test.value), fields["name"])
			if test.value == "second" {
				require.Equal(t, PlannedConfiguration{
					Kind: PlannedConfigurationPending, PendingRefs: []string{"resource.upstream.endpoint"},
				}, target.Configuration)
				target.Configuration.PendingRefs[0] = "resource.changed.id"
				require.Equal(t, []string{"resource.upstream.endpoint"},
					evaluation.configurations[test.configuration].planned.PendingRefs)
			} else {
				require.Equal(t, test.configuration, target.Configuration.Record.Address)
				values, _ := target.Configuration.Record.Value.ObjectFields()
				require.Equal(t, StringValue(test.value), values["endpoint"])
				target.Configuration.Record.Address = "library-config.changed"
				require.Equal(t, test.configuration,
					evaluation.configurations[test.configuration].planned.Record.Address)
			}
		})
	}
	require.Zero(t, runs)
}

func TestPlanEvaluationV2ActionTargetHandlesMissingConfiguration(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "empty"}[empty], func(t *testing.T) {
			runs := 0
			library := actionTargetLibrary(t, &runs)
			library.Configuration = configurationRegistration(1, nil)
			if empty {
				library.Configuration = &cfg.ConfigurationType[*struct{}]{
					SchemaVersion: 1, New: func() *struct{} { return &struct{}{} },
				}
			}
			executor, evaluation, _ := newActionTargetEvaluation(
				t, map[string]*Library{"cloud": library}, "targets",
			)
			target, err := executor.planEvaluationV2ActionTarget(
				evaluation, executor.DAG.Nodes["action.main"],
			)
			if empty {
				require.NoError(t, err)
				require.Equal(t, "library-config.cloud", target.Configuration.Record.Address)
				require.Equal(t, operationObject(t, map[string]EncodedValue{}),
					target.Configuration.Record.Value)
			} else {
				require.ErrorContains(t, err, `library "cloud" requires a configuration`)
				require.Nil(t, target)
			}
			require.Zero(t, runs)
		})
	}
}

func TestPlanEvaluationV2ActionTargetRejectsInvalidSetup(t *testing.T) {
	runs := 0
	library := actionTargetLibrary(t, &runs)
	executor, evaluation, _ := newActionTargetEvaluation(
		t, map[string]*Library{"cloud": library}, "targets",
	)
	node := executor.DAG.Nodes["action.main"]
	for _, test := range []struct {
		name    string
		change  func(*Node)
		message string
	}{
		{"kind", func(n *Node) { n.Kind = NodeResource }, "node must be a primitive action"},
		{"composite", func(n *Node) { n.CompositeSyntaxBody = &syntax.FactoryBody{} },
			"node must be a primitive action"},
		{"address", func(n *Node) { n.Address = "resource.main" }, "address"},
		{"instances", func(n *Node) { n.ForEach = &lang.ObjectLit{} }, "must be expanded first"},
		{"alias", func(n *Node) { n.Alias = "missing" }, `library "missing" is not imported`},
		{"export", func(n *Node) { n.Type = "missing" }, `has no action "missing"`},
		{"empty export", func(n *Node) { n.Type = "" }, "export is required"},
		{"path", func(n *Node) { n.LibraryPath = "example.com/other" },
			"action library path does not match import"},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := *node
			test.change(&changed)
			target, err := executor.planEvaluationV2ActionTarget(evaluation, &changed)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
		})
	}
	var missing *Executor
	_, err := missing.planEvaluationV2ActionTarget(evaluation, node)
	require.ErrorContains(t, err, "executor is required")
	_, err = (&Executor{}).planEvaluationV2ActionTarget(evaluation, node)
	require.ErrorContains(t, err, "dependency graph is required")
	_, err = executor.planEvaluationV2ActionTarget(nil, node)
	require.ErrorContains(t, err, "version 2 plan evaluation is required")
	_, err = executor.planEvaluationV2ActionTarget(evaluation, nil)
	require.ErrorContains(t, err, "action node is required")
	library.Actions["notify"] = nil
	_, err = executor.planEvaluationV2ActionTarget(evaluation, node)
	require.ErrorContains(t, err, "action registration is required")
	require.Zero(t, runs)
}

type actionTargetRegistration struct {
	ActionRegistration
	newReceiver func() any
}

func (r *actionTargetRegistration) NewReceiver() any { return r.newReceiver() }

func TestPlanEvaluationV2ActionTargetRejectsInvalidReceiver(t *testing.T) {
	for _, test := range []struct {
		name    string
		factory func() any
		message string
	}{
		{"nil", func() any { return nil }, "receiver must be a non-nil pointer to a struct"},
		{"nil pointer", func() any { return (*actionTargetInput)(nil) },
			"receiver must be a non-nil pointer to a struct"},
		{"scalar", func() any { return new(string) }, "receiver must be a non-nil pointer to a struct"},
		{"unsupported", func() any { return &struct{ Value chan int }{} }, "unsupported"},
		{"panic", func() any { panic("constructor failed") }, "constructor failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runs := 0
			library := actionTargetLibrary(t, &runs)
			library.Actions["notify"] = &actionTargetRegistration{newReceiver: test.factory}
			executor, evaluation, _ := newActionTargetEvaluation(
				t, map[string]*Library{"cloud": library}, "targets",
			)
			target, err := executor.planEvaluationV2ActionTarget(
				evaluation, executor.DAG.Nodes["action.main"],
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
			require.Zero(t, runs)
		})
	}
}

func TestPlanEvaluationV2ActionTargetRejectsInvalidInputs(t *testing.T) {
	runs := 0
	executor, evaluation, _ := newActionTargetEvaluation(
		t, map[string]*Library{"cloud": actionTargetLibrary(t, &runs)}, "targets",
	)
	ubtest.Run(t, actionTargetFixtureDir+"/invalid", func(
		name string, src []byte,
	) (string, []string) {
		body, err := lang.ParseExpr(name, src)
		require.NoError(t, err)
		node := *executor.DAG.Nodes["action.main"]
		node.Body = body
		target, err := executor.planEvaluationV2ActionTarget(evaluation, &node)
		require.Nil(t, target)
		if err != nil {
			return "", []string{err.Error()}
		}
		return "", nil
	})
	require.Zero(t, runs)
}

func TestPlanEvaluationV2ActionTargetClassifiesTriggers(t *testing.T) {
	for _, test := range []struct {
		name     string
		first    string
		second   string
		decision Decision
	}{
		{"unchanged inputs", "{name: 'same'}", "{name: 'same'}", DecisionSkip},
		{"changed inputs", "{name: 'first'}", "{name: 'second'}", DecisionRerun},
		{"default applied", "{name: 'same'}", "{size: 2, name: 'same'}", DecisionSkip},
		{"absent and null", "{name: 'same'}", "{name: 'same', nullable: null}", DecisionRerun},
		{"explicit trigger", "{name: 'first', @trigger: 1}",
			"{name: 'second', @trigger: 1}", DecisionSkip},
		{"changed trigger", "{name: 'same', @trigger: 1}",
			"{name: 'same', @trigger: 2}", DecisionRerun},
		{"integer precision", "{name: 'same', @trigger: 9007199254740992}",
			"{name: 'same', @trigger: 9007199254740993}", DecisionRerun},
		{"trigger ordering", "{name: 'same', @trigger: {a: 1, b: 2}}",
			"{name: 'same', @trigger: {b: 2, a: 1}}", DecisionSkip},
		{"always", "{name: 'same', @trigger: 'always'}",
			"{name: 'same', @trigger: 'always'}", DecisionRerun},
		{"pending input", "{name: 'same', @trigger: 1}",
			"{name: resource.upstream.id, @trigger: 1}", DecisionRerun},
		{"pending trigger", "{name: 'same', @trigger: [1, resource.upstream.id]}",
			"{name: 'same', @trigger: [1, resource.upstream.id]}", DecisionRerun},
	} {
		t.Run(test.name, func(t *testing.T) {
			runs := 0
			executor, evaluation, _ := newActionTargetEvaluation(
				t, map[string]*Library{"cloud": actionTargetLibrary(t, &runs)}, "targets",
			)
			node := *executor.DAG.Nodes["action.main"]
			node.Body = parseValue(t, test.first)
			first, err := executor.planEvaluationV2ActionTarget(evaluation, &node)
			require.NoError(t, err)
			node.Body = parseValue(t, test.second)
			second, err := executor.planEvaluationV2ActionTarget(evaluation, &node)
			require.NoError(t, err)
			prior := operationActionState(t)
			prior.Binding, prior.TriggerHash = first.Binding, first.TriggerHash
			step, err := planActionStep(actionPlanningRequest{
				Address: node.Address, DependsOn: []string{}, Desired: second, Prior: &prior,
			})
			require.NoError(t, err)
			require.NoError(t, step.Validate())
			require.Equal(t, test.decision, step.Operation.Action.Decision)
			require.Equal(t, second, step.Operation.Action.Desired)
			if test.name == "always" || test.name == "pending trigger" {
				require.Empty(t, second.TriggerHash)
			} else {
				require.Len(t, second.TriggerHash, 64)
			}
			require.Zero(t, runs)
		})
	}
}

func TestPlanEvaluationV2ActionTargetHashesCanonicalBindings(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "inputs", true: "explicit"}[explicit], func(t *testing.T) {
			runs := 0
			library := actionTargetLibrary(t, &runs)
			executor, evaluation, _ := newActionTargetEvaluation(t, map[string]*Library{
				"cloud": library, "renamed": library,
			}, "targets")
			node := *executor.DAG.Nodes["action.main"]
			if explicit {
				node.Body = parseValue(t, "{name: 'same', @trigger: 1}")
			}
			first, err := executor.planEvaluationV2ActionTarget(evaluation, &node)
			require.NoError(t, err)
			node.Alias = "renamed"
			node.LibraryPath = ""
			renamed, err := executor.planEvaluationV2ActionTarget(evaluation, &node)
			require.NoError(t, err)
			require.Equal(t, first, renamed)
			library.LibraryPath = "example.com/other"
			changed, err := executor.planEvaluationV2ActionTarget(evaluation, &node)
			require.NoError(t, err)
			require.NotEqual(t, first.TriggerHash, changed.TriggerHash)
			library.LibraryPath = first.Binding.LibraryPath
			library.Actions["other"] = library.Actions["notify"]
			node.Type = "other"
			changed, err = executor.planEvaluationV2ActionTarget(evaluation, &node)
			require.NoError(t, err)
			require.NotEqual(t, first.TriggerHash, changed.TriggerHash)
			require.Zero(t, runs)
		})
	}
}

func TestPlanEvaluationV2ActionTargetResolvesTriggerReferences(t *testing.T) {
	runs := 0
	executor, evaluation, _ := newActionTargetEvaluation(
		t, map[string]*Library{"cloud": actionTargetLibrary(t, &runs)}, "targets",
	)
	node := executor.DAG.Nodes["action.triggered"]
	pending, err := executor.planEvaluationV2ActionTarget(evaluation, node)
	require.NoError(t, err)
	require.Empty(t, pending.TriggerHash)
	require.False(t, pending.Inputs.HasPending())
	evaluation.run.eval.Resources["upstream"] = map[string]any{"id": "resolved"}
	resolved, err := executor.planEvaluationV2ActionTarget(evaluation, node)
	require.NoError(t, err)
	require.NotEmpty(t, resolved.TriggerHash)
	require.Equal(t, pending.Inputs, resolved.Inputs)
	require.Empty(t, pending.TriggerHash)
	require.Zero(t, runs)
}

func TestPlanEvaluationV2ActionTargetReportsTriggerErrors(t *testing.T) {
	runs := 0
	executor, evaluation, _ := newActionTargetEvaluation(
		t, map[string]*Library{"cloud": actionTargetLibrary(t, &runs)}, "targets",
	)
	node := *executor.DAG.Nodes["action.main"]
	node.Body = parseValue(t, "{name: 'same', @trigger: [resource.upstream.id, true + 1]}")
	target, err := executor.planEvaluationV2ActionTarget(evaluation, &node)
	require.ErrorContains(t, err, "action.main: action trigger")
	require.Nil(t, target)
	require.Zero(t, runs)
}
