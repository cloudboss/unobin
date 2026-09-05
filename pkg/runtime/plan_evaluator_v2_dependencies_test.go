package runtime

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/stateref"
)

func newPlanEvaluationV2DependencyInstances(t *testing.T) (*Executor, *planEvaluationV2) {
	t.Helper()
	libraries := planEvaluationV2Libraries()
	libraries["plain"] = &Library{LibraryPath: "example.com/plain"}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-evaluator-v2/dependencies", "instances")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
		newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
	)
	require.NoError(t, err)
	return executor, evaluation
}

func TestPlanEvaluationV2NodeDependenciesExpandsInstances(t *testing.T) {
	executor, evaluation := newPlanEvaluationV2DependencyInstances(t)
	escaped, err := stateref.AppendInstanceKey("resource.servers", "a'b\\c/d")
	require.NoError(t, err)
	want := []string{"resource.servers['']", escaped, "resource.servers['blue']"}
	for _, address := range []string{"output.servers", "library-config.cloud"} {
		node := executor.DAG.Nodes[address]
		dependencies, err := executor.planEvaluationV2NodeDependencies(evaluation, node)
		require.NoError(t, err)
		require.Equal(t, want, dependencies)
	}
	dependencies, err := executor.planEvaluationV2NodeDependencies(
		evaluation, executor.DAG.Nodes["output.empty"],
	)
	require.NoError(t, err)
	require.Equal(t, []string{}, dependencies)
}

func TestPlanEvaluationV2RequestsRecordInstanceDependencies(t *testing.T) {
	executor, evaluation := newPlanEvaluationV2DependencyInstances(t)
	for _, address := range []string{"output.servers", "library-config.cloud"} {
		node := executor.DAG.Nodes[address]
		var request planStepV2Request
		var err error
		if node.Kind == NodeOutput {
			request, err = executor.planEvaluationV2OutputRequest(evaluation, node)
		} else {
			request, err = executor.planEvaluationV2LibraryConfigurationRequest(evaluation, node)
		}
		require.NoError(t, err)
		dependencies, err := executor.planEvaluationV2NodeDependencies(evaluation, node)
		require.NoError(t, err)
		require.Equal(t, dependencies, request.DependsOn)
		request.DependsOn[0] = "resource.changed"
		step, err := request.Plan(
			context.Background(), newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
		)
		require.NoError(t, err)
		require.Equal(t, dependencies, step.DependsOn)
		require.NoError(t, step.Validate())
		steps := []PlanStepV2{*step}
		for _, dependency := range dependencies {
			steps = append(steps, applyScheduleV2ResourceStep(
				t, dependency, []string{}, DecisionCreate,
			))
		}
		var applied []string
		err = runApplyScheduleV2(context.Background(), steps, 1, func(
			_ context.Context, step PlanStepV2,
		) error {
			applied = append(applied, step.Address)
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, append(slices.Clone(dependencies), address), applied)
	}
}

func TestPlanEvaluationV2NodeDependenciesScopesNestedInstances(t *testing.T) {
	libraries := planEvaluationV2Libraries()
	libraries["plain"] = &Library{LibraryPath: "example.com/plain"}
	body := ubtest.ReadValidFixture(
		t, "testdata/ub/plan-evaluator-v2/dependencies", "composite-body",
	)
	child := syntaxResourceComposite(t, "box", body)
	child.Libraries = libraries
	outerBody := ubtest.ReadValidFixture(
		t, planEvaluationV2ForEachFixtureDir, "configuration-outer-body",
	)
	outer := syntaxResourceComposite(t, "box", outerBody)
	outer.Libraries = map[string]*Library{"child": {
		LibraryPath: "example.com/child", ResourceComposites: map[string]*CompositeType{"box": child},
	}}
	imports := map[string]*Library{"app": {
		LibraryPath: "example.com/app", ResourceComposites: map[string]*CompositeType{"box": outer},
	}}
	src := ubtest.ReadValidFixture(t, planEvaluationV2ForEachFixtureDir, "configurations")
	dag, source := syntaxDAGAndBody(t, src, imports)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: imports}
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
		newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
	)
	require.NoError(t, err)
	template := "resource.apps/resource.boxes"
	var all []string
	for _, key := range []string{"a/b", "z"} {
		parent := "resource.apps['" + key + "']/resource.boxes['inner']"
		node := *dag.Nodes[template+"/library-config.cloud"]
		node.Address = parent + "/library-config.cloud"
		request, err := executor.planEvaluationV2LibraryConfigurationRequest(evaluation, &node)
		require.NoError(t, err)
		expected := []string{
			parent + "/resource.endpoints['']", parent + "/resource.endpoints['blue']",
		}
		require.Equal(t, expected, request.DependsOn)
		all = append(all, node.Address)
		all = append(all, expected...)
		for _, kind := range []NodeKind{NodeResource, NodeDataSource, NodeAction} {
			node := *dag.Nodes[template+"/"+string(kind)+".consumer"]
			node.Address = parent + "/" + string(kind) + ".consumer"
			dependencies, err := executor.planEvaluationV2NodeDependencies(evaluation, &node)
			require.NoError(t, err)
			require.Equal(t, []string{parent + "/library-config.cloud"}, dependencies)
		}
		boundary := *dag.Nodes[template]
		boundary.Address = parent
		dependencies, err := executor.planEvaluationV2NodeDependencies(evaluation, &boundary)
		require.NoError(t, err)
		require.Equal(t, []string{
			parent + "/action.consumer", parent + "/data-source.consumer",
			parent + "/library-config.cloud", parent + "/resource.consumer",
			parent + "/resource.endpoints['']", parent + "/resource.endpoints['blue']",
		}, dependencies)
	}
	output := &Node{Address: "output.all", Kind: NodeOutput}
	dag.Edges[output.Address] = []string{
		template + "/resource.endpoints", template + "/library-config.cloud",
	}
	dependencies, err := executor.planEvaluationV2NodeDependencies(evaluation, output)
	require.NoError(t, err)
	slices.Sort(all)
	require.Equal(t, all, dependencies)
	require.Equal(t, []string{template + "/resource.endpoints", "input.name"},
		dag.Edges[template+"/library-config.cloud"])
	require.Equal(t, template+"/library-config.cloud",
		dag.Nodes[template+"/library-config.cloud"].Address)
}

