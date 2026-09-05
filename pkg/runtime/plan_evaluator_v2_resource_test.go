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

const resourceTargetFixtureDir = "testdata/ub/plan-evaluator-v2/resource-target"

func resourceTargetDefault(t *testing.T, name string) string {
	t.Helper()
	src := ubtest.ReadValidFixture(t, resourceTargetFixtureDir, "default-values")
	body := syntaxFactoryBody(t, src)
	for _, local := range body.Locals {
		if local.Name.Name == name {
			value, err := lang.FormatExpr(local.Value)
			require.NoError(t, err)
			return value
		}
	}
	t.Fatalf("default %q is missing from fixture", name)
	return ""
}

func TestPlanEvaluationV2ResourceTargetUsesDefaultsAndSensitivity(t *testing.T) {
	library := &Library{
		LibraryPath: "example.com/cloud",
		Defaults: map[string][]lang.DefaultSpec{
			"resource.server": {{Field: "input.size", Value: resourceTargetDefault(t, "size")}},
		},
		Schema: &LibrarySchema{
			Resources: map[string]*TypeSchema{
				"server": {SensitiveInputs: []string{"size"}, SensitiveOutputs: []string{"id"}},
			},
		},
	}
	libraries := map[string]*Library{"cloud": library}
	src := ubtest.ReadValidFixture(t, resourceTargetFixtureDir, "defaults-sensitive")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{"token": StringValue("secret")}),
		newPlanEvaluationV2Snapshot(t),
		pass,
	)
	require.NoError(t, err)
	capture := &registeredPlanningCapture{}
	registration := newRegisteredPlanningResource(
		t, registeredPlanningDefinition(1, IdentityConfiguration), capture, "server",
	)
	target, err := executor.planEvaluationV2ResourceTarget(
		evaluation, dag.Nodes["resource.main"], registration,
	)
	require.NoError(t, err)
	require.NoError(t, target.Validate())
	require.Equal(t, Binding{LibraryPath: library.LibraryPath, Export: "server"}, target.Binding)
	require.Equal(t, operationObject(t, map[string]EncodedValue{
		"name": StringValue("secret"), "size": IntegerValue(2),
	}), target.Inputs)
	require.Equal(t, []string{"/name", "/size"}, target.SensitiveInputPaths)
	require.Equal(t, []string{"/id"}, target.SensitiveOutputPaths)
	definition, err := resolveLibraryConfigurationDefinition(library.LibraryPath, library)
	require.NoError(t, err)
	empty := operationObject(t, map[string]EncodedValue{})
	record, err := definition.newConfigurationRecord("", empty, nil, nil)
	require.NoError(t, err)
	require.Equal(t, PlannedConfiguration{
		Kind: PlannedConfigurationConcrete, Record: &record,
	}, target.Configuration)
	require.Empty(t, capture.reads)

	step, err := planRegisteredResourceStep(
		context.Background(), pass, registeredResourcePlanningRequest{
			Address: "resource.main", DependsOn: []string{}, Desired: target,
			DesiredConfigType: &definition, DesiredRegistration: registration,
		},
	)
	require.NoError(t, err)
	require.Equal(t, DecisionCreate, step.Operation.Resource.Decision)
	require.True(t, pass.outputsInvalidated("resource.main"))
}

