package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func stateMoveFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/state-moves-plan", name)
}

func TestPlanAppliesRootStateMoveBeforePlanning(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, stateMovePlanEntry("resource.old"))

	plan := runPlan(t, stateMoveRootSource(t), resourceModules(&resourceCounters{}), store)

	require.Equal(t, []PlannedEntryMove{
		{From: "resource.old", To: "resource.new"},
	}, plan.StateMoves)
	step := stepFor(plan, "resource.new")
	require.NotNil(t, step)
	assert.Equal(t, DecisionNoOp, step.Decision)
	assert.Nil(t, stepFor(plan, "resource.old"))
}

func TestPlanStateMoveAlreadyAppliedIsNoOp(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, stateMovePlanEntry("resource.new"))

	plan := runPlan(t, stateMoveRootSource(t), resourceModules(&resourceCounters{}), store)

	assert.Empty(t, plan.StateMoves)
	assert.Equal(t, DecisionNoOp, stepFor(plan, "resource.new").Decision)
}

func TestPlanStateMoveAbsentSourceCreatesNormally(t *testing.T) {
	store := newStateStore(t)
	plan := runPlan(t, stateMoveRootSource(t), resourceModules(&resourceCounters{}), store)

	assert.Empty(t, plan.StateMoves)
	assert.Equal(t, DecisionCreate, stepFor(plan, "resource.new").Decision)
}

func TestPlanStateMoveSourceAndDestinationConflict(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMovePlanEntry("resource.old"),
		stateMovePlanEntry("resource.new"),
	)
	exec := planTestExecutor(
		t, stateMoveRootSource(t), resourceModules(&resourceCounters{}), store, stack,
	)

	_, err := exec.Plan(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "destination already exists at resource.new")
}

func TestPlanCollapsesRootStateMoveChain(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, stateMovePlanEntry("resource.old"))

	plan := runPlan(
		t,
		stateMoveFixture(t, "root-chain"),
		resourceModules(&resourceCounters{}),
		store,
	)

	require.Equal(t, []PlannedEntryMove{
		{From: "resource.old", To: "resource.new"},
	}, plan.StateMoves)
	assert.Equal(t, DecisionNoOp, stepFor(plan, "resource.new").Decision)
}

func TestPlanAppliesBoundaryAndCompositeStateMovesTogether(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMoveBoundaryEntry("w.box", "resource.old-app"),
		stateMovePlanEntry("resource.old-app/resource.old"),
	)

	plan := runPlan(t, stateMoveFixture(t, "boundary-and-composite"), stateMoveCompositeLibs(t), store)

	require.Equal(t, []PlannedEntryMove{
		{From: "resource.old-app", To: "resource.app"},
		{
			From: "resource.old-app/resource.old",
			To:   "resource.app/resource.new",
		},
	}, plan.StateMoves)
	assert.Equal(t, DecisionNoOp, stepFor(plan, "resource.app/resource.new").Decision)
}

func TestPlanAppliesCompositeBodyStateMove(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMoveBoundaryEntry("w.box", "resource.app"),
		stateMovePlanEntry("resource.app/resource.old"),
	)

	plan := runPlan(t, stateMoveFixture(t, "composite-body"), stateMoveCompositeLibs(t), store)

	require.Equal(t, []PlannedEntryMove{
		{
			From: "resource.app/resource.old",
			To:   "resource.app/resource.new",
		},
	}, plan.StateMoves)
	assert.Equal(t, DecisionNoOp, stepFor(plan, "resource.app/resource.new").Decision)
	assert.Nil(t, stepFor(plan, "resource.app/resource.old"))
}

func TestPlanAppliesCompositeBodyStateMoveUnderEveryKey(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMoveBoundaryEntry("w.box", "resource.apps['blue']"),
		stateMovePlanEntry("resource.apps['blue']/resource.old"),
		stateMoveBoundaryEntry("w.box", "resource.apps['red']"),
		stateMovePlanEntry("resource.apps['red']/resource.old"),
	)
	exec := planTestExecutor(
		t,
		stateMoveFixture(t, "composite-body-every-key"),
		stateMoveCompositeLibs(t),
		store,
		stack,
	)
	exec.Inputs = map[string]any{"configs": map[string]any{"blue": true, "red": true}}

	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.ElementsMatch(t, []PlannedEntryMove{
		{
			From: "resource.apps['blue']/resource.old",
			To:   "resource.apps['blue']/resource.new",
		},
		{
			From: "resource.apps['red']/resource.old",
			To:   "resource.apps['red']/resource.new",
		},
	}, plan.StateMoves)
	assert.Equal(t, DecisionNoOp, stepFor(plan, "resource.apps['blue']/resource.new").Decision)
	assert.Equal(t, DecisionNoOp, stepFor(plan, "resource.apps['red']/resource.new").Decision)
}

