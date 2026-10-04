package project

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestRetryDependencyCandidate(t *testing.T) {
	unsupported := &libraryapi.UnsupportedMajorError{Required: libraryapi.Version{Major: 2}}
	missing := &golibrary.CompatibilityError{Kind: golibrary.MissingDeclaration}
	authoring := &golibrary.CompatibilityError{Kind: golibrary.UnsupportedField}
	tests := []struct {
		name  string
		err   error
		retry bool
	}{
		{name: "no failure"},
		{name: "unsupported major", err: unsupported, retry: true},
		{name: "newer minor", err: &libraryapi.NewerMinorError{}, retry: true},
		{name: "missing declaration", err: missing, retry: true},
		{name: "core floor", err: &golibrary.CoreFloorError{}, retry: true},
		{name: "selected package", err: &deps.SourceSelectionError{}, retry: true},
		{name: "module path", err: &resolve.ModulePathError{}, retry: true},
		{name: "wrapped", err: fmt.Errorf("context: %w", unsupported), retry: true},
		{name: "all joined failures are retryable",
			err: errors.Join(unsupported, missing), retry: true},
		{name: "operational failure", err: fs.ErrPermission},
		{name: "authoring failure", err: authoring},
		{name: "invalid declaration", err: &golibrary.CompatibilityError{
			Kind: golibrary.InvalidDeclaration,
		}},
		{name: "configuration blocks retry", err: &golibrary.ConfigurationSourceError{
			Cause: unsupported,
		}},
		{name: "descriptor blocks retry", err: &golibrary.CoreDescriptorError{Cause: unsupported}},
		{name: "toolchain pin", err: &golibrary.ToolchainPinError{}},
		{name: "joined authoring error", err: errors.Join(unsupported, authoring)},
		{name: "joined operational error", err: errors.Join(unsupported, fs.ErrPermission)},
		{name: "nested joined operational error", err: fmt.Errorf("context: %w",
			errors.Join(unsupported, errors.Join(missing, fs.ErrPermission)))},
		{name: "text is not a classification", err: errors.New(unsupported.Error())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.retry, retryDependencyCandidate(tt.err))
		})
	}
}

func TestDependencyTrialDiagnosticsRetainsSelectedOrigin(t *testing.T) {
	dependency := deps.Dependency{URL: "example.com/lib"}
	app := deps.Dependency{URL: "example.com/app"}
	chain := []deps.RequirementStep{
		{Requires: app, MinimumVersion: "v1.0.0"},
		{Dependency: app, Version: "v1.0.0", Requires: dependency, MinimumVersion: "v2.0.0"},
	}
	original := diagnostic.Diagnostic{
		Code: "unobin.library-api.unsupported-major", Severity: diagnostic.SeverityError,
		Message: "unsupported library",
		LibraryCompatibility: &diagnostic.LibraryCompatibilityDetails{
			Dependency: dependency.String(), Version: "v2.0.0", Commit: "selected-commit",
			RequiredAPI: "2.0", ImplementedAPIs: []string{"1.0"},
		},
	}
	cause := &libraryapi.UnsupportedMajorError{Required: libraryapi.Version{Major: 2}}
	err := diagnostic.WithDiagnostics(cause, original)
	got := dependencyTrialDiagnostics(err, "v0.1.0", "latest", "v0.0.1", &deps.Resolution{
		Selection: map[deps.Dependency]string{dependency: "v2.0.0"},
		Chains:    map[deps.Dependency][]deps.RequirementStep{dependency: chain},
	})
	require.Len(t, got, 1)
	want := original
	want.LibraryCompatibility = &diagnostic.LibraryCompatibilityDetails{
		Dependency: dependency.String(), Version: "v2.0.0", Commit: "selected-commit",
		RequiredAPI: "2.0", ImplementedAPIs: []string{"1.0"},
		CandidateVersion: "v0.1.0", Query: "latest", Floor: "v0.0.1",
		RequirementChain: []diagnostic.LibraryRequirementStep{
			{Dependency: "project.ub", Requires: app.String(), MinimumVersion: "v1.0.0"},
			{Dependency: app.String(), Version: "v1.0.0",
				Requires: dependency.String(), MinimumVersion: "v2.0.0"},
		},
	}
	assert.Equal(t, want, got[0])
	assert.Empty(t, original.LibraryCompatibility.CandidateVersion)
	assert.Empty(t, original.LibraryCompatibility.RequirementChain)
}

