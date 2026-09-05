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

const dataSourceTargetFixtureDir = "testdata/ub/plan-evaluator-v2/data-source-target"

type dataSourceTargetInput struct {
	Name     string   `ub:"name"`
	Size     uint8    `ub:"size"`
	Nullable *string  `ub:"nullable"`
	Zones    []string `ub:"zones"`

	reads *int
}

type dataSourceTargetOutput struct {
	ID string `ub:"id"`
}

func (d *dataSourceTargetInput) Read(context.Context, any) (*dataSourceTargetOutput, error) {
	*d.reads++
	return &dataSourceTargetOutput{ID: d.Name}, nil
}

func dataSourceTargetLibrary(t *testing.T, reads *int) *Library {
	t.Helper()
	return &Library{
		LibraryPath: "example.com/cloud",
		DataSources: map[string]DataSourceRegistration{
			"lookup": MakeDataSourceWith[dataSourceTargetInput, *dataSourceTargetOutput, any](
				func() *dataSourceTargetInput { return &dataSourceTargetInput{reads: reads} },
			),
		},
		Defaults: map[string][]lang.DefaultSpec{
			"data-source.lookup": {{Field: "input.size", Value: resourceTargetDefault(t, "size")}},
		},
		Schema: &LibrarySchema{DataSources: map[string]*TypeSchema{
			"lookup": {SensitiveInputs: []string{"size"}, SensitiveOutputs: []string{"id"}},
		}},
	}
}