func TestPlanAppliesNestedCompositeBodyStateMoveUnderKey(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMoveBoundaryEntry("outer.web", "resource.apps['blue']"),
		stateMoveBoundaryEntry("inner.box", "resource.apps['blue']/resource.child"),
		stateMovePlanEntry("resource.apps['blue']/resource.child/resource.old"),
	)
	exec := planTestExecutor(
		t,
		stateMoveFixture(t, "nested-under-key"),
		stateMoveNestedCompositeLibs(t),
		store,
		stack,
	)
	exec.Inputs = map[string]any{"configs": map[string]any{"blue": true}}

	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, []PlannedEntryMove{
		{
			From: "resource.apps['blue']/resource.child/resource.old",
			To:   "resource.apps['blue']/resource.child/resource.new",
		},
	}, plan.StateMoves)
	assert.Equal(t,
		DecisionNoOp,
		stepFor(plan, "resource.apps['blue']/resource.child/resource.new").Decision,
	)
}

func TestPlanAppliesCompositeBodyPrefixStateMove(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMoveBoundaryEntry("w.outer", "resource.app"),
		stateMoveBoundaryEntry("inner.box", "resource.app/resource.old-child"),
		stateMovePlanEntry("resource.app/resource.old-child/resource.new"),
	)

	plan := runPlan(t, stateMoveFixture(t, "composite-prefix"), stateMoveCompositePrefixLibs(t), store)

	require.Equal(t, []PlannedEntryMove{
		{
			From: "resource.app/resource.old-child",
			To:   "resource.app/resource.child",
		},
		{
			From: "resource.app/resource.old-child/resource.new",
			To:   "resource.app/resource.child/resource.new",
		},
	}, plan.StateMoves)
	assert.Equal(t,
		DecisionNoOp,
		stepFor(plan, "resource.app/resource.child/resource.new").Decision,
	)
}

func TestPlanAppliesNestedCompositeBodyStateMove(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMoveBoundaryEntry("outer.web", "resource.app"),
		stateMoveBoundaryEntry("inner.box", "resource.app/resource.child"),
		stateMovePlanEntry("resource.app/resource.child/resource.old"),
	)

	plan := runPlan(t, stateMoveFixture(t, "nested-composite"), stateMoveNestedCompositeLibs(t), store)

	require.Equal(t, []PlannedEntryMove{
		{
			From: "resource.app/resource.child/resource.old",
			To:   "resource.app/resource.child/resource.new",
		},
	}, plan.StateMoves)
	assert.Equal(t,
		DecisionNoOp,
		stepFor(plan, "resource.app/resource.child/resource.new").Decision,
	)
}

func TestPlanAppliesRootBoundaryMoveWithNestedCompositeStateMove(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMoveBoundaryEntry("outer.web", "resource.old-app"),
		stateMoveBoundaryEntry("inner.box", "resource.old-app/resource.child"),
		stateMovePlanEntry("resource.old-app/resource.child/resource.old"),
	)

	plan := runPlan(
		t,
		stateMoveFixture(t, "root-boundary-nested"),
		stateMoveNestedCompositeLibs(t),
		store,
	)

	require.Equal(t, []PlannedEntryMove{
		{From: "resource.old-app", To: "resource.app"},
		{
			From: "resource.old-app/resource.child",
			To:   "resource.app/resource.child",
		},
		{
			From: "resource.old-app/resource.child/resource.old",
			To:   "resource.app/resource.child/resource.new",
		},
	}, plan.StateMoves)
	assert.Equal(t,
		DecisionNoOp,
		stepFor(plan, "resource.app/resource.child/resource.new").Decision,
	)
}

