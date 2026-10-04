package root

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	cmddeps "github.com/cloudboss/unobin/cmd/unobin/root/deps"
	"github.com/cloudboss/unobin/internal/cmdconfig"
	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestCommandsCheckToolchainBeforeResolverOrTags(t *testing.T) {
	for _, command := range []string{"compile", "check", "print-graph", "sync", "get"} {
		for _, invalidCore := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/core=%t", command, invalidCore), func(t *testing.T) {
				root := t.TempDir()
				writeSchemaDependencyFactory(t, root)
				project := ubtest.ReadValidFixture(t, "testdata/ub/library-compatibility", "project")
				projectPath := filepath.Join(root, deps.ProjectFileName)
				require.NoError(t, os.WriteFile(projectPath, []byte(project), 0o644))
				t.Cleanup(cmdconfig.SetDepsListTagsForTest(func(string) ([]string, error) {
					t.Fatal("tags listed before checking the toolchain")
					return nil, nil
				}))
				stubCompileResolver(t, nil)
				cmdconfig.NewResolver = func(string) (resolve.Resolver, error) {
					t.Fatal("resolver created before checking the toolchain")
					return nil, nil
				}
				args := []string{command, "-p", filepath.Join(root, "factory.ub"), "--format", "json"}
				if command == "get" || command == "sync" {
					args = append([]string{"deps"}, args...)
				}
				if command == "get" {
					args = append(args, "example.com/aws@v0.1.0")
				}
				if command == "compile" {
					args = append(args, "-o", filepath.Join(root, "build"))
				}
				if invalidCore {
					args = append(args, "--replace-unobin", t.TempDir())
				}
				for _, command := range []*cobra.Command{
					CompileCmd, CheckCmd, PrintGraphCmd, cmddeps.GetCmd, cmddeps.SyncCmd,
				} {
					resetFlags(command)
				}
				rootCmd := &cobra.Command{Use: "unobin", SilenceUsage: true, SilenceErrors: true}
				rootCmd.AddCommand(CompileCmd, CheckCmd, PrintGraphCmd, DepsCmd)
				var output bytes.Buffer
				rootCmd.SetOut(&output)
				rootCmd.SetErr(&output)
				rootCmd.SetArgs(args)
				err := rootCmd.Execute()
				out := output.String()
				require.Error(t, err)
				code := "unobin.library-api.toolchain-pin"
				if invalidCore {
					code = "unobin.library-api.core-descriptor"
				}
				require.Contains(t, out, code)
				after, err := os.ReadFile(projectPath)
				require.NoError(t, err)
				require.Equal(t, project, string(after))
				require.NoFileExists(t, filepath.Join(root, deps.ProjectLockFileName))
				require.NoDirExists(t, filepath.Join(root, "build"))
			})
		}
	}
}

func TestCommandsUseCompatibilityContextForLibraryReads(t *testing.T) {
	for _, command := range []string{"compile", "check", "print-graph", "sync", "get"} {
		for _, condition := range []string{"newer minor", "injected minor", "core floor"} {
			t.Run(command+"/"+condition, func(t *testing.T) {
				root := t.TempDir()
				factory := ubtest.ReadValidFixture(t, "testdata/ub/library-compatibility", "factory")
				require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
				dep := deps.Dependency{URL: "example.com/lib"}
				project := &deps.Project{Requires: map[deps.Dependency]deps.Requirement{
					dep: {Version: "v0.1.0"},
				}}
				_, err := deps.WriteProjectChange(filepath.Join(root, deps.ProjectFileName), project)
				require.NoError(t, err)
				lock := deps.NewProjectLock()
				lock.ToolchainVersion = "v0.1.0"
				lock.Deps[dep.String()] = &deps.ProjectLockDep{
					Kind: deps.ProjectLockKindGo, Version: "v0.1.0", Commit: "c1",
				}
				_, err = deps.WriteProjectLockChange(filepath.Join(root, deps.ProjectLockFileName), lock)
				require.NoError(t, err)
				beforeProject, err := os.ReadFile(filepath.Join(root, deps.ProjectFileName))
				require.NoError(t, err)
				beforeLock, err := os.ReadFile(filepath.Join(root, deps.ProjectLockFileName))
				require.NoError(t, err)
				library := t.TempDir()
				require.NoError(t, os.CopyFS(library,
					os.DirFS("../../../pkg/deps/testdata/go/compatibility")))
				path := filepath.Join(library, "library.go")
				if condition != "core floor" {
					source, err := os.ReadFile(path)
					require.NoError(t, err)
					source = []byte(strings.ReplaceAll(string(source),
						`RequiredAPI: "1.0"`, `RequiredAPI: "1.1"`))
					require.NoError(t, os.WriteFile(path, source, 0o644))
				} else {
					mod := "module example.com/lib\n\ngo 1.26.2\n\n" +
						"require github.com/cloudboss/unobin v0.2.0\n"
					require.NoError(t, os.WriteFile(filepath.Join(library, "go.mod"), []byte(mod), 0o644))
				}
				if condition == "injected minor" {
					descriptor := libraryapi.Descriptor{
						FormatVersion: 1, ImplementedAPIs: []string{"1.1"}, GeneratorAPI: "1.1",
					}
					t.Cleanup(cmdconfig.SetLibraryAPIDescriptorForTest(&descriptor))
				}
				t.Cleanup(cmdconfig.SetDepsListTagsForTest(func(string) ([]string, error) {
					return []string{"v0.1.0"}, nil
				}))
				source := &resolve.Source{Path: library, Commit: "c1"}
				remotes := map[string]*resolve.Source{
					remoteSourceKey(dep.URL, "", "v0.1.0"): source,
					remoteSourceKey(dep.URL, "", "c1"):     source,
				}
				args := []string{command, "-p", filepath.Join(root, "factory.ub"), "--format", "json"}
				if command == "sync" || command == "get" {
					args = append([]string{"deps"}, args...)
				}
				if command == "get" {
					args = append(args, "example.com/lib@v0.1.0")
				}
				if command == "compile" {
					args = append(args, "-o", filepath.Join(root, "build"))
				}
				out, err := runCommandWithRemotes(t, remotes, args...)
				if condition == "injected minor" {
					require.NoError(t, err, out)
					return
				}
				require.Error(t, err)
				code := "unobin.library-api.newer-minor"
				if condition == "core floor" {
					code = "unobin.library-api.core-floor"
				}
				require.Contains(t, out, code)
				require.Contains(t, out, `"unobin-version":"v0.1.0"`)
				after, err := os.ReadFile(filepath.Join(root, deps.ProjectFileName))
				require.NoError(t, err)
				require.Equal(t, beforeProject, after)
				after, err = os.ReadFile(filepath.Join(root, deps.ProjectLockFileName))
				require.NoError(t, err)
				require.Equal(t, beforeLock, after)
				require.NoDirExists(t, filepath.Join(root, "build"))
			})
		}
	}
}
