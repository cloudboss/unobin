package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/state"
	"github.com/stretchr/testify/require"
)

func TestApplyPlanCreatesResource(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
outputs: {
  id: resource.core.thing.one.id
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	mods := resourceModules(&c)

	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Store:   store,
		Stack:   stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)

	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	res, err := exec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, "fake-alpha", res.Outputs["id"])
	require.Equal(t, int64(1), c.creates)
}

func TestApplyPlanComposite(t *testing.T) {
	composite := parseStack(t, `
resources: {
  core: { thing: { one: { name: var.name, size: 1 } } }
}
outputs: {
  id: resource.core.thing.one.id
}
`)
	var c resourceCounters
	mods := resourceModules(&c)
	mods["w"] = &Module{
		Name: "w",
		Composites: map[string]*CompositeType{
			"box": {Name: "box", Body: composite},
		},
	}
	src := `
resources: {
  w: { box: { x: { name: 'alpha' } } }
}
outputs: {
  out: resource.w.box.x.id
}
`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Store:   store,
		Stack:   stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	res, err := exec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, "fake-alpha", res.Outputs["out"])
	require.Equal(t, int64(1), c.creates)

	snap, err := store.Current()
	require.NoError(t, err)
	addresses := make([]string, len(snap.Entries))
	types := make([]state.EntryType, len(snap.Entries))
	for i, e := range snap.Entries {
		addresses[i] = e.Address
		types[i] = e.Type
	}
	require.ElementsMatch(t,
		[]string{"resource.w.box.x", "resource.w.box.x/core.thing.one"},
		addresses)
	require.Contains(t, types, state.EntryModuleCall)
	require.Contains(t, types, state.EntryLeaf)
}

func TestApplyPlanCompositeOrphan(t *testing.T) {
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
			"box": {Name: "box", Body: composite},
		},
	}
	first := `
resources: {
  core: { thing: { keep: { name: 'kept', size: 7 } } }
  w:    { box:   { x:    { name: 'alpha' } } }
}
`
	second := `
resources: {
  core: { thing: { keep: { name: 'kept', size: 7 } } }
}
`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}

	planAndApply := func(src string) *Plan {
		exec := &Executor{
			DAG:     BuildDAG(parseStack(t, src), mods),
			Modules: mods,
			Store:   store,
			Stack:   stack,
		}
		plan, err := exec.Plan(context.Background())
		require.NoError(t, err)
		encoded, err := EncodePlan(plan)
		require.NoError(t, err)
		pf, err := DecodePlan(encoded)
		require.NoError(t, err)
		_, err = exec.ApplyPlan(context.Background(), pf)
		require.NoError(t, err)
		return plan
	}

	planAndApply(first)
	require.Equal(t, int64(3), c.creates,
		"first apply creates two internals plus one root resource")
	require.Equal(t, int64(0), c.deletes)

	plan := planAndApply(second)
	require.Equal(t, int64(2), c.deletes,
		"both internals are destroyed when the call site goes away")

	destroyed := []string{}
	for _, step := range plan.Steps {
		if step.Decision == DecisionDestroy {
			destroyed = append(destroyed, step.Address)
		}
	}
	require.ElementsMatch(t,
		[]string{
			"resource.w.box.x/core.thing.one",
			"resource.w.box.x/core.thing.two",
		},
		destroyed,
		"the plan reports both internals as destroys")

	snap, err := store.Current()
	require.NoError(t, err)
	addresses := []string{}
	for _, e := range snap.Entries {
		addresses = append(addresses, e.Address)
	}
	require.Equal(t, []string{"resource.core.thing.keep"}, addresses,
		"only the root-level resource that stays in source remains in state")
}

func TestApplyPlanCompositeWithRootVarArgs(t *testing.T) {
	// The plan and apply phases run separately and apply does not
	// have access to the root inputs that plan used. The composite
	// boundary's args are evaluated at plan time and must seed the
	// composite scope at apply time so internals can read them.
	composite := parseStack(t, `
inputs: {
  who: { type: string }
}
resources: {
  core: { thing: { greet: { name: var.who, size: 1 } } }
}
`)
	var c resourceCounters
	mods := resourceModules(&c)
	mods["w"] = &Module{
		Name: "w",
		Composites: map[string]*CompositeType{
			"hello": {Name: "hello", Body: composite},
		},
	}
	src := `
inputs: {
  who: { type: string }
}
resources: {
  w: { hello: { x: { who: var.who } } }
}
`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}

	planExec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Inputs:  map[string]any{"who": "world"},
		Store:   store,
		Stack:   stack,
	}
	plan, err := planExec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	// Apply runs without root inputs, mirroring the stack binary's
	// `apply` subcommand which reads only the plan file.
	applyExec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), mods),
		Modules: mods,
		Store:   store,
		Stack:   stack,
	}
	_, err = applyExec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, int64(1), c.creates)

	snap, err := store.Current()
	require.NoError(t, err)
	leaf := findEntryByAddr(snap, "resource.w.hello.x/core.thing.greet")
	require.NotNil(t, leaf)
	require.Equal(t, "world", leaf.Inputs["name"])
}

func findEntryByAddr(snap *state.Snapshot, addr string) *state.Entry {
	for _, e := range snap.Entries {
		if e.Address == addr {
			return e
		}
	}
	return nil
}