func TestPlanAppliesKeyedRootBoundaryMoveWithNestedCompositeStateMove(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMoveBoundaryEntry("outer.web", "resource.apps['blue']"),
		stateMoveBoundaryEntry("inner.box", "resource.apps['blue']/resource.child"),
		stateMovePlanEntry("resource.apps['blue']/resource.child/resource.old"),
	)
	exec := planTestExecutor(
		t,
		stateMoveFixture(t, "keyed-root-boundary"),
		stateMoveNestedCompositeLibs(t),
		store,
		stack,
	)
	exec.Inputs = map[string]any{"configs": map[string]any{"green": true}}

	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, []PlannedEntryMove{
		{
			From: "resource.apps['blue']",
			To:   "resource.apps['green']",
		},
		{
			From: "resource.apps['blue']/resource.child",
			To:   "resource.apps['green']/resource.child",
		},
		{
			From: "resource.apps['blue']/resource.child/resource.old",
			To:   "resource.apps['green']/resource.child/resource.new",
		},
	}, plan.StateMoves)
	assert.Equal(t,
		DecisionNoOp,
		stepFor(plan, "resource.apps['green']/resource.child/resource.new").Decision,
	)
}

func TestApplyPlanExecutesRecordedStateMove(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, stateMovePlanEntry("resource.old"))
	exec := planTestExecutor(
		t, stateMoveRootSource(t), resourceModules(&resourceCounters{}), store, stack,
	)

	_, err := planAndApply(exec)
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	assert.Nil(t, snap.Find("resource.old"))
	ent := snap.Find("resource.new")
	require.NotNil(t, ent)
	assert.Equal(t, "core", ent.Binding.Alias)
	assert.Equal(t, "thing", ent.Binding.Export)
}

func TestApplyPersistsStateMoveBeforeLaterUpdateError(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMovePlanEntry("resource.old"),
		stateMoveUpdateFailureEntry(),
	)
	exec := planTestExecutor(
		t,
		stateMoveFixture(t, "persist-before-error"),
		stateMoveUpdateFailureLibs(),
		store,
		stack,
	)

	_, err := planAndApply(exec)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "intentional update failure")
	snap, err := store.Current()
	require.NoError(t, err)
	assert.Nil(t, snap.Find("resource.old"))
	assert.NotNil(t, snap.Find("resource.new"))
}

func TestApplyPlanRejectsMissingRecordedStateMoveSource(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := planTestExecutor(
		t, stateMoveRootSource(t), resourceModules(&resourceCounters{}), store, stack,
	)
	pf := stateMovePlanFile(exec, "")

	_, err := exec.ApplyPlan(context.Background(), pf)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no entry at resource.old")
}

func TestApplyPlanRejectsRecordedStateMoveDestinationConflict(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMovePlanEntry("resource.old"),
		stateMovePlanEntry("resource.new"),
	)
	rev, err := store.CurrentRev()
	require.NoError(t, err)
	exec := planTestExecutor(
		t, stateMoveRootSource(t), resourceModules(&resourceCounters{}), store, stack,
	)
	pf := stateMovePlanFile(exec, rev)

	_, err = exec.ApplyPlan(context.Background(), pf)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "destination already exists at resource.new")
}

func TestDestroyStateMoveRequiresPriorImportAlias(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMovePlanEntryWithBinding("old", "thing", "resource.previous"),
	)
	libs := stateMoveNextOnlyLibs()
	exec := planTestExecutor(t, stateMoveFixture(t, "destroy-prior-alias"), libs, store, stack)
	exec.Destroy = true

	_, err := exec.Plan(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), `resource.current: read: library "old" is not imported`)
}

func stateMovePlanFile(exec *Executor, rev string) *PlanFile {
	return &PlanFile{
		FormatVersion: PlanFormatVersion,
		Factory: FactoryRef{
			Name:            exec.Factory.Name,
			Version:         exec.Factory.Version,
			ContentRevision: exec.Factory.ContentRevision,
		},
		Stack:    exec.Store.Stack(),
		StateRev: rev,
		StateMoves: []PlannedEntryMove{
			{From: "resource.old", To: "resource.new"},
		},
	}
}

func stateMoveCompositeLibs(t *testing.T) map[string]*Library {
	t.Helper()
	body := parseSyntaxCompositeFixture(t, stateMoveFixture(t, "composite-lib-box")).body
	libs := resourceModules(&resourceCounters{})
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": {Name: "box", Kind: NodeResource, SyntaxBody: &body, Libraries: libs},
		},
	}
	return libs
}