type recordingDependencyResolver struct {
	wrapped resolve.Resolver
	calls   *[]string
	failure error
}

func (r recordingDependencyResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	if remote, ok := ref.(*resolve.RemoteImport); ok {
		*r.calls = append(*r.calls, remote.URL+"@"+remote.Version)
		if r.failure != nil && remote.Version == "v0.2.0" {
			return nil, r.failure
		}
	}
	return r.wrapped.Resolve(ref)
}

func candidateLibrarySource(t *testing.T, module, api string) *resolve.Source {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir,
		os.DirFS("../deps/testdata/go/compatibility")))
	code, err := os.ReadFile(filepath.Join(dir, "library.go"))
	require.NoError(t, err)
	updated := strings.ReplaceAll(string(code), `RequiredAPI: "1.0"`, `RequiredAPI: "`+api+`"`)
	if api == "" {
		updated = strings.ReplaceAll(string(code),
			"\t\tCompatibility: runtime.LibraryCompatibility{RequiredAPI: \"1.0\"},\n", "")
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "library.go"), []byte(updated), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module "+module+"\n\ngo 1.26.2\n"), 0o644))
	return &resolve.Source{FS: os.DirFS(dir), Path: dir, Commit: "commit-" + api}
}

func stubRecordingDependencyResolver(
	t *testing.T, remotes map[string]*resolve.Source, failure error,
) *[]string {
	t.Helper()
	calls := []string{}
	previous := newCompileResolver
	newCompileResolver = func(root string) (resolve.Resolver, error) {
		return recordingDependencyResolver{
			wrapped: &fakeResolver{local: resolve.NewLocalResolver(root), remotes: remotes},
			calls:   &calls, failure: failure,
		}, nil
	}
	t.Cleanup(func() { newCompileResolver = previous })
	return &calls
}