func newDataSourceTargetEvaluation(
	t *testing.T,
	libraries map[string]*Library,
	fixture string,
) (*Executor, *planEvaluationV2, *planningPassState) {
	t.Helper()
	src := ubtest.ReadValidFixture(t, dataSourceTargetFixtureDir, fixture)
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

func TestPlanEvaluationV2DataSourceTargetPreparesTypedInputs(t *testing.T) {
	reads := 0
	library := dataSourceTargetLibrary(t, &reads)
	executor, evaluation, _ := newDataSourceTargetEvaluation(
		t, map[string]*Library{"cloud": library}, "targets",
	)
	target, err := executor.planEvaluationV2DataSourceTarget(
		evaluation, executor.DAG.Nodes["data-source.main"],
	)
	require.NoError(t, err)
	require.NoError(t, target.Validate())
	definition, err := resolveLibraryConfigurationDefinition(library.LibraryPath, library)
	require.NoError(t, err)
	record, err := definition.newConfigurationRecord(
		"", operationObject(t, map[string]EncodedValue{}), nil, nil,
	)
	require.NoError(t, err)
	require.Equal(t, &PlannedDataSourceTarget{
		Binding: Binding{LibraryPath: "example.com/cloud", Export: "lookup"},
		Inputs: operationObject(t, map[string]EncodedValue{
			"name": StringValue("secret"), "size": IntegerValue(2),
			"nullable": AbsentValue(), "zones": AbsentValue(),
		}),
		Configuration:       PlannedConfiguration{Kind: PlannedConfigurationConcrete, Record: &record},
		SensitiveInputPaths: []string{"/name", "/size"}, SensitiveOutputPaths: []string{"/id"},
	}, target)
	require.Zero(t, reads)
	require.Equal(t, map[string]any{"token": "secret"}, evaluation.run.eval.Inputs)
}

func TestPlanEvaluationV2DataSourceTargetPreservesPendingValues(t *testing.T) {
	reads := 0
	executor, evaluation, _ := newDataSourceTargetEvaluation(
		t, map[string]*Library{"cloud": dataSourceTargetLibrary(t, &reads)}, "targets",
	)
	node := executor.DAG.Nodes["data-source.partial"]
	target, err := executor.planEvaluationV2DataSourceTarget(evaluation, node)
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
	operation, err := planDataSourceOperation(context.Background(), dataSourcePlanningRequest{
		Address: node.Address, DependsOn: []string{}, Desired: target,
	}, dataSourcePlanningCallbacks{})
	require.NoError(t, err)
	require.Equal(t, DecisionRead, operation.Decision)
	require.Nil(t, operation.ObservedOutputs)

	evaluation.run.eval.Resources["upstream"] = map[string]any{
		"id": "resolved", "zone": "east", "size": int64(3),
	}
	resolved, err := executor.planEvaluationV2DataSourceTarget(evaluation, node)
	require.NoError(t, err)
	require.False(t, resolved.Inputs.HasPending())
	fields, _ := resolved.Inputs.ObjectFields()
	require.Equal(t, StringValue("resolved"), fields["name"])
	require.Equal(t, IntegerValue(3), fields["size"])
	require.Equal(t, mustResourceList(t, []EncodedValue{
		StringValue("known"), StringValue("east"),
	}), fields["zones"])
	require.True(t, target.Inputs.HasPending())
	require.Zero(t, reads)
}

func TestPlanEvaluationV2DataSourceTargetUsesScopedConfigurations(t *testing.T) {
	reads := 0
	library := dataSourceTargetLibrary(t, &reads)
	library.Configuration = planEvaluationV2Libraries()["cloud"].Configuration
	libraries := map[string]*Library{"cloud": library, "renamed": library}
	body := ubtest.ReadValidFixture(t, dataSourceTargetFixtureDir, "composite-body")
	composite := syntaxResourceComposite(t, "application", body)
	composite.Libraries = map[string]*Library{"cloud": library}
	libraries["app"] = &Library{
		LibraryPath: "example.com/app", ResourceComposites: map[string]*CompositeType{
			"application": composite,
		},
	}
	executor, evaluation, pass := newDataSourceTargetEvaluation(t, libraries, "configurations")
	for _, test := range []struct{ address, configuration, value string }{
		{"data-source.first", "library-config.cloud", "first"},
		{"data-source.second", "library-config.renamed", "second"},
		{"resource.nested/data-source.child", "resource.nested/library-config.cloud", "nested"},
	} {
		t.Run(test.address, func(t *testing.T) {
			node := executor.DAG.Nodes[test.address]
			target, err := executor.planEvaluationV2DataSourceTarget(evaluation, node)
			require.ErrorContains(t, err, "has not been evaluated")
			require.Nil(t, target)
			request, err := executor.planEvaluationV2LibraryConfigurationRequest(
				evaluation, executor.DAG.Nodes[test.configuration],
			)
			require.NoError(t, err)
			_, err = request.Plan(context.Background(), pass)
			require.NoError(t, err)
			target, err = executor.planEvaluationV2DataSourceTarget(evaluation, node)
			require.NoError(t, err)
			require.Equal(t, Binding{LibraryPath: library.LibraryPath, Export: "lookup"}, target.Binding)
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
	require.Zero(t, reads)
}

func TestPlanEvaluationV2DataSourceTargetHandlesMissingConfiguration(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "empty"}[empty], func(t *testing.T) {
			reads := 0
			library := dataSourceTargetLibrary(t, &reads)
			library.Configuration = configurationRegistration(1, nil)
			if empty {
				library.Configuration = &cfg.ConfigurationType[*struct{}]{
					SchemaVersion: 1, New: func() *struct{} { return &struct{}{} },
				}
			}
			executor, evaluation, _ := newDataSourceTargetEvaluation(
				t, map[string]*Library{"cloud": library}, "targets",
			)
			target, err := executor.planEvaluationV2DataSourceTarget(
				evaluation, executor.DAG.Nodes["data-source.main"],
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
			require.Zero(t, reads)
		})
	}
}

func TestPlanEvaluationV2DataSourceTargetRejectsInvalidSetup(t *testing.T) {
	reads := 0
	library := dataSourceTargetLibrary(t, &reads)
	executor, evaluation, _ := newDataSourceTargetEvaluation(
		t, map[string]*Library{"cloud": library}, "targets",
	)
	node := executor.DAG.Nodes["data-source.main"]
	for _, test := range []struct {
		name    string
		change  func(*Node)
		message string
	}{
		{"kind", func(n *Node) { n.Kind = NodeResource }, "node must be a primitive data source"},
		{"composite", func(n *Node) { n.CompositeSyntaxBody = &syntax.FactoryBody{} },
			"node must be a primitive data source"},
		{"address", func(n *Node) { n.Address = "resource.main" }, "address"},
		{"instances", func(n *Node) { n.ForEach = &lang.ObjectLit{} }, "must be expanded first"},
		{"alias", func(n *Node) { n.Alias = "missing" }, `library "missing" is not imported`},
		{"export", func(n *Node) { n.Type = "missing" }, `has no data source "missing"`},
		{"empty export", func(n *Node) { n.Type = "" }, "export is required"},
		{"path", func(n *Node) { n.LibraryPath = "example.com/other" },
			"data-source library path does not match import"},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := *node
			test.change(&changed)
			target, err := executor.planEvaluationV2DataSourceTarget(evaluation, &changed)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
		})
	}
	var missing *Executor
	_, err := missing.planEvaluationV2DataSourceTarget(evaluation, node)
	require.ErrorContains(t, err, "executor is required")
	_, err = (&Executor{}).planEvaluationV2DataSourceTarget(evaluation, node)
	require.ErrorContains(t, err, "dependency graph is required")
	_, err = executor.planEvaluationV2DataSourceTarget(nil, node)
	require.ErrorContains(t, err, "version 2 plan evaluation is required")
	_, err = executor.planEvaluationV2DataSourceTarget(evaluation, nil)
	require.ErrorContains(t, err, "data-source node is required")
	library.DataSources["lookup"] = nil
	_, err = executor.planEvaluationV2DataSourceTarget(evaluation, node)
	require.ErrorContains(t, err, "data-source registration is required")
	require.Zero(t, reads)
}

