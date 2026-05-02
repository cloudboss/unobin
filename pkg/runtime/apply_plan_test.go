package runtime

import (
	"context"
	"testing"

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
		DAG:     BuildDAG(parseStack(t, src)),
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
		DAG: BuildDAG(parseStack(t, src)), Modules: mods, Store: store, Stack: stack,
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

func TestApplyPlanRefusesOnStackMismatch(t *testing.T) {
	src := `description: 'x'`
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}

	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src)),
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

func TestDecodePlanRejectsBadFormatVersion(t *testing.T) {
	bad := []byte(`{"format-version": 99, "stack": {"name": "x"}, "steps": []}`)
	_, err := DecodePlan(bad)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported format-version")
}
