package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func newPlanEvaluationV2ConfigurationInstances(t *testing.T) *Executor {
	t.Helper()
	cloud := planEvaluationV2Libraries()["cloud"]
	cloud.DataSources = map[string]DataSourceRegistration{
		"lookup": MakeDataSource[planEvaluationV2EachInput, any, any](),
	}
	cloud.Actions = map[string]ActionRegistration{
		"notify": MakeAction[planEvaluationV2EachInput, any, any](),
	}
	body := ubtest.ReadValidFixture(t, planEvaluationV2ForEachFixtureDir, "configuration-body")
	child := syntaxResourceComposite(t, "box", body)
	child.Libraries = map[string]*Library{"cloud": cloud}
	outerBody := ubtest.ReadValidFixture(
		t, planEvaluationV2ForEachFixtureDir, "configuration-outer-body",
	)
	outer := syntaxResourceComposite(t, "box", outerBody)
	outer.Libraries = map[string]*Library{"child": {
		LibraryPath: "example.com/child", ResourceComposites: map[string]*CompositeType{"box": child},
	}}
	libraries := map[string]*Library{"app": {
		LibraryPath: "example.com/app", ResourceComposites: map[string]*CompositeType{"box": outer},
	}}
	src := ubtest.ReadValidFixture(t, planEvaluationV2ForEachFixtureDir, "configurations")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	return &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
}

func TestPlanEvaluationV2ConfigurationUsesNestedInstances(t *testing.T) {
	executor := newPlanEvaluationV2ConfigurationInstances(t)
	snapshot := newPlanEvaluationV2Snapshot(t)
	registration := newRegisteredPlanningResource(
		t, registeredPlanningDefinition(1, IdentityConfiguration), &registeredPlanningCapture{}, "server",
	)
	templateParent := "resource.apps/resource.boxes"
	configurationNode := executor.DAG.Nodes[templateParent+"/library-config.cloud"]
	original := *configurationNode
	records := map[string]ConfigurationRecord{}
	for passNumber := range 2 {
		pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
		evaluation, err := executor.preparePlanEvaluationV2(
			operationObject(t, map[string]EncodedValue{}), snapshot, pass,
		)
		require.NoError(t, err)
		parents, err := executor.expandPlanEvaluationV2Node(
			evaluation, executor.DAG.Nodes["resource.apps"],
		)
		require.NoError(t, err)
		for i, parent := range parents {
			boundary := *executor.DAG.Nodes[templateParent]
			boundary.Address = parent.Address + "/resource.boxes"
			instances, err := executor.expandPlanEvaluationV2Node(evaluation, &boundary)
			require.NoError(t, err)
			require.Equal(t, []string{boundary.Address + "['inner']"},
				planEvaluationV2NodeAddresses(instances))
			instance := instances[0].Address
			node := *configurationNode
			node.Address = instance + "/library-config.cloud"
			request, err := executor.planEvaluationV2LibraryConfigurationRequest(evaluation, &node)
			require.NoError(t, err)
			step, err := request.Plan(context.Background(), pass)
			require.NoError(t, err)
			require.NoError(t, step.Validate())
			result := step.Operation.LibraryConfiguration.Result
			require.NotNil(t, result.Record)
			record := *result.Record
			require.Equal(t, node.Address, record.Address)
			require.Equal(t, "example.com/cloud", record.LibraryPath)
			require.Equal(t, operationObject(t, map[string]EncodedValue{
				"endpoint": StringValue([]string{"first", "second"}[i]),
				"region":   StringValue("us-east-1"),
			}), record.Value)
			require.Equal(t, []string{"/endpoint"}, record.SensitivePaths)
			require.Equal(t, result, evaluation.configurations[node.Address].planned)
			if passNumber == 0 {
				records[node.Address] = record
				action := operationActionState(t)
				action.Configuration = record
				addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
					Address: instance + "/action.child", Kind: state.StateAction,
					Payload: state.StatePayload{Kind: state.StateAction, Action: &action},
				})
			} else {
				require.Equal(t, records[node.Address], record)
			}
			for _, kind := range []NodeKind{NodeResource, NodeDataSource, NodeAction} {
				targetNode := *executor.DAG.Nodes[templateParent+"/"+string(kind)+".child"]
				targetNode.Address = instance + "/" + string(kind) + ".child"
				var configuration PlannedConfiguration
				switch kind {
				case NodeResource:
					target, err := executor.planEvaluationV2ResourceTarget(evaluation, &targetNode, registration)
					require.NoError(t, err)
					configuration = target.Configuration
				case NodeDataSource:
					target, err := executor.planEvaluationV2DataSourceTarget(evaluation, &targetNode)
					require.NoError(t, err)
					configuration = target.Configuration
				case NodeAction:
					target, err := executor.planEvaluationV2ActionTarget(evaluation, &targetNode)
					require.NoError(t, err)
					configuration = target.Configuration
				}
				require.Equal(t, result, configuration)
				configuration.Record.SensitivePaths[0] = "/changed"
				require.Equal(t, []string{"/endpoint"}, result.Record.SensitivePaths)
			}
		}
		require.NotContains(t, evaluation.configurations, configurationNode.Address)
		require.Equal(t, original, *configurationNode)
	}
}

