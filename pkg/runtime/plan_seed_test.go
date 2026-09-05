package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// subnetLike has a tag input and a stable id output; updating it
// never changes the id, like a cloud subnet whose tags are synced.
type subnetLike struct {
	Tag string
}

type subnetLikeOutput struct {
	Tag string
	ID  string
}

func subnetLikeDefinition() ResourceDefinition[subnetLike, *subnetLikeOutput, any] {
	return ResourceDefinition[subnetLike, *subnetLikeOutput, any]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[subnetLike, *subnetLikeOutput]{
			Version: 1,
			Scope:   IdentityConfiguration,
		},
	}
}

func (r *subnetLike) Create(_ context.Context, _ any) (*subnetLikeOutput, error) {
	return &subnetLikeOutput{Tag: r.Tag, ID: "subnet-1"}, nil
}

func (r *subnetLike) Read(
	_ context.Context,
	_ any,
	prior *subnetLikeOutput,
) (*subnetLikeOutput, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *subnetLike) Update(
	_ context.Context,
	_ any,
	_ Prior[subnetLike, *subnetLikeOutput],
) (*subnetLikeOutput, error) {
	return &subnetLikeOutput{Tag: r.Tag, ID: "subnet-1"}, nil
}

func (r *subnetLike) Delete(_ context.Context, _ any, _ *subnetLikeOutput) error { return nil }

// instanceLike replace-marks its ref field, like an instance pinned
// to a subnet id.
type instanceLike struct {
	Ref string
}

type instanceLikeOutput struct {
	Ref string
	ID  string
}

func instanceLikeDefinition() ResourceDefinition[instanceLike, *instanceLikeOutput, any] {
	return ResourceDefinition[instanceLike, *instanceLikeOutput, any]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[instanceLike, *instanceLikeOutput]{
			Version: 1,
			Scope:   IdentityConfiguration,
		},
		Replacement: ReplacementRules[instanceLike, *instanceLikeOutput]{
			Inputs: []ReplacementRule[instanceLike]{
				ReplaceWhenChanged(InputField(func(v *instanceLike) *string { return &v.Ref })),
			},
		},
	}
}

func (r *instanceLike) Create(_ context.Context, _ any) (*instanceLikeOutput, error) {
	return &instanceLikeOutput{Ref: r.Ref, ID: "inst-1"}, nil
}

func (r *instanceLike) Read(
	_ context.Context,
	_ any,
	prior *instanceLikeOutput,
) (*instanceLikeOutput, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *instanceLike) Update(
	_ context.Context,
	_ any,
	_ Prior[instanceLike, *instanceLikeOutput],
) (*instanceLikeOutput, error) {
	return &instanceLikeOutput{Ref: r.Ref, ID: "inst-1"}, nil
}

func (r *instanceLike) Delete(_ context.Context, _ any, _ *instanceLikeOutput) error { return nil }

// pinnedResource replace-marks its tag and mints a fresh id on every
// create, so replacing it hands downstream readers a new value. Its
// update never runs: any input change forces a replace.
type pinnedResource struct {
	Tag string

	gen *int64
}

type pinnedResourceOutput struct {
	Tag string
	ID  string
}

func pinnedResourceDefinition() ResourceDefinition[pinnedResource, *pinnedResourceOutput, any] {
	return ResourceDefinition[pinnedResource, *pinnedResourceOutput, any]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[pinnedResource, *pinnedResourceOutput]{
			Version: 1,
			Scope:   IdentityConfiguration,
		},
		Replacement: ReplacementRules[pinnedResource, *pinnedResourceOutput]{
			Inputs: []ReplacementRule[pinnedResource]{
				ReplaceWhenChanged(InputField(func(v *pinnedResource) *string { return &v.Tag })),
			},
		},
	}
}

func (r *pinnedResource) Create(_ context.Context, _ any) (*pinnedResourceOutput, error) {
	*r.gen++
	return &pinnedResourceOutput{Tag: r.Tag, ID: fmt.Sprintf("gen-%d", *r.gen)}, nil
}

func (r *pinnedResource) Read(
	_ context.Context,
	_ any,
	prior *pinnedResourceOutput,
) (*pinnedResourceOutput, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *pinnedResource) Update(
	_ context.Context, _ any, _ Prior[pinnedResource, *pinnedResourceOutput],
) (*pinnedResourceOutput, error) {
	return nil, errors.New("pinnedResource update should never run")
}

func (r *pinnedResource) Delete(_ context.Context, _ any, _ *pinnedResourceOutput) error {
	return nil
}

// Every Update output becomes pending, including a provider ID that the
// implementation happens to preserve. A dependent immutable input must
// therefore plan replacement until its value becomes known at apply.
func TestUpdateMakesDownstreamReplacementInputPending(t *testing.T) {
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet": MakeResource[subnetLike, *subnetLikeOutput, any](
					subnetLikeDefinition(),
				),
				"instance": MakeResource[instanceLike, *instanceLikeOutput, any](
					instanceLikeDefinition(),
				),
			},
		},
	}
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "cascade-update"), libs)

	applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "1"},
	})

	second := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "2"},
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionUpdate, findStep(t, plan, "resource.a").Decision)
	inst := findStep(t, plan, "resource.it")
	require.Equal(t, DecisionReplace, inst.Decision)
	require.Equal(t, []string{"resource.a.id"}, inst.UnresolvedInputs["ref"])

	_, err = planAndApplyExisting(second, plan)
	require.NoError(t, err)

	third := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "2"},
	}
	plan, err = third.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, findStep(t, plan, "resource.a").Decision)
	require.Equal(t, DecisionNoOp, findStep(t, plan, "resource.it").Decision)
}

