package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func newCatalogPlanningLibrary(capture *registeredPlanningCapture, name string) *Library {
	return &Library{
		Configuration: configurationRegistration(1, nil),
		Resources: map[string]ResourceRegistration{
			"server": MakeResourceWith[
				registeredPlanningInput, *registeredPlanningOutput, *recordedConfiguration,
			](registeredPlanningDefinition(1, IdentityConfiguration), func() *registeredPlanningInput {
				return &registeredPlanningInput{capture: capture, registration: name}
			}),
		},
	}
}

func newCatalogPlanningExecutor(t *testing.T, catalog *LibraryCatalog) *Executor {
	t.Helper()
	libraries, err := catalog.Libraries(map[string]string{"cloud": "example.com/cloud"})
	require.NoError(t, err)
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-evaluator-v2/resource-request", "targets")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	return &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries, LibraryCatalog: catalog}
}

func TestLibraryCatalogResolvesResourceDefinition(t *testing.T) {
	library := newCatalogPlanningLibrary(&registeredPlanningCapture{}, "current")
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/cloud", New: func() *Library { return library }},
	})
	require.NoError(t, err)
	registration, configuration, err := catalog.resource(Binding{
		LibraryPath: "example.com/cloud", Export: "server",
	})
	require.NoError(t, err)
	require.Same(t, library.Resources["server"].resourceDefinition(), registration)
	require.Equal(t, "example.com/cloud", configuration.libraryPath)
	require.False(t, configuration.noConfig)
}

func TestPlanEvaluationV2RejectsUnavailableRecordedResources(t *testing.T) {
	for _, address := range []string{"resource.main", "resource.removed"} {
		for _, binding := range []Binding{
			{LibraryPath: "example.com/missing", Export: "server"},
			{LibraryPath: "example.com/cloud", Export: "missing"},
		} {
			t.Run(address+"/"+binding.LibraryPath+"/"+binding.Export, func(t *testing.T) {
				capture := &registeredPlanningCapture{}
				library := newCatalogPlanningLibrary(capture, "current")
				catalog, err := NewLibraryCatalog([]LibraryRegistration{
					{LibraryPath: "example.com/cloud", New: func() *Library { return library }},
				})
				require.NoError(t, err)
				executor := newCatalogPlanningExecutor(t, catalog)
				_, configuration := registeredOperationConfiguration(t, binding.LibraryPath, "same")
				prior := registeredPlanningTarget(t, registeredPlanningDefinition(1, IdentityConfiguration),
					library.Resources["server"].resourceDefinition(), binding, configuration,
					"server", 1, &registeredPlanningOutput{ID: "server-1", Value: "server"},
				)
				snapshot := newPlanEvaluationV2Snapshot(t)
				addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
					Address: address, Kind: state.StateResource,
					Payload: state.StatePayload{
						Kind: state.StateResource, Resource: &state.ResourceStatePayload{Target: prior},
					},
				})
				original, err := snapshot.Clone()
				require.NoError(t, err)
				pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
				evaluation, err := executor.preparePlanEvaluationV2(
					operationObject(t, map[string]EncodedValue{
						"name": StringValue("server"), "size": IntegerValue(1),
					}), snapshot, pass,
				)
				require.ErrorContains(t, err, address)
				require.ErrorContains(t, err, binding.LibraryPath)
				require.ErrorContains(t, err, binding.Export)
				require.Nil(t, evaluation)
				require.Empty(t, capture.reads)
				require.Equal(t, original, snapshot)
			})
		}
	}
}

