package localstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/envencrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T) *LocalStore {
	t.Helper()
	s, err := NewLocalStore(t.TempDir(), "cluster-deploy", "prod-east-alpha", envencrypt.Noop{})
	require.NoError(t, err)
	return s
}

func TestLocalStorePathLayout(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocalStore(root, "cluster-deploy", "prod", envencrypt.Noop{})
	require.NoError(t, err)
	require.Equal(t, root, s.Root)
	require.Equal(t, "cluster-deploy", s.Stack)
	require.Equal(t, "prod", s.DeploymentID())

	rev, err := s.Write(sampleSnapshot())
	require.NoError(t, err)
	wantPath := filepath.Join(root, "cluster-deploy", "prod", "snapshots", rev+".json.enc")
	_, err = os.Stat(wantPath)
	require.NoError(t, err)
}

func TestLocalStoreRequiresStackAndDeployment(t *testing.T) {
	root := t.TempDir()
	_, err := NewLocalStore(root, "", "prod", envencrypt.Noop{})
	require.Error(t, err)
	_, err = NewLocalStore(root, "stack", "", envencrypt.Noop{})
	require.Error(t, err)
}

func TestLocalStoreRequiresEncrypter(t *testing.T) {
	_, err := NewLocalStore(t.TempDir(), "stack", "prod", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "encrypter")
}

func TestLocalStoreSiblingDeploymentsIsolated(t *testing.T) {
	root := t.TempDir()
	a, err := NewLocalStore(root, "stack", "prod", envencrypt.Noop{})
	require.NoError(t, err)
	b, err := NewLocalStore(root, "stack", "staging", envencrypt.Noop{})
	require.NoError(t, err)

	prodSnap := sampleSnapshot()
	prodSnap.DeploymentID = "prod"
	rev, err := a.Write(prodSnap)
	require.NoError(t, err)
	require.NoError(t, a.SetCurrent(rev))

	_, err = b.Current()
	require.True(t, errors.Is(err, sdkstate.ErrNoCurrent))
}

func TestLocalStoreCurrentEmpty(t *testing.T) {
	s := newStore(t)
	_, err := s.Current()
	require.True(t, errors.Is(err, sdkstate.ErrNoCurrent))
}

func TestLocalStoreWriteAndRead(t *testing.T) {
	s := newStore(t)
	snap := sampleSnapshot()

	rev, err := s.Write(snap)
	require.NoError(t, err)
	require.NotEmpty(t, rev)

	got, err := s.Get(rev)
	require.NoError(t, err)
	require.Equal(t, snap, got)
}

func TestLocalStoreSetCurrent(t *testing.T) {
	s := newStore(t)
	snap := sampleSnapshot()

	rev, err := s.Write(snap)
	require.NoError(t, err)
	require.NoError(t, s.SetCurrent(rev))

	gotRev, err := s.CurrentRev()
	require.NoError(t, err)
	require.Equal(t, rev, gotRev)

	got, err := s.Current()
	require.NoError(t, err)
	require.Equal(t, snap, got)
}

func TestLocalStoreSetCurrentRejectsUnknownRev(t *testing.T) {
	s := newStore(t)
	err := s.SetCurrent("2026-05-01T00:00:00Z")
	require.Error(t, err)
}

func TestLocalStoreDelete(t *testing.T) {
	s := newStore(t)
	rev, err := s.Write(sampleSnapshot())
	require.NoError(t, err)

	require.NoError(t, s.Delete(rev))
	_, err = s.Get(rev)
	require.Error(t, err)

	require.NoError(t, s.Delete(rev), "deleting an absent rev should be a no-op")
}

func TestLocalStoreSameContentDistinctRevs(t *testing.T) {
	s := newStore(t)
	snap := sampleSnapshot()

	a, err := s.Write(snap)
	require.NoError(t, err)
	b, err := s.Write(snap)
	require.NoError(t, err)
	require.NotEqual(t, a, b, "two writes should yield two distinct revs")
}

func TestLocalStoreDistinctRevsWhenClockStandsStill(t *testing.T) {
	frozen := time.Date(2026, 5, 12, 15, 30, 0, 0, time.UTC)
	t.Cleanup(func() { now = time.Now })
	now = func() time.Time { return frozen }

	s := newStore(t)
	seen := map[string]bool{}
	for range 5 {
		rev, err := s.Write(sampleSnapshot())
		require.NoError(t, err)
		require.False(t, seen[rev], "rev %q reused while clock was frozen", rev)
		seen[rev] = true
	}

	require.Equal(t, []string{
		frozen.Format(time.RFC3339Nano),
		frozen.Format(time.RFC3339Nano) + "_1",
		frozen.Format(time.RFC3339Nano) + "_2",
		frozen.Format(time.RFC3339Nano) + "_3",
		frozen.Format(time.RFC3339Nano) + "_4",
	}, mustList(t, s))
}

