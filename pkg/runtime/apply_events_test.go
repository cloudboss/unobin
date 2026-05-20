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
	Name string `mapstructure:"name"`
}

func (r *plainResource) SchemaVersion() int { return 1 }

func (r *plainResource) Create(_ context.Context, _ any) (any, error) {
	return map[string]any{"name": r.Name}, nil
}
func (r *plainResource) Read(_ context.Context, _, _ any) (any, error) {
	return nil, ErrNotFound
}
func (r *plainResource) Update(_ context.Context, _, _ any) (any, error) {
	return map[string]any{"name": r.Name}, nil
}
func (r *plainResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *plainResource) ReplaceFields() []string                  { return nil }

type plainFailResource struct {
	Name string `mapstructure:"name"`
}

func (r *plainFailResource) SchemaVersion() int { return 1 }

func (r *plainFailResource) Create(_ context.Context, _ any) (any, error) {
	return nil, errors.New("boom")
}
func (r *plainFailResource) Read(_ context.Context, _, _ any) (any, error) {
	return nil, ErrNotFound
}
func (r *plainFailResource) Update(_ context.Context, _, _ any) (any, error) {
	return nil, errors.New("unreachable")
}
func (r *plainFailResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *plainFailResource) ReplaceFields() []string                  { return nil }

func TestApplyEventsEmitsStartAndDonePerSuccessfulStep(t *testing.T) {
	mods := map[string]*Module{
		"r": {
			Name: "r",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResource[plainResource, any](),
			},
		},
	}
	src := `
resources: {
  r: {
    thing: {
      one: { name: 'one' }
      two: { name: 'two' }
    }
  }
}
`
	events := make(chan ApplyEvent, 32)
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src), mods),
		Modules:     mods,
		Store:       newStateStore(t),
		Stack:       state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
		Parallelism: 2,
		Events:      events,
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
	assert.Equal(t, 1, starts["resource.r.thing.one"])
	assert.Equal(t, 1, dones["resource.r.thing.one"])
	assert.Equal(t, 1, starts["resource.r.thing.two"])
	assert.Equal(t, 1, dones["resource.r.thing.two"])
}

func TestApplyEventsEmitsFailEvent(t *testing.T) {
	mods := map[string]*Module{
		"r": {
			Name: "r",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResource[plainFailResource, any](),
			},
		},
	}
	src := `
resources: {
  r: {
    thing: {
      bad: { name: 'bad' }
    }
  }
}
`
	events := make(chan ApplyEvent, 8)
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src), mods),
		Modules:     mods,
		Store:       newStateStore(t),
		Stack:       state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
		Parallelism: 2,
		Events:      events,
	}
	_, err := planAndApply(exec)
	close(events)
	require.Error(t, err)

	var sawStart, sawFail bool
	for ev := range events {
		switch ev.Stage {
		case StageStart:
			if ev.Address == "resource.r.thing.bad" {
				sawStart = true
			}
		case StageFail:
			if ev.Address == "resource.r.thing.bad" {
				sawFail = true
				assert.NotNil(t, ev.Err)
			}
		}
	}
	assert.True(t, sawStart)
	assert.True(t, sawFail)
}