func TestPlanEvaluationV2ResourceTargetUsesPlannedConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		prior    string
		decision Decision
		reasons  []string
	}{
		{"unchanged", "same", DecisionNoOp, []string{}},
		{"changed", "old", DecisionReplace, []string{"configuration"}},
		{"pending", "old", DecisionReplace,
			[]string{"configuration-pending"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			library := &Library{
				LibraryPath: "example.com/cloud", Configuration: configurationRegistration(1, nil),
			}
			libraries := map[string]*Library{"cloud": library}
			src := ubtest.ReadValidFixture(t, resourceTargetFixtureDir, "configuration-"+test.name)
			dag, source := syntaxDAGAndBody(t, src, libraries)
			executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
			)
			require.NoError(t, err)
			configurationRequest, err := executor.planEvaluationV2LibraryConfigurationRequest(
				evaluation, dag.Nodes["library-config.cloud"],
			)
			require.NoError(t, err)
			configurationStep, err := configurationRequest.Plan(context.Background(), pass)
			require.NoError(t, err)
			capture := &registeredPlanningCapture{}
			definition := registeredPlanningDefinition(1, IdentityConfiguration)
			registration := newRegisteredPlanningResource(t, definition, capture, "server")
			target, err := executor.planEvaluationV2ResourceTarget(
				evaluation, dag.Nodes["resource.main"], registration,
			)
			require.NoError(t, err)
			require.Equal(t, configurationStep.Operation.LibraryConfiguration.Result, target.Configuration)
			require.Empty(t, capture.reads)

			configuration := evaluation.configurations["library-config.cloud"]
			values, ok := configurationStep.Operation.LibraryConfiguration.Inputs.ObjectFields()
			require.True(t, ok)
			values["endpoint"] = StringValue(test.prior)
			record, err := configuration.definition.newConfigurationRecord(
				"library-config.cloud", operationObject(t, values), nil, nil,
			)
			require.NoError(t, err)
			prior := registeredPlanningTarget(
				t, definition, registration, target.Binding, record, "server", 1,
				&registeredPlanningOutput{ID: "server-1", Value: "server"},
			)
			step, err := planRegisteredResourceStep(
				context.Background(), pass, registeredResourcePlanningRequest{
					Address: "resource.main", DependsOn: []string{"library-config.cloud"},
					Desired: target, DesiredConfigType: &configuration.definition,
					DesiredRegistration: registration, Prior: &prior,
					PriorConfigType: &configuration.definition, PriorRegistration: registration,
				},
			)
			require.NoError(t, err)
			require.Equal(t, test.decision, step.Operation.Resource.Decision)
			require.Equal(t, test.reasons, step.Operation.Resource.Reasons)
			require.Equal(t, []string{"server:" + test.prior}, capture.reads)

			if target.Configuration.Record != nil {
				digest := configuration.planned.Record.Digest
				target.Configuration.Record.Digest = "changed"
				require.Equal(t, digest,
					evaluation.configurations["library-config.cloud"].planned.Record.Digest)
			} else {
				target.Configuration.PendingRefs[0] = "resource.changed.url"
				require.Equal(t, []string{"resource.endpoint.url"}, configuration.planned.PendingRefs)
			}
		})
	}
}

func TestPlanEvaluationV2ResourceTargetPreservesPartialInputs(t *testing.T) {
	library := &Library{
		LibraryPath: "example.com/cloud",
		Defaults: map[string][]lang.DefaultSpec{
			"resource.server": {
				{Field: "input.name", Value: resourceTargetDefault(t, "name")},
				{Field: "input.network.note", Value: resourceTargetDefault(t, "note")},
			},
		},
	}
	libraries := map[string]*Library{"cloud": library}
	src := ubtest.ReadValidFixture(t, resourceTargetFixtureDir, "partial-inputs")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	inputs := operationObject(t, map[string]EncodedValue{
		"network": operationObject(t, map[string]EncodedValue{"subnet-id": StringValue("subnet")}),
	})
	evaluation, err := executor.preparePlanEvaluationV2(
		inputs, newPlanEvaluationV2Snapshot(t), newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
	)
	require.NoError(t, err)
	registration := &resourceDefinitionRegistration{
		prepareInputsFunc: func(values map[string]any) (EncodedValue, error) {
			encoded, _, err := prepareResourceInputs[resourceValueInput](values)
			return encoded, err
		},
	}
	target, err := executor.planEvaluationV2ResourceTarget(
		evaluation, dag.Nodes["resource.main"], registration,
	)
	require.NoError(t, err)
	name, err := PendingEncodedValue([]string{"resource.upstream.id"})
	require.NoError(t, err)
	zone, err := PendingEncodedValue([]string{"resource.upstream.zone"})
	require.NoError(t, err)
	require.Equal(t, mustResourceObject(t, map[string]EncodedValue{
		"name": name, "optional": AbsentValue(), "nullable": NullValue(),
		"zones": mustResourceList(t, []EncodedValue{StringValue("known"), zone}),
		"network": mustResourceObject(t, map[string]EncodedValue{
			"subnet-id": StringValue("subnet"), "note": StringValue("note"),
		}),
		"labels": AbsentValue(), "endpoints": AbsentValue(), "enabled": AbsentValue(),
		"ratio": AbsentValue(), "small": AbsentValue(),
	}), target.Inputs)
	require.Equal(t, map[string]any{
		"network": map[string]any{"subnet-id": "subnet"},
	}, evaluation.run.eval.Inputs)
}

