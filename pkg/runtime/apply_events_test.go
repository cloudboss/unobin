package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type plainResource struct {
	Name string
}

type plainResourceOutput struct{ Name string }

func (r *plainResource) SchemaVersion() int { return 1 }

func (r *plainResource) Create(_ context.Context, _ any) (*plainResourceOutput, error) {
	return &plainResourceOutput{Name: r.Name}, nil
}
func (r *plainResource) Read(
	_ context.Context,
	_ any,
	_ *plainResourceOutput,
) (*plainResourceOutput, error) {
	return nil, ErrNotFound
}
func (r *plainResource) Update(
	_ context.Context, _ any, _ Prior[plainResource, *plainResourceOutput],
) (*plainResourceOutput, error) {
	return &plainResourceOutput{Name: r.Name}, nil
}
func (r *plainResource) Delete(_ context.Context, _ any, _ *plainResourceOutput) error {
	return nil
}
func (r *plainResource) ReplaceFields() []string { return nil }

type plainFailResource struct {
	Name string
}

type plainFailResourceOutput struct{ Name string }

func (r *plainFailResource) SchemaVersion() int { return 1 }

func (r *plainFailResource) Create(_ context.Context, _ any) (*plainFailResourceOutput, error) {
	return nil, errors.New("boom")
}
func (r *plainFailResource) Read(
	_ context.Context,
	_ any,
	_ *plainFailResourceOutput,
) (*plainFailResourceOutput, error) {
	return nil, ErrNotFound
}
func (r *plainFailResource) Update(
	_ context.Context, _ any, _ Prior[plainFailResource, *plainFailResourceOutput],
) (*plainFailResourceOutput, error) {
	return nil, errors.New("unreachable")
}
func (r *plainFailResource) Delete(_ context.Context, _ any, _ *plainFailResourceOutput) error {
	return nil
}
func (r *plainFailResource) ReplaceFields() []string { return nil }

func TestApplyEventsEmitsStartAndDonePerSuccessfulStep(t *testing.T) {
	libs := map[string]*Library{
		"r": {
			Name: "r",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResource[plainResource, *plainResourceOutput, any](),
			},
		},
	}
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-events", "successful-steps"), libs)
	events := make(chan ApplyEvent, 32)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  2,
		Events:       events,
	}
	_, err := planAndApply(exec)
	close(events)
	require.NoError(t, err)

	starts := map[string]int{}
	dones := map[string]int{}
	for ev := range events {
		switch ev.Stage {
		case StageStart:
			starts[ev.Address]++
		case StageDone:
			dones[ev.Address]++
		case StageFail:
			t.Fatalf("unexpected fail event: %+v", ev)
		}
	}
	assert.Equal(t, 1, starts["resource.one"])
	assert.Equal(t, 1, dones["resource.one"])
	assert.Equal(t, 1, starts["resource.two"])
	assert.Equal(t, 1, dones["resource.two"])
}

func TestApplyEventsEmitsFailEvent(t *testing.T) {
	libs := map[string]*Library{
		"r": {
			Name: "r",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResource[plainFailResource, *plainFailResourceOutput, any](),
			},
		},
	}
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-events", "fail-event"), libs)
	events := make(chan ApplyEvent, 8)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  2,
		Events:       events,
	}
	_, err := planAndApply(exec)
	close(events)
	require.Error(t, err)

	var sawStart, sawFail bool
	for ev := range events {
		switch ev.Stage {
		case StageStart:
			if ev.Address == "resource.bad" {
				sawStart = true
			}
		case StageFail:
			if ev.Address == "resource.bad" {
				sawFail = true
				assert.NotNil(t, ev.Err)
			}
		}
	}
	assert.True(t, sawStart)
	assert.True(t, sawFail)
}
