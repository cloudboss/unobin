package root

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
)

type dependencyPartialGolden struct {
	ProjectFile string              `json:"project-file"`
	LockFile    string              `json:"lock-file"`
	Direct      int                 `json:"direct"`
	Indirect    int                 `json:"indirect"`
	Selected    int                 `json:"selected"`
	Files       []filechange.Change `json:"files"`
	Error       string              `json:"error"`
}

func TestDependencyPartialCommandErrorGolden(t *testing.T) {
	root := &cobra.Command{Use: "unobin"}
	parent := &cobra.Command{Use: "deps"}
	command := &cobra.Command{Use: "sync"}
	root.AddCommand(parent)
	parent.AddCommand(command)
	var out bytes.Buffer
	command.SetOut(&out)
	result := &dependencyWriteResult{Files: []filechange.Change{{
		Path: deps.ProjectFileName, Action: filechange.ActionCreated,
	}}}

	err := dependencyCommandFailure(
		command, cmdout.FormatJSON, result, errors.New("lock write failed"),
	)
	require.Error(t, err)
	want, err := os.ReadFile("testdata/dependency-command-error-partial.json")
	require.NoError(t, err)
	require.Equal(t, string(want), out.String())
}

func TestDependencyWritePartialGolden(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, deps.ProjectLockFileName)
	require.NoError(t, os.Mkdir(lockPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(lockPath, "keep"), []byte("x"), 0o644))
	project := &deps.Project{Requires: map[deps.Dependency]deps.Requirement{}}
	projectLock := deps.NewProjectLock()
	projectLock.ToolchainVersion = "dev"

	result, writeErr := writeDependencyFiles(dir, project, projectLock)
	require.Error(t, writeErr)
	require.NotNil(t, result)
	view := dependencyPartialGolden{
		ProjectFile: result.ProjectFile,
		LockFile:    result.LockFile,
		Direct:      result.Direct,
		Indirect:    result.Indirect,
		Selected:    result.Selected,
		Files:       result.Files,
		Error:       strings.ReplaceAll(writeErr.Error(), dir, "$TMP"),
	}
	body, err := json.MarshalIndent(view, "", "  ")
	require.NoError(t, err)
	body = append(body, '\n')
	want, err := os.ReadFile("testdata/dependency-write-partial.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(body))
}

func TestDependencyCommandsReportEffectiveSources(t *testing.T) {
	for _, verb := range []string{"get", "sync"} {
		for _, format := range []string{"text", "json", "unobin"} {
			t.Run(verb+"/"+format, func(t *testing.T) {
				setCLIVersion(t, "v0.1.0")
				root := t.TempDir()
				factory := ubtest.ReadValidFixture(t,
					"testdata/ub/library-compatibility", "factory")
				require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"),
					[]byte(factory), 0o644))
				dep := deps.Dependency{URL: "example.com/lib"}
				local := candidateLibrarySource(t, dep.URL, "1.0")
				project := &deps.Project{
					UnobinVersion: "v9.0.0",
					Requires: map[deps.Dependency]deps.Requirement{
						dep: {Version: deps.ReplacementSentinel},
					},
					Replace: map[deps.Dependency]string{dep: local.Path},
				}
				_, err := deps.WriteProjectChange(
					filepath.Join(root, deps.ProjectFileName), project)
				require.NoError(t, err)
				calls := stubRecordingDependencyResolver(t, nil, nil)
				t.Cleanup(SetDepsListTagsForTest(func(string) ([]string, error) {
					return []string{"v0.1.0"}, nil
				}))
				command := &cobra.Command{Use: verb}
				addFormatFlag(command)
				require.NoError(t, command.Flags().Set("format", format))
				var stdout, stderr bytes.Buffer
				command.SetOut(&stdout)
				command.SetErr(&stderr)
				cfg := &depsSyncConfig{stackPath: root, replaceUnobin: findUnobinRoot(t)}
				if verb == "get" {
					err = runDepsGet(command, cfg, dep.String())
				} else {
					err = runDepsSync(command, cfg)
				}
				require.NoError(t, err)
				assert.Empty(t, *calls)
				if format == "text" {
					assert.Empty(t, stdout.String())
					assert.Contains(t, stderr.String(), "notice: Using local package "+dep.URL+
						" from "+local.Path)
					assert.Contains(t, stderr.String(), "notice: the project pins unobin v9.0.0")
					assert.Equal(t, 1, strings.Count(stderr.String(), "Using local package"))
				} else {
					assert.Empty(t, stderr.String())
					assert.Contains(t, stdout.String(), "unobin.library-api.module-source")
					assert.Contains(t, stdout.String(), "unobin.compile.replaced-toolchain")
					if format == "json" {
						var result struct {
							Diagnostics []diagnostic.Diagnostic `json:"diagnostics"`
						}
						require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
						require.Len(t, result.Diagnostics, 2)
						assert.Equal(t, local.Path,
							result.Diagnostics[1].LibraryCompatibility.Replacement)
						assert.Empty(t, result.Diagnostics[1].LibraryCompatibility.Version)
						assert.Empty(t, result.Diagnostics[1].LibraryCompatibility.Commit)
					}
				}
			})
		}
	}
}
