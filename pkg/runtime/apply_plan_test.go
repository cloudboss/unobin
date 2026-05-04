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
