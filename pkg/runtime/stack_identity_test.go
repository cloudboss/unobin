package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type stackIdentityBackend struct {
	state.Backend
	lockCalls int
}

func (s *stackIdentityBackend) Lock(ctx context.Context) (state.Lock, error) {
	s.lockCalls++
	return s.Backend.Lock(ctx)
}

func TestApplyPlanStackIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stack string
		valid bool
	}{
		{name: "matching stack", stack: "prod", valid: true},
		{name: "different stack", stack: "other"},
		{name: "missing stack"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &stackIdentityBackend{Backend: newStateStore(t)}
			executor, plan := emptyApplyExecutor(t, store)
			plan.Stack = tc.stack
			result, err := executor.ApplyPlan(t.Context(), plan)
			if tc.valid {
				require.NoError(t, err)
				assert.Equal(t, 1, store.lockCalls)
				require.NotNil(t, result)
				snapshot, err := store.Current()
				require.NoError(t, err)
				assert.Equal(t, "prod", snapshot.Stack)
				return
			}
			require.ErrorContains(t, err, "stack")
			assert.Zero(t, store.lockCalls)
			assert.Nil(t, result)
			failure, ok := AsApplyFailure(err)
			require.True(t, ok)
			assert.Equal(t, ApplyFailureSetup, failure.Stage)
			_, err = store.CurrentRev()
			assert.ErrorIs(t, err, state.ErrNoCurrent)
		})
	}
}