func stateMoveCompositePrefixLibs(t *testing.T) map[string]*Library {
	t.Helper()
	innerBody := parseSyntaxCompositeFixture(t, stateMoveFixture(t, "prefix-inner-box")).body
	outerBody := parseSyntaxCompositeFixture(t, stateMoveFixture(t, "prefix-outer")).body
	core := resourceModules(&resourceCounters{})["core"]
	inner := &Library{
		Name: "inner",
		ResourceComposites: map[string]*CompositeType{
			"box": {
				Name:       "box",
				Kind:       NodeResource,
				SyntaxBody: &innerBody,
				Libraries:  map[string]*Library{"core": core},
			},
		},
	}
	return map[string]*Library{
		"core": core,
		"w": {
			Name: "w",
			ResourceComposites: map[string]*CompositeType{
				"outer": {
					Name:       "outer",
					Kind:       NodeResource,
					SyntaxBody: &outerBody,
					Libraries:  map[string]*Library{"inner": inner},
				},
			},
		},
	}
}

func stateMoveNestedCompositeLibs(t *testing.T) map[string]*Library {
	t.Helper()
	innerBody := parseSyntaxCompositeFixture(t, stateMoveFixture(t, "nested-inner-box")).body
	outerBody := parseSyntaxCompositeFixture(t, stateMoveFixture(t, "nested-outer-web")).body
	core := resourceModules(&resourceCounters{})["core"]
	inner := &Library{
		Name: "inner",
		ResourceComposites: map[string]*CompositeType{
			"box": {
				Name:       "box",
				Kind:       NodeResource,
				SyntaxBody: &innerBody,
				Libraries:  map[string]*Library{"core": core},
			},
		},
	}
	return map[string]*Library{
		"core": core,
		"outer": {
			Name: "outer",
			ResourceComposites: map[string]*CompositeType{
				"web": {
					Name:       "web",
					Kind:       NodeResource,
					SyntaxBody: &outerBody,
					Libraries:  map[string]*Library{"inner": inner},
				},
			},
		},
	}
}

func stateMoveBoundaryEntry(binding, address string) *state.Entry {
	alias, export, ok := strings.Cut(binding, ".")
	if !ok {
		panic("invalid test binding")
	}
	return &state.Entry{
		Address: address,
		Type:    state.EntryLibraryCall,
		Kind:    "resource",
		Binding: &state.Binding{Alias: alias, Export: export},
	}
}

type stateMoveUpdateFailureResource struct {
	Name string
	Size int64
}

func (r *stateMoveUpdateFailureResource) Create(_ context.Context, _ any) (any, error) {
	return map[string]any{"id": r.Name, "size": r.Size}, nil
}

func (r *stateMoveUpdateFailureResource) Read(_ context.Context, _ any, prior any) (any, error) {
	return prior, nil
}

func (r *stateMoveUpdateFailureResource) Update(
	_ context.Context,
	_ any,
	_ Prior[stateMoveUpdateFailureResource, any],
) (any, error) {
	return nil, errors.New("intentional update failure")
}

func (r *stateMoveUpdateFailureResource) Delete(_ context.Context, _ any, _ any) error {
	return nil
}

func (r *stateMoveUpdateFailureResource) ReplaceFields() []string { return nil }

func (r *stateMoveUpdateFailureResource) SchemaVersion() int { return 1 }

func stateMoveUpdateFailureLibs() map[string]*Library {
	libs := resourceModules(&resourceCounters{})
	libs["bad"] = &Library{
		Name: "bad",
		Resources: map[string]ResourceRegistration{
			"thing": MakeResource[stateMoveUpdateFailureResource, any, any](),
		},
	}
	return libs
}

func stateMoveUpdateFailureEntry() *state.Entry {
	return &state.Entry{
		Address:       "resource.fail",
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Binding:       &state.Binding{Alias: "bad", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "beta", "size": int64(1)},
		Outputs:       map[string]any{"id": "beta", "size": int64(1)},
	}
}

func stateMoveRootSource(t testing.TB) string {
	t.Helper()
	return stateMoveFixture(t, "root")
}

func stateMovePlanEntry(address string) *state.Entry {
	return stateMovePlanEntryWithBinding("core", "thing", address)
}

func stateMovePlanEntryWithBinding(alias, export, address string) *state.Entry {
	return &state.Entry{
		Address:       address,
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Binding:       &state.Binding{Alias: alias, Export: export},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha", "size": int64(1)},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha", "size": int64(1)},
	}
}

func stateMoveNextOnlyLibs() map[string]*Library {
	core := resourceModules(&resourceCounters{})["core"]
	return map[string]*Library{
		"next": {
			Name:      "next",
			Resources: core.Resources,
		},
	}
}
