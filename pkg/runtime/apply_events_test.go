package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type plainResource struct {
	Name string
}

func (r *plainResource) SchemaVersion() int { return 1 }

func (r *plainResource) Create(_ context.Context, _ any) (any, error) {
	return map[string]any{"name": r.Name}, nil
}
func (r *plainResource) Read(_ context.Context, _, _ any) (any, error) {
	return nil, ErrNotFound
}
func (r *plainResource) Update(
	_ context.Context, _ any, _ Prior[plainResource, any],
) (any, error) {
	return map[string]any{"name": r.Name}, nil
}
func (r *plainResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *plainResource) ReplaceFields() []string                  { return nil }

type plainFailResource struct {
	Name string
}

func (r *plainFailResource) SchemaVersion() int { return 1 }

func (r *plainFailResource) Create(_ context.Context, _ any) (any, error) {
	return nil, errors.New("boom")
}
func (r *plainFailResource) Read(_ context.Context, _, _ any) (any, error) {
	return nil, ErrNotFound
}
func (r *plainFailResource) Update(
	_ context.Context, _ any, _ Prior[plainFailResource, any],
) (any, error) {
	return nil, errors.New("unreachable")
}
func (r *plainFailResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *plainFailResource) ReplaceFields() []string                  { return nil }

func TestApplyEventsEmitsStartAndDonePerSuccessfulStep(t *testing.T) {
	libs := map[string]*Library{
		"r": {
			Name: "r",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResource[plainResource, any](),
			},
		},
	}
	src := `
resources: { one: r.thing { name: 'one' }, two: r.thing { name: 'two' } }
`
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
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
				"thing": MakeResource[plainFailResource, any](),
			},
		},
	}
	src := `
resources: { bad: r.thing { name: 'bad' } }
`
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
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
