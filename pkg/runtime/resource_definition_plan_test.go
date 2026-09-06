package runtime

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type equivalentResource struct {
	Name string
	Size int64
}

type equivalentOutput struct {
	ID   string
	Name string
	Size int64
}

func equivalentResourceDefinition() ResourceDefinition[
	equivalentResource,
	*equivalentOutput,
	any,
] {
	return ResourceDefinition[equivalentResource, *equivalentOutput, any]{
		InputSemantics: InputSemantics[equivalentResource]{
			Rules: []InputRule[equivalentResource]{
				EqualBy(InputField(func(v *equivalentResource) *string { return &v.Name }), equivalentName),
			},
		},
		SchemaVersion: 1,
		Identity: ResourceIdentity[equivalentResource, *equivalentOutput]{
			Version: 1,
			Scope:   IdentityConfiguration,
		},
		Replacement: ReplacementRules[equivalentResource, *equivalentOutput]{
			Inputs: []ReplacementRule[equivalentResource]{
				ReplaceWhenChanged(InputField(func(v *equivalentResource) *string { return &v.Name })),
			},
		},
	}
}

func (r *equivalentResource) Create(_ context.Context, _ any) (*equivalentOutput, error) {
	return &equivalentOutput{ID: "equivalent-" + r.Name, Name: r.Name, Size: r.Size}, nil
}

func (r *equivalentResource) Read(
	_ context.Context, _ any, prior *equivalentOutput,
) (*equivalentOutput, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *equivalentResource) Update(
	_ context.Context, _ any, prior Prior[equivalentResource, *equivalentOutput],
) (*equivalentOutput, error) {
	prior.Outputs.Name = r.Name
	prior.Outputs.Size = r.Size
	return prior.Outputs, nil
}

func (r *equivalentResource) Delete(_ context.Context, _ any, _ *equivalentOutput) error {
	return nil
}

type resourceDependencyCounters struct {
	consumerUpdates int64
	consumerRef     atomic.Value
}

type versionOutputResource struct {
	Value string
}

type versionOutput struct {
	Version string
}

func versionOutputResourceDefinition() ResourceDefinition[
	versionOutputResource,
	*versionOutput,
	any,
] {
	return ResourceDefinition[versionOutputResource, *versionOutput, any]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[versionOutputResource, *versionOutput]{
			Version: 1,
			Scope:   IdentityConfiguration,
		},
	}
}

func (r *versionOutputResource) Create(_ context.Context, _ any) (*versionOutput, error) {
	return &versionOutput{Version: "version-" + r.Value}, nil
}

func (r *versionOutputResource) Read(
	_ context.Context, _ any, prior *versionOutput,
) (*versionOutput, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *versionOutputResource) Update(
	_ context.Context, _ any, _ Prior[versionOutputResource, *versionOutput],
) (*versionOutput, error) {
	return &versionOutput{Version: "version-" + r.Value}, nil
}

func (r *versionOutputResource) Delete(_ context.Context, _ any, _ *versionOutput) error {
	return nil
}

type versionConsumer struct {
	Ref string

	counters *resourceDependencyCounters
}

type versionConsumerOutput struct{ Ref string }

func versionConsumerDefinition() ResourceDefinition[
	versionConsumer,
	*versionConsumerOutput,
	any,
] {
	return ResourceDefinition[versionConsumer, *versionConsumerOutput, any]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[versionConsumer, *versionConsumerOutput]{
			Version: 1,
			Scope:   IdentityConfiguration,
		},
	}
}

func (r *versionConsumer) Create(_ context.Context, _ any) (*versionConsumerOutput, error) {
	r.counters.consumerRef.Store(r.Ref)
	return &versionConsumerOutput{Ref: r.Ref}, nil
}

func (r *versionConsumer) Read(
	_ context.Context, _ any, prior *versionConsumerOutput,
) (*versionConsumerOutput, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *versionConsumer) Update(
	_ context.Context, _ any, prior Prior[versionConsumer, *versionConsumerOutput],
) (*versionConsumerOutput, error) {
	atomic.AddInt64(&r.counters.consumerUpdates, 1)
	r.counters.consumerRef.Store(r.Ref)
	out := *prior.Outputs
	out.Ref = r.Ref
	return &out, nil
}

func (r *versionConsumer) Delete(_ context.Context, _ any, _ *versionConsumerOutput) error {
	return nil
}