func TestGetTriesCompatibleCandidates(t *testing.T) {
	tests := []struct {
		name, query, api, code, alter string
	}{
		{name: "automatic unsupported major", api: "2.0", code: "unsupported-major"},
		{name: "latest newer minor", query: "latest", api: "1.1", code: "newer-minor"},
		{name: "major prefix", query: "v0", api: "2.0", code: "unsupported-major"},
		{name: "minor prefix", query: "v0.2", api: "2.0", code: "unsupported-major"},
		{name: "missing declaration", api: "", code: "missing-declaration"},
		{name: "core floor", api: "1.0", code: "core-floor", alter: "core"},
		{name: "module major", api: "1.0", code: "module-source", alter: "major"},
		{name: "missing Go package", api: "1.0", code: "module-source", alter: "missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			factory := ubtest.ReadValidFixture(t,
				"testdata/ub/library-compatibility", "factory")
			require.NoError(t, os.WriteFile(
				filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
			lowerVersion := "v0.1.0"
			if tt.query == "v0.2" {
				lowerVersion = "v0.2.0-rc.1"
			}
			higher := candidateLibrarySource(t, "example.com/lib", tt.api)
			switch tt.alter {
			case "core":
				require.NoError(t, os.WriteFile(filepath.Join(higher.Path, "go.mod"), []byte(
					"module example.com/lib\n\ngo 1.26.2\n\n"+
						"require github.com/cloudboss/unobin v0.2.0\n"), 0o644))
			case "major":
				require.NoError(t, os.WriteFile(filepath.Join(higher.Path, "go.mod"),
					[]byte("module example.com/lib/v2\n\ngo 1.26.2\n"), 0o644))
			case "missing":
				require.NoError(t, os.Remove(filepath.Join(higher.Path, "library.go")))
			}
			lower := candidateLibrarySource(t, "example.com/lib", "1.0")
			calls := stubRecordingDependencyResolver(t, map[string]*resolve.Source{
				remoteSourceKey("example.com/lib", "", "v0.2.0"):     higher,
				remoteSourceKey("example.com/lib", "", lowerVersion): lower,
			}, nil)
			t.Cleanup(SetDepsListTagsForTest(func(string) ([]string, error) {
				return []string{"v0.0.1", lowerVersion, "v0.2.0"}, nil
			}))
			arg := "example.com/lib"
			if tt.query != "" {
				arg += "@" + tt.query
			}
			announcements := []string{}
			operation, err := getDependency(&Options{Path: root}, arg, io.Discard,
				func(dependency deps.Dependency, version string) {
					announcements = append(announcements, dependency.String()+"@"+version)
				})
			require.NoError(t, err)
			assert.Equal(t, lowerVersion, operation.Version)
			assert.Equal(t, []string{"example.com/lib@v0.2.0", "example.com/lib@" + lowerVersion},
				*calls)
			assert.Equal(t, []string{"example.com/lib@" + lowerVersion}, announcements)
			project, err := deps.ReadProject(os.DirFS(root))
			require.NoError(t, err)
			assert.Equal(t, deps.Requirement{Version: lowerVersion},
				project.Requires[deps.Dependency{URL: "example.com/lib"}])
			lock, err := deps.ReadProjectLock(os.DirFS(root))
			require.NoError(t, err)
			assert.Equal(t, lowerVersion, lock.Deps["example.com/lib"].Version)
			require.Len(t, operation.Diagnostics, 1)
			notice := operation.Diagnostics[0]
			assert.Equal(t, "unobin.library-api."+tt.code, notice.Code)
			assert.Equal(t, diagnostic.SeverityInfo, notice.Severity)
			assert.Equal(t, "v0.2.0", notice.LibraryCompatibility.CandidateVersion)
			assert.Equal(t, "v0.2.0", notice.LibraryCompatibility.Version)
			assert.Equal(t, []diagnostic.LibraryRequirementStep{{
				Dependency: "project.ub", Requires: "example.com/lib", MinimumVersion: "v0.2.0",
			}}, notice.LibraryCompatibility.RequirementChain)
		})
	}
}

func TestGetStopsOnAuthoringAndOperationalFailures(t *testing.T) {
	for _, operational := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "authoring", true: "operational"}[operational],
			func(t *testing.T) {
				root := t.TempDir()
				factory := ubtest.ReadValidFixture(t,
					"testdata/ub/library-compatibility", "factory")
				require.NoError(t, os.WriteFile(
					filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
				library := candidateLibrarySource(t, "example.com/lib", "2.0")
				path := filepath.Join(library.Path, "library.go")
				code, err := os.ReadFile(path)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(code),
					`RequiredAPI: "2.0"`, `RequiredAPI: "2.0", Unknown: "x"`)), 0o644))
				var failure error
				if operational {
					failure = fs.ErrPermission
				}
				calls := stubRecordingDependencyResolver(t, map[string]*resolve.Source{
					remoteSourceKey("example.com/lib", "", "v0.2.0"): library,
				}, failure)
				t.Cleanup(SetDepsListTagsForTest(func(string) ([]string, error) {
					return []string{"v0.1.0", "v0.2.0"}, nil
				}))
				_, err = getDependency(&Options{Path: root}, "example.com/lib", io.Discard, nil)
				require.Error(t, err)
				if operational {
					assert.ErrorIs(t, err, failure)
				} else {
					var authoring *golibrary.CompatibilityError
					require.ErrorAs(t, err, &authoring)
					assert.Equal(t, golibrary.UnsupportedField, authoring.Kind)
				}
				assert.Equal(t, []string{"example.com/lib@v0.2.0"}, *calls)
				require.NoFileExists(t, filepath.Join(root, deps.ProjectFileName))
				require.NoFileExists(t, filepath.Join(root, deps.ProjectLockFileName))
			})
	}
}

