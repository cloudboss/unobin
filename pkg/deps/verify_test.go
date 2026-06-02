package deps

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyMatchingHash(t *testing.T) {
	lock := NewLock()
	lock.Deps["github.com/x/y//lib"] = &LockedDep{
		Kind: LockKindUB, Version: "v1.0.0", Commit: "c1", Hash: "h1",
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "lib", "c1"): {Hash: "h1"},
	}}
	mismatches, err := Verify(lock, r)
	require.NoError(t, err)
	assert.Empty(t, mismatches)
}

func TestVerifyDetectsMismatch(t *testing.T) {
	lock := NewLock()
	lock.Deps["github.com/x/y//lib"] = &LockedDep{
		Kind: LockKindUB, Version: "v1.0.0", Commit: "c1", Hash: "h1",
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "lib", "c1"): {Hash: "tampered"},
	}}
	mismatches, err := Verify(lock, r)
	require.NoError(t, err)
	require.Len(t, mismatches, 1)
	assert.Contains(t, mismatches[0], "github.com/x/y//lib")
	assert.Contains(t, mismatches[0], "hash mismatch")
}

func TestVerifySkipsGoEntries(t *testing.T) {
	lock := NewLock()
	lock.Deps["github.com/x/golib"] = &LockedDep{Kind: LockKindGo, Version: "v1.0.0", Commit: "c1"}
	// An empty resolver: if Verify tried to fetch the go entry it would
	// error, so an empty result proves go entries are skipped.
	r := &fakeResolver{sources: map[string]*resolve.Source{}}
	mismatches, err := Verify(lock, r)
	require.NoError(t, err)
	assert.Empty(t, mismatches)
}

func TestVerifyResolveError(t *testing.T) {
	lock := NewLock()
	lock.Deps["github.com/x/y//lib"] = &LockedDep{
		Kind: LockKindUB, Version: "v1.0.0", Commit: "c1", Hash: "h1",
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{}}
	_, err := Verify(lock, r)
	require.Error(t, err)
}
