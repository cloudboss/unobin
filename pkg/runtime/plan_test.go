package runtime

import (
	"context"
	"testing"

	"github.com/cloudboss/unobin/pkg/localstate"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

func runPlan(t *testing.T, src string, modules map[string]*Module, store *localstate.LocalStore) *Plan {
	t.Helper()
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), modules),
		Modules: modules,
		Store:   store,
		Stack:   state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	return plan
}

func decisionFor(plan *Plan, addr string) Decision {
	if s := stepFor(plan, addr); s != nil {
		return s.Decision
	}
	return ""
}

func stepFor(plan *Plan, addr string) *PlanStep {
	for _, s := range plan.Steps {
		if s.Address == addr {
			return s
		}
	}
	return nil
}

func TestPlanForEachResourceEmitsOneStepPerInstance(t *testing.T) {
	src := `
resources: {
  core: {
    thing: {
      many: {
        @for-each: var.configs
        name:      @each.key
        size:      @each.value
      }
    }
  }
}
`
	var c resourceCounters
	mods := resourceModules(&c)
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Inputs:  map[string]any{"configs": map[string]any{"alpha": int64(1), "beta": int64(2)}},
		Store:   newStateStore(t),
		Stack:   state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)

	alpha := stepFor(plan, "resource.core.thing.many['alpha']")
	require.NotNil(t, alpha, "alpha instance step")
	require.Equal(t, DecisionCreate, alpha.Decision)
	require.Equal(t, "alpha", alpha.Inputs["name"])
	require.Equal(t, int64(1), alpha.Inputs["size"])

	beta := stepFor(plan, "resource.core.thing.many['beta']")
	require.NotNil(t, beta, "beta instance step")
	require.Equal(t, DecisionCreate, beta.Decision)
	require.Equal(t, "beta", beta.Inputs["name"])
	require.Equal(t, int64(2), beta.Inputs["size"])

	require.Nil(t, stepFor(plan, "resource.core.thing.many"),
		"no plan step for the template address itself")
}

func TestPlanForEachOrphanInstanceDestroyed(t *testing.T) {
	src := `
resources: {
  core: {
    thing: {
      many: {
        @for-each: var.configs
        name:      @each.key
        size:      @each.value
      }
    }
  }
}
`
	var c resourceCounters
	mods := resourceModules(&c)
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Inputs:  map[string]any{"configs": map[string]any{"alpha": int64(1), "beta": int64(2)}},
		Store:   store,
		Stack:   stack,
	}
	applyOnce(t, exec)

	exec.Inputs = map[string]any{"configs": map[string]any{"alpha": int64(1)}}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)

	beta := stepFor(plan, "resource.core.thing.many['beta']")
	require.NotNil(t, beta, "removed instance shows up as orphan")
	require.Equal(t, DecisionDestroy, beta.Decision)
}

func TestPlanComposite(t *testing.T) {
	composite := parseStack(t, `
resources: {
  core: {
    thing: {
      one: { name: var.name, size: 1 }
      two: { name: var.name, size: 2 }
    }
  }
}
`)
	var c resourceCounters
	mods := resourceModules(&c)
	mods["w"] = &Module{
		Name: "w",
		Composites: map[string]*CompositeType{
			"pair": {Name: "pair", Body: composite},
		},
	}
	stackSrc := `
resources: {
  w: { pair: { x: { name: 'alpha' } } }
}
`
	plan := runPlan(t, stackSrc, mods, newStateStore(t))

	boundary := stepFor(plan, "resource.w.pair.x")
	require.NotNil(t, boundary)
	require.Equal(t, NodeComposite, boundary.Kind)
	require.Equal(t, DecisionEval, boundary.Decision)
	require.Equal(t, "alpha", boundary.Inputs["name"])

	one := stepFor(plan, "resource.w.pair.x/core.thing.one")
	require.NotNil(t, one)
	require.Equal(t, NodeResource, one.Kind)
	require.Equal(t, DecisionCreate, one.Decision)
	require.Equal(t, "alpha", one.Inputs["name"])

	two := stepFor(plan, "resource.w.pair.x/core.thing.two")
	require.NotNil(t, two)
	require.Equal(t, DecisionCreate, two.Decision)
}

func TestPlanCompositeInternalActionSkipsAfterRun(t *testing.T) {
	composite := parseStack(t, `
inputs: { phrase: { type: string } }
actions: {
  core: {
    echo: { say: { echo: var.phrase } }
  }
}
outputs: {
  said: action.core.echo.say.echo
}
`)
	mods := testModules()
	mods["w"] = &Module{
		Name: "w",
		Composites: map[string]*CompositeType{
			"box": {Name: "box", Body: composite},
		},
	}
	stackSrc := `
resources: {
  w: { box: { x: { phrase: 'hello' } } }
}
`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, stackSrc), mods),
		Modules: mods,
		Store:   store,
		Stack:   stack,
	}
	applyOnce(t, exec)

	plan := runPlan(t, stackSrc, mods, store)
	step := stepFor(plan, "resource.w.box.x/action.core.echo.say")
	require.NotNil(t, step,
		"internal action should appear as a plan step under its composite-prefixed address")
	require.Equal(t, NodeAction, step.Kind)
	require.Equal(t, DecisionSkip, step.Decision,
		"second plan should skip the internal action whose trigger hash matches state")
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
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	})

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
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, first), mods), Modules: mods, Store: store, Stack: stack,
	})

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
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, first), mods), Modules: mods, Store: store, Stack: stack,
	})

	plan := runPlan(t, second, mods, store)
	require.Equal(t, DecisionReplace, decisionFor(plan, "resource.core.thing.one"))
}

