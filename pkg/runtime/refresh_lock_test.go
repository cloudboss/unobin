package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type refreshLockGolden struct {
	Cases []refreshLockCaseGolden `json:"cases"`
}

type refreshLockCaseGolden struct {
	Name               string `json:"name"`
	ResultNonNull      bool   `json:"result-non-null"`
	Refreshed          int    `json:"refreshed"`
	Removed            int    `json:"removed"`
	WrittenRevision    bool   `json:"written-revision"`
	CurrentMatches     bool   `json:"current-matches"`
	Error              string `json:"error"`
	UnlockError        bool   `json:"unlock-error"`
	UnderlyingUnlocked bool   `json:"underlying-unlocked"`
}

func TestRefreshLockFailureGolden(t *testing.T) {
	result := refreshLockGolden{}
	result.Cases = append(result.Cases, refreshAfterWriteUnlockFailure(t))
	result.Cases = append(result.Cases, refreshWithoutWriteUnlockFailure(t))
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/refresh-lock.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func refreshAfterWriteUnlockFailure(t *testing.T) refreshLockCaseGolden {
	t.Helper()
	executor := newFactoryApplyExecutor(t, &factoryApplyCapture{})
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	_, err = executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	store := executor.Store
	wrapped := &refreshV2UnlockBackend{
		unlockFailureBackend: &unlockFailureBackend{Backend: store},
		SnapshotBackendV2:    store.(state.SnapshotBackendV2),
	}
	executor.Store = wrapped
	result, err := executor.RefreshV2(context.Background())
	current, currentErr := store.CurrentRev()
	require.NoError(t, currentErr)
	var unlockError *StateUnlockError
	return refreshLockCaseGolden{
		Name: "after state write", ResultNonNull: result != nil,
		Refreshed: result.Refreshed, Removed: result.Dropped,
		WrittenRevision: result.WrittenRev != "",
		CurrentMatches:  result.WrittenRev == current,
		Error:           runtimeErrorString(err), UnlockError: errors.As(err, &unlockError),
		UnderlyingUnlocked: wrapped.unlocked,
	}
}

func refreshWithoutWriteUnlockFailure(t *testing.T) refreshLockCaseGolden {
	t.Helper()
	executor := newFactoryApplyExecutor(t, &factoryApplyCapture{})
	wrapped := &refreshV2UnlockBackend{
		unlockFailureBackend: &unlockFailureBackend{Backend: executor.Store},
		SnapshotBackendV2:    executor.Store.(state.SnapshotBackendV2),
	}
	executor.Store = wrapped
	result, err := executor.RefreshV2(context.Background())
	var unlockError *StateUnlockError
	return refreshLockCaseGolden{
		Name: "without state write", ResultNonNull: result != nil,
		Refreshed: result.Refreshed, Removed: result.Dropped,
		WrittenRevision: result.WrittenRev != "",
		Error:           runtimeErrorString(err), UnlockError: errors.As(err, &unlockError),
		UnderlyingUnlocked: wrapped.unlocked,
	}
}

type unlockFailureBackend struct {
	state.Backend
	unlocked bool
}

func (b *unlockFailureBackend) Lock(ctx context.Context) (state.Lock, error) {
	lock, err := b.Backend.Lock(ctx)
	if err != nil {
		return nil, err
	}
	return &unlockFailureLock{Lock: lock, backend: b}, nil
}

type unlockFailureLock struct {
	state.Lock
	backend *unlockFailureBackend
}

func (l *unlockFailureLock) Unlock() error {
	if err := l.Lock.Unlock(); err != nil {
		return err
	}
	l.backend.unlocked = true
	return errors.New("unlock failed")
}
