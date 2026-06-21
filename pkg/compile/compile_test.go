package compile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/ubtest"
	"github.com/stretchr/testify/require"
)

func TestParseFactorySyntaxSourceFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/parse-factory",
		func(name string, src []byte) (string, []string) {
			_, body, err := ParseFactorySyntaxSource(name+".ub", src)
			if err != nil {
				return "", []string{err.Error()}
			}
			return body, nil
		},
		ubtest.Idempotent(),
		ubtest.Repeat(5),
	)
}

func TestDecideSelectedUnobin(t *testing.T) {
	tests := []struct {
		name       string
		listOutput string
		expected   string
		wantNotice string
		wantErr    string
	}{
		{
			name:       "selected equals expected",
			listOutput: "v0.1.0\n",
			expected:   "v0.1.0",
		},
		{
			name:       "replaced module proceeds with a notice",
			listOutput: "v0.0.0 replaced\n",
			expected:   "v0.0.0",
			wantNotice: "replaced",
		},
		{
			name:       "replaced module proceeds even when the version differs",
			listOutput: "v0.2.0 replaced\n",
			expected:   "v0.1.0",
			wantNotice: "replaced",
		},
		{
			name:       "newer selection without replace is refused",
			listOutput: "v0.2.0\n",
			expected:   "v0.1.0",
			wantErr:    "upgrade unobin to v0.2.0",
		},
		{
			name:       "unreadable output is refused",
			listOutput: "",
			expected:   "v0.1.0",
			wantErr:    "selected version",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notice, err := decideSelectedUnobin(tt.listOutput, tt.expected)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.wantNotice == "" {
				require.Empty(t, notice)
			} else {
				require.Contains(t, notice, tt.wantNotice)
			}
		})
	}
}

type failingResolver struct{}

func (failingResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	return nil, fmt.Errorf("unexpected remote fetch for %T", ref)
}

type staticSourceResolver struct {
	src *resolve.Source
}

func (r staticSourceResolver) Resolve(resolve.ImportRef) (*resolve.Source, error) {
	return r.src, nil
}

func TestWrapLockedSourcesRequiresUBProjectMarker(t *testing.T) {
	fsys := fstest.MapFS{
		"library.ub": &fstest.MapFile{Data: []byte("thing: resource {}\n")},
	}
	hash, err := deps.HashUBProject(fsys)
	require.NoError(t, err)
	lock := deps.NewLock()
	lock.Deps["example.com/repo"] = &deps.LockedDep{
		Kind: deps.LockKindUB, Version: "v1.0.0", Commit: "c1", Hash: hash,
	}
	resolver := WrapLockedSources(staticSourceResolver{src: &resolve.Source{FS: fsys}}, lock)

	_, err = resolver.Resolve(&resolve.RemoteImport{URL: "example.com/repo", Version: "v1.0.0"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "expected UB project marker")
}

func TestWrapReplacesSubdirMatching(t *testing.T) {
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	library := filepath.Join(root, "library-c")
	for _, dir := range []string{
		filepath.Join(checkout, "library-c"),
		library,
		filepath.Join(library, "subpkg"),
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(checkout, deps.ManifestFileName),
		[]byte("manifest: { requires: {} }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(library, deps.ManifestFileName),
		[]byte("manifest: { requires: {} }\n"), 0o644))

	cases := []struct {
		name    string
		replace map[deps.Dependency]string
		ref     *resolve.RemoteImport
		want    string
	}{
		{
			name: "repository replacement appends subdir",
			replace: map[deps.Dependency]string{
				{URL: "example.com/repo"}: "./checkout",
			},
			ref:  &resolve.RemoteImport{URL: "example.com/repo", Subdir: "library-c"},
			want: filepath.Join(checkout, "library-c"),
		},
		{
			name: "exact subdir replacement uses local root",
			replace: map[deps.Dependency]string{
				{URL: "example.com/repo", Subdir: "library-c"}: "./library-c",
			},
			ref:  &resolve.RemoteImport{URL: "example.com/repo", Subdir: "library-c"},
			want: library,
		},
		{
			name: "exact subdir replacement appends child package",
			replace: map[deps.Dependency]string{
				{URL: "example.com/repo", Subdir: "library-c"}: "./library-c",
			},
			ref: &resolve.RemoteImport{
				URL: "example.com/repo", Subdir: "library-c/subpkg",
			},
			want: filepath.Join(library, "subpkg"),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resolver, err := WrapReplaces(failingResolver{}, root, "", tt.replace)
			require.NoError(t, err)

			src, err := resolver.Resolve(tt.ref)
			require.NoError(t, err)
			require.Equal(t, tt.want, src.Path)
		})
	}
}

func TestWrapReplacesRejectsPackageReplacementWithoutMarker(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "helloer"), 0o755))

	_, err := WrapReplaces(failingResolver{}, root, "", map[deps.Dependency]string{
		{URL: "example.com/repo", Subdir: "ub/helloer"}: "./helloer",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no manifest.ub or go.mod")
}

func TestWithReplacedVersionsUsesReplacementID(t *testing.T) {
	versions := withReplacedVersions(nil, false, map[deps.Dependency]string{
		{URL: "example.com/repo"}:                      "./checkout",
		{URL: "example.com/repo", Subdir: "library-c"}: "./library-c",
	}, nil)

	require.Equal(t, replacedVersion, versions["example.com/repo"])
	require.Equal(t, replacedVersion, versions["example.com/repo//library-c"])
	require.NotContains(t, versions, "example.com/repo/library-c")
}

func TestAddManifestReplacesUsesResolvedGoPath(t *testing.T) {
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	library := filepath.Join(root, "library-c")
	for _, dir := range []string{
		filepath.Join(checkout, "library-c"),
		library,
		filepath.Join(library, "subpkg"),
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}

	replaces := map[string]string{}
	err := addManifestReplaces(replaces, root, map[deps.Dependency]string{
		{URL: "example.com/repo"}:                      "./checkout",
		{URL: "example.com/repo", Subdir: "library-c"}: "./library-c",
	}, map[string]string{
		"example.com/repo/other":            "v1.0.0",
		"example.com/repo/library-c":        "v1.0.0",
		"example.com/repo/library-c/subpkg": "v1.0.0",
	})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(checkout, "other"), replaces["example.com/repo/other"])
	require.Equal(t, library, replaces["example.com/repo/library-c"])
	require.Equal(t,
		filepath.Join(library, "subpkg"),
		replaces["example.com/repo/library-c/subpkg"])
}