func TestPlanEvaluationV2ResourceTargetResolvesAliasesAndCompositeScope(t *testing.T) {
	libraries := planEvaluationV2Libraries()
	libraries["renamed"] = libraries["cloud"]
	body := ubtest.ReadValidFixture(t, resourceTargetFixtureDir, "composite-body")
	composite := syntaxResourceComposite(t, "application", body)
	composite.Libraries = map[string]*Library{"cloud": libraries["cloud"]}
	libraries["app"] = &Library{
		LibraryPath:        "example.com/app",
		ResourceComposites: map[string]*CompositeType{"application": composite},
	}
	src := ubtest.ReadValidFixture(t, resourceTargetFixtureDir, "aliases-composite")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
	)
	require.NoError(t, err)
	registration := newRegisteredPlanningResource(
		t, registeredPlanningDefinition(1, IdentityConfiguration), &registeredPlanningCapture{}, "server",
	)
	for _, test := range []struct{ address, config, name string }{
		{"resource.first", "library-config.cloud", "first"},
		{"resource.second", "library-config.renamed", "second"},
		{"resource.nested/resource.child", "resource.nested/library-config.cloud", "nested"},
	} {
		request, err := executor.planEvaluationV2LibraryConfigurationRequest(
			evaluation, dag.Nodes[test.config],
		)
		require.NoError(t, err)
		_, err = request.Plan(context.Background(), pass)
		require.NoError(t, err)
		target, err := executor.planEvaluationV2ResourceTarget(
			evaluation, dag.Nodes[test.address], registration,
		)
		require.NoError(t, err)
		require.Equal(t, Binding{LibraryPath: "example.com/cloud", Export: "server"}, target.Binding)
		require.Equal(t, test.config, target.Configuration.Record.Address)
		fields, ok := target.Configuration.Record.Value.ObjectFields()
		require.True(t, ok)
		require.Equal(t, StringValue(test.name), fields["endpoint"])
		require.Equal(t, operationObject(t, map[string]EncodedValue{
			"name": StringValue(test.name), "size": IntegerValue(1),
		}), target.Inputs)
	}
}

func TestPlanEvaluationV2ResourceTargetAllowsEmptyConfiguration(t *testing.T) {
	library := &Library{
		LibraryPath: "example.com/cloud",
		Configuration: &cfg.ConfigurationType[*struct{}]{
			SchemaVersion: 1, New: func() *struct{} { return &struct{}{} },
		},
	}
	libraries := map[string]*Library{"cloud": library}
	src := ubtest.ReadValidFixture(t, resourceTargetFixtureDir, "empty-configuration")
	dag := syntaxDAG(t, src, libraries)
	executor := &Executor{DAG: dag, Libraries: libraries}
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
		newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
	)
	require.NoError(t, err)
	registration := newRegisteredPlanningResource(
		t, registeredPlanningDefinition(1, IdentityConfiguration), &registeredPlanningCapture{}, "server",
	)
	target, err := executor.planEvaluationV2ResourceTarget(
		evaluation, dag.Nodes["resource.main"], registration,
	)
	require.NoError(t, err)
	require.Equal(t, "library-config.cloud", target.Configuration.Record.Address)
	require.Equal(t, operationObject(t, map[string]EncodedValue{}), target.Configuration.Record.Value)
}

