package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type conditionalPolicy struct {
	QueueURL string `ub:"queue-url"`
	Policy   string
	calls    *conditionalCalls
}

type conditionalPolicyOutput struct{}

func (r *conditionalPolicy) Create(context.Context, any) (*conditionalPolicyOutput, error) {
	r.calls.creates.Add(1)
	return &conditionalPolicyOutput{}, nil
}

func (*conditionalPolicy) Read(
	context.Context, any, Prior[conditionalPolicy, *conditionalPolicyOutput, any],
) (*conditionalPolicyOutput, error) {
	return &conditionalPolicyOutput{}, nil
}

func (r *conditionalPolicy) Update(
	context.Context, any, Prior[conditionalPolicy, *conditionalPolicyOutput, any],
) (*conditionalPolicyOutput, error) {
	r.calls.updates.Add(1)
	return &conditionalPolicyOutput{}, nil
}

func (r *conditionalPolicy) Delete(
	context.Context, any, Prior[conditionalPolicy, *conditionalPolicyOutput, any],
) error {
	r.calls.deletes.Add(1)
	return nil
}

func TestPendingPolicyWithEmptyOutputsAppliesOnce(t *testing.T) {
	var calls conditionalCalls
	libs := map[string]*Library{"core": {
		Name: "core",
		Resources: map[string]ResourceRegistration{
			"queue": MakeResource[conditionalSource, *conditionalOutput, any](
				testResourceDefinition[conditionalSource, *conditionalOutput, any](),
			),
			"policy": MakeResourceWith[conditionalPolicy, *conditionalPolicyOutput, any](
				ResourceDefinition[conditionalPolicy, *conditionalPolicyOutput, any]{
					SchemaVersion: 1,
					Replace: Replacement[conditionalPolicy, *conditionalPolicyOutput, any]{
						Fields: []AnyInputField[conditionalPolicy]{
							InputField(func(in *conditionalPolicy) *string { return &in.QueueURL }),
						},
					},
				},
				func() *conditionalPolicy { return &conditionalPolicy{calls: &calls} },
			),
		},
	}}
	dag, source := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/plan-seed", "conditional-policy"), libs)
	exec := &Executor{
		DAG: dag, SyntaxSource: source, Libraries: libs, Store: newStateStore(t),
		Factory: state.FactoryInfo{Name: "policy", Version: "v0", ContentRevision: "c0"},
		Inputs:  map[string]any{"tag": "one"},
	}
	applyOnce(t, exec)
	calls.creates.Store(0)
	exec.Inputs = map[string]any{"tag": "two"}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	step := findStep(t, plan, "resource.policy")
	require.Equal(t, map[string][]string{
		"queue-url": {"resource.queue.id"}, "policy": {"resource.queue.id"},
	}, step.UnresolvedInputs)
	require.Equal(t, []string{"queue-url"}, step.PendingReplacementReasons)
	require.Empty(t, step.ReplacementReasons)
	require.Empty(t, step.ObservedOutputs)
	events := make(chan ApplyEvent, 16)
	exec.Events = events
	_, err = planAndApplyExisting(exec, plan)
	require.NoError(t, err)
	close(events)
	require.Equal(t, DecisionNoOp,
		requireApplyEvent(t, readApplyEvents(events), "resource.policy", StageDone).Decision)
	require.Zero(t, calls.creates.Load())
	require.Zero(t, calls.updates.Load())
	require.Zero(t, calls.deletes.Load())
}
