package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
)

const planEvaluationV2ForEachFixtureDir = "testdata/ub/plan-evaluator-v2/for-each"

type planEvaluationV2EachInput struct {
	Name string `ub:"name"`
	Size int    `ub:"size"`
}

func (*planEvaluationV2EachInput) Read(context.Context, any) (any, error) {
	panic("unexpected provider read")
}

func (*planEvaluationV2EachInput) Run(context.Context, any) (any, error) {
	panic("unexpected provider run")
}

func TestExpandPlanEvaluationV2NodePreparesEveryTargetKind(t *testing.T) {
	evaluations := 0
	cloud := &Library{
		LibraryPath: "example.com/cloud",
		DataSources: map[string]DataSourceRegistration{
			"lookup": MakeDataSource[planEvaluationV2EachInput, any, any](),
		},
		Actions: map[string]ActionRegistration{
			"notify": MakeAction[planEvaluationV2EachInput, any, any](),
		},
		Functions: map[string]FunctionType{
			"instances": MakeFunc("instances", "Returns instance sizes.", func() (any, error) {
				evaluations++
				return map[string]any{"z": int64(3), "a/b": int64(2)}, nil
			}),
		},
	}
	body := ubtest.ReadValidFixture(t, planEvaluationV2ForEachFixtureDir, "body")
	app := &Library{LibraryPath: "example.com/app"}
	for _, kind := range []NodeKind{NodeResource, NodeDataSource, NodeAction} {
		app.AddComposite(syntaxComposite(t, "box", kind, body))
	}
	libraries := map[string]*Library{"cloud": cloud, "app": app}
	src := ubtest.ReadValidFixture(t, planEvaluationV2ForEachFixtureDir, "targets")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	registration := newRegisteredPlanningResource(
		t, registeredPlanningDefinition(1, IdentityConfiguration), &registeredPlanningCapture{}, "server",
	)
	addresses := []string{
		"resource.many", "data-source.many", "action.many",
		"resource.boxes", "data-source.boxes", "action.boxes",
	}
	for passNumber := 1; passNumber <= 2; passNumber++ {
		evaluation, err := executor.preparePlanEvaluationV2(
			operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
			newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
		)
		require.NoError(t, err)
		for _, address := range addresses {
			node := dag.Nodes[address]
			original := *node
			instances, err := executor.expandPlanEvaluationV2Node(evaluation, node)
			require.NoError(t, err)
			require.Equal(t, []string{address + "['a/b']", address + "['z']"},
				planEvaluationV2NodeAddresses(instances))
			again, err := executor.expandPlanEvaluationV2Node(evaluation, node)
			require.NoError(t, err)
			require.Equal(t, instances, again)
			var triggers []string
			for i, instance := range instances {
				require.Nil(t, instance.ForEach)
				require.NotSame(t, node, instance)
				var inputs EncodedValue
				switch {
				case instance.IsComposite():
					target, err := executor.planEvaluationV2CompositeTarget(evaluation, instance)
					require.NoError(t, err)
					inputs = target.Inputs
				case instance.Kind == NodeResource:
					target, err := executor.planEvaluationV2ResourceTarget(evaluation, instance, registration)
					require.NoError(t, err)
					inputs = target.Inputs
				case instance.Kind == NodeDataSource:
					target, err := executor.planEvaluationV2DataSourceTarget(evaluation, instance)
					require.NoError(t, err)
					inputs = target.Inputs
				case instance.Kind == NodeAction:
					target, err := executor.planEvaluationV2ActionTarget(evaluation, instance)
					require.NoError(t, err)
					inputs = target.Inputs
					triggers = append(triggers, target.TriggerHash)
				}
				key := []string{"a/b", "z"}[i]
				require.Equal(t, operationObject(t, map[string]EncodedValue{
					"name": StringValue(key), "size": IntegerValue(int64(i + 2)),
				}), inputs)
			}
			if len(triggers) > 0 {
				require.NotEmpty(t, triggers[0])
				require.NotEqual(t, triggers[0], triggers[1])
			}
			require.Equal(t, original, *node)
		}
		require.Equal(t, passNumber*len(addresses), evaluations)
		require.Empty(t, evaluation.run.eval.Each)
	}
}

