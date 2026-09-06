package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

func TestStateRefV2PreservesProviderArguments(t *testing.T) {
	body := map[string]any{
		"directory": "state", "attempts": int64(3), "delay": 1.5,
		"labels": map[string]any{"1": "first"}, "options": []any{true, nil},
	}
	ref, err := NewStateRefV2("local", body)
	require.NoError(t, err)
	require.NoError(t, ref.Validate())
	values, err := ref.Values()
	require.NoError(t, err)
	require.Equal(t, body, values)
	values["directory"] = "changed"
	again, err := ref.Values()
	require.NoError(t, err)
	require.Equal(t, "state", again["directory"])
}

func TestStateRefV2RejectsPendingArguments(t *testing.T) {
	ref, err := NewStateRefV2("local", map[string]any{
		"directory": PendingValue{Refs: []string{"resource.disk.path"}},
	})
	require.ErrorContains(t, err, "state backend body must be concrete")
	require.Nil(t, ref)
}

func TestPlanV2StoresBackendBeforeSealing(t *testing.T) {
	executor, snapshot := newStateMoveV2Executor(t)
	executor.Store, executor.Factory = newStateStore(t), snapshot.Factory
	executor.Inputs = map[string]any{"name": "server", "size": int64(1)}
	backend, err := NewStateRefV2("local", map[string]any{"directory": "state"})
	require.NoError(t, err)
	executor.PlanBackend = backend
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	require.Equal(t, backend, plan.Backend)
	bytes, err := SealPlanV2(*plan, encrypters.Noop{})
	require.NoError(t, err)
	opened, err := OpenPlanV2(bytes, func(*StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, *plan, opened)
	require.NoError(t, opened.Validate())
}
