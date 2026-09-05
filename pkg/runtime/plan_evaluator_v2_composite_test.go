package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

const compositeTargetFixtureDir = "testdata/ub/plan-evaluator-v2/composite-target"

func compositeTargetLibrary(t *testing.T, fixture string) *Library {
	t.Helper()
	body := ubtest.ReadValidFixture(t, compositeTargetFixtureDir, fixture)
	library := &Library{LibraryPath: "example.com/app"}
	for _, kind := range []NodeKind{NodeResource, NodeDataSource, NodeAction} {
		library.AddComposite(syntaxComposite(t, "box", kind, body))
	}
	return library
}

func newCompositeTargetEvaluation(
	t *testing.T,
	libraries map[string]*Library,
	fixture string,
) (*Executor, *planEvaluationV2) {
	t.Helper()
	src := ubtest.ReadValidFixture(t, compositeTargetFixtureDir, fixture)
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{"token": StringValue("secret")}),
		newPlanEvaluationV2Snapshot(t), pass,
	)
	require.NoError(t, err)
	return executor, evaluation
}

func TestPlanEvaluationV2CompositeTargetPreparesTypedInputs(t *testing.T) {
	library := compositeTargetLibrary(t, "body")
	executor, evaluation := newCompositeTargetEvaluation(
		t, map[string]*Library{"app": library, "renamed": library}, "targets",
	)
	for _, test := range []struct {
		address string
		kind    NodeKind
		name    string
		paths   []string
	}{
		{"resource.main", NodeResource, "secret", []string{"/name", "/password"}},
		{"resource.renamed", NodeResource, "secret", []string{"/name", "/password"}},
		{"data-source.main", NodeDataSource, "data", []string{"/password"}},
		{"action.main", NodeAction, "action", []string{"/password"}},
	} {
		t.Run(test.address, func(t *testing.T) {
			node := executor.DAG.Nodes[test.address]
			require.NotNil(t, node)
			target, err := executor.planEvaluationV2CompositeTarget(evaluation, node)
			require.NoError(t, err)
			require.Equal(t, &PlannedCompositeTarget{
				Category: test.kind, Binding: Binding{LibraryPath: library.LibraryPath, Export: "box"},
				Inputs: operationObject(t, map[string]EncodedValue{
					"name": StringValue(test.name), "password": AbsentValue(),
					"settings": AbsentValue(), "labels": AbsentValue(),
				}),
				SensitiveInputPaths: test.paths, SensitiveOutputPaths: []string{"/password", "/private"},
			}, target)
			step, err := planCompositeStep(compositePlanningRequest{
				Address: node.Address, DependsOn: []string{}, Category: test.kind, Desired: target,
			})
			require.NoError(t, err)
			require.Equal(t, DecisionEval, step.Operation.Composite.Decision)
			require.Equal(t, target, step.Operation.Composite.Desired)
			target.SensitiveOutputPaths[0] = "/changed"
			require.Equal(t, []string{"/password", "/private"},
				step.Operation.Composite.Desired.SensitiveOutputPaths)
		})
	}
	require.Equal(t, map[string]any{"token": "secret"}, evaluation.run.eval.Inputs)
}

