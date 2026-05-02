package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// echoAction is a tiny test action: takes an Echo string, returns it
// in its outputs.
type echoAction struct {
	Echo string `mapstructure:"echo"`
}

func (a *echoAction) Run(_ context.Context) (any, error) {
	return map[string]any{"echo": a.Echo}, nil
}

func testInfo(t *testing.T, src string) Info {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	require.NoError(t, os.Chdir(t.TempDir()))

	return Info{
		StackName:    "test-stack",
		StackVersion: "v0.1.0",
		StackCommit:  "abcdef",
		StackSource:  src,
		Modules: map[string]*runtime.Module{
			"core": {
				Name: "core",
				Actions: map[string]runtime.ActionType{
					"echo": {
						Name: "echo",
						New:  func() runtime.Action { return &echoAction{} },
					},
				},
			},
		},
	}
}

func runRoot(t *testing.T, info Info, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd(info)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestVersion(t *testing.T) {
	info := testInfo(t, "description: 'x'")
	out, err := runRoot(t, info, "version")
	require.NoError(t, err)
	require.Contains(t, out, "test-stack v0.1.0 (commit abcdef)")
}

func TestApplyAndOutput(t *testing.T) {
	info := testInfo(t, `
actions: {
  core: {
    echo: { hi: { echo: 'hello world' } }
  }
}
outputs: {
  said: action.core.echo.hi.echo
}
`)
	apply, err := runRoot(t, info, "apply")
	require.NoError(t, err)
	require.Contains(t, apply, "said = hello world")

	all, err := runRoot(t, info, "output")
	require.NoError(t, err)
	require.Contains(t, all, "said = hello world")

	one, err := runRoot(t, info, "output", "said")
	require.NoError(t, err)
	require.Contains(t, one, "hello world")
}

func TestOutputUnknownName(t *testing.T) {
	info := testInfo(t, `outputs: { x: 'y' }`)
	_, err := runRoot(t, info, "apply")
	require.NoError(t, err)
	_, err = runRoot(t, info, "output", "missing")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no output")
}

func TestOutputBeforeApply(t *testing.T) {
	info := testInfo(t, `outputs: { x: 'y' }`)
	_, err := runRoot(t, info, "output")
	require.Error(t, err)
}

func TestApplyParseError(t *testing.T) {
	info := testInfo(t, `not valid syntax {{`)
	_, err := runRoot(t, info, "apply")
	require.Error(t, err)
}

func TestRootIsCobraTree(t *testing.T) {
	info := testInfo(t, "description: 'x'")
	root := newRootCmd(info)
	require.IsType(t, &cobra.Command{}, root)
	require.Equal(t, "test-stack", root.Use)
	subs := map[string]bool{}
	for _, c := range root.Commands() {
		subs[c.Name()] = true
	}
	require.True(t, subs["version"])
	require.True(t, subs["apply"])
	require.True(t, subs["output"])
}

// Ensure t.TempDir is visible to the loadStore call (which writes to
// `.unobin/state` relative to cwd) by chdir-ing in testInfo.
var _ = filepath.Join
