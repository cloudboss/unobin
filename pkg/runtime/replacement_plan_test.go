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

func (r *equivalentResource) Create(_ context.Context, _ any) (*equivalentOutput, error) {
	return &equivalentOutput{ID: "equivalent-" + r.Name, Name: r.Name, Size: r.Size}, nil
}

func (r *equivalentResource) Read(
	_ context.Context, _ any, prior Prior[equivalentResource, *equivalentOutput, any],
) (*equivalentOutput, error) {
	if prior.Outputs == nil {
		return nil, ErrNotFound
	}
	return prior.Outputs, nil
}

func (r *equivalentResource) Update(
	_ context.Context, _ any, prior Prior[equivalentResource, *equivalentOutput, any],
) (*equivalentOutput, error) {
	prior.Outputs.Name = r.Name
	prior.Outputs.Size = r.Size
	return prior.Outputs, nil
}

func (r *equivalentResource) Delete(
	_ context.Context, _ any, _ Prior[equivalentResource, *equivalentOutput, any],
) error {
	return nil
}

func equivalentResourceDefinition() ResourceDefinition[
	equivalentResource,
	*equivalentOutput,
	any,
] {
	name := InputField(func(input *equivalentResource) *string { return &input.Name })
	return ResourceDefinition[equivalentResource, *equivalentOutput, any]{
		SchemaVersion: 1,
		Equality: []InputEqualityRule[equivalentResource]{
			EqualBy(name, equivalentName),
		},
		Replace: Replacement[equivalentResource, *equivalentOutput, any]{
			Fields: []AnyInputField[equivalentResource]{name},
		},
	}
}

type versionCounters struct {
	consumerUpdates int64
	consumerRef     atomic.Value
	remoteVersion   atomic.Value
}

type versionResource struct {
	Value string

	counters *versionCounters
}

type versionOutput struct {
	Version string
}

func (r *versionResource) Create(_ context.Context, _ any) (*versionOutput, error) {
	return &versionOutput{Version: "version-" + r.Value}, nil
}

func (r *versionResource) Read(
	_ context.Context, _ any, prior Prior[versionResource, *versionOutput, any],
) (*versionOutput, error) {
	if prior.Outputs == nil {
		return nil, ErrNotFound
	}
	if r.counters != nil {
		if version, ok := r.counters.remoteVersion.Load().(string); ok {
			return &versionOutput{Version: version}, nil
		}
	}
	return prior.Outputs, nil
}

func (r *versionResource) Update(
	_ context.Context, _ any, _ Prior[versionResource, *versionOutput, any],
) (*versionOutput, error) {
	return &versionOutput{Version: "version-" + r.Value}, nil
}

func (r *versionResource) Delete(
	_ context.Context, _ any, _ Prior[versionResource, *versionOutput, any],
) error {
	return nil
}

type versionConsumer struct {
	Ref string

	counters *versionCounters
}

type versionConsumerOutput struct {
	Ref string
}

func (r *versionConsumer) Create(_ context.Context, _ any) (*versionConsumerOutput, error) {
	r.counters.consumerRef.Store(r.Ref)
	return &versionConsumerOutput{Ref: r.Ref}, nil
}

func (r *versionConsumer) Read(
	_ context.Context, _ any, prior Prior[versionConsumer, *versionConsumerOutput, any],
) (*versionConsumerOutput, error) {
	if prior.Outputs == nil {
		return nil, ErrNotFound
	}
	return prior.Outputs, nil
}

func (r *versionConsumer) Update(
	_ context.Context, _ any, prior Prior[versionConsumer, *versionConsumerOutput, any],
) (*versionConsumerOutput, error) {
	atomic.AddInt64(&r.counters.consumerUpdates, 1)
	r.counters.consumerRef.Store(r.Ref)
	prior.Outputs.Ref = r.Ref
	return prior.Outputs, nil
}

func (r *versionConsumer) Delete(
	_ context.Context, _ any, _ Prior[versionConsumer, *versionConsumerOutput, any],
) error {
	return nil
}

func TestSemanticEqualitySuppressesReplaceAndPersistsDesiredInput(t *testing.T) {
	store := newStateStore(t)
	libs := replacementPlanModules(nil)
	applyOnce(t, replacementPlanExecutor(t, replacementPlanFixture(t, "equivalent-initial"), libs, store))

	exec := replacementPlanExecutor(t, replacementPlanFixture(t, "equivalent-name"), libs, store)
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	step := findStep(t, plan, "resource.one")
	require.Equal(t, DecisionNoOp, step.Decision)
	require.Empty(t, step.ReplacementReasons)

	_, err = planAndApplyExisting(exec, plan)
	require.NoError(t, err)
	snapshot, err := store.Current()
	require.NoError(t, err)
	require.Equal(t, "alpha", snapshot.Find("resource.one").Inputs["name"])
}