func TestApplyPlanCompositeUpdateInPlace(t *testing.T) {
	composite := parseStack(t, `
resources: {
  core: { thing: { one: { name: var.name, size: 1 } } }
}
`)
	var c resourceCounters
	mods := resourceModules(&c)
	mods["w"] = &Module{
		Name: "w",
		Composites: map[string]*CompositeType{
			"box": {Name: "box", Body: composite},
		},
	}
	first := `
resources: {
  w: { box: { x: { name: 'alpha' } } }
}
`
	second := `
resources: {
  w: { box: { x: { name: 'alpha' } } }
}
`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}

	planAndApply := func(src string) {
		exec := &Executor{
			DAG:     BuildDAG(parseStack(t, src), mods),
			Modules: mods,
			Store:   store,
			Stack:   stack,
		}
		plan, err := exec.Plan(context.Background())
		require.NoError(t, err)
		encoded, err := EncodePlan(plan)
		require.NoError(t, err)
		pf, err := DecodePlan(encoded)
		require.NoError(t, err)
		_, err = exec.ApplyPlan(context.Background(), pf)
		require.NoError(t, err)
	}

	planAndApply(first)
	require.Equal(t, int64(1), c.creates)
	planAndApply(second)
	require.Equal(t, int64(1), c.creates,
		"unchanged composite call site does not recreate internals")
	require.Equal(t, int64(0), c.deletes)
	require.Equal(t, int64(0), c.updates)
}

func TestApplyPlanRefusesOnStateRevDrift(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	mods := resourceModules(&c)

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	// Drift: a separate apply changes state out from under our plan.
	_, err = exec.Run(context.Background())
	require.NoError(t, err)

	_, err = exec.ApplyPlan(context.Background(), pf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "state-rev drift")
}

func TestApplyPlanWaitsForLock(t *testing.T) {
	src := `
resources: {
  core: { thing: { one: { name: 'alpha', size: 1 } } }
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	mods := resourceModules(&c)
	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), mods), Modules: mods, Store: store, Stack: stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	held, err := store.Lock(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Unlock() })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = exec.ApplyPlan(ctx, pf)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestApplyPlanRefusesOnStackMismatch(t *testing.T) {
	src := `description: 'x'`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}

	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src), nil),
		Modules: map[string]*Module{},
		Store:   store,
		Stack:   stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)

	// Apply against a different stack identity.
	exec.Stack = state.StackInfo{Name: "different", Version: "v0", Commit: "c0"}
	_, err = exec.ApplyPlan(context.Background(), pf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "different")
}

func TestEncodeDecodePlan(t *testing.T) {
	plan := &Plan{
		Stack:        state.StackInfo{Name: "x", Version: "v1", Commit: "abc"},
		DeploymentID: "prod",
		StateRev:     "2026-05-01T00:00:00.000000000Z",
		Steps: []*PlanStep{
			{
				Address:  "resource.core.thing.x",
				Kind:     NodeResource,
				Decision: DecisionCreate,
				Inputs:   map[string]any{"name": "x"},
			},
		},
	}
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)
	require.Equal(t, plan.Stack.Name, pf.Stack.Name)
	require.Equal(t, plan.StateRev, pf.StateRev)
	require.Equal(t, "resource.core.thing.x", pf.Steps[0].Address)
	require.Equal(t, DecisionCreate, pf.Steps[0].Decision)
}

func TestActionRerunsWhenTriggerSourceChanges(t *testing.T) {
	src := func(name string) string {
		return `
resources: {
  core: { thing: { one: { name: '` + name + `', size: 1 } } }
}
actions: {
  core: {
    echo: {
      observe: {
        @trigger: resource.core.thing.one.id
        echo:     'observed'
      }
    }
  }
}
`
	}

	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	var resCounters resourceCounters
	var actionRuns int64
	mods := map[string]*Module{
		"core": {
			Name: "core",
			Resources: map[string]ResourceType{
				"thing": {
					Name:          "thing",
					SchemaVersion: 1,
					New: func() Resource {
						return &countingResource{counters: &resCounters}
					},
				},
			},
			Actions: map[string]ActionType{
				"echo": {
					Name: "echo",
					New: func() Action {
						return &countingAction{runs: &actionRuns}
					},
				},
			},
		},
	}

	planAndApply := func(stackSrc string) {
		exec := &Executor{
			DAG:     BuildDAG(parseStack(t, stackSrc), mods),
			Modules: mods,
			Store:   store,
			Stack:   stack,
		}
		plan, err := exec.Plan(context.Background())
		require.NoError(t, err)
		encoded, err := EncodePlan(plan)
		require.NoError(t, err)
		pf, err := DecodePlan(encoded)
		require.NoError(t, err)
		_, err = exec.ApplyPlan(context.Background(), pf)
		require.NoError(t, err)
	}

	// First run: fresh state. Resource is created; action runs even though
	// its trigger references a not-yet-created resource.
	planAndApply(src("alpha"))
	require.Equal(t, int64(1), atomic.LoadInt64(&actionRuns))

	// Second run, same source: resource is unchanged, action should skip.
	planAndApply(src("alpha"))
	require.Equal(t, int64(1), atomic.LoadInt64(&actionRuns),
		"action should skip on the second run when upstream is unchanged")

	// Third run with the resource's name changed: ReplaceFields=["name"]
	// triggers a replace, which the action treats as a rerun signal.
	planAndApply(src("beta"))
	require.Equal(t, int64(2), atomic.LoadInt64(&actionRuns),
		"action should rerun when its upstream resource is changing")
}

func TestDecodePlanRejectsBadFormatVersion(t *testing.T) {
	bad := []byte(`{"format-version": 99, "stack": {"name": "x"}, "steps": []}`)
	_, err := DecodePlan(bad)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported format-version")
}
