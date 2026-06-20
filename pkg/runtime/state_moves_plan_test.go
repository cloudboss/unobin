package runtime

import (
	"context"
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
