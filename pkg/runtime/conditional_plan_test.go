package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type conditionalSource struct {
	Tag string
	Ref string
}

func (r *conditionalSource) Create(context.Context, any) (*conditionalOutput, error) {
	return &conditionalOutput{ID: r.Ref}, nil
}

func (r *conditionalSource) Read(
	_ context.Context, _ any, prior Prior[conditionalSource, *conditionalOutput, any],
) (*conditionalOutput, error) {
	return prior.Outputs, nil
}

func (r *conditionalSource) Update(
	_ context.Context, _ any, _ Prior[conditionalSource, *conditionalOutput, any],
) (*conditionalOutput, error) {
	return &conditionalOutput{ID: r.Ref}, nil
}

func (*conditionalSource) Delete(
	context.Context, any, Prior[conditionalSource, *conditionalOutput, any],
) error {
	return nil
}

type conditionalCalls struct {
	creates atomic.Int32
	updates atomic.Int32
	deletes atomic.Int32
}

type conditionalConsumer struct {
	Ref  string
	Name string
	Size int64

	calls *conditionalCalls
}

type conditionalOutput struct {
	ID string
}

func (r *conditionalConsumer) Create(context.Context, any) (*conditionalOutput, error) {
	r.calls.creates.Add(1)
	return &conditionalOutput{ID: r.Name + ":" + r.Ref}, nil
}

func (*conditionalConsumer) Read(
	_ context.Context, _ any, prior Prior[conditionalConsumer, *conditionalOutput, any],
) (*conditionalOutput, error) {
	return prior.Outputs, nil
}

func (r *conditionalConsumer) Update(
	_ context.Context, _ any, prior Prior[conditionalConsumer, *conditionalOutput, any],
) (*conditionalOutput, error) {
	r.calls.updates.Add(1)
	return prior.Outputs, nil
}

func (r *conditionalConsumer) Delete(
	_ context.Context, _ any, prior Prior[conditionalConsumer, *conditionalOutput, any],
) error {
	if r.Ref != prior.Inputs.Ref || r.Name != prior.Inputs.Name {
		return fmt.Errorf("delete received desired inputs instead of the recorded target")
	}
	r.calls.deletes.Add(1)
	return nil
}