func TestPlanUpdateRevertsDrift(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	mods := resourceModules(&c)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	})

	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		for k, v := range m {
			out[k] = v
		}
		out["size"] = int64(99)
		return out, nil
	}

	plan := runPlan(t, src, mods, store)
	step := stepFor(plan, "resource.core.thing.one")
	require.NotNil(t, step)
	require.Equal(t, DecisionUpdate, step.Decision,
		"drift with no input change should plan a revert via Update")
	require.True(t, step.Drift(), "step should report drift")
	require.NotEqual(t, step.PriorOutputs["size"], step.ObservedOutputs["size"])
}

func TestPlanMigratesPriorOutputsOnSchemaBump(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}

	prior := state.NewSnapshot(stack, store.DeploymentID())
	prior.Entries = []*state.Entry{{
		Address:       "resource.core.thing.one",
		Type:          state.EntryLeaf,
		Kind:          "thing",
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha", "size": float64(1)},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha", "size": float64(1)},
	}}
	rev, err := store.Write(prior)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	var c resourceCounters
	mods := map[string]*Module{
		"core": {
			Name: "core",
			Resources: map[string]ResourceType{
				"thing": {
					Name:          "thing",
					SchemaVersion: 2,
					New:           func() Resource { return &countingResource{counters: &c} },
					Migrate: func(_ int, st map[string]any) (map[string]any, error) {
						out := map[string]any{}
						for k, v := range st {
							out[k] = v
						}
						if v, ok := out["id"]; ok {
							out["name-id"] = v
							delete(out, "id")
						}
						return out, nil
					},
				},
			},
		},
	}

	var seenByRead any
	c.readFn = func(prior any) (any, error) {
		seenByRead = prior
		return prior, nil
	}

	plan := runPlan(t, src, mods, store)
	step := stepFor(plan, "resource.core.thing.one")
	require.NotNil(t, step)
	require.Equal(t, DecisionNoOp, step.Decision)

	rcv, ok := seenByRead.(map[string]any)
	require.True(t, ok)
	require.NotContains(t, rcv, "id", "Read should see the migrated outputs")
	require.Equal(t, "fake-alpha", rcv["name-id"])
	require.NotContains(t, step.PriorOutputs, "id",
		"PriorOutputs on the plan step should be the migrated outputs")
	require.Equal(t, "fake-alpha", step.PriorOutputs["name-id"])
}

func TestPlanErrorsWhenSchemaBumpHasNoMigrate(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}

	prior := state.NewSnapshot(stack, store.DeploymentID())
	prior.Entries = []*state.Entry{{
		Address:       "resource.core.thing.one",
		Type:          state.EntryLeaf,
		Kind:          "thing",
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha", "size": float64(1)},
		Outputs:       map[string]any{"id": "fake-alpha"},
	}}
	rev, err := store.Write(prior)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	var c resourceCounters
	mods := map[string]*Module{
		"core": {
			Name: "core",
			Resources: map[string]ResourceType{
				"thing": {
					Name:          "thing",
					SchemaVersion: 2,
					New:           func() Resource { return &countingResource{counters: &c} },
				},
			},
		},
	}

	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Store:   store,
		Stack:   stack,
	}
	_, err = exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Migrate")
}

func TestPlanRecordsUnresolvedFieldRefs(t *testing.T) {
	src := `
resources: {
  core: {
    thing: {
      one: { name: 'alpha', size: 1 }
      two: { name: resource.core.thing.one.name, size: 2 }
    }
  }
}
`
	var c resourceCounters
	plan := runPlan(t, src, resourceModules(&c), newStateStore(t))

	two := stepFor(plan, "resource.core.thing.two")
	require.NotNil(t, two)
	require.Equal(t, DecisionCreate, two.Decision)
	require.Equal(t, []string{"resource.core.thing.one.name"}, two.UnresolvedInputs["name"])
	require.NotContains(t, two.UnresolvedInputs, "size",
		"resolved fields should not appear in UnresolvedInputs")
	require.Nil(t, two.Inputs["name"],
		"the unresolved field's value should be nil so the renderer can spot it")
	require.Equal(t, int64(2), two.Inputs["size"])
}

func TestPlanCreateWhenResourceIsGone(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	mods := resourceModules(&c)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	})

	c.readFn = func(any) (any, error) { return nil, ErrNotFound }

	plan := runPlan(t, src, mods, store)
	step := stepFor(plan, "resource.core.thing.one")
	require.NotNil(t, step)
	require.Equal(t, DecisionCreate, step.Decision,
		"a missing resource with prior state should plan a recreate")
	require.True(t, step.Gone(), "step should report Gone")
	require.Empty(t, step.ObservedOutputs)
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
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, first), mods), Modules: mods, Store: store, Stack: stack,
	})

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
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, first), mods), Modules: mods, Store: store, Stack: stack,
	})

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
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	})

	plan := runPlan(t, src, mods, store)
	require.Equal(t, DecisionSkip, decisionFor(plan, "action.core.echo.hi"))
}

func TestPlanRecordsStateRev(t *testing.T) {
	src := `description: 'x'`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	applyOnce(t, &Executor{
		DAG:     BuildDAG(parseStack(t, src), nil),
		Modules: map[string]*Module{},
		Store:   store,
		Stack:   stack,
	})

	plan := runPlan(t, src, map[string]*Module{}, store)
	require.NotEmpty(t, plan.StateRev)
}
