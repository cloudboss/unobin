package runtime

import (
	"context"
	"maps"
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

func TestReconcileTargets(t *testing.T) {
	res := func(addr string, dec Decision) PlanStep {
		return PlanStep{Address: addr, Kind: NodeResource, Decision: dec}
	}
	tests := []struct {
		name  string
		steps []PlanStep
		deps  map[string][]string
		want  []string
	}{
		{
			name:  "create with resource dependency",
			steps: []PlanStep{res("a", DecisionCreate), res("b", DecisionCreate)},
			deps:  map[string][]string{"b": {"a"}},
			want:  []string{"a"},
		},
		{
			name:  "sink create with no dependencies",
			steps: []PlanStep{res("a", DecisionCreate)},
			deps:  nil,
			want:  nil,
		},
		{
			name:  "no-op dependency of a create is re-read",
			steps: []PlanStep{res("a", DecisionNoOp), res("b", DecisionCreate)},
			deps:  map[string][]string{"b": {"a"}},
			want:  []string{"a"},
		},
		{
			name: "action dependency is excluded",
			steps: []PlanStep{
				res("b", DecisionCreate),
				{Address: "x", Kind: NodeAction, Decision: DecisionRerun},
			},
			deps: map[string][]string{"b": {"x"}},
			want: nil,
		},
		{
			name: "composite call site dependency is excluded",
			steps: []PlanStep{
				res("b", DecisionCreate),
				{Address: "c", Kind: NodeResource, Decision: DecisionCreate, Composite: true},
			},
			deps: map[string][]string{"b": {"c"}},
			want: nil,
		},
		{
			name:  "destroy is not a mutator",
			steps: []PlanStep{res("a", DecisionNoOp), res("d", DecisionDestroy)},
			deps:  map[string][]string{"d": {"a"}},
			want:  nil,
		},
		{
			name:  "destroyed dependency is not reconciled",
			steps: []PlanStep{res("b", DecisionCreate), res("g", DecisionDestroy)},
			deps:  map[string][]string{"b": {"g"}},
			want:  nil,
		},
		{
			name: "two changed steps share a dependency",
			steps: []PlanStep{
				res("a", DecisionCreate), res("b", DecisionCreate), res("c", DecisionCreate),
			},
			deps: map[string][]string{"b": {"a"}, "c": {"a"}},
			want: []string{"a"},
		},
		{
			name:  "no changed steps",
			steps: []PlanStep{res("a", DecisionNoOp)},
			deps:  nil,
			want:  nil,
		},
		{
			name:  "update mutator",
			steps: []PlanStep{res("a", DecisionNoOp), res("b", DecisionUpdate)},
			deps:  map[string][]string{"b": {"a"}},
			want:  []string{"a"},
		},
		{
			name:  "replace mutator",
			steps: []PlanStep{res("a", DecisionNoOp), res("b", DecisionReplace)},
			deps:  map[string][]string{"b": {"a"}},
			want:  []string{"a"},
		},
		{
			name: "action rerun mutator with resource dependency",
			steps: []PlanStep{
				res("a", DecisionNoOp),
				{Address: "act", Kind: NodeAction, Decision: DecisionRerun},
			},
			deps: map[string][]string{"act": {"a"}},
			want: []string{"a"},
		},
		{
			name: "data read is not a mutator",
			steps: []PlanStep{
				res("a", DecisionNoOp),
				{Address: "d", Kind: NodeData, Decision: DecisionRead},
			},
			deps: map[string][]string{"d": {"a"}},
			want: nil,
		},
		{
			name: "skipped action is not a mutator",
			steps: []PlanStep{
				res("a", DecisionNoOp),
				{Address: "act", Kind: NodeAction, Decision: DecisionSkip},
			},
			deps: map[string][]string{"act": {"a"}},
			want: nil,
		},
		{
			name:  "dependency missing from steps",
			steps: []PlanStep{res("b", DecisionCreate)},
			deps:  map[string][]string{"b": {"ghost"}},
			want:  nil,
		},
		{
			name:  "multiple dependencies keep dependency order",
			steps: []PlanStep{res("a1", DecisionNoOp), res("a2", DecisionNoOp), res("b", DecisionCreate)},
			deps:  map[string][]string{"b": {"a1", "a2"}},
			want:  []string{"a1", "a2"},
		},
		{
			name: "mixed dependencies keep only the resource",
			steps: []PlanStep{
				res("a", DecisionNoOp),
				{Address: "act", Kind: NodeAction, Decision: DecisionRerun},
				res("b", DecisionCreate),
			},
			deps: map[string][]string{"b": {"act", "a"}},
			want: []string{"a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pf := &PlanFile{Steps: tt.steps}
			got := reconcileTargets(pf, tt.deps)
			require.Equal(t, tt.want, got)
			// The result is order-deterministic, so repeated calls return
			// the identical slice.
			for range 5 {
				require.Equal(t, tt.want, reconcileTargets(pf, tt.deps))
			}
		})
	}
}

// reconcileSrc is two resources where b reads a's size, so b depends on
// a. After apply b is a sink (nothing depends on it).
const reconcileSrc = `
resources: {
  core.thing.a: { name: 'a', size: 1 }
  core.thing.b: { name: 'b', size: resource.core.thing.a.size }
}
`

func findEntry(t *testing.T, snap *state.Snapshot, addr string) *state.Entry {
	t.Helper()
	for _, e := range snap.Entries {
		if e.Address == addr {
			return e
		}
	}
	t.Fatalf("entry %q not found in snapshot", addr)
	return nil
}

func TestApplyReconcilesMutatedDependency(t *testing.T) {
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	// A sibling-induced side effect is modeled as a live Read returning a
	// different size than the resource was created with.
	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		maps.Copy(out, m)
		out["size"] = int64(99)
		return out, nil
	}

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, reconcileSrc), libs), Libraries: libs, Store: store, Factory: stack,
	}
	applyOnce(t, exec)

	snap, err := store.Current()
	require.NoError(t, err)
	a := findEntry(t, snap, "resource.core.thing.a")
	b := findEntry(t, snap, "resource.core.thing.b")
	require.EqualValues(t, 99, a.Outputs["size"],
		"the dependency is re-read after apply and its settled output reaches state")
	require.EqualValues(t, 1, b.Outputs["size"],
		"the sink is not re-read, so its applied output stands")
}

