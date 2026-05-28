package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func runWaitFor(t *testing.T, a *WaitForAction) *WaitForActionOutput {
	t.Helper()
	res, err := a.Run(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, res)
	return res
}

func TestWaitForSucceedsImmediately(t *testing.T) {
	wr := runWaitFor(t, &WaitForAction{Argv: []string{"true"}})
	require.Equal(t, 1, wr.Attempts)
	require.True(t, wr.Duration > 0)
}

func TestWaitForRetriesUntilSuccess(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(dir, "ready")

	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = os.WriteFile(flag, []byte{}, 0o644)
	}()

	wr := runWaitFor(t, &WaitForAction{
		Argv:     []string{"test", "-e", flag},
		Interval: 20 * time.Millisecond,
		Timeout:  2 * time.Second,
	})
	require.Greater(t, wr.Attempts, 1)
}

func TestWaitForTimesOut(t *testing.T) {
	_, err := (&WaitForAction{
		Argv:     []string{"false"},
		Interval: 10 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
	}).Run(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timed out")
}

func TestWaitForRequiresArgv(t *testing.T) {
	_, err := (&WaitForAction{}).Run(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "argv is required")
}

func TestWaitForAbortsOnStartFailure(t *testing.T) {
	_, err := (&WaitForAction{
		Argv:    []string{"unobin-no-such-binary-xyz"},
		Timeout: time.Second,
	}).Run(context.Background(), nil)
	require.Error(t, err)
}

func TestWaitForContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := (&WaitForAction{
		Argv:     []string{"false"},
		Interval: 5 * time.Millisecond,
		Timeout:  5 * time.Second,
	}).Run(ctx, nil)
	require.Error(t, err)
}

func TestCoreModuleRegistersWaitFor(t *testing.T) {
	at, ok := Library().Actions["wait-for"]
	require.True(t, ok)
	_, ok = at.NewReceiver().(*WaitForAction)
	require.True(t, ok)
}
