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
	source := refreshFixture(t, "resource-one")
	var counters resourceCounters
	store := newStateStore(t)
	libraries := resourceModules(&counters)
	factory := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, refreshTestExecutor(t, source, libraries, store, factory))
	wrapped := &unlockFailureBackend{Backend: store}
	result, err := refreshTestExecutor(t, source, libraries, wrapped, factory).
		Refresh(context.Background())
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
	store := newStateStore(t)
	wrapped := &unlockFailureBackend{Backend: store}
	factory := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	result, err := refreshTestExecutor(
		t, refreshFixture(t, "empty"), map[string]*Library{}, wrapped, factory,
	).Refresh(context.Background())
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
