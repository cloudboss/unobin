package resolve

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func sampleLock() *LockFile {
	return &LockFile{
		Version: CurrentLockVersion,
		Imports: map[string]*LockEntry{
			"aws": {
				Kind:           LockKindGoLibrary,
				URL:            "github.com/cloudboss/unobin-cloud",
				Subdir:         "aws",
				Constraint:     "v1.2.3",
				ResolvedCommit: "abc123def456",
				SubdirHash:     "sha256:deadbeef",
			},
			"net": {
				Kind:           LockKindUBLibrary,
				URL:            "github.com/me/libraries",
				Subdir:         "network",
				Constraint:     "v0.4.0",
				ResolvedCommit: "fed098cba321",
				SubdirHash:     "sha256:cafef00d",
			},
		},
	}
}

func TestLockRoundTrip(t *testing.T) {
	in := sampleLock()
	b, err := EncodeLockFile(in)
	require.NoError(t, err)

	out, err := DecodeLockFile(b)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestLockFileWriteRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LockFileName)

	require.NoError(t, WriteLockFile(path, sampleLock()))

	got, err := ReadLockFile(path)
	require.NoError(t, err)
	require.Equal(t, sampleLock(), got)
}

func TestLockReadMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadLockFile(filepath.Join(dir, LockFileName))
	require.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestLockEncodeStable(t *testing.T) {
	// Two encodes of the same lock produce identical bytes - keys are
	// sorted by encoding/json so map order is deterministic.
	in := sampleLock()
	a, err := EncodeLockFile(in)
	require.NoError(t, err)
	b, err := EncodeLockFile(in)
	require.NoError(t, err)
	require.Equal(t, string(a), string(b))
}

func TestLockEncodeOmitsEmptySubdir(t *testing.T) {
	lf := &LockFile{
		Version: CurrentLockVersion,
		Imports: map[string]*LockEntry{
			"utils": {
				Kind:           LockKindGoLibrary,
				URL:            "github.com/me/utils",
				Constraint:     "v0.1.0",
				ResolvedCommit: "abc",
				SubdirHash:     "sha256:1",
			},
		},
	}
	b, err := EncodeLockFile(lf)
	require.NoError(t, err)
	require.NotContains(t, string(b), `"subdir":`)
	require.Contains(t, string(b), `"subdir-hash":`)
}

func TestLockRejectsUnsupportedVersion(t *testing.T) {
	b := []byte(`{"version": 99, "imports": {}}`)
	_, err := DecodeLockFile(b)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported version")
}

func TestLockRejectsBadKind(t *testing.T) {
	b := []byte(`{
  "version": 1,
  "imports": {
    "x": {
      "kind": "bogus",
      "url": "github.com/x/y",
      "constraint": "v1",
      "resolved-commit": "a",
      "subdir-hash": "b"
    }
  }
}`)
	_, err := DecodeLockFile(b)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown kind")
}

func TestLockRejectsMissingFields(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing-kind",
			body: `{"url": "x", "constraint": "v1", "resolved-commit": "a", "subdir-hash": "b"}`,
			want: "missing `kind`",
		},
		{
			name: "missing-url",
			body: `{"kind": "go-library", "constraint": "v1", "resolved-commit": "a", "subdir-hash": "b"}`,
			want: "missing `url`",
		},
		{
			name: "missing-constraint",
			body: `{"kind": "go-library", "url": "x", "resolved-commit": "a", "subdir-hash": "b"}`,
			want: "missing `constraint`",
		},
		{
			name: "missing-commit",
			body: `{"kind": "go-library", "url": "x", "constraint": "v1", "subdir-hash": "b"}`,
			want: "missing `resolved-commit`",
		},
		{
			name: "missing-hash",
			body: `{"kind": "go-library", "url": "x", "constraint": "v1", "resolved-commit": "a"}`,
			want: "missing `subdir-hash`",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := []byte(`{"version": 1, "imports": {"x": ` + c.body + `}}`)
			_, err := DecodeLockFile(b)
			require.Error(t, err)
			require.True(t, strings.Contains(err.Error(), c.want),
				"error %q should contain %q", err.Error(), c.want)
		})
	}
}

func TestLockEncodeRejectsBadEntry(t *testing.T) {
	lf := &LockFile{
		Version: CurrentLockVersion,
		Imports: map[string]*LockEntry{
			"x": {Kind: "weird", URL: "u", Constraint: "v", ResolvedCommit: "c", SubdirHash: "h"},
		},
	}
	_, err := EncodeLockFile(lf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown kind")
}

func TestNewLockFile(t *testing.T) {
	lf := NewLockFile()
	require.Equal(t, CurrentLockVersion, lf.Version)
	require.NotNil(t, lf.Imports)
	require.Empty(t, lf.Imports)
}
