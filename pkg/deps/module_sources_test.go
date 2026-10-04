package deps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestResolveSelectedModule(t *testing.T) {
	cases := []struct {
		name       string
		dep        Dependency
		version    string
		modulePath string
		replaced   bool
		invalid    bool
	}{
		{name: "module root without a library record", dep: Dependency{URL: "example.com/lib"},
			version: "v0.3.0", modulePath: "example.com/lib"},
		{name: "nested selected project", dep: Dependency{URL: "example.com/lib", Subdir: "config"},
			version: "v0.3.0", modulePath: "example.com/lib/config"},
		{name: "major module path", dep: Dependency{URL: "example.com/lib"},
			version: "v2.0.0-beta.1", modulePath: "example.com/lib/v2"},
		{name: "wrong major module path", dep: Dependency{URL: "example.com/lib"},
			version: "v2.0.0-beta.1", modulePath: "example.com/lib", invalid: true},
		{name: "local replacement without a floor", dep: Dependency{URL: "example.com/lib"},
			modulePath: "example.com/lib", replaced: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(
				"module "+tc.modulePath+"\n\ngo 1.26.2\n"), 0o644))
			selection := map[Dependency]string{tc.dep: tc.version}
			replace := map[Dependency]string{}
			ref := remoteProjectRef(ProjectID(tc.dep), tc.version)
			if tc.dep.Subdir != "" {
				selection[Dependency{URL: tc.dep.URL}] = "v0.1.0"
			}
			if tc.replaced {
				delete(selection, tc.dep)
				replace[tc.dep] = dir
				ref.Version = ""
			}
			r := &fakeResolver{sources: map[string]*resolve.Source{
				srcKey(ref.URL, ref.Subdir, ref.Version): {
					FS: os.DirFS(dir), Path: dir, Commit: "selected-commit",
				},
			}}
			module, err := ResolveSelectedModule(tc.modulePath+"/entry", selection, r, replace)
			if tc.invalid {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "/v2")
				return
			}
			require.NoError(t, err)
			expected := golibrary.ModuleSource{
				Path: tc.modulePath, Dir: dir, Dependency: tc.dep.String(),
				Version: tc.version, Commit: "selected-commit",
			}
			if tc.replaced {
				expected.Version, expected.Replacement = ReplacementSentinel, dir
			}
			assert.Equal(t, expected, module)
			assert.Equal(t, []*resolve.RemoteImport{ref}, r.refs)
		})
	}
}

func TestResolveSelectedModuleDoesNotDiscoverUndeclaredProjects(t *testing.T) {
	r := &fakeResolver{}
	module, err := ResolveSelectedModule("example.com/other/config", map[Dependency]string{
		{URL: "example.com/lib"}: "v0.3.0",
	}, r, nil)
	require.NoError(t, err)
	assert.Equal(t, golibrary.ModuleSource{}, module)
	assert.Empty(t, r.refs)
}

func TestResolveSelectedModuleKeepsSourceReadErrors(t *testing.T) {
	dir := t.TempDir()
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("example.com/lib", "", "v0.3.0"): {FS: os.DirFS(dir), Path: dir},
	}}
	_, err := ResolveSelectedModule("example.com/lib/config", map[Dependency]string{
		{URL: "example.com/lib"}: "v0.3.0",
	}, r, nil)
	require.ErrorIs(t, err, os.ErrNotExist)
}