func TestPlanEvaluationV2UsesCatalogForRecordedResource(t *testing.T) {
	for _, path := range []string{"example.com/cloud", "example.com/old"} {
		t.Run(path, func(t *testing.T) {
			capture := &registeredPlanningCapture{}
			current := newCatalogPlanningLibrary(capture, "current")
			old := newCatalogPlanningLibrary(capture, "old")
			catalog, err := NewLibraryCatalog([]LibraryRegistration{
				{LibraryPath: "example.com/cloud", New: func() *Library { return current }},
				{LibraryPath: "example.com/old", New: func() *Library { return old }},
			})
			require.NoError(t, err)
			executor := newCatalogPlanningExecutor(t, catalog)
			binding := Binding{LibraryPath: path, Export: "server"}
			registration, _, err := catalog.resource(binding)
			require.NoError(t, err)
			_, configuration := registeredOperationConfiguration(t, path, "same")
			configurationType, err := resolveLibraryConfigurationDefinition(path, current)
			require.NoError(t, err)
			configuration, err = configurationType.newConfigurationRecord(
				"library-config.previous-alias", configuration.Value, nil, nil,
			)
			require.NoError(t, err)
			prior := registeredPlanningTarget(t, registeredPlanningDefinition(1, IdentityConfiguration),
				registration, binding, configuration, "server", 1,
				&registeredPlanningOutput{ID: "server-1", Value: "server"},
			)
			snapshot := newPlanEvaluationV2Snapshot(t)
			addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
				Address: "resource.main", Kind: state.StateResource,
				Payload: state.StatePayload{
					Kind: state.StateResource, Resource: &state.ResourceStatePayload{Target: prior},
				},
			})
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{
					"name": StringValue("server"), "size": IntegerValue(1),
				}), snapshot, pass,
			)
			require.NoError(t, err)
			request, err := executor.planEvaluationV2ResourceRequest(
				evaluation, executor.DAG.Nodes["resource.main"], nil,
			)
			require.NoError(t, err)
			require.Empty(t, capture.reads)
			config, err := executor.planEvaluationV2LibraryConfigurationRequest(
				evaluation, executor.DAG.Nodes["library-config.cloud"],
			)
			require.NoError(t, err)
			_, err = config.Plan(context.Background(), pass)
			require.NoError(t, err)
			step, err := request.Plan(context.Background(), pass)
			require.NoError(t, err)
			require.NoError(t, step.Validate())
			if path == "example.com/cloud" {
				require.Equal(t, DecisionNoOp, step.Operation.Resource.Decision)
				require.Equal(t, []string{"current:same"}, capture.reads)
			} else {
				require.Equal(t, DecisionReplace, step.Operation.Resource.Decision)
				require.Equal(t, []string{"old:same"}, capture.reads)
			}
		})
	}
}

func TestPlanEvaluationV2RejectsUnavailableDesiredResourceBeforeEvaluation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Executor)
		want   string
	}{
		{"missing export", func(e *Executor) { e.DAG.Nodes["resource.many"].Type = "missing" },
			`resource "missing" in library "example.com/cloud"`},
		{"missing import", func(e *Executor) { delete(e.Libraries, "cloud") },
			`library "cloud" is not imported`},
		{"conflicting path", func(e *Executor) {
			e.DAG.Nodes["resource.many"].LibraryPath = "example.com/other"
		}, "resource library path does not match import"},
	} {
		t.Run(test.name, func(t *testing.T) {
			capture := &registeredPlanningCapture{}
			catalog, err := NewLibraryCatalog([]LibraryRegistration{
				{LibraryPath: "example.com/cloud", New: func() *Library {
					return newCatalogPlanningLibrary(capture, "current")
				}},
			})
			require.NoError(t, err)
			executor := newCatalogPlanningExecutor(t, catalog)
			test.change(executor)
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
				newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
			)
			require.ErrorContains(t, err, test.want)
			require.Nil(t, evaluation)
			require.Empty(t, capture.reads)
		})
	}
}

func TestLibraryCatalogResolvesResourceWithoutConfiguration(t *testing.T) {
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/plain", New: func() *Library {
			return &Library{Resources: map[string]ResourceRegistration{
				"plain": MakeResource[plainResource, *plainResourceOutput, any](plainResourceDefinition()),
			}}
		}},
	})
	require.NoError(t, err)
	registration, configuration, err := catalog.resource(Binding{
		LibraryPath: "example.com/plain", Export: "plain",
	})
	require.NoError(t, err)
	require.NotNil(t, registration)
	require.True(t, configuration.noConfig)
	require.Equal(t, "example.com/plain", configuration.libraryPath)
}

func TestLibraryCatalogRejectsInvalidResourceConfiguration(t *testing.T) {
	library := newCatalogPlanningLibrary(&registeredPlanningCapture{}, "current")
	library.Configuration = configurationRegistration(0, nil)
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/cloud", New: func() *Library { return library }},
	})
	require.NoError(t, err)
	registration, configuration, err := catalog.resource(Binding{
		LibraryPath: "example.com/cloud", Export: "server",
	})
	require.ErrorContains(t, err, `resource "server" in library "example.com/cloud"`)
	require.ErrorContains(t, err, "configuration schema version")
	require.Nil(t, registration)
	require.Nil(t, configuration)
}