func TestGetValidatesTheSelectedReleaseInsteadOfTheRequestedFloor(t *testing.T) {
	for _, test := range []struct {
		name, query, floor string
	}{
		{name: "exact", query: "v0.1.0", floor: "v0.9.0"},
		{name: "automatic", floor: "v0.1.0"},
		{name: "latest", query: "latest", floor: "v0.1.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			factory := ubtest.ReadValidFixture(t, "testdata/ub/dependency-candidates", "factory")
			require.NoError(t, os.WriteFile(
				filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
			app, library := deps.Dependency{URL: "example.com/app"},
				deps.Dependency{URL: "example.com/lib"}
			project := &deps.Project{Requires: map[deps.Dependency]deps.Requirement{
				app: {Version: "v0.1.0"}, library: {Version: test.floor},
			}}
			_, err := deps.WriteProjectChange(filepath.Join(root, deps.ProjectFileName), project)
			require.NoError(t, err)
			appDir := t.TempDir()
			body := ubtest.ReadValidFixture(t, "testdata/ub/dependency-candidates", "library")
			require.NoError(t, os.WriteFile(
				filepath.Join(appDir, "library.ub"), []byte(body), 0o644))
			_, err = deps.WriteProjectChange(
				filepath.Join(appDir, deps.ProjectFileName), &deps.Project{
					Requires: map[deps.Dependency]deps.Requirement{library: {Version: "v0.2.0"}},
				})
			require.NoError(t, err)
			calls := stubRecordingDependencyResolver(t, map[string]*resolve.Source{
				remoteSourceKey(app.URL, "", "v0.1.0"): {
					FS: os.DirFS(appDir), Path: appDir, Commit: "app-commit",
				},
				remoteSourceKey(library.URL, "", "v0.1.0"): candidateLibrarySource(
					t, library.URL, "2.0"),
				remoteSourceKey(library.URL, "", "v0.2.0"): candidateLibrarySource(
					t, library.URL, "1.0"),
			}, nil)
			t.Cleanup(SetDepsListTagsForTest(func(string) ([]string, error) {
				return []string{"v0.1.0"}, nil
			}))
			announcements := []string{}
			arg := library.String()
			if test.query != "" {
				arg += "@" + test.query
			}
			var output bytes.Buffer
			operation, err := getDependency(&Options{Path: root},
				arg, &output, func(dep deps.Dependency, version string) {
					announcements = append(announcements, dep.String()+"@"+version)
				})
			require.NoError(t, err)
			assert.Equal(t, "v0.1.0", operation.Version)
			assert.Equal(t, "v0.2.0", operation.SelectedVersion)
			assert.Equal(t, []string{"example.com/app@v0.1.0", "example.com/lib@v0.2.0"}, *calls)
			assert.Equal(t, []string{"example.com/lib@v0.2.0"}, announcements)
			project, err = deps.ReadProject(os.DirFS(root))
			require.NoError(t, err)
			assert.Equal(t, deps.Requirement{Version: "v0.1.0"}, project.Requires[library])
			assert.Equal(t, deps.Requirement{Version: "v0.1.0"}, project.Requires[app])
			lock, err := deps.ReadProjectLock(os.DirFS(root))
			require.NoError(t, err)
			assert.Equal(t, "v0.2.0", lock.Deps[library.String()].Version)
		})
	}
}

func TestGetStartsEachTransitiveTrialFresh(t *testing.T) {
	root := t.TempDir()
	factory := ubtest.ReadValidFixture(t, "testdata/ub/dependency-candidates", "indirect-factory")
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
	app, library := deps.Dependency{URL: "example.com/app"}, deps.Dependency{URL: "example.com/lib"}
	remotes := map[string]*resolve.Source{}
	for _, version := range []string{"v0.1.0", "v0.2.0"} {
		dir := t.TempDir()
		body := ubtest.ReadValidFixture(t, "testdata/ub/dependency-candidates", "library")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "library.ub"), []byte(body), 0o644))
		_, err := deps.WriteProjectChange(filepath.Join(dir, deps.ProjectFileName), &deps.Project{
			Requires: map[deps.Dependency]deps.Requirement{library: {Version: version}},
		})
		require.NoError(t, err)
		remotes[remoteSourceKey(app.URL, "", version)] = &resolve.Source{
			FS: os.DirFS(dir), Path: dir, Commit: "app-" + version,
		}
	}
	remotes[remoteSourceKey(library.URL, "", "v0.1.0")] =
		candidateLibrarySource(t, library.URL, "1.0")
	remotes[remoteSourceKey(library.URL, "", "v0.2.0")] =
		candidateLibrarySource(t, library.URL, "2.0")
	calls := stubRecordingDependencyResolver(t, remotes, nil)
	t.Cleanup(SetDepsListTagsForTest(func(string) ([]string, error) {
		return []string{"v0.1.0", "v0.2.0"}, nil
	}))
	operation, err := getDependency(&Options{Path: root}, app.String(), io.Discard, nil)
	require.NoError(t, err)
	assert.Equal(t, "v0.1.0", operation.Version)
	assert.Equal(t, []string{
		"example.com/app@v0.2.0", "example.com/lib@v0.2.0",
		"example.com/app@v0.1.0", "example.com/lib@v0.1.0",
	}, *calls)
	require.Len(t, operation.Diagnostics, 1)
	assert.Equal(t, []diagnostic.LibraryRequirementStep{
		{Dependency: "project.ub", Requires: app.String(), MinimumVersion: "v0.2.0"},
		{Dependency: app.String(), Version: "v0.2.0", Requires: library.String(),
			MinimumVersion: "v0.2.0"},
	}, operation.Diagnostics[0].LibraryCompatibility.RequirementChain)
	lock, err := deps.ReadProjectLock(os.DirFS(root))
	require.NoError(t, err)
	assert.Equal(t, "v0.1.0", lock.Deps[library.String()].Version)
}

