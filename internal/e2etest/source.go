package e2etest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cmdroot "github.com/cloudboss/unobin/cmd/unobin/root"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func runSourceCase(t *testing.T, cfg config, executable string, c SourceCase) {
	t.Helper()
	workspace := copyCaseToWorkspace(t, c.Dir)
	if err := copySourceModules(workspace, cfg.e2eLibraryDir); err != nil {
		t.Fatal(err)
	}
	runRoot, cleanup, err := rootCommandRunner(workspace, c)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	expansions := map[string]string{
		"WORKSPACE":       workspace,
		"REPO_ROOT":       cfg.repoRoot,
		"E2E_LIBRARY_DIR": cfg.e2eLibraryDir,
	}
	for _, cmd := range c.Commands {
		cmd = sourceCommand(c, cmd)
		cmd = expandCommand(cmd, expansions)
		got, err := runSourceCommand(t.Context(), workspace, executable, c, cmd, runRoot)
		if err != nil {
			t.Fatalf("%s: %v", cmd.Name, err)
		}
		got = normalizeCommandResult(got, cfg.repoRoot)
		if err := compareCommandGoldens(c.Dir, cmd, got, *update); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.Files) == 0 {
		return
	}
	files, err := readFileResults(workspace, c.Files)
	if err != nil {
		t.Fatal(err)
	}
	if err := compareFileGoldens(c.Dir, c.Files, files, *update); err != nil {
		t.Fatal(err)
	}
}

func sourceCasesNeedProcess(cases []SourceCase) bool {
	for _, c := range cases {
		if sourceExecutor(c) == "process" {
			return true
		}
	}
	return false
}

func sourceExecutor(c SourceCase) string {
	if c.Executor != "" {
		return c.Executor
	}
	return "process"
}

func sourceCommand(c SourceCase, cmd Command) Command {
	if cmd.Dir == "" {
		cmd.Dir = c.RootPath
	}
	return cmd
}

func runSourceCommand(
	ctx context.Context,
	workspace string,
	executable string,
	c SourceCase,
	cmd Command,
	runRoot rootCommandFunc,
) (CommandResult, error) {
	if sourceExecutor(c) == "root" {
		return runRoot(ctx, workspace, cmd)
	}
	return runCommand(ctx, workspace, executable, cmd)
}

type rootCommandFunc func(context.Context, string, Command) (CommandResult, error)

func copySourceModules(workspace string, e2eLibraryDir string) error {
	if e2eLibraryDir == "" {
		return nil
	}
	target := filepath.Join(workspace, "modules", "e2elib")
	_, err := os.Stat(target)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat source module target: %w", err)
	}
	return copyTree(e2eLibraryDir, target)
}

func expandCommand(cmd Command, values map[string]string) Command {
	cmd.Args = expandWithValues(cmd.Args, values)
	cmd.Dir = expandStringWithValues(cmd.Dir, values)
	if len(cmd.Env) == 0 {
		return cmd
	}
	env := make(map[string]string, len(cmd.Env))
	for key, value := range cmd.Env {
		env[key] = expandStringWithValues(value, values)
	}
	cmd.Env = env
	return cmd
}

func expandWithValues(in []string, values map[string]string) []string {
	out := make([]string, 0, len(in))
	for _, value := range in {
		out = append(out, expandStringWithValues(value, values))
	}
	return out
}

func expandStringWithValues(value string, values map[string]string) string {
	return os.Expand(value, func(name string) string {
		if value, ok := values[name]; ok {
			return value
		}
		return "$" + name
	})
}

func rootCommandRunner(workspace string, c SourceCase) (rootCommandFunc, func(), error) {
	if sourceExecutor(c) != "root" {
		return nil, func() {}, nil
	}
	remotes, err := sourceRemoteMap(workspace, c.Remotes)
	if err != nil {
		return nil, nil, err
	}
	restoreResolver := cmdroot.SetCompileResolverForTest(func(root string) (resolve.Resolver, error) {
		return &sourceResolver{
			local:   resolve.NewLocalResolver(root),
			remotes: remotes,
		}, nil
	})
	restoreTags := cmdroot.SetDepsListTagsForTest(func(url string) ([]string, error) {
		if tags, ok := c.Tags[url]; ok {
			return tags, nil
		}
		return nil, fmt.Errorf("fake tags: no tags for %s", url)
	})
	cleanup := func() {
		restoreTags()
		restoreResolver()
	}
	return runRootCommand, cleanup, nil
}

func sourceRemoteMap(workspace string, remotes []RemoteSource) (map[string]*resolve.Source, error) {
	out := make(map[string]*resolve.Source, len(remotes))
	for _, remote := range remotes {
		path := filepath.Join(workspace, filepath.FromSlash(remote.Path))
		source := &resolve.Source{
			Commit: remote.Commit,
			FS:     os.DirFS(path),
		}
		if remote.SourcePath {
			source.Path = path
		}
		out[remote.Key] = source
	}
	return out, nil
}

