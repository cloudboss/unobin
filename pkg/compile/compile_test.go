package compile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/require"
)

func TestParseFactorySourceAcceptsSourceDeclaredFactory(t *testing.T) {
	src := []byte(`factory: {
  imports: { std: 'github.com/example/std' }
  inputs: { path: { type: string } }
  resources: {
    hello: std.fs-file { path: var.path }
  }
  outputs: {
    path: { value: resource.hello.path }
  }
}
`)

	sf, body, err := ParseFactorySyntaxSource("factory.ub", src)
	require.NoError(t, err)
	require.Equal(t, syntax.FileFactory, sf.Kind)
	require.NotNil(t, sf.Factory)
	require.Contains(t, body, "factory:")
	require.Contains(t, body, "imports:")
	require.Contains(t, body, "hello: std.fs-file")
	require.Contains(t, body, "resource.hello.path")
	require.NotEmpty(t, sf.Factory.Body.Resources)
}

func TestParseFactorySourceRejectsUnwrappedFactory(t *testing.T) {
	src := []byte(`
inputs: {}
resources: {}
`)

	_, _, err := ParseFactorySyntaxSource("factory.ub", src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "factory.ub must declare factory")
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

func (f failingResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	return nil, fmt.Errorf("unexpected remote fetch for %T", ref)
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

	cases := []struct {
		name    string
		replace map[deps.Dependency]string
		ref     *resolve.RemoteImport
		want    string
	}{
		{
			name: "repository replacement appends subdir",
			replace: map[deps.Dependency]string{
				{URL: "github.com/acme/repo"}: "./checkout",
			},
			ref:  &resolve.RemoteImport{URL: "github.com/acme/repo", Subdir: "library-c"},
			want: filepath.Join(checkout, "library-c"),
		},
		{
			name: "exact subdir replacement uses local root",
			replace: map[deps.Dependency]string{
				{URL: "github.com/acme/repo", Subdir: "library-c"}: "./library-c",
			},
			ref:  &resolve.RemoteImport{URL: "github.com/acme/repo", Subdir: "library-c"},
			want: library,
		},
		{
			name: "exact subdir replacement appends child package",
			replace: map[deps.Dependency]string{
				{URL: "github.com/acme/repo", Subdir: "library-c"}: "./library-c",
			},
			ref: &resolve.RemoteImport{
				URL: "github.com/acme/repo", Subdir: "library-c/subpkg",
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

func TestWithReplacedVersionsUsesReplacementID(t *testing.T) {
	versions := withReplacedVersions(nil, false, map[deps.Dependency]string{
		{URL: "github.com/acme/repo"}:                      "./checkout",
		{URL: "github.com/acme/repo", Subdir: "library-c"}: "./library-c",
	}, nil)

	require.Equal(t, replacedVersion, versions["github.com/acme/repo"])
	require.Equal(t, replacedVersion, versions["github.com/acme/repo//library-c"])
	require.NotContains(t, versions, "github.com/acme/repo/library-c")
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
		{URL: "github.com/acme/repo"}:                      "./checkout",
		{URL: "github.com/acme/repo", Subdir: "library-c"}: "./library-c",
	}, map[string]string{
		"github.com/acme/repo/other":            "v1.0.0",
		"github.com/acme/repo/library-c":        "v1.0.0",
		"github.com/acme/repo/library-c/subpkg": "v1.0.0",
	})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(checkout, "other"), replaces["github.com/acme/repo/other"])
	require.Equal(t, library, replaces["github.com/acme/repo/library-c"])
	require.Equal(t,
		filepath.Join(library, "subpkg"),
		replaces["github.com/acme/repo/library-c/subpkg"])
}