func TestGetPreservesAutomaticDirectAndIndirectFloors(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		for _, query := range []string{"", "latest"} {
			t.Run(fmt.Sprintf("indirect=%t/query=%s", indirect, query), func(t *testing.T) {
				root := t.TempDir()
				fixture, name := "testdata/ub/library-compatibility", "factory"
				if indirect {
					fixture, name = "testdata/ub/dependency-candidates", "indirect-factory"
				}
				factory := ubtest.ReadValidFixture(t, fixture, name)
				require.NoError(t, os.WriteFile(
					filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
				library := deps.Dependency{URL: "example.com/lib"}
				project := &deps.Project{Requires: map[deps.Dependency]deps.Requirement{
					library: {Version: "v0.2.0", Indirect: indirect},
				}}
				remotes := map[string]*resolve.Source{
					remoteSourceKey(library.URL, "", "v0.2.0"): candidateLibrarySource(
						t, library.URL, "2.0"),
					remoteSourceKey(library.URL, "", "v0.1.0"): candidateLibrarySource(
						t, library.URL, "1.0"),
				}
				if indirect {
					app := deps.Dependency{URL: "example.com/app"}
					project.SetRequire(app, "v0.1.0", false)
					dir := t.TempDir()
					body := ubtest.ReadValidFixture(t,
						"testdata/ub/dependency-candidates", "library")
					require.NoError(t, os.WriteFile(
						filepath.Join(dir, "library.ub"), []byte(body), 0o644))
					_, err := deps.WriteProjectChange(filepath.Join(dir, deps.ProjectFileName),
						&deps.Project{Requires: map[deps.Dependency]deps.Requirement{
							library: {Version: "v0.1.0"},
						}})
					require.NoError(t, err)
					remotes[remoteSourceKey(app.URL, "", "v0.1.0")] = &resolve.Source{
						FS: os.DirFS(dir), Path: dir, Commit: "app-commit",
					}
				}
				projectPath, lockPath := filepath.Join(root, deps.ProjectFileName),
					filepath.Join(root, deps.ProjectLockFileName)
				_, err := deps.WriteProjectChange(projectPath, project)
				require.NoError(t, err)
				oldLock := deps.NewProjectLock()
				oldLock.ToolchainVersion = "v0.1.0"
				_, err = deps.WriteProjectLockChange(lockPath, oldLock)
				require.NoError(t, err)
				beforeProject, err := os.ReadFile(projectPath)
				require.NoError(t, err)
				beforeLock, err := os.ReadFile(lockPath)
				require.NoError(t, err)
				calls := stubRecordingDependencyResolver(t, remotes, nil)
				t.Cleanup(SetDepsListTagsForTest(func(string) ([]string, error) {
					return []string{"v0.1.0", "v0.2.0"}, nil
				}))
				arg := library.String()
				if query != "" {
					arg += "@" + query
				}
				_, err = getDependency(&Options{Path: root}, arg, io.Discard, nil)
				require.Error(t, err)
				diagnostics := diagnostic.FromError(err, diagnostic.ConvertOptions{})
				codes := []string{}
				for _, d := range diagnostics {
					codes = append(codes, d.Code)
					assert.Equal(t, "v0.2.0", d.LibraryCompatibility.Floor)
				}
				assert.ElementsMatch(t, []string{
					"unobin.library-api.unsupported-major",
					"unobin.library-api.no-compatible-version",
				}, codes)
				assert.NotContains(t, *calls, library.String()+"@v0.1.0")
				after, err := os.ReadFile(projectPath)
				require.NoError(t, err)
				assert.Equal(t, beforeProject, after)
				after, err = os.ReadFile(lockPath)
				require.NoError(t, err)
				assert.Equal(t, beforeLock, after)
				operation, err := getDependency(&Options{Path: root},
					library.String()+"@v0.1.0", io.Discard, nil)
				require.NoError(t, err)
				assert.Equal(t, "v0.1.0", operation.Version)
				assert.Equal(t, indirect, operation.Indirect)
			})
		}
	}
}

func TestGetReportsAFloorThatExcludesEveryAvailableRelease(t *testing.T) {
	root := t.TempDir()
	factory := ubtest.ReadValidFixture(t,
		"testdata/ub/library-compatibility", "factory")
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
	library := deps.Dependency{URL: "example.com/lib"}
	project := &deps.Project{Requires: map[deps.Dependency]deps.Requirement{
		library: {Version: "v0.9.0"},
	}}
	projectPath := filepath.Join(root, deps.ProjectFileName)
	_, err := deps.WriteProjectChange(projectPath, project)
	require.NoError(t, err)
	before, err := os.ReadFile(projectPath)
	require.NoError(t, err)
	calls := stubRecordingDependencyResolver(t, nil, nil)
	t.Cleanup(SetDepsListTagsForTest(func(string) ([]string, error) {
		return []string{"v0.1.0", "v0.2.0"}, nil
	}))
	_, err = getDependency(&Options{Path: root}, library.String(), io.Discard, nil)
	require.Error(t, err)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, "unobin.library-api.no-compatible-version", ds[0].Code)
	assert.Equal(t, "v0.9.0", ds[0].LibraryCompatibility.Floor)
	assert.Equal(t, []diagnostic.LibraryRequirementStep{{
		Dependency: "project.ub", Requires: library.String(), MinimumVersion: "v0.9.0",
	}}, ds[0].LibraryCompatibility.RequirementChain)
	assert.Empty(t, *calls)
	after, err := os.ReadFile(projectPath)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	require.NoFileExists(t, filepath.Join(root, deps.ProjectLockFileName))
}