func TestPlanEvaluationV2ConfigurationInstanceDefersPendingValues(t *testing.T) {
	executor := newPlanEvaluationV2ConfigurationInstances(t)
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
	)
	require.NoError(t, err)
	parent := "resource.apps['a/b']/resource.boxes['inner']"
	template := "resource.apps/resource.boxes"
	source := executor.DAG.Nodes[template+"/library-config.cloud"]
	node := *source
	node.Address = parent + "/library-config.cloud"
	body := ubtest.ReadValidFixture(t, planEvaluationV2ForEachFixtureDir, "pending-configuration")
	pending := syntaxDAG(t, body, planEvaluationV2Libraries())
	node.Body = pending.Nodes["library-config.cloud"].Body
	executor.DAG.Edges[source.Address] = []string{
		template + "/resource.endpoint", "resource.apps/data-source.settings",
		"data-source.global", "resource.apps2/resource.other", "input.name",
	}
	request, err := executor.planEvaluationV2LibraryConfigurationRequest(evaluation, &node)
	require.NoError(t, err)
	expectedDependencies := []string{
		"data-source.global", "resource.apps2/resource.other",
		"resource.apps['a/b']/data-source.settings", parent + "/resource.endpoint",
	}
	require.Equal(t, expectedDependencies, request.DependsOn)
	request.DependsOn[0] = "data-source.changed"
	step, err := request.Plan(context.Background(), pass)
	require.NoError(t, err)
	require.Equal(t, expectedDependencies, step.DependsOn)
	require.Equal(t, []string{
		template + "/resource.endpoint", "resource.apps/data-source.settings",
		"data-source.global", "resource.apps2/resource.other", "input.name",
	}, executor.DAG.Edges[source.Address])
	result := step.Operation.LibraryConfiguration.Result
	require.Equal(t, PlannedConfiguration{
		Kind: PlannedConfigurationPending, PendingRefs: []string{"resource.endpoint.url"},
	}, result)
	targetNode := *executor.DAG.Nodes[template+"/data-source.child"]
	targetNode.Address = parent + "/data-source.child"
	target, err := executor.planEvaluationV2DataSourceTarget(evaluation, &targetNode)
	require.NoError(t, err)
	require.Equal(t, result, target.Configuration)
	target.Configuration.PendingRefs[0] = "resource.changed.url"
	require.Equal(t, []string{"resource.endpoint.url"}, result.PendingRefs)
	require.Nil(t, evaluation.configurations[node.Address].decoded)
	delete(evaluation.configurations, node.Address)
	evaluation.configurations[source.Address] = planEvaluationV2Configuration{planned: result}
	_, err = executor.planEvaluationV2DataSourceTarget(evaluation, &targetNode)
	require.ErrorContains(t, err, node.Address+`" has not been evaluated`)
}

func TestPlanEvaluationV2ConfigurationInstanceUsesEmptyDefaults(t *testing.T) {
	executor := newPlanEvaluationV2ConfigurationInstances(t)
	template := "resource.apps/resource.boxes"
	library := executor.DAG.Nodes[template].Libraries["cloud"]
	library.Configuration = &cfg.ConfigurationType[*struct{}]{
		SchemaVersion: 1, New: func() *struct{} { return &struct{}{} },
	}
	delete(executor.DAG.Nodes, template+"/library-config.cloud")
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
		newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
	)
	require.NoError(t, err)
	node := *executor.DAG.Nodes[template+"/action.child"]
	node.Address = "resource.apps['z']/resource.boxes['inner']/action.child"
	target, err := executor.planEvaluationV2ActionTarget(evaluation, &node)
	require.NoError(t, err)
	require.Equal(t, "resource.apps['z']/resource.boxes['inner']/library-config.cloud",
		target.Configuration.Record.Address)
	require.Equal(t, operationObject(t, map[string]EncodedValue{}), target.Configuration.Record.Value)
}
