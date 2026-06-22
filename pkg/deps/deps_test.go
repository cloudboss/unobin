package deps

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/ubtest"
)

func TestParseDependency(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		want    Dependency
		wantErr bool
	}{
		{
			name: "url only",
			id:   "github.com/cloudboss/unobin",
			want: Dependency{URL: "github.com/cloudboss/unobin"},
		},
		{
			name: "url and subdir",
			id:   "github.com/cloudboss/unobin//pkg/libraries/core",
			want: Dependency{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/core"},
		},
		{
			name: "extra slashes around separator",
			id:   "github.com/x/y///sub",
			want: Dependency{URL: "github.com/x/y", Subdir: "sub"},
		},
		{name: "no slash in url", id: "github.com", wantErr: true},
		{name: "empty url before separator", id: "//sub", wantErr: true},
		{name: "empty", id: "", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseDependency(c.id)
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestDependencyStringRoundTrip(t *testing.T) {
	ids := []string{
		"github.com/cloudboss/unobin",
		"github.com/cloudboss/unobin//pkg/libraries/core",
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			dep, err := ParseDependency(id)
			require.NoError(t, err)
			assert.Equal(t, id, dep.String())
		})
	}
}

func TestReplacementFor(t *testing.T) {
	replace := map[Dependency]string{
		{URL: "example.com/repo"}:                         "./checkout",
		{URL: "example.com/repo", Subdir: "library-c"}:    "./library-c",
		{URL: "example.com/repo", Subdir: "libs/metrics"}: "./metrics",
	}
	cases := []struct {
		name      string
		dep       Dependency
		wantPath  string
		wantDep   Dependency
		wantExact bool
		wantRest  string
	}{
		{
			name:     "repository replacement covers root",
			dep:      Dependency{URL: "example.com/repo"},
			wantPath: "./checkout",
			wantDep:  Dependency{URL: "example.com/repo"},
		},
		{
			name:     "repository replacement appends subdir",
			dep:      Dependency{URL: "example.com/repo", Subdir: "other"},
			wantPath: "./checkout",
			wantDep:  Dependency{URL: "example.com/repo"},
			wantRest: "other",
		},
		{
			name:      "exact subdir replacement wins",
			dep:       Dependency{URL: "example.com/repo", Subdir: "library-c"},
			wantPath:  "./library-c",
			wantDep:   Dependency{URL: "example.com/repo", Subdir: "library-c"},
			wantExact: true,
		},
		{
			name:      "exact subdir replacement covers child package",
			dep:       Dependency{URL: "example.com/repo", Subdir: "library-c/subpkg"},
			wantPath:  "./library-c",
			wantDep:   Dependency{URL: "example.com/repo", Subdir: "library-c"},
			wantExact: true,
			wantRest:  "subpkg",
		},
		{
			name:      "longer subdir replacement wins",
			dep:       Dependency{URL: "example.com/repo", Subdir: "libs/metrics/http"},
			wantPath:  "./metrics",
			wantDep:   Dependency{URL: "example.com/repo", Subdir: "libs/metrics"},
			wantExact: true,
			wantRest:  "http",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ReplacementFor(replace, tt.dep)
			require.True(t, ok)
			assert.Equal(t, tt.wantPath, got.Path)
			assert.Equal(t, tt.wantDep, got.Dep)
			assert.Equal(t, tt.wantExact, got.Exact)
			assert.Equal(t, tt.wantRest, got.Suffix)
		})
	}
}

func depsFixture(t testing.TB, name string) []byte {
	t.Helper()
	return []byte(ubtest.ReadFixture(t, "testdata/ub/deps/"+name+".ub"))
}

func TestReadProject(t *testing.T) {
	fsys := fstest.MapFS{
		ProjectFileName: &fstest.MapFile{Data: depsFixture(t, "valid/read-requirements")},
	}
	m, err := ReadProject(fsys)
	require.NoError(t, err)
	assert.Equal(t, map[Dependency]Requirement{
		{URL: "github.com/cloudboss/unobin-library-std", Subdir: "x"}: {Version: "v0.1.0"},
		{URL: "github.com/me/net", Subdir: "vpc"}:                     {Version: "v2.0.0"},
	}, m.Requires)
}