func TestInputEqualityClassifiesAndPersistsDesiredInputs(t *testing.T) {
	for _, tt := range []struct {
		fixture  string
		decision Decision
	}{
		{fixture: "equivalent-name", decision: DecisionNoOp},
		{fixture: "equivalent-name-and-size", decision: DecisionUpdate},
	} {
		t.Run(tt.fixture, func(t *testing.T) {
			store := newStateStore(t)
			libraries := resourcePlanModules(nil)
			initial := resourcePlanExecutor(t, resourcePlanFixture(t, "equivalent-initial"),
				libraries, store)
			firstPlan, err := initial.PlanV2(context.Background())
			require.NoError(t, err)
			_, err = initial.ApplyPlanV2(context.Background(), firstPlan)
			require.NoError(t, err)
			executor := resourcePlanExecutor(t, resourcePlanFixture(t, tt.fixture), libraries, store)
			plan, err := executor.PlanV2(context.Background())
			require.NoError(t, err)
			require.Len(t, plan.Steps, 1)
			require.Equal(t, "resource.one", plan.Steps[0].Address)
			operation := plan.Steps[0].Operation.Resource
			require.Equal(t, tt.decision, operation.Decision)
			require.Empty(t, operation.Reasons)
			result, err := executor.ApplyPlanV2(context.Background(), plan)
			require.NoError(t, err)
			snapshot, err := store.GetV2(result.WrittenRev)
			require.NoError(t, err)
			target := snapshot.Find("resource.one").Payload.Resource.Target
			require.Equal(t, operation.Desired.Inputs, target.Inputs)
			fields, _ := target.Inputs.ObjectFields()
			require.Equal(t, StringValue("alpha"), fields["name"])
			if tt.decision == DecisionNoOp {
				require.Equal(t, *operation.Observation.Outputs, target.Outputs)
				require.Equal(t, *operation.Observation.Identity, target.Identity)
			}
		})
	}
}

func TestInputEqualityDoesNotApplyToApplyPremise(t *testing.T) {
	store := newStateStore(t)
	libraries := resourcePlanModules(nil)
	source := resourcePlanFixture(t, "equivalent-input")
	executor := resourcePlanExecutor(t, source, libraries, store)
	executor.Inputs = map[string]any{"n": "ref:alpha"}
	initial, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	applied, err := executor.ApplyPlanV2(context.Background(), initial)
	require.NoError(t, err)
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Steps, 1)
	require.Equal(t, DecisionNoOp, plan.Steps[0].Operation.Resource.Decision)
	plan.Inputs = operationObject(t, map[string]EncodedValue{"n": StringValue("alpha")})
	plan.Digest, err = planFileV2Digest(*plan)
	require.NoError(t, err)
	require.NoError(t, plan.Validate())
	_, err = executor.ApplyPlanV2(context.Background(), plan)
	require.ErrorContains(t, err, "desired inputs do not match the saved plan")
	current, err := store.CurrentRev()
	require.NoError(t, err)
	require.Equal(t, applied.WrittenRev, current)
}

func TestResourceUpdateMarksOutputPending(t *testing.T) {
	counters := &resourceDependencyCounters{}
	store := newStateStore(t)
	libraries := resourcePlanModules(counters)
	source := resourcePlanFixture(t, "unknown-output")
	executor := resourcePlanExecutor(t, source, libraries, store)
	executor.Inputs = map[string]any{"value": "one"}
	initial, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	_, err = executor.ApplyPlanV2(context.Background(), initial)
	require.NoError(t, err)
	executor.Inputs = map[string]any{"value": "two"}
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	operations := map[string]*ResourcePlanOperation{}
	for _, step := range plan.Steps {
		operations[step.Address] = step.Operation.Resource
	}
	require.Contains(t, operations, "resource.upstream")
	require.Equal(t, DecisionUpdate, operations["resource.upstream"].Decision)
	require.Contains(t, operations, "resource.downstream")
	downstream := operations["resource.downstream"]
	require.Equal(t, DecisionUpdate, downstream.Decision)
	inputs, _ := downstream.Desired.Inputs.ObjectFields()
	refs, pending := inputs["ref"].PendingRefs()
	require.True(t, pending)
	require.Equal(t, []string{"resource.upstream.version"}, refs)
	_, err = executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	require.EqualValues(t, 1, counters.consumerUpdates)
	require.Equal(t, "version-two", counters.consumerRef.Load())
}

func resourcePlanModules(counters *resourceDependencyCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"equivalent": MakeResource[equivalentResource, *equivalentOutput, any](
					equivalentResourceDefinition(),
				),
				"versioned": MakeResource[versionOutputResource, *versionOutput, any](
					versionOutputResourceDefinition(),
				),
				"consumer": MakeResourceWith[versionConsumer, *versionConsumerOutput, any](
					versionConsumerDefinition(),

					func() *versionConsumer {
						return &versionConsumer{counters: counters}
					},
				),
			},
		},
	}
}

func resourcePlanExecutor(
	t *testing.T,
	src string,
	libs map[string]*Library,
	store state.Backend,
) *Executor {
	t.Helper()
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/resource-plan", New: func() *Library { return libs["core"] }},
	})
	require.NoError(t, err)
	dag, body := syntaxDAGAndBody(t, src, libs)
	return &Executor{DAG: dag, SyntaxSource: body, Libraries: libs, LibraryCatalog: catalog,
		Store: store, Factory: state.FactoryInfo{
			Name: "test-stack", Version: "v0", ContentRevision: "c0",
		},
	}
}

func resourcePlanFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/resource-plan", name)
}

func equivalentName(a, b string) bool {
	return strings.TrimPrefix(a, "ref:") == b || strings.TrimPrefix(b, "ref:") == a
}