type sourceResolver struct {
	local   *resolve.LocalResolver
	remotes map[string]*resolve.Source
}

func (r *sourceResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	if li, ok := ref.(*resolve.LocalImport); ok {
		return r.local.Resolve(li)
	}
	ri, ok := ref.(*resolve.RemoteImport)
	if !ok {
		return nil, fmt.Errorf("fake resolver: unsupported ref type %T", ref)
	}
	if src, found := r.remotes[sourceRemoteKey(ri.URL, ri.Subdir, ri.Version)]; found {
		return sourceWithFS(src), nil
	}
	if ri.Subdir != "" {
		prefix := ri.Subdir + "/"
		version, ok := strings.CutPrefix(ri.Version, prefix)
		if ok {
			key := sourceRemoteKey(ri.URL, ri.Subdir, version)
			if src, found := r.remotes[key]; found {
				return sourceWithFS(src), nil
			}
		}
	}
	return nil, fmt.Errorf(
		"fake resolver: no source for %s",
		sourceRemoteKey(ri.URL, ri.Subdir, ri.Version),
	)
}

func sourceWithFS(src *resolve.Source) *resolve.Source {
	if src == nil || src.FS != nil || src.Path == "" {
		return src
	}
	clone := *src
	clone.FS = os.DirFS(src.Path)
	return &clone
}

func sourceRemoteKey(url, subdir, version string) string {
	key := url + "@" + version
	if subdir != "" {
		key = url + "//" + subdir + "@" + version
	}
	return key
}

func runRootCommand(ctx context.Context, workspace string, cmd Command) (CommandResult, error) {
	dir, err := commandDir(workspace, cmd.Dir)
	if err != nil {
		return CommandResult{}, err
	}
	root := newSourceRootCommand()
	root.SetContext(ctx)
	root.SetArgs(cmd.Args)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	restoreDir, err := chdir(dir)
	if err != nil {
		return CommandResult{}, err
	}
	defer restoreDir()
	restoreEnv := setCommandEnv(cmd.Env)
	defer restoreEnv()
	err = root.Execute()
	result := CommandResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err != nil {
		result.ExitCode = 1
	}
	return result, nil
}

func newSourceRootCommand() *cobra.Command {
	resetSourceCommand(cmdroot.VersionCmd)
	resetSourceCommand(cmdroot.CompileCmd)
	resetSourceCommand(cmdroot.GenerateCmd)
	resetSourceCommand(cmdroot.FmtCmd)
	resetSourceCommand(cmdroot.PrintGraphCmd)
	resetSourceCommand(cmdroot.DepsCmd)
	root := &cobra.Command{
		Use:          "unobin",
		SilenceUsage: true,
	}
	root.AddCommand(
		cmdroot.VersionCmd,
		cmdroot.CompileCmd,
		cmdroot.GenerateCmd,
		cmdroot.FmtCmd,
		cmdroot.PrintGraphCmd,
		cmdroot.DepsCmd,
	)
	return root
}

func resetSourceCommand(cmd *cobra.Command) {
	resetFlagSet(cmd.Flags())
	resetFlagSet(cmd.PersistentFlags())
	for _, child := range cmd.Commands() {
		resetSourceCommand(child)
	}
}

func resetFlagSet(flags *pflag.FlagSet) {
	flags.VisitAll(func(f *pflag.Flag) {
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	})
}

func chdir(dir string) (func(), error) {
	old, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Chdir(dir); err != nil {
		return nil, err
	}
	return func() { _ = os.Chdir(old) }, nil
}

func setCommandEnv(env map[string]string) func() {
	previous := make(map[string]string, len(env))
	present := make(map[string]bool, len(env))
	for key, value := range env {
		old, ok := os.LookupEnv(key)
		previous[key] = old
		present[key] = ok
		_ = os.Setenv(key, value)
	}
	return func() {
		for key, value := range previous {
			if present[key] {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}
}

func buildUnobinCLI(ctx context.Context, repoRoot string, outDir string) (string, error) {
	binary := filepath.Join(outDir, "unobin")
	ldflags := "-X github.com/cloudboss/unobin/cmd/unobin/root.Version=v0.0.0"
	cmd := exec.CommandContext(
		ctx,
		"go",
		"build",
		"-ldflags",
		ldflags,
		"-o",
		binary,
		"./cmd/unobin",
	)
	cmd.Dir = repoRoot
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf(
			"build unobin CLI: %w\nstdout:\n%s\nstderr:\n%s",
			err,
			stdout.String(),
			stderr.String(),
		)
	}
	return binary, nil
}
