package runtime

import (
	"context"
	"testing"

	"github.com/cloudboss/unobin/pkg/state"
	"github.com/stretchr/testify/require"
)

func runPlan(t *testing.T, src string, modules map[string]*Module, store *state.LocalStore) *Plan {
	t.Helper()
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src)),
		Modules: modules,
		Store:   store,
		Stack:   state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	return plan
}

func decisionFor(plan *Plan, addr string) Decision {
	for _, s := range plan.Steps {
		if s.Address == addr {
			return s.Decision
		}
	}
	return ""
}

func TestPlanCreateForFreshResource(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	var c resourceCounters
	plan := runPlan(t, src, resourceModules(&c), newStateStore(t))
	require.Equal(t, DecisionCreate, decisionFor(plan, "resource.core.thing.one"))
	require.Equal(t, int64(0), c.creates, "Plan should not invoke Create")
}

func TestPlanNoOpForUnchanged(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	mods := resourceModules(&c)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	_, err := (&Executor{
		DAG: BuildDAG(parseStack(t, src)), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

	plan := runPlan(t, src, mods, store)
	require.Equal(t, DecisionNoOp, decisionFor(plan, "resource.core.thing.one"))
}

func TestPlanUpdateForNonReplaceFieldChange(t *testing.T) {
	first := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	second := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 99 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	mods := resourceModules(&c)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	_, err := (&Executor{
		DAG: BuildDAG(parseStack(t, first)), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

	plan := runPlan(t, second, mods, store)
	require.Equal(t, DecisionUpdate, decisionFor(plan, "resource.core.thing.one"))
}

func TestPlanReplaceForReplaceFieldChange(t *testing.T) {
	first := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	second := `
resources: {
  core: { thing: { one: { name: 'beta', size: 1 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	mods := resourceModules(&c)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	_, err := (&Executor{
		DAG: BuildDAG(parseStack(t, first)), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

	plan := runPlan(t, second, mods, store)
	require.Equal(t, DecisionReplace, decisionFor(plan, "resource.core.thing.one"))
}

func TestPlanDestroyForOrphan(t *testing.T) {
	first := `
resources: {
  core: {
    thing: {
      keep: { name: 'a', size: 1 }
      orph: { name: 'b', size: 2 }
    }
  }
}
`
	second := `
resources: {
  core: { thing: { keep: { name: 'a', size: 1 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	mods := resourceModules(&c)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	_, err := (&Executor{
		DAG: BuildDAG(parseStack(t, first)), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

	plan := runPlan(t, second, mods, store)
	require.Equal(t, DecisionNoOp, decisionFor(plan, "resource.core.thing.keep"))
	require.Equal(t, DecisionDestroy, decisionFor(plan, "resource.core.thing.orph"))
}

func TestPlanRerunForChangedAction(t *testing.T) {
	first := `
actions: {
  core: { echo: { hi: { echo: 'one' } } }
}
`
	second := `
actions: {
  core: { echo: { hi: { echo: 'two' } } }
}
`
	mods := map[string]*Module{
		"core": {
			Name: "core",
			Actions: map[string]ActionType{
				"echo": {Name: "echo", New: func() Action { return &echoAction{} }},
			},
		},
	}
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	_, err := (&Executor{
		DAG: BuildDAG(parseStack(t, first)), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

	plan := runPlan(t, second, mods, store)
	require.Equal(t, DecisionRerun, decisionFor(plan, "action.core.echo.hi"))
}

func TestPlanSkipForUnchangedAction(t *testing.T) {
	src := `
actions: {
  core: { echo: { hi: { echo: 'same' } } }
}
`
	mods := map[string]*Module{
		"core": {
			Name: "core",
			Actions: map[string]ActionType{
				"echo": {Name: "echo", New: func() Action { return &echoAction{} }},
			},
		},
	}
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	_, err := (&Executor{
		DAG: BuildDAG(parseStack(t, src)), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

	plan := runPlan(t, src, mods, store)
	require.Equal(t, DecisionSkip, decisionFor(plan, "action.core.echo.hi"))
}

func TestPlanRecordsStateRev(t *testing.T) {
	src := `description: 'x'`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	_, err := (&Executor{
		DAG: BuildDAG(parseStack(t, src)), Modules: map[string]*Module{}, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

	plan := runPlan(t, src, map[string]*Module{}, store)
	require.NotEmpty(t, plan.StateRev)
}