type dataSourceTargetRegistration struct {
	DataSourceRegistration
	newReceiver func() any
}

func (r *dataSourceTargetRegistration) NewReceiver() any { return r.newReceiver() }

func TestPlanEvaluationV2DataSourceTargetRejectsInvalidReceiver(t *testing.T) {
	for _, test := range []struct {
		name    string
		factory func() any
		message string
	}{
		{"nil", func() any { return nil }, "receiver must be a non-nil pointer to a struct"},
		{"nil pointer", func() any { return (*dataSourceTargetInput)(nil) },
			"receiver must be a non-nil pointer to a struct"},
		{"scalar", func() any { return new(string) }, "receiver must be a non-nil pointer to a struct"},
		{"unsupported", func() any { return &struct{ Value chan int }{} }, "unsupported"},
		{"panic", func() any { panic("constructor failed") }, "constructor failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reads := 0
			library := dataSourceTargetLibrary(t, &reads)
			library.DataSources["lookup"] = &dataSourceTargetRegistration{newReceiver: test.factory}
			executor, evaluation, _ := newDataSourceTargetEvaluation(
				t, map[string]*Library{"cloud": library}, "targets",
			)
			target, err := executor.planEvaluationV2DataSourceTarget(
				evaluation, executor.DAG.Nodes["data-source.main"],
			)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, target)
			require.Zero(t, reads)
		})
	}
}

func TestPlanEvaluationV2DataSourceTargetRejectsInvalidInputs(t *testing.T) {
	reads := 0
	executor, evaluation, _ := newDataSourceTargetEvaluation(
		t, map[string]*Library{"cloud": dataSourceTargetLibrary(t, &reads)}, "targets",
	)
	ubtest.Run(t, dataSourceTargetFixtureDir+"/invalid", func(
		name string, src []byte,
	) (string, []string) {
		body, err := lang.ParseExpr(name, src)
		require.NoError(t, err)
		node := *executor.DAG.Nodes["data-source.main"]
		node.Body = body
		target, err := executor.planEvaluationV2DataSourceTarget(evaluation, &node)
		require.Nil(t, target)
		if err != nil {
			return "", []string{err.Error()}
		}
		return "", nil
	})
	require.Zero(t, reads)
}