func TestPlanEvaluationV2CompositeTargetPreservesPendingValues(t *testing.T) {
	executor, evaluation := newCompositeTargetEvaluation(
		t, map[string]*Library{"app": compositeTargetLibrary(t, "body")}, "targets",
	)
	node := executor.DAG.Nodes["resource.partial"]
	target, err := executor.planEvaluationV2CompositeTarget(evaluation, node)
	require.NoError(t, err)
	name, err := PendingEncodedValue([]string{"resource.upstream.id"})
	require.NoError(t, err)
	zone, err := PendingEncodedValue([]string{"resource.upstream.zone"})
	require.NoError(t, err)
	label, err := PendingEncodedValue([]string{"resource.upstream.label"})
	require.NoError(t, err)
	labels, err := MapValue(map[string]EncodedValue{"fixed": StringValue("known"), "pending": label})
	require.NoError(t, err)
	require.Equal(t, operationObject(t, map[string]EncodedValue{
		"name": name, "password": NullValue(), "labels": labels,
		"settings": operationObject(t, map[string]EncodedValue{
			"size":  IntegerValue(2),
			"zones": mustResourceList(t, []EncodedValue{StringValue("known"), zone}),
		}),
	}), target.Inputs)
	step, err := planCompositeStep(compositePlanningRequest{
		Address: node.Address, Category: node.Kind, DependsOn: []string{}, Desired: target,
	})
	require.NoError(t, err)
	require.Equal(t, DecisionEval, step.Operation.Composite.Decision)

	evaluation.run.eval.Resources["upstream"] = map[string]any{
		"id": "resolved", "zone": "east", "label": "value",
	}
	resolved, err := executor.planEvaluationV2CompositeTarget(evaluation, node)
	require.NoError(t, err)
	resolvedLabels, err := MapValue(map[string]EncodedValue{
		"fixed": StringValue("known"), "pending": StringValue("value"),
	})
	require.NoError(t, err)
	require.Equal(t, operationObject(t, map[string]EncodedValue{
		"name": StringValue("resolved"), "password": NullValue(), "labels": resolvedLabels,
		"settings": operationObject(t, map[string]EncodedValue{
			"size":  IntegerValue(2),
			"zones": mustResourceList(t, []EncodedValue{StringValue("known"), StringValue("east")}),
		}),
	}), resolved.Inputs)
	require.True(t, target.Inputs.HasPending())
}

func TestPlanEvaluationV2CompositeTargetUsesNestedScope(t *testing.T) {
	child := compositeTargetLibrary(t, "body")
	child.LibraryPath = "example.com/child"
	body := ubtest.ReadValidFixture(t, compositeTargetFixtureDir, "nested-body")
	outer := syntaxResourceComposite(t, "outer", body)
	outer.Libraries = map[string]*Library{"child": child}
	library := &Library{
		LibraryPath: "example.com/app", ResourceComposites: map[string]*CompositeType{"outer": outer},
	}
	executor, evaluation := newCompositeTargetEvaluation(
		t, map[string]*Library{"app": library}, "nested",
	)
	for _, name := range []string{"first", "second"} {
		node := executor.DAG.Nodes["resource."+name+"/resource.child"]
		target, err := executor.planEvaluationV2CompositeTarget(evaluation, node)
		require.NoError(t, err)
		require.Equal(t, Binding{LibraryPath: "example.com/child", Export: "box"}, target.Binding)
		require.Equal(t, operationObject(t, map[string]EncodedValue{
			"name": StringValue(name + "-child"), "password": AbsentValue(),
			"settings": AbsentValue(), "labels": AbsentValue(),
		}), target.Inputs)
	}
}

func TestPlanEvaluationV2CompositeTargetAcceptsEmptyInputs(t *testing.T) {
	executor, evaluation := newCompositeTargetEvaluation(
		t, map[string]*Library{"app": compositeTargetLibrary(t, "empty-body")}, "invalid-inputs",
	)
	target, err := executor.planEvaluationV2CompositeTarget(
		evaluation, executor.DAG.Nodes["resource.missing"],
	)
	require.NoError(t, err)
	require.Equal(t, operationObject(t, map[string]EncodedValue{}), target.Inputs)
	require.Equal(t, []string{}, target.SensitiveInputPaths)
	require.Equal(t, []string{}, target.SensitiveOutputPaths)
}

