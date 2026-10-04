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

	cmddeps "github.com/cloudboss/unobin/cmd/unobin/root/deps"
	"github.com/cloudboss/unobin/internal/cmdconfig"
	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/project"
)

func TestDependencyPartialCommandErrorGolden(t *testing.T) {
	root := &cobra.Command{Use: "unobin"}
	parent := &cobra.Command{Use: "deps"}
	command := &cobra.Command{Use: "sync"}
	root.AddCommand(parent)
	parent.AddCommand(command)
	var out bytes.Buffer
	command.SetOut(&out)
	result := &project.DependencyWriteResult{Files: []filechange.Change{{
		Path: deps.ProjectFileName, Action: filechange.ActionCreated,
	}}}

	err := cmdout.WriteOperationError(
		command, cmdout.FormatJSON, result.Files, errors.New("lock write failed"),
	)
	require.Error(t, err)
	want, err := os.ReadFile("testdata/dependency-command-error-partial.json")
	require.NoError(t, err)
	require.Equal(t, string(want), out.String())
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
				local := compatibleLibrarySource(t)
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
				stubCompileResolver(t, nil)
				t.Cleanup(cmdconfig.SetDepsListTagsForTest(func(string) ([]string, error) {
					return []string{"v0.1.0"}, nil
				}))
				resetFlags(cmddeps.GetCmd)
				resetFlags(cmddeps.SyncCmd)
				command := &cobra.Command{Use: "unobin", SilenceUsage: true, SilenceErrors: true}
				command.AddCommand(DepsCmd)
				var stdout, stderr bytes.Buffer
				command.SetOut(&stdout)
				command.SetErr(&stderr)
				args := []string{
					"deps", verb, "--path", root, "--replace-unobin", findUnobinRoot(t),
					"--format", format,
				}
				if verb == "get" {
					args = append(args, dep.String())
				}
				command.SetArgs(args)
				err = command.Execute()
				require.NoError(t, err)
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