func TestConditionalResourcePlanAppliesOnce(t *testing.T) {
	tests := []struct {
		name        string
		replaceRef  bool
		ref         string
		newName     string
		size        int64
		want        Decision
		allowed     []Decision
		reasons     []string
		appliedName string
		lateReplace bool
		wantErr     string
	}{
		{
			name: "mutable reference stays equal", ref: "original", newName: "name", size: 1,
			want: DecisionNoOp, allowed: []Decision{DecisionNoOp, DecisionUpdate},
		},
		{
			name: "replacement reference stays equal", replaceRef: true,
			ref: "original", newName: "name", size: 1, want: DecisionNoOp,
			allowed: []Decision{DecisionNoOp, DecisionUpdate, DecisionReplace},
		},
		{
			name: "known mutable change", replaceRef: true,
			ref: "original", newName: "name", size: 2, want: DecisionUpdate,
			allowed: []Decision{DecisionUpdate, DecisionReplace},
		},
		{
			name: "confirmed replacement loses pending reason", replaceRef: true,
			ref: "original", newName: "renamed", size: 1, want: DecisionReplace,
			allowed: []Decision{DecisionReplace}, reasons: []string{"name"},
		},
		{
			name: "mutable reference changes", ref: "changed", newName: "name", size: 1,
			want: DecisionUpdate, allowed: []Decision{DecisionNoOp, DecisionUpdate},
		},
		{
			name: "replacement reference changes", replaceRef: true,
			ref: "changed", newName: "name", size: 1, want: DecisionReplace,
			allowed: []Decision{DecisionNoOp, DecisionUpdate, DecisionReplace},
		},
		{
			name: "known input changes during apply", replaceRef: true,
			ref: "original", newName: "name", size: 1, appliedName: "unexpected",
			allowed: []Decision{DecisionNoOp, DecisionUpdate, DecisionReplace},
			wantErr: "inputs changed since the plan was computed",
		},
		{
			name: "unplanned replacement decision", lateReplace: true,
			ref: "original", newName: "renamed", size: 1,
			allowed: []Decision{DecisionUpdate},
			wantErr: "decision changed since the plan was computed",
		},
		{
			name: "unplanned replacement reason", replaceRef: true, lateReplace: true,
			ref: "original", newName: "renamed", size: 1,
			allowed: []Decision{DecisionUpdate, DecisionReplace},
			wantErr: "decision changed since the plan was computed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls conditionalCalls
			var tail versionCounters
			ref := InputField(func(in *conditionalConsumer) *string { return &in.Ref })
			definition := ResourceDefinition[conditionalConsumer, *conditionalOutput, any]{
				SchemaVersion: 1,
				Equality: []InputEqualityRule[conditionalConsumer]{
					EqualBy(ref, func(a, b string) bool {
						if b == "" {
							panic("equality received a pending placeholder")
						}
						return a == b
					}),
				},
				Replace: Replacement[conditionalConsumer, *conditionalOutput, any]{
					Fields: []AnyInputField[conditionalConsumer]{
						InputField(func(in *conditionalConsumer) *string { return &in.Name }),
					},
				},
				StableID: func(_ conditionalConsumer, out *conditionalOutput) (string, error) {
					return out.ID, nil
				},
			}
			if tt.replaceRef {
				definition.Replace.Rules = []ReplacementRule[conditionalConsumer]{
					ReplaceWhen(ref, func(_, desired string) bool {
						if desired == "" {
							panic("replacement received a pending placeholder")
						}
						return true
					}),
				}
			}
			var replaceName atomic.Bool
			if tt.lateReplace {
				name := InputField(func(in *conditionalConsumer) *string { return &in.Name })
				definition.Replace.Fields = nil
				definition.Replace.Rules = append(definition.Replace.Rules,
					ReplaceWhen(name, func(_, _ string) bool { return replaceName.Load() }))
			}
			libs := map[string]*Library{"core": {
				Name: "core",
				Resources: map[string]ResourceRegistration{
					"source": MakeResource[conditionalSource, *conditionalOutput, any](
						testResourceDefinition[conditionalSource, *conditionalOutput, any](),
					),
					"consumer": MakeResourceWith[conditionalConsumer, *conditionalOutput, any](
						definition, func() *conditionalConsumer { return &conditionalConsumer{calls: &calls} },
					),
					"tail": MakeResourceWith[versionConsumer, *versionConsumerOutput, any](
						testResourceDefinition[versionConsumer, *versionConsumerOutput, any](),
						func() *versionConsumer { return &versionConsumer{counters: &tail} },
					),
				},
			}}
			store := newStateStore(t)
			dag, source := syntaxDAGAndBody(t,
				ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "conditional-update"), libs)
			exec := &Executor{
				DAG: dag, SyntaxSource: source, Libraries: libs, Store: store,
				Factory: state.FactoryInfo{Name: "conditional", Version: "v0", ContentRevision: "c0"},
				Inputs:  map[string]any{"tag": "1", "ref": "original", "name": "name", "size": int64(1)},
			}
			applyOnce(t, exec)
			calls.creates.Store(0)
			exec.Inputs = map[string]any{
				"tag": "2", "ref": tt.ref, "name": tt.newName, "size": tt.size,
			}
			plan, err := exec.Plan(context.Background())
			require.NoError(t, err)
			step := findStep(t, plan, "resource.consumer")
			require.Equal(t, tt.allowed, step.AllowedDecisions)
			require.Equal(t, tt.reasons, step.ReplacementReasons)
			if tt.replaceRef {
				require.Equal(t, []string{"ref"}, step.PendingReplacementReasons)
				require.NotNil(t, step.ExpectedStableID)
			}
			encoded, err := EncodePlan(plan)
			require.NoError(t, err)
			pf, err := DecodePlan(encoded)
			require.NoError(t, err)
			require.Equal(t, 2, pf.FormatVersion)
			before, err := json.Marshal(pf)
			require.NoError(t, err)
			events := make(chan ApplyEvent, 32)
			apply := &Executor{
				DAG: dag, SyntaxSource: source, Libraries: libs, Store: store,
				Factory: exec.Factory, Events: events,
			}
			if tt.appliedName != "" {
				apply.Inputs = map[string]any{
					"tag": "2", "ref": tt.ref, "name": tt.appliedName, "size": tt.size,
				}
			}
			replaceName.Store(tt.lateReplace)
			result, err := apply.ApplyPlan(context.Background(), pf)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Zero(t, calls.creates.Load())
				require.Zero(t, calls.updates.Load())
				require.Zero(t, calls.deletes.Load())
				return
			}
			require.NoError(t, err)
			close(events)
			collected := readApplyEvents(events)
			require.Equal(t, tt.want,
				requireApplyEvent(t, collected, "resource.consumer", StageStart).Decision)
			require.Equal(t, tt.want,
				requireApplyEvent(t, collected, "resource.consumer", StageDone).Decision)
			after, err := json.Marshal(pf)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after), "apply must preserve the approved plan")
			wantCreates, wantUpdates, wantDeletes := int32(0), int32(0), int32(0)
			if tt.want == DecisionReplace {
				wantCreates, wantDeletes = 1, 1
			}
			if tt.want == DecisionUpdate {
				wantUpdates = 1
			}
			require.Equal(t, wantCreates, calls.creates.Load())
			require.Equal(t, wantUpdates, calls.updates.Load())
			require.Equal(t, wantDeletes, calls.deletes.Load())
			wantID := "name:original"
			if tt.want == DecisionReplace {
				wantID = tt.newName + ":" + tt.ref
			}
			require.Equal(t, wantID, result.Outputs["id"])
			snapshot, err := store.Current()
			require.NoError(t, err)
			require.Equal(t, map[string]any{
				"name": tt.newName, "ref": tt.ref, "size": float64(tt.size),
			}, snapshot.Find("resource.consumer").Inputs)
		})
	}
}