func TestPlanEvaluationV2CompositeTargetUsesConfigurationSchema(t *testing.T) {
	library := compositeTargetLibrary(t, "configuration-body")
	composite := library.ResourceComposites["box"]
	composite.LibraryConfigSchemas = map[string]LibraryConfigSchema{
		"example.com/cloud": {Path: "example.com/cloud", Fields: []typecheck.ObjectField{
			{Name: "endpoint", Type: typecheck.TString()},
			{Name: "token", Type: typecheck.TString(), Optional: true},
		}},
	}
	executor, evaluation := newCompositeTargetEvaluation(
		t, map[string]*Library{"app": library}, "configuration",
	)
	node := executor.DAG.Nodes["resource.main"]
	target, err := executor.planEvaluationV2CompositeTarget(evaluation, node)
	require.NoError(t, err)
	require.Equal(t, operationObject(t, map[string]EncodedValue{
		"cloud": operationObject(t, map[string]EncodedValue{
			"endpoint": StringValue("east"), "token": AbsentValue(),
		}),
	}), target.Inputs)
	node.LibraryConfigSchemas = nil
	_, err = executor.planEvaluationV2CompositeTarget(evaluation, node)
	require.ErrorContains(t, err, "has no resolved schema")
}

func TestPlanEvaluationV2CompositeTargetRejectsInvalidInputs(t *testing.T) {
	executor, evaluation := newCompositeTargetEvaluation(
		t, map[string]*Library{"app": compositeTargetLibrary(t, "body")}, "invalid-inputs",
	)
	for _, test := range []struct{ name, message string }{
		{"missing", `field "name": required but not provided`},
		{"unknown", `unknown field "extra"`},
		{"scalar", "expected string"},
		{"null", "expected string"},
		{"nested", `field "size": expected integer`},
		{"list", "element 0: expected string"},
		{"map", `key "wrong": expected string`},
		{"expression", `field "name"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			target, err := executor.planEvaluationV2CompositeTarget(
				evaluation, executor.DAG.Nodes["resource."+test.name],
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
		})
	}
}

func TestPlanEvaluationV2CompositeTargetRejectsInvalidSetup(t *testing.T) {
	library := compositeTargetLibrary(t, "body")
	executor, evaluation := newCompositeTargetEvaluation(
		t, map[string]*Library{"app": library}, "targets",
	)
	node := executor.DAG.Nodes["resource.main"]
	for _, test := range []struct {
		name    string
		change  func(*Node)
		message string
	}{
		{"kind", func(n *Node) { n.Kind = NodeOutput }, "node must be a composite"},
		{"primitive", func(n *Node) { n.CompositeSyntaxBody = nil }, "node must be a composite"},
		{"address", func(n *Node) { n.Address = "action.main" }, "address"},
		{"instances", func(n *Node) { n.ForEach = &lang.ObjectLit{} }, "must be expanded first"},
		{"alias", func(n *Node) { n.Alias = "missing" }, `library "missing" is not imported`},
		{"export", func(n *Node) { n.Type = "missing" }, "has no resource composite"},
		{"empty export", func(n *Node) { n.Type = "" }, "export is required"},
		{"path", func(n *Node) { n.LibraryPath = "example.com/other" },
			"composite library path does not match import"},
		{"body", func(n *Node) { n.Body = nil }, "body must be an object literal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := *node
			test.change(&changed)
			target, err := executor.planEvaluationV2CompositeTarget(evaluation, &changed)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
		})
	}
	var missing *Executor
	_, err := missing.planEvaluationV2CompositeTarget(evaluation, node)
	require.ErrorContains(t, err, "executor is required")
	_, err = (&Executor{}).planEvaluationV2CompositeTarget(evaluation, node)
	require.ErrorContains(t, err, "dependency graph is required")
	_, err = executor.planEvaluationV2CompositeTarget(nil, node)
	require.ErrorContains(t, err, "version 2 plan evaluation is required")
	_, err = executor.planEvaluationV2CompositeTarget(evaluation, nil)
	require.ErrorContains(t, err, "composite node is required")
	library.ResourceComposites["box"] = nil
	_, err = executor.planEvaluationV2CompositeTarget(evaluation, node)
	require.ErrorContains(t, err, "has no resource composite")
}