func planEvaluationV2NodeAddresses(nodes []*Node) []string {
	addresses := make([]string, len(nodes))
	for i, node := range nodes {
		addresses[i] = node.Address
	}
	return addresses
}

func TestExpandPlanEvaluationV2NodeUsesEnclosingInstance(t *testing.T) {
	cloud := &Library{LibraryPath: "example.com/cloud"}
	child := &Library{LibraryPath: "example.com/child"}
	body := ubtest.ReadValidFixture(t, planEvaluationV2ForEachFixtureDir, "body")
	child.AddComposite(syntaxResourceComposite(t, "box", body))
	outerBody := ubtest.ReadValidFixture(t, planEvaluationV2ForEachFixtureDir, "nested-body")
	outer := syntaxResourceComposite(t, "box", outerBody)
	outer.Libraries = map[string]*Library{"cloud": cloud, "child": child}
	libraries := map[string]*Library{"app": {
		LibraryPath: "example.com/app", ResourceComposites: map[string]*CompositeType{"box": outer},
	}}
	src := ubtest.ReadValidFixture(t, planEvaluationV2ForEachFixtureDir, "nested")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
		newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
	)
	require.NoError(t, err)
	registration := newRegisteredPlanningResource(
		t, registeredPlanningDefinition(1, IdentityConfiguration), &registeredPlanningCapture{}, "server",
	)
	parents, err := executor.expandPlanEvaluationV2Node(evaluation, dag.Nodes["resource.apps"])
	require.NoError(t, err)
	require.Equal(t, []string{"resource.apps['dev']", "resource.apps['prod']"},
		planEvaluationV2NodeAddresses(parents))
	for i, parent := range parents {
		parentName := []string{"dev", "prod"}[i]
		size := IntegerValue(int64(3 - i))
		for _, name := range []string{"many", "plain", "boxes"} {
			template := dag.Nodes["resource.apps/resource."+name]
			node := *template
			node.Address = parent.Address + "/resource." + name
			instances, err := executor.expandPlanEvaluationV2Node(evaluation, &node)
			require.NoError(t, err)
			address, value := node.Address+"['blue']", "blue"
			if name == "plain" {
				address, value = node.Address, parentName
			}
			require.Equal(t, []string{address}, planEvaluationV2NodeAddresses(instances))
			var inputs EncodedValue
			if name == "boxes" {
				target, err := executor.planEvaluationV2CompositeTarget(evaluation, instances[0])
				require.NoError(t, err)
				require.Equal(t, child.LibraryPath, target.Binding.LibraryPath)
				inputs = target.Inputs
			} else {
				target, err := executor.planEvaluationV2ResourceTarget(evaluation, instances[0], registration)
				require.NoError(t, err)
				require.Equal(t, cloud.LibraryPath, target.Binding.LibraryPath)
				inputs = target.Inputs
			}
			require.Equal(t, operationObject(t, map[string]EncodedValue{
				"name": StringValue(value), "size": size,
			}), inputs)
			require.Equal(t, "resource.apps/resource."+name, template.Address)
		}
		scope := evaluation.run.composites[parent.Address]
		require.Equal(t, map[string]any{"name": parentName, "size": int64(3 - i)}, scope.Inputs)
		require.Empty(t, scope.Each)
	}
	require.Empty(t, evaluation.run.eval.Each)
}

func TestExpandPlanEvaluationV2NodeHandlesEmptyAndEscapedKeys(t *testing.T) {
	node := &Node{
		Address: "resource.many", Kind: NodeResource, ForEach: parseValue(t, "input.instances"),
	}
	executor := &Executor{DAG: &DAG{Nodes: map[string]*Node{node.Address: node}}}
	evaluation := &planEvaluationV2{run: &runState{
		eval:             &EvalContext{Inputs: map[string]any{"instances": map[string]any{}}},
		forEachInstances: map[string]map[string]any{},
	}}
	nodes, err := executor.expandPlanEvaluationV2Node(evaluation, node)
	require.NoError(t, err)
	require.Equal(t, []*Node{}, nodes)
	values := map[string]any{"": "empty", "a'b\\c/d": "escaped"}
	evaluation.run.eval.Inputs["instances"] = values
	evaluation.run.forEachInstances = map[string]map[string]any{}
	nodes, err = executor.expandPlanEvaluationV2Node(evaluation, node)
	require.NoError(t, err)
	keys := []string{"", "a'b\\c/d"}
	require.Len(t, nodes, len(keys))
	for i, instance := range nodes {
		template, key, keyed := splitEntryKey(instance.Address)
		require.True(t, keyed)
		require.Equal(t, node.Address, template)
		require.Equal(t, keys[i], key)
		scope, err := executor.planEvaluationV2Scope(evaluation, instance)
		require.NoError(t, err)
		require.Equal(t, map[string]lang.EachValue{
			"@each": {Key: key, Value: values[key]},
		}, scope.Each)
	}
	missing := *node
	missing.Address, missing.ForEach = "resource.many['removed']", nil
	scope, err := executor.planEvaluationV2Scope(evaluation, &missing)
	require.ErrorIs(t, err, ErrInstanceGone)
	require.Nil(t, scope)
}