func TestReadProjectEmptyRequires(t *testing.T) {
	fsys := fstest.MapFS{
		ProjectFileName: &fstest.MapFile{Data: depsFixture(t, "valid/empty-requires")},
	}
	m, err := ReadProject(fsys)
	require.NoError(t, err)
	assert.Empty(t, m.Requires)
}

func TestReadProjectMissingFile(t *testing.T) {
	_, err := ReadProject(fstest.MapFS{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestReadProjectRejectsBadVersionField(t *testing.T) {
	fsys := fstest.MapFS{
		ProjectFileName: &fstest.MapFile{Data: depsFixture(t, "invalid/bad-version-field")},
	}
	_, err := ReadProject(fsys)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not a valid project field")
}

func TestReadProjectRejectsBadDependencyURL(t *testing.T) {
	fsys := fstest.MapFS{
		ProjectFileName: &fstest.MapFile{Data: depsFixture(t, "invalid/bad-dependency-url")},
	}
	_, err := ReadProject(fsys)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repo URL must contain a host and a path")
}

func TestReadProjectRejectsBadFloor(t *testing.T) {
	fsys := fstest.MapFS{
		ProjectFileName: &fstest.MapFile{Data: depsFixture(t, "invalid/bad-floor")},
	}
	_, err := ReadProject(fsys)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not a valid version")
}

func TestEncodeProject(t *testing.T) {
	m := &Project{Requires: map[Dependency]Requirement{
		{URL: "github.com/cloudboss/unobin"}:        {Version: "v0.1.2"},
		{URL: "github.com/cloudboss/helloer-stuff"}: {Version: "v0.1.0"},
	}}
	want := depsFixture(t, "valid/encoded-requirements")
	assert.Equal(t, string(want), string(EncodeProject(m)))
}

func TestEncodeProjectEmpty(t *testing.T) {
	want := depsFixture(t, "valid/encoded-empty")
	assert.Equal(t, string(want), string(EncodeProject(&Project{})))
}

func TestProjectCanBeReadAgain(t *testing.T) {
	m := &Project{Requires: map[Dependency]Requirement{
		{URL: "github.com/x/y", Subdir: "sub"}: {Version: "v1.2.3"},
		{URL: "github.com/a/b"}:                {Version: "v0.1.0"},
	}}
	fsys := fstest.MapFS{ProjectFileName: &fstest.MapFile{Data: EncodeProject(m)}}
	got, err := ReadProject(fsys)
	require.NoError(t, err)
	assert.Equal(t, m.Requires, got.Requires)
}

func TestReadProjectWithReplace(t *testing.T) {
	fsys := fstest.MapFS{
		ProjectFileName: &fstest.MapFile{Data: depsFixture(t, "valid/with-replace")},
	}
	m, err := ReadProject(fsys)
	require.NoError(t, err)
	assert.Equal(t, map[Dependency]Requirement{
		{URL: "github.com/x/y"}: {Version: "v1.0.0"},
	}, m.Requires)
	assert.Equal(t, map[Dependency]string{
		{URL: "github.com/cloudboss/unobin-library-aws"}: "../../../..",
	}, m.Replace)
}

func TestEncodeProjectWithReplace(t *testing.T) {
	m := &Project{
		Requires: map[Dependency]Requirement{{URL: "github.com/x/y"}: {Version: "v1.0.0"}},
		Replace: map[Dependency]string{
			{URL: "github.com/cloudboss/unobin-library-aws"}: "../../../..",
		},
	}
	want := depsFixture(t, "valid/encoded-replace")
	assert.Equal(t, string(want), string(EncodeProject(m)))
}

func TestReplaceCanBeReadAgain(t *testing.T) {
	m := &Project{
		Requires: map[Dependency]Requirement{{URL: "github.com/a/b"}: {Version: "v0.1.0"}},
		Replace:  map[Dependency]string{{URL: "github.com/c/d"}: "../local/d"},
	}
	fsys := fstest.MapFS{ProjectFileName: &fstest.MapFile{Data: EncodeProject(m)}}
	got, err := ReadProject(fsys)
	require.NoError(t, err)
	assert.Equal(t, m.Requires, got.Requires)
	assert.Equal(t, m.Replace, got.Replace)
}
