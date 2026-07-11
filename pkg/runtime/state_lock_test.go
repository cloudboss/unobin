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

type stateLockGolden struct {
	Cases []stateLockCaseGolden `json:"cases"`
}

type stateLockCaseGolden struct {
	Name        string `json:"name"`
	Error       string `json:"error"`
	UnlockError bool   `json:"unlock-error"`
	UnlockCalls int    `json:"unlock-calls"`
}

func TestStateLockGolden(t *testing.T) {
	result := stateLockGolden{}
	for _, tc := range []struct {
		name       string
		acquireErr error
		operation  error
		unlockErr  error
	}{
		{name: "success"},
		{name: "acquire failure", acquireErr: errors.New("lock unavailable")},
		{name: "operation failure", operation: errors.New("operation failed")},
		{name: "unlock failure", unlockErr: errors.New("unlock failed")},
		{
			name:      "operation and unlock failure",
			operation: errors.New("operation failed"), unlockErr: errors.New("unlock failed"),
		},
	} {
		lock := &goldenStateLock{err: tc.unlockErr}
		store := &goldenLockBackend{lock: lock, err: tc.acquireErr}
		release, err := AcquireStateLock(context.Background(), store)
		if err == nil {
			err = release(tc.operation)
		}
		var unlockError *StateUnlockError
		result.Cases = append(result.Cases, stateLockCaseGolden{
			Name: tc.name, Error: runtimeErrorString(err),
			UnlockError: errors.As(err, &unlockError), UnlockCalls: lock.calls,
		})
	}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/state-lock.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

type goldenLockBackend struct {
	state.Backend
	lock state.Lock
	err  error
}

func (b *goldenLockBackend) Lock(context.Context) (state.Lock, error) {
	return b.lock, b.err
}

type goldenStateLock struct {
	err   error
	calls int
}

func (l *goldenStateLock) Unlock() error {
	l.calls++
	return l.err
}

func runtimeErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
