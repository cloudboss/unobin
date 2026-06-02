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

func TestEncodeLockWholeOutput(t *testing.T) {
	want := `{
  "version": 1,
  "deps": {
    "github.com/aws/some-go-lib": {
      "kind": "go",
      "version": "v1.2.3",
      "commit": "def456"
    },
    "github.com/cloudboss/unobin//pkg/libraries/core": {
      "kind": "ub",
      "version": "v0.1.0",
      "commit": "abc123",
      "hash": "sha256:deadbeef"
    }
  }
}
`
	got, err := EncodeLock(sampleLock())
	require.NoError(t, err)
	assert.Equal(t, want, string(got))
}

func TestLockRoundTrip(t *testing.T) {
	b, err := EncodeLock(sampleLock())
	require.NoError(t, err)
	got, err := DecodeLock(b)
	require.NoError(t, err)
	assert.Equal(t, sampleLock(), got)

	b2, err := EncodeLock(sampleLock())
	require.NoError(t, err)
	assert.Equal(t, b, b2, "encoding must be deterministic")
}

func TestDecodeLockRejects(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{
			name: "unsupported version",
			json: `{"version":2,"deps":{}}`,
			want: "unsupported version",
		},
		{
			name: "nil entry",
			json: `{"version":1,"deps":{"github.com/x/y":null}}`,
			want: "nil entry",
		},
		{
			name: "missing kind",
			json: `{"version":1,"deps":{"github.com/x/y":{"version":"v1","commit":"a"}}}`,
			want: "missing `kind`",
		},
		{
			name: "unknown kind",
			json: `{"version":1,"deps":{"github.com/x/y":{"kind":"rust","version":"v1","commit":"a"}}}`,
			want: "unknown kind",
		},
		{
			name: "missing version",
			json: `{"version":1,"deps":{"github.com/x/y":{"kind":"go","commit":"a"}}}`,
			want: "missing `version`",
		},
		{
			name: "missing commit",
			json: `{"version":1,"deps":{"github.com/x/y":{"kind":"go","version":"v1"}}}`,
			want: "missing `commit`",
		},
		{
			name: "ub without hash",
			json: `{"version":1,"deps":{"github.com/x/y":{"kind":"ub","version":"v1","commit":"a"}}}`,
			want: "ub dependency missing `hash`",
		},
		{
			name: "go with hash",
			json: `{"version":1,"deps":{"github.com/x/y":{"kind":"go","version":"v1","commit":"a","hash":"x"}}}`,
			want: "go dependency must not set `hash`",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeLock([]byte(c.json))
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func TestWriteAndReadLock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteLock(filepath.Join(dir, LockFileName), sampleLock()))
	got, err := ReadLock(os.DirFS(dir))
	require.NoError(t, err)
	assert.Equal(t, sampleLock(), got)
}

func TestReadLockMissing(t *testing.T) {
	_, err := ReadLock(fstest.MapFS{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}
