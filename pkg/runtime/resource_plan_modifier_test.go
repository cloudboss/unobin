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

var (
	_ InputEquivalencer[equivalentResource] = (*equivalentResource)(nil)
	_ ResourcePlanModifier[
		planModifierResource,
		*planModifierOutput,
		any,
	] = (*planModifierResource)(nil)
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

func (r *equivalentResource) SchemaVersion() int { return 1 }

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

func (r *equivalentResource) ReplaceFields() []string { return []string{"name"} }

func (r *equivalentResource) EquivalentInput(
	field string, prior, current equivalentResource,
) bool {
	if field != "name" {
		return false
	}
	return equivalentName(prior.Name, current.Name)
}

type modifierCounters struct {
	consumerUpdates int64
	consumerRef     atomic.Value
}

type planModifierResource struct {
	Value string
}

type planModifierOutput struct {
	Version string
}

func (r *planModifierResource) SchemaVersion() int { return 1 }

func (r *planModifierResource) Create(_ context.Context, _ any) (*planModifierOutput, error) {
	return &planModifierOutput{Version: "version-" + r.Value}, nil
}

func (r *planModifierResource) Read(
	_ context.Context, _ any, prior *planModifierOutput,
) (*planModifierOutput, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *planModifierResource) Update(
	_ context.Context, _ any, _ Prior[planModifierResource, *planModifierOutput],
) (*planModifierOutput, error) {
	return &planModifierOutput{Version: "version-" + r.Value}, nil
}

func (r *planModifierResource) Delete(_ context.Context, _ any, _ *planModifierOutput) error {
	return nil
}

func (r *planModifierResource) ReplaceFields() []string { return nil }

func (r *planModifierResource) ModifyResourcePlan(
	req ResourcePlanRequest[planModifierResource, *planModifierOutput, any],
	resp *ResourcePlanResponse,
) error {
	if req.HasPriorState && Changed(req.PriorInputs.Value, req.CurrentInputs.Value) {
		resp.MarkOutputUnknown("version")
	}
	return nil
}

type versionConsumer struct {
	Ref string

	counters *modifierCounters
}

func (r *versionConsumer) SchemaVersion() int { return 1 }

func (r *versionConsumer) Create(_ context.Context, _ any) (map[string]any, error) {
	r.counters.consumerRef.Store(r.Ref)
	return map[string]any{"ref": r.Ref}, nil
}

func (r *versionConsumer) Read(
	_ context.Context, _ any, prior map[string]any,
) (map[string]any, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *versionConsumer) Update(
	_ context.Context, _ any, prior Prior[versionConsumer, map[string]any],
) (map[string]any, error) {
	atomic.AddInt64(&r.counters.consumerUpdates, 1)
	r.counters.consumerRef.Store(r.Ref)
	prior.Outputs["ref"] = r.Ref
	return prior.Outputs, nil
}

func (r *versionConsumer) Delete(_ context.Context, _ any, _ map[string]any) error {
	return nil
}

func (r *versionConsumer) ReplaceFields() []string { return nil }

func TestInputEquivalencerSuppressesReplace(t *testing.T) {
	store := newStateStore(t)
	libs := resourcePlanModules(nil)
	applyOnce(t, resourcePlanExecutor(t, resourcePlanFixture(t, "equivalent-initial"), libs, store))

	plan := runPlan(t, resourcePlanFixture(t, "equivalent-name"), libs, store)
	step := findStep(t, plan, "resource.one")
	require.Equal(t, DecisionNoOp, step.Decision)
	require.Empty(t, step.ReplaceTriggers)
}

func TestInputEquivalencerKeepsMutableChangeAsUpdate(t *testing.T) {
	store := newStateStore(t)
	libs := resourcePlanModules(nil)
	applyOnce(t, resourcePlanExecutor(t, resourcePlanFixture(t, "equivalent-initial"), libs, store))

	plan := runPlan(t, resourcePlanFixture(t, "equivalent-name-and-size"), libs, store)
	step := findStep(t, plan, "resource.one")
	require.Equal(t, DecisionUpdate, step.Decision)
	require.Empty(t, step.ReplaceTriggers)
}

func TestInputEquivalencerAppliesToApplyPremise(t *testing.T) {
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
	require.NoError(t, err)
}

func TestResourcePlanModifierMarksOutputUnknown(t *testing.T) {
	counters := &modifierCounters{}
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

func resourcePlanModules(counters *modifierCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"equivalent": MakeResource[equivalentResource, *equivalentOutput, any](),
				"versioned":  MakeResource[planModifierResource, *planModifierOutput, any](),
				"consumer": MakeResourceWith[versionConsumer, map[string]any, any](
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
