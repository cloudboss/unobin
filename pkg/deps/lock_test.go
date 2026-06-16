package deps

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleLock() *Lock {
	l := NewLock()
	l.Deps["github.com/cloudboss/unobin//pkg/libraries/core"] = &LockedDep{
		Kind: LockKindUB, Version: "v0.1.0", Commit: "abc123", Hash: "sha256:deadbeef",
	}
	l.Deps["github.com/aws/some-go-lib"] = &LockedDep{
		Kind: LockKindGo, Version: "v1.2.3", Commit: "def456",
	}
	return l
}

func sampleSourceLock() *Lock {
	l := sampleLock()
	l.ToolchainVersion = "v0.4.2"
	return l
}

func TestEncodeSourceLockWholeOutput(t *testing.T) {
	want := `lock: {
  version:   1
  toolchain: { unobin-version: 'v0.4.2' }
  deps: {
    'github.com/aws/some-go-lib': { kind: go, version: 'v1.2.3', commit: 'def456' }
    'github.com/cloudboss/unobin//pkg/libraries/core': {
      kind:    ub
      version: 'v0.1.0'
      commit:  'abc123'
      hash:    'sha256:deadbeef'
    }
  }
}
`
	got, err := EncodeSourceLock(sampleSourceLock())
	require.NoError(t, err)
	assert.Equal(t, want, string(got))
}

func TestEncodeSourceLockRejectsUnprefixedHash(t *testing.T) {
	lock := sampleSourceLock()
	lock.Deps["github.com/cloudboss/unobin//pkg/libraries/core"].Hash = "deadbeef"

	_, err := EncodeSourceLock(lock)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash must include an algorithm prefix")
}

func TestSourceLockCodec(t *testing.T) {
	b, err := EncodeSourceLock(sampleSourceLock())
	require.NoError(t, err)
	got, err := DecodeSourceLock(b)
	require.NoError(t, err)
	assert.Equal(t, sampleSourceLock(), got)

	b2, err := EncodeSourceLock(sampleSourceLock())
	require.NoError(t, err)
	assert.Equal(t, b, b2, "encoding must be deterministic")
}

func TestWriteAndReadSourceLock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteSourceLock(filepath.Join(dir, SourceLockFileName), sampleSourceLock()))
	got, err := ReadLock(os.DirFS(dir))
	require.NoError(t, err)
	assert.Equal(t, sampleSourceLock(), got)
}

func TestReadLockMissing(t *testing.T) {
	_, err := ReadLock(fstest.MapFS{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestLockRepoVersions(t *testing.T) {
	l := NewLock()
	l.Deps["github.com/x/y"] = &LockedDep{Kind: LockKindGo, Version: "v0.9.0", Commit: "c"}
	l.Deps["github.com/x/y//a"] = &LockedDep{Kind: LockKindGo, Version: "v1.0.0", Commit: "c"}
	l.Deps["github.com/x/y//b"] = &LockedDep{Kind: LockKindGo, Version: "v1.1.0", Commit: "c"}
	l.Deps["github.com/z/w//ub"] = &LockedDep{
		Kind: LockKindUB, Version: "v2.0.0", Commit: "c", Hash: "h",
	}
	got, err := l.RepoVersions()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"github.com/x/y":     "v0.9.0",
		"github.com/x/y//a":  "v1.0.0",
		"github.com/x/y//b":  "v1.1.0",
		"github.com/z/w//ub": "v2.0.0",
	}, got)
}
