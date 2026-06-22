package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/cloudboss/unobin/pkg/ubtest"
)

func TestOverlayDefaults(t *testing.T) {
	value := func(field, value string) lang.DefaultSpec {
		return lang.DefaultSpec{Field: field, Value: value}
	}
	tests := []struct {
		name       string
		inputs     map[string]any
		specs      []lang.DefaultSpec
		unresolved map[string][]string
		want       map[string]any
		wantErr    string
	}{
		{
			name:   "fills a missing field",
			inputs: map[string]any{"name": "a"},
			specs:  []lang.DefaultSpec{value("input.mode", "420")},
			want:   map[string]any{"name": "a", "mode": int64(420)},
		},
		{
			name:   "keeps a null field",
			inputs: map[string]any{"mode": nil},
			specs:  []lang.DefaultSpec{value("input.mode", "420")},
			want:   map[string]any{"mode": nil},
		},
		{
			name:   "keeps a set value",
			inputs: map[string]any{"mode": int64(384)},
			specs:  []lang.DefaultSpec{value("input.mode", "420")},
			want:   map[string]any{"mode": int64(384)},
		},
		{
			name:   "keeps a set zero value",
			inputs: map[string]any{"mode": int64(0)},
			specs:  []lang.DefaultSpec{value("input.mode", "420")},
			want:   map[string]any{"mode": int64(0)},
		},
		{
			name:   "keeps a set false",
			inputs: map[string]any{"on": false},
			specs:  []lang.DefaultSpec{value("input.on", "true")},
			want:   map[string]any{"on": false},
		},
		{
			name:   "fills string and boolean literals",
			inputs: map[string]any{},
			specs: []lang.DefaultSpec{
				value("input.method", "'GET'"),
				value("input.follow", "true"),
				value("input.ratio", "0.5"),
			},
			want: map[string]any{"method": "GET", "follow": true, "ratio": 0.5},
		},
		{
			name:   "optional marker fills nothing",
			inputs: map[string]any{},
			specs:  []lang.DefaultSpec{{Field: "input.dir", Optional: true}},
			want:   map[string]any{},
		},
		{
			name:   "fills a nested field when its parent is present",
			inputs: map[string]any{"code": map[string]any{"inline": "x"}},
			specs:  []lang.DefaultSpec{value("input.code.retries", "3")},
			want: map[string]any{
				"code": map[string]any{"inline": "x", "retries": int64(3)},
			},
		},
		{
			name:   "does not invent an absent parent object",
			inputs: map[string]any{},
			specs:  []lang.DefaultSpec{value("input.code.retries", "3")},
			want:   map[string]any{},
		},
		{
			name:   "does not descend into a null parent",
			inputs: map[string]any{"code": nil},
			specs:  []lang.DefaultSpec{value("input.code.retries", "3")},
			want:   map[string]any{"code": nil},
		},
		{
			name:   "does not descend into a non-object parent",
			inputs: map[string]any{"code": "inline"},
			specs:  []lang.DefaultSpec{value("input.code.retries", "3")},
			want:   map[string]any{"code": "inline"},
		},
		{
			name:       "skips a field waiting on an upstream output",
			inputs:     map[string]any{"mode": nil},
			specs:      []lang.DefaultSpec{value("input.mode", "420")},
			unresolved: map[string][]string{"mode": {"resource.core.thing.a.id"}},
			want:       map[string]any{"mode": nil},
		},
		{
			name:    "a literal that does not parse names the field",
			inputs:  map[string]any{},
			specs:   []lang.DefaultSpec{value("input.mode", "{")},
			wantErr: `default for "mode"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := overlayDefaults(tt.inputs, tt.specs, tt.unresolved)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, tt.inputs)
		})
	}
}

// defaultsExecutor plans one fixture whose thing type declares a Value
// default for size and an Optional marker for region.
func defaultsExecutor(t *testing.T, fixture string) (*Executor, *local.Store) {
	t.Helper()
	libs := resourceModules(&resourceCounters{})
	libs["core"].Defaults = map[string][]lang.DefaultSpec{
		"resource.thing": {
			{Field: "input.size", Value: "7"},
			{Field: "input.region", Optional: true},
		},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/defaults", fixture)
	store := newStateStore(t)
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	return &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        store,
		Factory:      state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"},
	}, store
}

func TestPlanFillsDeclaredDefaults(t *testing.T) {
	exec, _ := defaultsExecutor(t, "default-size-omitted")
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Steps, 1)
	require.Equal(t, map[string]any{"name": "a", "size": int64(7)}, plan.Steps[0].Inputs)
}

func TestPlanKeepsExplicitValueOverDefault(t *testing.T) {
	exec, _ := defaultsExecutor(t, "default-size-explicit")
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Steps, 1)
	require.Equal(t, map[string]any{"name": "a", "size": int64(9)}, plan.Steps[0].Inputs)
}

// TestPlanConstraintSeesDefault proves the overlay runs before the
// constraint check: the rule requires size above 5, the body omits
// size, and the declared default 7 satisfies it.
func TestPlanConstraintSeesDefault(t *testing.T) {
	exec, _ := defaultsExecutor(t, "default-size-omitted")
	exec.Libraries["core"].Constraints = map[string][]lang.ConstraintSpec{
		"resource.thing": {{Kind: "predicate", When: "true", Require: "input.size > 5"}},
	}
	_, err := exec.Plan(context.Background())
	require.NoError(t, err)
}

// TestPlanDefaultDoesNotMaskForwardRef proves a field waiting on an
// upstream output is not filled with its default: thing b's size waits
// on a's id, only known after apply, so it must stay pending rather
// than read as 7.
func TestPlanDefaultDoesNotMaskForwardRef(t *testing.T) {
	plan := planTwoThingsWithSizeDefault(t)
	for _, step := range plan.Steps {
		if step.Address != "resource.b" {
			continue
		}
		require.Contains(t, step.UnresolvedInputs, "size")
		require.Equal(t, PendingValue{Refs: []string{"resource.a.id"}}, step.Inputs["size"])
		return
	}
	t.Fatal("no step for resource.b")
}

// TestPlanSeedsDefaultedInputForReferences proves a defaulted input
// joins the upstream node's referenceable attributes: a's size is
// filled with 7 at plan, so b's name reference to it resolves rather
// than waiting for apply.
func TestPlanSeedsDefaultedInputForReferences(t *testing.T) {
	plan := planTwoThingsWithSizeDefault(t)
	for _, step := range plan.Steps {
		if step.Address != "resource.b" {
			continue
		}
		require.Equal(t, int64(7), step.Inputs["name"])
		return
	}
	t.Fatal("no step for resource.b")
}

// planTwoThingsWithSizeDefault plans two thing nodes whose type
// declares size to default to 7: b's name reads a's defaulted size
// input, and b's size reads a's id, an output only known after apply.
func planTwoThingsWithSizeDefault(t *testing.T) *Plan {
	t.Helper()
	libs := resourceModules(&resourceCounters{})
	libs["core"].Defaults = map[string][]lang.DefaultSpec{
		"resource.thing": {{Field: "input.size", Value: "7"}},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/defaults", "default-forward-refs")
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"},
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	return plan
}

// TestApplyFillsDeclaredDefaults proves the apply-side evaluation fills
// defaults too: the created resource decodes size 7 and echoes it into
// its outputs, and the state entry records the defaulted input.
func TestApplyFillsDeclaredDefaults(t *testing.T) {
	exec, store := defaultsExecutor(t, "default-size-omitted")
	_, err := planAndApply(exec)
	require.NoError(t, err)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 1)
	// The snapshot has been through the store's JSON encoding, which
	// reads numbers back as float64, so compare numerically.
	require.EqualValues(t, 7, snap.Entries[0].Inputs["size"])
	require.EqualValues(t, 7, snap.Entries[0].Outputs["size"])
}