func TestLocalStoreListChronological(t *testing.T) {
	s := newStore(t)
	require.Empty(t, mustList(t, s))

	first := sampleSnapshot()
	first.DeploymentID = "first"
	a, err := s.Write(first)
	require.NoError(t, err)

	second := sampleSnapshot()
	second.DeploymentID = "second"
	b, err := s.Write(second)
	require.NoError(t, err)

	got := mustList(t, s)
	require.Equal(t, []string{a, b}, got, "List should return revs in chronological order")
}

func TestLocalStoreCurrentSurvivesNewWrites(t *testing.T) {
	s := newStore(t)

	first := sampleSnapshot()
	first.DeploymentID = "first"
	rev, err := s.Write(first)
	require.NoError(t, err)
	require.NoError(t, s.SetCurrent(rev))

	second := sampleSnapshot()
	second.DeploymentID = "second"
	_, err = s.Write(second)
	require.NoError(t, err)

	got, err := s.Current()
	require.NoError(t, err)
	require.Equal(t, "first", got.DeploymentID)
}

func mustList(t *testing.T, s *LocalStore) []string {
	t.Helper()
	got, err := s.List()
	require.NoError(t, err)
	return got
}

func TestLocalStoreWithEnvKeyEncrypter(t *testing.T) {
	setKey(t, "UB_TEST_KEY")
	enc, err := envencrypt.NewEnvKey("UB_TEST_KEY")
	require.NoError(t, err)

	s, err := NewLocalStore(t.TempDir(), "stack", "prod", enc)
	require.NoError(t, err)

	snap := sampleSnapshot()
	rev, err := s.Write(snap)
	require.NoError(t, err)

	onDisk, err := os.ReadFile(filepath.Join(s.dir, "snapshots", rev+".json.enc"))
	require.NoError(t, err)
	require.NotContains(t, string(onDisk), "cluster-deploy")
	require.NotContains(t, string(onDisk), "vpc-abc")

	got, err := s.Get(rev)
	require.NoError(t, err)
	require.Equal(t, snap, got)
}

func TestLocalStoreLockExcludesSecondHolder(t *testing.T) {
	s := newStore(t)
	first, err := s.Lock(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Unlock() })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = s.Lock(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestLocalStoreLockReacquiresAfterUnlock(t *testing.T) {
	s := newStore(t)
	first, err := s.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, first.Unlock())

	second, err := s.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, second.Unlock())
}

func TestLocalStoreLockBlocksUntilReleased(t *testing.T) {
	s := newStore(t)
	first, err := s.Lock(context.Background())
	require.NoError(t, err)

	got := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		l, err := s.Lock(ctx)
		if err == nil {
			_ = l.Unlock()
		}
		got <- err
	}()

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, first.Unlock())

	select {
	case err := <-got:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("second Lock did not return after first released")
	}
}

func TestLocalStoreForceUnlockClearsLock(t *testing.T) {
	s := newStore(t)
	_, err := s.Lock(context.Background())
	require.NoError(t, err)

	require.NoError(t, s.ForceUnlock())

	again, err := s.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, again.Unlock())
}

func TestLocalStoreForceUnlockNoLockIsOK(t *testing.T) {
	s := newStore(t)
	require.NoError(t, s.ForceUnlock())
}

func TestLocalStoreWrongKeyCantDecrypt(t *testing.T) {
	root := t.TempDir()

	setKey(t, "UB_TEST_KEY_A")
	encA, err := envencrypt.NewEnvKey("UB_TEST_KEY_A")
	require.NoError(t, err)
	a, err := NewLocalStore(root, "stack", "prod", encA)
	require.NoError(t, err)
	rev, err := a.Write(sampleSnapshot())
	require.NoError(t, err)

	setKey(t, "UB_TEST_KEY_B")
	encB, err := envencrypt.NewEnvKey("UB_TEST_KEY_B")
	require.NoError(t, err)
	b, err := NewLocalStore(root, "stack", "prod", encB)
	require.NoError(t, err)

	_, err = b.Get(rev)
	require.Error(t, err)
	require.Contains(t, err.Error(), "decrypt")
}
