package runtime

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestApplyErrorPopulatesFailureFields(t *testing.T) {
	libs := map[string]*Library{
		"slow": {
			Name: "slow",
			Resources: map[string]ResourceRegistration{
				"fail": MakeResource[slowFailResource, any, any](),
			},
		},
	}
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-error", "failing-resource"), libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  2,
	}
	_, err := planAndApply(exec)
	require.Error(t, err)

	var ae *ApplyError
	require.True(t, errors.As(err, &ae), "want *ApplyError, got %T", err)
	assert.Equal(t, "resource.boom", ae.Address)
	assert.Equal(t, NodeResource, ae.Kind)
	assert.Equal(t, DecisionCreate, ae.Decision)
	assert.Equal(t, "slow", ae.Library)
	assert.NotNil(t, ae.Err)
	assert.Contains(t, ae.Err.Error(), "slow-fail")
}

func TestApplyErrorCountsSkippedAndSucceeded(t *testing.T) {
	libs := map[string]*Library{
		"slow": {
			Name: "slow",
			Resources: map[string]ResourceRegistration{
				"fail": MakeResource[slowFailResource, any, any](),
				"r":    MakeResource[slowResource, any, any](),
			},
		},
	}
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-error", "sibling-skipped"), libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  4,
	}
	_, err := planAndApply(exec)
	require.Error(t, err)

	var ae *ApplyError
	require.True(t, errors.As(err, &ae))
	assert.Equal(t, 1, ae.SkippedCount, "after-upstream depends on the failed step")
	assert.GreaterOrEqual(t, ae.SucceededCount, 1, "the sibling can complete alongside")
}