func TestPlanEvaluationV2ResourceTargetRejectsInvalidSetup(t *testing.T) {
	libraries := planEvaluationV2Libraries()
	src := ubtest.ReadValidFixture(t, resourceTargetFixtureDir, "required-configuration")
	dag := syntaxDAG(t, src, libraries)
	executor := &Executor{DAG: dag, Libraries: libraries}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
	)
	require.NoError(t, err)
	registration := newRegisteredPlanningResource(
		t, registeredPlanningDefinition(1, IdentityConfiguration), &registeredPlanningCapture{}, "server",
	)
	node := dag.Nodes["resource.main"]
	_, err = executor.planEvaluationV2ResourceTarget(evaluation, node, registration)
	require.ErrorContains(t, err,
		`library configuration "library-config.cloud" has not been evaluated`)
	request, err := executor.planEvaluationV2LibraryConfigurationRequest(
		evaluation, dag.Nodes["library-config.cloud"],
	)
	require.NoError(t, err)
	_, err = request.Plan(context.Background(), pass)
	require.NoError(t, err)
	for _, test := range []struct {
		name    string
		change  func(*Node)
		message string
	}{
		{"kind", func(n *Node) { n.Kind = NodeAction }, "node must be a primitive resource"},
		{"composite", func(n *Node) { n.CompositeSyntaxBody = &syntax.FactoryBody{} },
			"node must be a primitive resource"},
		{"address", func(n *Node) { n.Address = "output.main" }, "address"},
		{"instances", func(n *Node) { n.ForEach = &lang.ObjectLit{} }, "must be expanded first"},
		{"alias", func(n *Node) { n.Alias = "missing" }, `library "missing" is not imported`},
		{"export", func(n *Node) { n.Type = "" }, "export is required"},
		{"path", func(n *Node) { n.LibraryPath = "example.com/other" },
			"resource library path does not match import"},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := *node
			test.change(&changed)
			target, err := executor.planEvaluationV2ResourceTarget(evaluation, &changed, registration)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
		})
	}
	var missing *Executor
	_, err = missing.planEvaluationV2ResourceTarget(evaluation, node, registration)
	require.ErrorContains(t, err, "executor is required")
	_, err = (&Executor{}).planEvaluationV2ResourceTarget(evaluation, node, registration)
	require.ErrorContains(t, err, "dependency graph is required")
	_, err = executor.planEvaluationV2ResourceTarget(nil, node, registration)
	require.ErrorContains(t, err, "version 2 plan evaluation is required")
	_, err = executor.planEvaluationV2ResourceTarget(evaluation, nil, registration)
	require.ErrorContains(t, err, "resource node is required")
	_, err = executor.planEvaluationV2ResourceTarget(evaluation, node, nil)
	require.ErrorContains(t, err, "resource registration is required")
	delete(dag.Nodes, "library-config.cloud")
	_, err = executor.planEvaluationV2ResourceTarget(evaluation, node, registration)
	require.ErrorContains(t, err, `library "cloud" requires a configuration`)
}

func TestPlanEvaluationV2ResourceTargetRejectsInvalidInputs(t *testing.T) {
	libraries := map[string]*Library{"cloud": {LibraryPath: "example.com/cloud"}}
	src := ubtest.ReadValidFixture(t, resourceTargetFixtureDir, "empty-configuration")
	dag := syntaxDAG(t, src, libraries)
	executor := &Executor{DAG: dag, Libraries: libraries}
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
		newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
	)
	require.NoError(t, err)
	registration := newRegisteredPlanningResource(
		t, registeredPlanningDefinition(1, IdentityConfiguration), &registeredPlanningCapture{}, "server",
	)
	ubtest.Run(t, resourceTargetFixtureDir+"/invalid", func(
		name string, src []byte,
	) (string, []string) {
		body, err := lang.ParseExpr(name, src)
		require.NoError(t, err)
		node := *dag.Nodes["resource.main"]
		node.Body = body
		target, err := executor.planEvaluationV2ResourceTarget(evaluation, &node, registration)
		require.Nil(t, target)
		if err != nil {
			return "", []string{err.Error()}
		}
		return "", nil
	})
}
