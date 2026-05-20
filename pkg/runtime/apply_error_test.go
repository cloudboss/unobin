package runtime

import (
	"errors"
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyErrorPopulatesFailureFields(t *testing.T) {
	mods := map[string]*Module{
		"slow": {
			Name: "slow",
			Resources: map[string]ResourceRegistration{
				"fail": MakeResource[slowFailResource, any](),
			},
		},
	}
	src := `
resources: {
  slow: {
    fail: {
      boom: { name: 'boom', delay-ms: 5 }
    }
  }
}
`
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src), mods),
		Modules:     mods,
		Store:       newStateStore(t),
		Stack:       state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
		Parallelism: 2,
	}
	_, err := planAndApply(exec)
	require.Error(t, err)

	var ae *ApplyError
	require.True(t, errors.As(err, &ae), "want *ApplyError, got %T", err)
	assert.Equal(t, "resource.slow.fail.boom", ae.Address)
	assert.Equal(t, NodeResource, ae.Kind)
	assert.Equal(t, DecisionCreate, ae.Decision)
	assert.Equal(t, "slow", ae.Module)
	assert.NotNil(t, ae.Err)
	assert.Contains(t, ae.Err.Error(), "slow-fail")
}

func TestApplyErrorCountsSkippedAndSucceeded(t *testing.T) {
	mods := map[string]*Module{
		"slow": {
			Name: "slow",
			Resources: map[string]ResourceRegistration{
				"fail": MakeResource[slowFailResource, any](),
				"r":    MakeResource[slowResource, any](),
			},
		},
	}
	src := `
resources: {
  slow: {
    fail: {
      upstream: { name: 'upstream', delay-ms: 5 }
    }
    r: {
      sibling: { name: 'sibling', delay-ms: 5 }
      after-upstream: {
        name:     resource.slow.fail.upstream.name
        delay-ms: 5
      }
    }
  }
}
`
	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src), mods),
		Modules:     mods,
		Store:       newStateStore(t),
		Stack:       state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
		Parallelism: 4,
	}
	_, err := planAndApply(exec)
	require.Error(t, err)

	var ae *ApplyError
	require.True(t, errors.As(err, &ae))
	assert.Equal(t, 1, ae.SkippedCount, "after-upstream depends on the failed step")
	assert.GreaterOrEqual(t, ae.SucceededCount, 1, "the sibling can complete alongside")
}
