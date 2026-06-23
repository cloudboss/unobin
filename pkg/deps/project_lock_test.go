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

	"github.com/cloudboss/unobin/internal/ubtest"
)

func sampleProjectLockBase() *ProjectLock {
	l := NewProjectLock()
	l.Deps["github.com/cloudboss/unobin//pkg/libraries/core"] = &ProjectLockDep{
		Kind: ProjectLockKindUB, Version: "v0.1.0", Commit: "abc123", Hash: "sha256:deadbeef",
	}
	l.Deps["github.com/aws/some-go-lib"] = &ProjectLockDep{
		Kind: ProjectLockKindGo, Version: "v1.2.3", Commit: "def456",
	}
	return l
}

func sampleProjectLock() *ProjectLock {
	l := sampleProjectLockBase()
	l.ToolchainVersion = "v0.4.2"
	return l
}

func TestEncodeProjectLockWholeOutput(t *testing.T) {
	want := `project-lock: {
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
	got, err := EncodeProjectLock(sampleProjectLock())
	require.NoError(t, err)
	assert.Equal(t, want, string(got))
}

func TestEncodeProjectLockRejectsUnprefixedHash(t *testing.T) {
	projectLock := sampleProjectLock()
	projectLock.Deps["github.com/cloudboss/unobin//pkg/libraries/core"].Hash = "deadbeef"

	_, err := EncodeProjectLock(projectLock)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash must include an algorithm prefix")
}

func TestReplacementSentinelHelpers(t *testing.T) {
	assert.Equal(t, "v0.0.0-unobin-replaced", ReplacementSentinel)
	assert.True(t, IsReplacementSentinel("v0.0.0-unobin-replaced"))
	assert.False(t, IsReplacementSentinel("v0.0.1"))

	got, err := GoReplacementSentinel("example.com/lib")
	require.NoError(t, err)
	assert.Equal(t, "v0.0.0-unobin-replaced", got)

	got, err = GoReplacementSentinel("example.com/lib/v2")
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0-unobin-replaced", got)
}

func TestEncodeProjectLockRejectsReplacementSentinel(t *testing.T) {
	projectLock := sampleProjectLock()
	projectLock.Deps["github.com/aws/some-go-lib"].Version = ReplacementSentinel

	_, err := EncodeProjectLock(projectLock)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "v0.0.0-unobin-replaced is reserved")
}

func TestReadProjectLockRejectsReplacementSentinel(t *testing.T) {
	src := ubtest.ReadFixture(t, "testdata/ub/project-lock/invalid/replacement-sentinel.ub")
	_, err := DecodeProjectLock([]byte(src))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "v0.0.0-unobin-replaced is reserved")
}

func TestProjectLockCodec(t *testing.T) {
	b, err := EncodeProjectLock(sampleProjectLock())
	require.NoError(t, err)
	got, err := DecodeProjectLock(b)
	require.NoError(t, err)
	assert.Equal(t, sampleProjectLock(), got)

	b2, err := EncodeProjectLock(sampleProjectLock())
	require.NoError(t, err)
	assert.Equal(t, b, b2, "encoding must be deterministic")
}

func TestReadProjectLockUsesProjectLockFile(t *testing.T) {
	got, err := ReadProjectLock(fstest.MapFS{
		ProjectLockFileName: &fstest.MapFile{Data: []byte(
			ubtest.ReadValidFixture(t, "testdata/ub/project-lock", "basic"),
		)},
	})
	require.NoError(t, err)
	assert.Equal(t, &ProjectLock{
		Version:          1,
		ToolchainVersion: "v0.4.2",
		Deps:             map[string]*ProjectLockDep{},
	}, got)
}

func TestWriteAndReadProjectLock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteProjectLock(filepath.Join(dir, ProjectLockFileName), sampleProjectLock()))
	got, err := ReadProjectLock(os.DirFS(dir))
	require.NoError(t, err)
	assert.Equal(t, sampleProjectLock(), got)
}

func TestReadProjectLockMissing(t *testing.T) {
	_, err := ReadProjectLock(fstest.MapFS{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestProjectLockRepoVersions(t *testing.T) {
	l := NewProjectLock()
	l.Deps["github.com/x/y"] = &ProjectLockDep{Kind: ProjectLockKindGo, Version: "v0.9.0", Commit: "c"}
	l.Deps["github.com/x/y//a"] = &ProjectLockDep{Kind: ProjectLockKindGo, Version: "v1.0.0", Commit: "c"}
	l.Deps["github.com/x/y//b"] = &ProjectLockDep{Kind: ProjectLockKindGo, Version: "v1.1.0", Commit: "c"}
	l.Deps["github.com/z/w//ub"] = &ProjectLockDep{
		Kind: ProjectLockKindUB, Version: "v2.0.0", Commit: "c", Hash: "h",
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