func TestSyncRejectsAnIncompatibleDiscoveredOwner(t *testing.T) {
	root := t.TempDir()
	factory := ubtest.ReadValidFixture(t,
		"testdata/ub/library-compatibility", "factory")
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
	newer := candidateLibrarySource(t, "example.com/lib", "2.0")
	older := candidateLibrarySource(t, "example.com/lib", "1.0")
	calls := stubRecordingDependencyResolver(t, map[string]*resolve.Source{
		remoteSourceKey("example.com/lib", "", "v0.2.0"): newer,
		remoteSourceKey("example.com/lib", "", "v0.1.0"): older,
	}, nil)
	t.Cleanup(SetDepsListTagsForTest(func(string) ([]string, error) {
		return []string{"v0.1.0", "v0.2.0"}, nil
	}))
	_, err := syncDependencies(&Options{Path: root}, io.Discard)
	var unsupported *libraryapi.UnsupportedMajorError
	require.ErrorAs(t, err, &unsupported)
	assert.Equal(t, []string{"example.com/lib@v0.2.0"}, *calls)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Contains(t, ds[0].Hint, "unobin deps get")
	require.NoFileExists(t, filepath.Join(root, deps.ProjectFileName))
	require.NoFileExists(t, filepath.Join(root, deps.ProjectLockFileName))
}

