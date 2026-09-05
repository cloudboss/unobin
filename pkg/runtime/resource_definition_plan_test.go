package runtime

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
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

func TestInputEqualitySuppressesReplace(t *testing.T) {
	store := newStateStore(t)
	libs := resourcePlanModules(nil)
	applyOnce(t, resourcePlanExecutor(t, resourcePlanFixture(t, "equivalent-initial"), libs, store))

	plan := runPlan(t, resourcePlanFixture(t, "equivalent-name"), libs, store)
	step := findStep(t, plan, "resource.one")
	require.Equal(t, DecisionNoOp, step.Decision)
	require.Empty(t, step.ReplaceTriggers)
}

func TestInputEqualityNoOpPersistsDesiredInputs(t *testing.T) {
	store := newStateStore(t)
	libs := resourcePlanModules(nil)
	initial := resourcePlanExecutor(
		t,
		resourcePlanFixture(t, "equivalent-initial"),
		libs,
		store,
	)
	applyOnce(t, initial)

	exec := resourcePlanExecutor(t, resourcePlanFixture(t, "equivalent-name"), libs, store)
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, findStep(t, plan, "resource.one").Decision)
	_, err = planAndApplyExisting(exec, plan)
	require.NoError(t, err)
	snapshot, err := store.Current()
	require.NoError(t, err)
	require.Equal(t, "alpha", snapshot.Find("resource.one").Inputs["name"])
}

func TestInputEqualityKeepsMutableChangeAsUpdate(t *testing.T) {
	store := newStateStore(t)
	libs := resourcePlanModules(nil)
	applyOnce(t, resourcePlanExecutor(t, resourcePlanFixture(t, "equivalent-initial"), libs, store))

	plan := runPlan(t, resourcePlanFixture(t, "equivalent-name-and-size"), libs, store)
	step := findStep(t, plan, "resource.one")
	require.Equal(t, DecisionUpdate, step.Decision)
	require.Empty(t, step.ReplaceTriggers)
}

func TestInputEqualityDoesNotApplyToApplyPremise(t *testing.T) {
	store := newStateStore(t)
	libs := resourcePlanModules(nil)
	src := resourcePlanFixture(t, "equivalent-input")
	first := resourcePlanExecutor(t, src, libs, store)
	first.Inputs = map[string]any{"n": "ref:alpha"}
	applyOnce(t, first)

	second := resourcePlanExecutor(t, src, libs, store)
	second.Inputs = map[string]any{"n": "ref:alpha"}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, findStep(t, plan, "resource.one").Decision)

	second.Inputs = map[string]any{"n": "alpha"}
	_, err = planAndApplyExisting(second, plan)
	require.ErrorContains(t, err, "inputs changed since the plan was computed")
}

func TestResourceUpdateMarksOutputPending(t *testing.T) {
	counters := &resourceDependencyCounters{}
	store := newStateStore(t)
	libs := resourcePlanModules(counters)
	src := resourcePlanFixture(t, "unknown-output")
	first := resourcePlanExecutor(t, src, libs, store)
	first.Inputs = map[string]any{"value": "one"}
	applyOnce(t, first)

	second := resourcePlanExecutor(t, src, libs, store)
	second.Inputs = map[string]any{"value": "two"}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionUpdate, findStep(t, plan, "resource.upstream").Decision)
	downstream := findStep(t, plan, "resource.downstream")
	require.Equal(t, DecisionUpdate, downstream.Decision)
	require.Contains(t, downstream.UnresolvedInputs, "ref")
	require.IsType(t, PendingValue{}, downstream.Inputs["ref"])

	_, err = planAndApplyExisting(second, plan)
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
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	return applyPlanTestExecutor(t, src, libs, store, stack)
}

func resourcePlanFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/resource-plan", name)
}

func equivalentName(a, b string) bool {
	return strings.TrimPrefix(a, "ref:") == b || strings.TrimPrefix(b, "ref:") == a
}