func TestSemanticEqualityKeepsMutableChangeAsUpdate(t *testing.T) {
	store := newStateStore(t)
	libs := replacementPlanModules(nil)
	applyOnce(t, replacementPlanExecutor(t, replacementPlanFixture(t, "equivalent-initial"), libs, store))

	plan := runPlan(t, replacementPlanFixture(t, "equivalent-name-and-size"), libs, store)
	step := findStep(t, plan, "resource.one")
	require.Equal(t, DecisionUpdate, step.Decision)
	require.Empty(t, step.ReplacementReasons)
}

func TestApplyPremiseRejectsSemanticallyEquivalentUnreviewedInput(t *testing.T) {
	store := newStateStore(t)
	libs := replacementPlanModules(nil)
	src := replacementPlanFixture(t, "equivalent-input")
	first := replacementPlanExecutor(t, src, libs, store)
	first.Inputs = map[string]any{"n": "ref:alpha"}
	applyOnce(t, first)

	second := replacementPlanExecutor(t, src, libs, store)
	second.Inputs = map[string]any{"n": "ref:alpha"}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	step := findStep(t, plan, "resource.one")
	require.Equal(
		t,
		DecisionNoOp,
		step.Decision,
		"reasons=%v prior=%v desired=%v",
		step.ReplacementReasons,
		step.PriorInputs,
		step.Inputs,
	)

	second.Inputs = map[string]any{"n": "alpha"}
	_, err = planAndApplyExisting(second, plan)
	require.Error(t, err)
	require.Contains(t, err.Error(), "inputs changed since the plan was computed")
}

func TestChangedInputKeepsDependentOutputPending(t *testing.T) {
	counters := &versionCounters{}
	store := newStateStore(t)
	libs := replacementPlanModules(counters)
	src := replacementPlanFixture(t, "unknown-output")
	first := replacementPlanExecutor(t, src, libs, store)
	first.Inputs = map[string]any{"value": "one"}
	applyOnce(t, first)

	second := replacementPlanExecutor(t, src, libs, store)
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

func TestRemoteDriftKeepsDependentOutputPending(t *testing.T) {
	counters := &versionCounters{}
	store := newStateStore(t)
	libs := replacementPlanModules(counters)
	src := replacementPlanFixture(t, "unknown-output")
	first := replacementPlanExecutor(t, src, libs, store)
	first.Inputs = map[string]any{"value": "one"}
	applyOnce(t, first)
	counters.remoteVersion.Store("version-drifted")

	second := replacementPlanExecutor(t, src, libs, store)
	second.Inputs = map[string]any{"value": "one"}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionUpdate, findStep(t, plan, "resource.upstream").Decision)
	downstream := findStep(t, plan, "resource.downstream")
	require.Equal(t, DecisionUpdate, downstream.Decision)
	require.Contains(t, downstream.UnresolvedInputs, "ref")
	require.IsType(t, PendingValue{}, downstream.Inputs["ref"])
}

func replacementPlanModules(counters *versionCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"equivalent": MakeResource[equivalentResource, *equivalentOutput, any](
					equivalentResourceDefinition(),
				),
				"versioned": MakeResourceWith[versionResource, *versionOutput, any](
					testResourceDefinition[versionResource, *versionOutput, any](),
					func() *versionResource {
						return &versionResource{counters: counters}
					},
				),
				"consumer": MakeResourceWith[versionConsumer, *versionConsumerOutput, any](
					testResourceDefinition[versionConsumer, *versionConsumerOutput, any](),
					func() *versionConsumer {
						return &versionConsumer{counters: counters}
					},
				),
			},
		},
	}
}

func replacementPlanExecutor(
	t *testing.T,
	src string,
	libs map[string]*Library,
	store state.Backend,
) *Executor {
	t.Helper()
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	return applyPlanTestExecutor(t, src, libs, store, stack)
}

func replacementPlanFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/resource-plan", name)
}

func equivalentName(a, b string) bool {
	return a == b || strings.TrimPrefix(a, "ref:") == b || strings.TrimPrefix(b, "ref:") == a
}