// A replace regenerates the object, so its prior outputs are not
// seeded: a downstream reader of a replace-marked field stays pending
// and plans its own replace, then applies with the fresh value.
func TestReplaceSuppressesPriorOutputs(t *testing.T) {
	var gen int64
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"pinned": MakeResourceWith[pinnedResource, *pinnedResourceOutput, any](
					pinnedResourceDefinition(),

					func() *pinnedResource { return &pinnedResource{gen: &gen} },
				),
				"instance": MakeResource[instanceLike, *instanceLikeOutput, any](
					instanceLikeDefinition(),
				),
			},
		},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "replace-suppresses")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t, src, libs)

	applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "1"},
	})

	second := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "2"},
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionReplace, findStep(t, plan, "resource.a").Decision)
	inst := findStep(t, plan, "resource.it")
	require.Equal(t, DecisionReplace, inst.Decision)
	require.Contains(t, inst.UnresolvedInputs, "ref")

	_, err = planAndApplyExisting(second, plan)
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.it")
	require.NotNil(t, ent)
	require.Equal(t, map[string]any{"ref": "gen-2"}, ent.Inputs)
}

// A composite's outputs are evaluated during the walk from what its
// internals seeded, so a reader of a composite output diffs a real
// value: an unchanged stack plans no-op instead of replacing the
// reader on every plan.
func TestCompositeOutputsSeedAtPlan(t *testing.T) {
	composite := syntaxResourceComposite(t, "net",
		ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "composite-net"))
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet": MakeResource[subnetLike, *subnetLikeOutput, any](
					subnetLikeDefinition(),
				),
				"instance": MakeResource[instanceLike, *instanceLikeOutput, any](
					instanceLikeDefinition(),
				),
			},
		},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*CompositeType{
				"net": composite,
			},
		},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "composite-output-seed")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t, src, libs)
	applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	})

	second := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	inst := findStep(t, plan, "resource.it")
	require.Equal(t, DecisionNoOp, inst.Decision)
	require.Empty(t, inst.UnresolvedInputs)
	require.Equal(t, "subnet-1", inst.Inputs["ref"])
}

// A composite output reading an internal about to be replaced stays
// pending: the suppression of the internal's prior outputs reaches
// through the boundary, so the reader plans its own replace and
// applies with the fresh value.
func TestCompositeOutputPendingWhenInternalReplaces(t *testing.T) {
	var gen int64
	composite := syntaxResourceComposite(t, "net",
		ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "composite-pinned"))
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"pinned": MakeResourceWith[pinnedResource, *pinnedResourceOutput, any](
					pinnedResourceDefinition(),

					func() *pinnedResource { return &pinnedResource{gen: &gen} },
				),
				"instance": MakeResource[instanceLike, *instanceLikeOutput, any](
					instanceLikeDefinition(),
				),
			},
		},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*CompositeType{
				"net": composite,
			},
		},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "composite-pending")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t, src, libs)
	applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "1"},
	})

	second := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
		Inputs: map[string]any{"t": "2"},
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionReplace,
		findStep(t, plan, "resource.x/resource.p").Decision)
	inst := findStep(t, plan, "resource.it")
	require.Equal(t, DecisionReplace, inst.Decision)
	require.Contains(t, inst.UnresolvedInputs, "ref")

	_, err = planAndApplyExisting(second, plan)
	require.NoError(t, err)
	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.it")
	require.NotNil(t, ent)
	require.Equal(t, map[string]any{"ref": "gen-2"}, ent.Inputs)
}

// A slash inside an instance key is not a nesting delimiter.
func TestForEachKeyWithSlashSeedsPriorOutputs(t *testing.T) {
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet": MakeResource[subnetLike, *subnetLikeOutput, any](
					subnetLikeDefinition(),
				),
				"instance": MakeResource[instanceLike, *instanceLikeOutput, any](
					instanceLikeDefinition(),
				),
			},
		},
	}
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "foreach-key-slash"), libs)

	applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	})

	second := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, findStep(t, plan, `resource.x['a/b']`).Decision)
	inst := findStep(t, plan, "resource.it")
	require.Equal(t, DecisionNoOp, inst.Decision)
	require.Empty(t, inst.UnresolvedInputs)
	require.Equal(t, "subnet-1", inst.Inputs["ref"])
}

// Each instance of a @for-each composite seeds its own outputs at its
// keyed address, so a reader of one instance's output diffs a real
// value on the second plan.
func TestForEachCompositeOutputsSeedAtPlan(t *testing.T) {
	composite := syntaxResourceComposite(t, "net",
		ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "composite-net"))
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"subnet": MakeResource[subnetLike, *subnetLikeOutput, any](
					subnetLikeDefinition(),
				),
				"instance": MakeResource[instanceLike, *instanceLikeOutput, any](
					instanceLikeDefinition(),
				),
			},
		},
		"w": {
			Name: "w",
			ResourceComposites: map[string]*CompositeType{
				"net": composite,
			},
		},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "foreach-composite-output")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	g, syntaxSource := syntaxDAGAndBody(t, src, libs)
	applyOnce(t, &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	})

	second := &Executor{
		DAG: g, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: stack,
	}
	plan, err := second.Plan(context.Background())
	require.NoError(t, err)
	inst := findStep(t, plan, "resource.it")
	require.Equal(t, DecisionNoOp, inst.Decision)
	require.Empty(t, inst.UnresolvedInputs)
	require.Equal(t, "subnet-1", inst.Inputs["ref"])
}