func TestPlanEvaluationV2NodeDependenciesReusesExpansionPerPass(t *testing.T) {
	executor, evaluation := newPlanEvaluationV2DependencyInstances(t)
	calls := 0
	executor.Libraries["plain"].Functions = map[string]FunctionType{
		"instances": MakeFunc("instances", "Returns instances.", func() (any, error) {
			calls++
			return map[string]any{"blue": "server"}, nil
		}),
	}
	producer := executor.DAG.Nodes["resource.servers"]
	producer.ForEach = parseValue(t, "plain.instances()")
	for pass := range 2 {
		for _, address := range []string{"output.servers", "library-config.cloud"} {
			dependencies, err := executor.planEvaluationV2NodeDependencies(
				evaluation, executor.DAG.Nodes[address],
			)
			require.NoError(t, err)
			require.Equal(t, []string{"resource.servers['blue']"}, dependencies)
		}
		nodes, err := executor.expandPlanEvaluationV2Node(evaluation, producer)
		require.NoError(t, err)
		require.Equal(t, []string{"resource.servers['blue']"}, planEvaluationV2NodeAddresses(nodes))
		require.Equal(t, pass+1, calls)
		evaluation, err = executor.preparePlanEvaluationV2(
			operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
			newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
		)
		require.NoError(t, err)
	}
}

func TestPlanEvaluationV2NodeDependenciesPreservesExplicitKeysAndDetachesEdges(t *testing.T) {
	executor, evaluation := newPlanEvaluationV2DependencyInstances(t)
	node := executor.DAG.Nodes["output.servers"]
	edges := []string{
		"resource.servers['blue']", "input.name", "resource.unknown", "resource.servers['blue']",
	}
	executor.DAG.Edges[node.Address] = slices.Clone(edges)
	dependencies, err := executor.planEvaluationV2NodeDependencies(evaluation, node)
	require.NoError(t, err)
	require.Equal(t, []string{"resource.servers['blue']", "resource.unknown"}, dependencies)
	dependencies[0] = "resource.changed"
	require.Equal(t, edges, executor.DAG.Edges[node.Address])
}

func TestPlanEvaluationV2RequestsRejectUnresolvedDependencyInstances(t *testing.T) {
	executor, evaluation := newPlanEvaluationV2DependencyInstances(t)
	executor.DAG.Nodes["resource.servers"].ForEach = parseValue(t, "resource.upstream.items")
	for _, address := range []string{"output.servers", "library-config.cloud"} {
		node := executor.DAG.Nodes[address]
		var request planStepV2Request
		var err error
		if node.Kind == NodeOutput {
			request, err = executor.planEvaluationV2OutputRequest(evaluation, node)
		} else {
			request, err = executor.planEvaluationV2LibraryConfigurationRequest(evaluation, node)
		}
		require.ErrorContains(t, err, address+": dependency resource.servers")
		require.ErrorContains(t, err, "@for-each")
		require.Equal(t, planStepV2Request{}, request)
	}
	require.Empty(t, evaluation.configurations)
}

func TestPlanEvaluationV2NodeDependenciesRejectsInvalidSetup(t *testing.T) {
	tests := []struct {
		name   string
		change func(**Executor, **planEvaluationV2, **Node)
		want   string
	}{
		{"executor", func(e **Executor, _ **planEvaluationV2, _ **Node) { *e = nil },
			"executor dependency graph is required"},
		{"graph", func(e **Executor, _ **planEvaluationV2, _ **Node) { (*e).DAG = nil },
			"executor dependency graph is required"},
		{"evaluation", func(_ **Executor, e **planEvaluationV2, _ **Node) { *e = nil },
			"version 2 plan evaluation is required"},
		{"run", func(_ **Executor, e **planEvaluationV2, _ **Node) { (*e).run = nil },
			"version 2 plan evaluation is required"},
		{"scope", func(_ **Executor, e **planEvaluationV2, _ **Node) { (*e).run.eval = nil },
			"version 2 plan evaluation is required"},
		{"cache", func(_ **Executor, e **planEvaluationV2, _ **Node) { (*e).run.forEachInstances = nil },
			"version 2 plan evaluation is required"},
		{"node", func(_ **Executor, _ **planEvaluationV2, n **Node) { *n = nil },
			"planning node is required"},
		{"address", func(_ **Executor, _ **planEvaluationV2, n **Node) { (*n).Address = "invalid" },
			"address"},
		{"kind", func(_ **Executor, _ **planEvaluationV2, n **Node) { (*n).Kind = "local" },
			"node kind is invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor, evaluation := newPlanEvaluationV2DependencyInstances(t)
			node := executor.DAG.Nodes["output.servers"]
			test.change(&executor, &evaluation, &node)
			dependencies, err := executor.planEvaluationV2NodeDependencies(evaluation, node)
			require.ErrorContains(t, err, test.want)
			require.Nil(t, dependencies)
		})
	}
}
