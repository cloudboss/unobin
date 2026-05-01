package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T) *LocalStore {
	t.Helper()
	s, err := NewLocalStore(t.TempDir(), "cluster-deploy", "prod-east-alpha")
	require.NoError(t, err)
	return s
}

func TestLocalStorePathLayout(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocalStore(root, "cluster-deploy", "prod")
	require.NoError(t, err)
	require.Equal(t, root, s.Root)
	require.Equal(t, "cluster-deploy", s.Stack)
	require.Equal(t, "prod", s.DeploymentID)

	sha, err := s.Write(sampleSnapshot())
	require.NoError(t, err)
	wantPath := filepath.Join(root, "cluster-deploy", "prod", "snapshots", sha+".json")
	_, err = os.Stat(wantPath)
	require.NoError(t, err)
}

func TestLocalStoreRequiresStackAndDeployment(t *testing.T) {
	root := t.TempDir()
	_, err := NewLocalStore(root, "", "prod")
	require.Error(t, err)
	_, err = NewLocalStore(root, "stack", "")
	require.Error(t, err)
}

func TestLocalStoreSiblingDeploymentsIsolated(t *testing.T) {
	root := t.TempDir()
	a, err := NewLocalStore(root, "stack", "prod")
	require.NoError(t, err)
	b, err := NewLocalStore(root, "stack", "staging")
	require.NoError(t, err)

	prodSnap := sampleSnapshot()
	prodSnap.DeploymentID = "prod"
	prodSHA, err := a.Write(prodSnap)
	require.NoError(t, err)
	require.NoError(t, a.SetCurrent(prodSHA))

	_, err = b.Current()
	require.True(t, errors.Is(err, ErrNoCurrent))
}

func TestLocalStoreCurrentEmpty(t *testing.T) {
	s := newStore(t)
	_, err := s.Current()
	require.True(t, errors.Is(err, ErrNoCurrent))
}

func TestLocalStoreWriteAndRead(t *testing.T) {
	s := newStore(t)
	snap := sampleSnapshot()

	sha, err := s.Write(snap)
	require.NoError(t, err)
	require.Len(t, sha, 64)

	got, err := s.Get(sha)
	require.NoError(t, err)
	require.Equal(t, snap, got)
}

func TestLocalStoreSetCurrent(t *testing.T) {
	s := newStore(t)
	snap := sampleSnapshot()

	sha, err := s.Write(snap)
	require.NoError(t, err)
	require.NoError(t, s.SetCurrent(sha))

	gotSHA, err := s.CurrentSHA()
	require.NoError(t, err)
	require.Equal(t, sha, gotSHA)

	got, err := s.Current()
	require.NoError(t, err)
	require.Equal(t, snap, got)
}

func TestLocalStoreSetCurrentRejectsUnknownSHA(t *testing.T) {
	s := newStore(t)
	err := s.SetCurrent("0123456789abcdef")
	require.Error(t, err)
}

func TestLocalStoreSameContentSameSHA(t *testing.T) {
	s := newStore(t)
	snap := sampleSnapshot()

	a, err := s.Write(snap)
	require.NoError(t, err)
	b, err := s.Write(snap)
	require.NoError(t, err)
	require.Equal(t, a, b)
}

func TestLocalStoreList(t *testing.T) {
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
	require.ElementsMatch(t, []string{a, b}, got)
}

func TestLocalStoreCurrentSurvivesNewWrites(t *testing.T) {
	s := newStore(t)

	first := sampleSnapshot()
	first.DeploymentID = "first"
	aSHA, err := s.Write(first)
	require.NoError(t, err)
	require.NoError(t, s.SetCurrent(aSHA))

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