func TestExpandPlanEvaluationV2NodeRejectsInvalidSetup(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Executor, *planEvaluationV2, *Node)
		want   string
	}{
		{"missing graph", func(e *Executor, _ *planEvaluationV2, _ *Node) {
			e.DAG = nil
		}, "dependency graph is required"},
		{"missing run", func(_ *Executor, e *planEvaluationV2, _ *Node) {
			e.run = nil
		}, "version 2 plan evaluation is required"},
		{"missing scope", func(_ *Executor, e *planEvaluationV2, _ *Node) {
			e.run.eval = nil
		}, "version 2 plan evaluation is required"},
		{"missing cache", func(_ *Executor, e *planEvaluationV2, _ *Node) {
			e.run.forEachInstances = nil
		}, "version 2 plan evaluation is required"},
		{"output", func(_ *Executor, _ *planEvaluationV2, n *Node) {
			n.Kind = NodeOutput
		}, "only resources, data sources, and actions can be expanded"},
		{"invalid address", func(_ *Executor, _ *planEvaluationV2, n *Node) {
			n.Address = "invalid"
		}, "address"},
		{"already keyed", func(_ *Executor, _ *planEvaluationV2, n *Node) {
			n.Address = "resource.many['blue']"
		}, "already has an instance key"},
		{"missing template", func(e *Executor, _ *planEvaluationV2, n *Node) {
			delete(e.DAG.Nodes, n.Address)
		}, "for-each template is required"},
		{"list", func(_ *Executor, _ *planEvaluationV2, n *Node) {
			n.ForEach = parseValue(t, "['blue']")
		}, "lists are not a valid iterable"},
		{"scalar", func(_ *Executor, _ *planEvaluationV2, n *Node) {
			n.ForEach = parseValue(t, "true")
		}, "expected a map"},
		{"unresolved", func(_ *Executor, _ *planEvaluationV2, n *Node) {
			n.ForEach = parseValue(t, "resource.upstream.items")
		}, "@for-each"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := &Node{Address: "resource.many", Kind: NodeResource, ForEach: parseValue(t, "{}")}
			executor := &Executor{DAG: &DAG{Nodes: map[string]*Node{node.Address: node}}}
			evaluation := &planEvaluationV2{run: &runState{
				eval: &EvalContext{}, forEachInstances: map[string]map[string]any{},
			}}
			test.change(executor, evaluation, node)
			nodes, err := executor.expandPlanEvaluationV2Node(evaluation, node)
			require.ErrorContains(t, err, test.want)
			require.Nil(t, nodes)
		})
	}
	nodes, err := (*Executor)(nil).expandPlanEvaluationV2Node(nil, nil)
	require.ErrorContains(t, err, "executor is required")
	require.Nil(t, nodes)
	executor := &Executor{DAG: &DAG{}}
	nodes, err = executor.expandPlanEvaluationV2Node(nil, nil)
	require.ErrorContains(t, err, "version 2 plan evaluation is required")
	require.Nil(t, nodes)
	evaluation := &planEvaluationV2{run: &runState{
		eval: &EvalContext{}, forEachInstances: map[string]map[string]any{},
	}}
	nodes, err = executor.expandPlanEvaluationV2Node(evaluation, nil)
	require.ErrorContains(t, err, "planning node is required")
	require.Nil(t, nodes)
}
