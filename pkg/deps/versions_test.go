package deps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDependencyTagPrefix(t *testing.T) {
	assert.Equal(t, "", Dependency{URL: "github.com/x/y"}.TagPrefix())
	assert.Equal(t, "net/", Dependency{URL: "github.com/x/y", Subdir: "net"}.TagPrefix())
	assert.Equal(t, "pkg/libraries/core/",
		Dependency{URL: "github.com/x/y", Subdir: "pkg/libraries/core"}.TagPrefix())
}

func TestDependencyTag(t *testing.T) {
	assert.Equal(t, "v1.0.0", Dependency{URL: "github.com/x/y"}.Tag("v1.0.0"))
	assert.Equal(t, "net/v1.0.0",
		Dependency{URL: "github.com/x/y", Subdir: "net"}.Tag("v1.0.0"))
}

func TestVersions(t *testing.T) {
	root := Dependency{URL: "github.com/x/y"}
	net := Dependency{URL: "github.com/x/y", Subdir: "net"}
	nested := Dependency{URL: "github.com/x/y", Subdir: "pkg/libraries/core"}
	cases := []struct {
		name string
		dep  Dependency
		tags []string
		want []string
	}{
		{
			name: "root sorted ascending",
			dep:  root,
			tags: []string{"v1.2.0", "v0.1.0", "v1.10.0", "v1.2.1"},
			want: []string{"v0.1.0", "v1.2.0", "v1.2.1", "v1.10.0"},
		},
		{
			name: "root ignores subdir tags",
			dep:  root,
			tags: []string{"v1.0.0", "net/v2.0.0"},
			want: []string{"v1.0.0"},
		},
		{
			name: "root ignores non-semver",
			dep:  root,
			tags: []string{"v1.0.0", "latest", "1.0.0", "release-1"},
			want: []string{"v1.0.0"},
		},
		{
			name: "subdir basic",
			dep:  net,
			tags: []string{"net/v1.0.0", "net/v1.1.0"},
			want: []string{"v1.0.0", "v1.1.0"},
		},
		{
			name: "subdir ignores root tags when project tags exist",
			dep:  net,
			tags: []string{"net/v1.0.0", "v2.0.0", "db/v3.0.0"},
			want: []string{"v1.0.0"},
		},
		{
			name: "subdir uses root tags when no project tags exist",
			dep:  net,
			tags: []string{"v1.0.0", "v1.1.0", "db/v3.0.0"},
			want: []string{"v1.0.0", "v1.1.0"},
		},
		{
			name: "subdir uses nearest parent project tags",
			dep:  Dependency{URL: "github.com/x/y", Subdir: "ub/project-b/comprehensions"},
			tags: []string{
				"v9.0.0",
				"ub/project-b/v0.1.0",
				"ub/project-b/v0.2.0",
			},
			want: []string{"v0.1.0", "v0.2.0"},
		},
		{
			name: "subdir ignores deeper prefix tags",
			dep:  net,
			tags: []string{"net/v1.0.0", "net/extra/v2.0.0"},
			want: []string{"v1.0.0"},
		},
		{
			name: "prerelease ordering",
			dep:  root,
			tags: []string{"v1.0.0", "v1.0.0-rc1", "v1.0.0-alpha"},
			want: []string{"v1.0.0-alpha", "v1.0.0-rc1", "v1.0.0"},
		},
		{
			name: "nested subdir uses its path prefix",
			dep:  nested,
			tags: []string{
				"v0.2.0",
				"pkg/libraries/core/v0.2.0",
				"pkg/libraries/core/v0.1.0",
				"pkg/libraries/v9.9.9",
			},
			want: []string{"v0.1.0", "v0.2.0"},
		},
		{
			name: "no matches",
			dep:  root,
			tags: []string{"other/v1.0.0"},
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Versions(c.dep, c.tags)
			assert.Equal(t, c.want, got)
			for range 3 {
				assert.Equal(t, got, Versions(c.dep, c.tags), "must be deterministic")
			}
		})
	}
}

func TestHighest(t *testing.T) {
	cases := []struct {
		name string
		vs   []string
		want string
	}{
		{name: "empty", vs: nil, want: ""},
		{name: "single", vs: []string{"v1.0.0"}, want: "v1.0.0"},
		{name: "picks max", vs: []string{"v1.2.0", "v1.10.0", "v1.3.0"}, want: "v1.10.0"},
		{name: "release beats prerelease", vs: []string{"v1.0.0-rc1", "v1.0.0"}, want: "v1.0.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Highest(c.vs))
		})
	}
}

func TestResolveVersion(t *testing.T) {
	root := Dependency{URL: "github.com/x/y"}
	sub := Dependency{URL: "github.com/x/y", Subdir: "net"}
	cases := []struct {
		name    string
		dep     Dependency
		query   string
		tags    []string
		want    string
		wantErr bool
	}{
		{name: "empty picks latest", dep: root, query: "",
			tags: []string{"v1.0.0", "v1.2.0", "v2.0.0"}, want: "v2.0.0"},
		{name: "latest keyword", dep: root, query: "latest",
			tags: []string{"v1.0.0", "v2.0.0"}, want: "v2.0.0"},
		{name: "exact present", dep: root, query: "v1.2.0",
			tags: []string{"v1.0.0", "v1.2.0", "v2.0.0"}, want: "v1.2.0"},
		{name: "exact absent", dep: root, query: "v1.5.0",
			tags: []string{"v1.0.0", "v2.0.0"}, wantErr: true},
		{name: "partial major", dep: root, query: "v1",
			tags: []string{"v1.0.0", "v1.9.0", "v2.0.0"}, want: "v1.9.0"},
		{name: "partial major-minor", dep: root, query: "v1.2",
			tags: []string{"v1.2.0", "v1.2.5", "v1.3.0"}, want: "v1.2.5"},
		{name: "partial excludes adjacent major", dep: root, query: "v1",
			tags: []string{"v1.0.0", "v10.0.0"}, want: "v1.0.0"},
		{name: "partial no match", dep: root, query: "v3",
			tags: []string{"v1.0.0", "v2.0.0"}, wantErr: true},
		{name: "no versions available", dep: root, query: "", tags: nil, wantErr: true},
		{name: "invalid query", dep: root, query: "garbage",
			tags: []string{"v1.0.0"}, wantErr: true},
		{name: "subdir dep uses prefixed tags", dep: sub, query: "latest",
			tags: []string{"net/v1.0.0", "net/v1.1.0", "v2.0.0"}, want: "v1.1.0"},
		{name: "subdir dep uses root tags when no project tags exist", dep: sub, query: "latest",
			tags: []string{"v1.0.0", "v2.0.0"}, want: "v2.0.0"},
		{name: "subdir dep exact query stays unprefixed", dep: sub, query: "v1.0.0",
			tags: []string{"net/v1.0.0", "net/v1.1.0", "v2.0.0"}, want: "v1.0.0"},
		{name: "subdir dep partial query stays unprefixed", dep: sub, query: "v1",
			tags: []string{"net/v1.0.0", "net/v1.1.0", "v2.0.0"}, want: "v1.1.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveVersion(c.dep, c.query, c.tags)
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}
