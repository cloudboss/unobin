package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanAppliesRootStateMoveBeforePlanning(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, stateMovePlanEntry("resource.old"))

	plan := runPlan(t, stateMoveRootSource(), resourceModules(&resourceCounters{}), store)

	require.Equal(t, []PlannedEntryMove{
		{From: "core.thing@resource.old", To: "core.thing@resource.new"},
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

	plan := runPlan(t, stateMoveRootSource(), resourceModules(&resourceCounters{}), store)

	assert.Empty(t, plan.StateMoves)
	assert.Equal(t, DecisionNoOp, stepFor(plan, "resource.new").Decision)
}

func TestPlanStateMoveAbsentSourceCreatesNormally(t *testing.T) {
	store := newStateStore(t)
	plan := runPlan(t, stateMoveRootSource(), resourceModules(&resourceCounters{}), store)

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
		t, stateMoveRootSource(), resourceModules(&resourceCounters{}), store, stack,
	)

	_, err := exec.Plan(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "destination already exists at core.thing@resource.new")
}

func TestPlanCollapsesRootStateMoveChain(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, stateMovePlanEntry("resource.old"))

	plan := runPlan(t, `
state-moves: [
  { from: 'core.thing@resource.old', to: 'core.thing@resource.middle' },
  { from: 'core.thing@resource.middle', to: 'core.thing@resource.new' },
]
resources: {
  new: core.thing { name: 'alpha', size: 1 }
}
`, resourceModules(&resourceCounters{}), store)

	require.Equal(t, []PlannedEntryMove{
		{From: "core.thing@resource.old", To: "core.thing@resource.new"},
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

	plan := runPlan(t, `
state-moves: [
  { from: 'w.box@resource.old-app', to: 'w.box@resource.app' },
]
resources: {
  app: w.box {}
}
`, stateMoveCompositeLibs(t), store)

	require.Equal(t, []PlannedEntryMove{
		{From: "w.box@resource.old-app", To: "w.box@resource.app"},
		{
			From: "core.thing@resource.old-app/resource.old",
			To:   "core.thing@resource.app/resource.new",
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

	plan := runPlan(t, `
resources: {
  app: w.box {}
}
`, stateMoveCompositeLibs(t), store)

	require.Equal(t, []PlannedEntryMove{
		{
			From: "core.thing@resource.app/resource.old",
			To:   "core.thing@resource.app/resource.new",
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
	exec := planTestExecutor(t, `
inputs: { configs: { type: map(boolean) } }
resources: {
  apps: w.box { @for-each: var.configs }
}
`, stateMoveCompositeLibs(t), store, stack)
	exec.Inputs = map[string]any{"configs": map[string]any{"blue": true, "red": true}}

	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.ElementsMatch(t, []PlannedEntryMove{
		{
			From: "core.thing@resource.apps['blue']/resource.old",
			To:   "core.thing@resource.apps['blue']/resource.new",
		},
		{
			From: "core.thing@resource.apps['red']/resource.old",
			To:   "core.thing@resource.apps['red']/resource.new",
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
	exec := planTestExecutor(t, `
inputs: { configs: { type: map(boolean) } }
resources: {
  apps: outer.web { @for-each: var.configs }
}
`, stateMoveNestedCompositeLibs(t), store, stack)
	exec.Inputs = map[string]any{"configs": map[string]any{"blue": true}}

	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, []PlannedEntryMove{
		{
			From: "core.thing@resource.apps['blue']/resource.child/resource.old",
			To:   "core.thing@resource.apps['blue']/resource.child/resource.new",
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

	plan := runPlan(t, `
resources: {
  app: w.outer {}
}
`, stateMoveCompositePrefixLibs(t), store)

	require.Equal(t, []PlannedEntryMove{
		{
			From: "inner.box@resource.app/resource.old-child",
			To:   "inner.box@resource.app/resource.child",
		},
		{
			From: "core.thing@resource.app/resource.old-child/resource.new",
			To:   "core.thing@resource.app/resource.child/resource.new",
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

	plan := runPlan(t, `
resources: {
  app: outer.web {}
}
`, stateMoveNestedCompositeLibs(t), store)

	require.Equal(t, []PlannedEntryMove{
		{
			From: "core.thing@resource.app/resource.child/resource.old",
			To:   "core.thing@resource.app/resource.child/resource.new",
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

	plan := runPlan(t, `
state-moves: [
  { from: 'outer.web@resource.old-app', to: 'outer.web@resource.app' },
]
resources: {
  app: outer.web {}
}
`, stateMoveNestedCompositeLibs(t), store)

	require.Equal(t, []PlannedEntryMove{
		{From: "outer.web@resource.old-app", To: "outer.web@resource.app"},
		{
			From: "inner.box@resource.old-app/resource.child",
			To:   "inner.box@resource.app/resource.child",
		},
		{
			From: "core.thing@resource.old-app/resource.child/resource.old",
			To:   "core.thing@resource.app/resource.child/resource.new",
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
	exec := planTestExecutor(t, `
inputs: { configs: { type: map(boolean) } }
state-moves: [
  {
    from: '''outer.web@resource.apps['blue']''',
    to:   '''outer.web@resource.apps['green']''',
  },
]
resources: {
  apps: outer.web { @for-each: var.configs }
}
`, stateMoveNestedCompositeLibs(t), store, stack)
	exec.Inputs = map[string]any{"configs": map[string]any{"green": true}}

	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, []PlannedEntryMove{
		{
			From: "outer.web@resource.apps['blue']",
			To:   "outer.web@resource.apps['green']",
		},
		{
			From: "inner.box@resource.apps['blue']/resource.child",
			To:   "inner.box@resource.apps['green']/resource.child",
		},
		{
			From: "core.thing@resource.apps['blue']/resource.child/resource.old",
			To:   "core.thing@resource.apps['green']/resource.child/resource.new",
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
		t, stateMoveRootSource(), resourceModules(&resourceCounters{}), store, stack,
	)

	_, err := planAndApply(exec)
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	assert.Nil(t, snap.Find("resource.old"))
	ent := snap.Find("resource.new")
	require.NotNil(t, ent)
	assert.Equal(t, "core", ent.Selector.Alias)
	assert.Equal(t, "thing", ent.Selector.Export)
}

func TestApplyPersistsStateMoveBeforeLaterUpdateError(t *testing.T) {
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		stateMovePlanEntry("resource.old"),
		stateMoveUpdateFailureEntry(),
	)
	exec := planTestExecutor(t, `
state-moves: [
  { from: 'core.thing@resource.old', to: 'core.thing@resource.new' },
]
resources: {
  new: core.thing { name: 'alpha', size: 1 }
  fail: bad.thing { name: 'beta', size: 2 }
}
`, stateMoveUpdateFailureLibs(), store, stack)

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
		t, stateMoveRootSource(), resourceModules(&resourceCounters{}), store, stack,
	)
	pf := stateMovePlanFile(exec, "")

	_, err := exec.ApplyPlan(context.Background(), pf)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no entry at core.thing@resource.old")
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
		t, stateMoveRootSource(), resourceModules(&resourceCounters{}), store, stack,
	)
	pf := stateMovePlanFile(exec, rev)

	_, err = exec.ApplyPlan(context.Background(), pf)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "destination already exists at core.thing@resource.new")
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
			{From: "core.thing@resource.old", To: "core.thing@resource.new"},
		},
	}
}

func stateMoveCompositeLibs(t *testing.T) map[string]*Library {
	t.Helper()
	body := parseSyntaxCompositeFixture(t, `
box: resource {
  state-moves: [
    { from: 'core.thing@resource.old', to: 'core.thing@resource.new' },
  ]
  resources: {
    new: core.thing { name: 'alpha', size: 1 }
  }
}
`).body
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
	innerBody := parseSyntaxCompositeFixture(t, `
box: resource {
  resources: {
    new: core.thing { name: 'alpha', size: 1 }
  }
}
`).body
	outerBody := parseSyntaxCompositeFixture(t, `
outer: resource {
  state-moves: [
    { from: 'inner.box@resource.old-child', to: 'inner.box@resource.child' },
  ]
  resources: {
    child: inner.box {}
  }
}
`).body
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
	innerBody := parseSyntaxCompositeFixture(t, `
box: resource {
  state-moves: [
    { from: 'core.thing@resource.old', to: 'core.thing@resource.new' },
  ]
  resources: {
    new: core.thing { name: 'alpha', size: 1 }
  }
}
`).body
	outerBody := parseSyntaxCompositeFixture(t, `
web: resource {
  resources: {
    child: inner.box {}
  }
}
`).body
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

func stateMoveBoundaryEntry(selector, address string) *state.Entry {
	ref, err := ParseEntryRef(selector + "@" + address)
	if err != nil {
		panic(err)
	}
	return &state.Entry{
		Address:  ref.Address,
		Type:     state.EntryLibraryCall,
		Kind:     "resource",
		Selector: &state.Selector{Alias: ref.Selector.Alias, Export: ref.Selector.Export},
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
		Selector:      &state.Selector{Alias: "bad", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "beta", "size": int64(1)},
		Outputs:       map[string]any{"id": "beta", "size": int64(1)},
	}
}

func stateMoveRootSource() string {
	return `
state-moves: [
  { from: 'core.thing@resource.old', to: 'core.thing@resource.new' },
]
resources: {
  new: core.thing { name: 'alpha', size: 1 }
}
`
}

func stateMovePlanEntry(address string) *state.Entry {
	return &state.Entry{
		Address:       address,
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Selector:      &state.Selector{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha", "size": int64(1)},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha", "size": int64(1)},
	}
}