func TestGetChecksCurrentLocalReplacementMetadata(t *testing.T) {
	root := t.TempDir()
	factory := ubtest.ReadValidFixture(t,
		"testdata/ub/library-compatibility", "factory")
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
	library := deps.Dependency{URL: "example.com/lib"}
	local := candidateLibrarySource(t, library.URL, "1.0")
	project := &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{
			library: {Version: deps.ReplacementSentinel},
		},
		Replace: map[deps.Dependency]string{library: local.Path},
	}
	projectPath, lockPath := filepath.Join(root, deps.ProjectFileName),
		filepath.Join(root, deps.ProjectLockFileName)
	_, err := deps.WriteProjectChange(projectPath, project)
	require.NoError(t, err)
	calls := stubRecordingDependencyResolver(t, nil, nil)
	t.Cleanup(SetDepsListTagsForTest(func(string) ([]string, error) {
		return []string{"v0.1.0", "v0.2.0"}, nil
	}))
	operation, err := getDependency(&Options{Path: root},
		library.String(), io.Discard, nil)
	require.NoError(t, err)
	assert.Equal(t, "v0.2.0", operation.Version)
	require.Len(t, operation.Diagnostics, 1)
	details := operation.Diagnostics[0].LibraryCompatibility
	assert.Equal(t, local.Path, details.Replacement)
	assert.Empty(t, details.Commit)
	assert.Empty(t, details.Version)
	lock, err := deps.ReadProjectLock(os.DirFS(root))
	require.NoError(t, err)
	assert.Empty(t, lock.Deps)
	beforeProject, err := os.ReadFile(projectPath)
	require.NoError(t, err)
	beforeLock, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	path := filepath.Join(local.Path, "library.go")
	code, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(code),
		`RequiredAPI: "1.0"`, `RequiredAPI: "2.0"`)), 0o644))
	_, err = getDependency(&Options{Path: root},
		library.String()+"@v0.2.0", io.Discard, nil)
	var unsupported *libraryapi.UnsupportedMajorError
	require.ErrorAs(t, err, &unsupported)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, local.Path, ds[0].LibraryCompatibility.Replacement)
	assert.Empty(t, ds[0].LibraryCompatibility.Commit)
	assert.Empty(t, *calls)
	after, err := os.ReadFile(projectPath)
	require.NoError(t, err)
	assert.Equal(t, beforeProject, after)
	after, err = os.ReadFile(lockPath)
	require.NoError(t, err)
	assert.Equal(t, beforeLock, after)
}