func TestApplyReconciledValueReachesOutputs(t *testing.T) {
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	src := `
resources: {
  core.thing.a: { name: 'a', size: 1 }
  core.thing.b: { name: 'b', size: resource.core.thing.a.size }
}
outputs: { a-size: { value: resource.core.thing.a.size } }
`
	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		maps.Copy(out, m)
		out["size"] = int64(99)
		return out, nil
	}

	res := applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})
	require.EqualValues(t, 99, res.Outputs["a-size"],
		"a stack output reads the reconciled value, not the one captured at apply")
}

func TestApplyReconcilesPreExistingDependency(t *testing.T) {
	ctx := context.Background()
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	// Apply one resource on its own.
	srcA := `
resources: { core.thing.a: { name: 'a', size: 1 } }
`
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, srcA), libs), Libraries: libs, Store: store, Factory: stack,
	})

	// Add a second resource that depends on the first. The plan reads the
	// first as an unchanged no-op; only after the plan does its live Read
	// start returning a mutated value, modeling the new dependent's side
	// effect at apply time.
	exec := &Executor{
		DAG: BuildDAG(parseStack(t, reconcileSrc), libs), Libraries: libs, Store: store, Factory: stack,
	}
	plan, err := exec.Plan(ctx)
	require.NoError(t, err)
	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		maps.Copy(out, m)
		if out["name"] == "a" {
			out["size"] = int64(99)
		}
		return out, nil
	}
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)
	_, err = exec.ApplyPlan(ctx, pf)
	require.NoError(t, err)

	require.EqualValues(t, 0, c.updates,
		"the pre-existing dependency stays a no-op, so no update runs")
	snap, err := store.Current()
	require.NoError(t, err)
	a := findEntry(t, snap, "resource.core.thing.a")
	require.EqualValues(t, 99, a.Outputs["size"],
		"a no-op dependency of a newly created resource is still reconciled")
}

func TestApplyReconcileFailureKeepsAppliedOutputs(t *testing.T) {
	tests := []struct {
		name   string
		readFn func(prior any) (any, error)
	}{
		{
			name:   "read error",
			readFn: func(any) (any, error) { return nil, context.DeadlineExceeded },
		},
		{
			name:   "resource reports absent",
			readFn: func(any) (any, error) { return nil, ErrNotFound },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c resourceCounters
			store := newStateStore(t)
			libs := resourceModules(&c)
			stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
			c.readFn = tt.readFn

			exec := &Executor{
				DAG:       BuildDAG(parseStack(t, reconcileSrc), libs),
				Libraries: libs,
				Store:     store,
				Factory:   stack,
			}
			_, err := planAndApply(exec)
			require.NoError(t, err, "a reconcile read failure does not fail the apply")

			snap, err := store.Current()
			require.NoError(t, err)
			a := findEntry(t, snap, "resource.core.thing.a")
			require.EqualValues(t, 1, a.Outputs["size"],
				"a resource whose reconcile read failed keeps its applied output")
		})
	}
}