func TestRejectedExactQueriesKeepExistingDependencyFiles(t *testing.T) {
	for _, test := range []struct {
		name, query, transitive string
	}{
		{name: "exact incompatible release", query: "v0.2.0", transitive: "v0.1.0"},
		{name: "incompatible transitive floor", query: "v0.1.0", transitive: "v0.2.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			factory := ubtest.ReadValidFixture(t, "testdata/ub/dependency-candidates", "factory")
			require.NoError(t, os.WriteFile(
				filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
			app, library := deps.Dependency{URL: "example.com/app"},
				deps.Dependency{URL: "example.com/lib"}
			projectPath, lockPath := filepath.Join(root, deps.ProjectFileName),
				filepath.Join(root, deps.ProjectLockFileName)
			project := &deps.Project{Requires: map[deps.Dependency]deps.Requirement{
				app: {Version: "v0.1.0"}, library: {Version: "v0.1.0"},
			}}
			_, err := deps.WriteProjectChange(projectPath, project)
			require.NoError(t, err)
			lock := deps.NewProjectLock()
			lock.ToolchainVersion = "v0.1.0"
			lock.Deps[library.String()] = &deps.ProjectLockDep{
				Kind: deps.ProjectLockKindGo, Version: "v0.1.0", Commit: "previous-commit",
			}
			_, err = deps.WriteProjectLockChange(lockPath, lock)
			require.NoError(t, err)
			beforeProject, err := os.ReadFile(projectPath)
			require.NoError(t, err)
			beforeLock, err := os.ReadFile(lockPath)
			require.NoError(t, err)
			appDir := t.TempDir()
			body := ubtest.ReadValidFixture(t, "testdata/ub/dependency-candidates", "library")
			require.NoError(t, os.WriteFile(
				filepath.Join(appDir, "library.ub"), []byte(body), 0o644))
			_, err = deps.WriteProjectChange(
				filepath.Join(appDir, deps.ProjectFileName), &deps.Project{
					Requires: map[deps.Dependency]deps.Requirement{
						library: {Version: test.transitive},
					},
				})
			require.NoError(t, err)
			stubRecordingDependencyResolver(t, map[string]*resolve.Source{
				remoteSourceKey(app.URL, "", "v0.1.0"): {
					FS: os.DirFS(appDir), Path: appDir, Commit: "app-commit",
				},
				remoteSourceKey(library.URL, "", "v0.1.0"): candidateLibrarySource(
					t, library.URL, "1.0"),
				remoteSourceKey(library.URL, "", "v0.2.0"): candidateLibrarySource(
					t, library.URL, "2.0"),
			}, nil)
			t.Cleanup(SetDepsListTagsForTest(func(string) ([]string, error) {
				return []string{"v0.1.0", "v0.2.0"}, nil
			}))
			operation, err := getDependency(&Options{Path: root},
				library.String()+"@"+test.query, io.Discard, nil)
			var unsupported *libraryapi.UnsupportedMajorError
			require.ErrorAs(t, err, &unsupported)
			assert.Nil(t, operation)
			diagnostics := diagnostic.FromError(err, diagnostic.ConvertOptions{})
			require.Len(t, diagnostics, 1)
			assert.Equal(t, "unobin.library-api.unsupported-major", diagnostics[0].Code)
			assert.Equal(t, "v0.2.0", diagnostics[0].LibraryCompatibility.Version)
			assert.Equal(t, test.query, diagnostics[0].LibraryCompatibility.CandidateVersion)
			if test.transitive == "v0.2.0" {
				assert.Equal(t, []diagnostic.LibraryRequirementStep{
					{Dependency: deps.ProjectFileName, Requires: app.String(),
						MinimumVersion: "v0.1.0"},
					{Dependency: app.String(), Version: "v0.1.0", Requires: library.String(),
						MinimumVersion: "v0.2.0"},
				}, diagnostics[0].LibraryCompatibility.RequirementChain)
			}
			after, err := os.ReadFile(projectPath)
			require.NoError(t, err)
			assert.Equal(t, beforeProject, after)
			after, err = os.ReadFile(lockPath)
			require.NoError(t, err)
			assert.Equal(t, beforeLock, after)
		})
	}
}
