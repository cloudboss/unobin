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

// applyVia runs `plan -o <tmp> [-c cfg]` then `apply <tmp>` and returns
// the apply output. Tests use this when they don't need to inspect the
// plan separately.
func applyVia(t *testing.T, info Info, configPath string) string {
	t.Helper()
	planFile := filepath.Join(t.TempDir(), "plan.json")
	args := []string{"plan", "-o", planFile}
	if configPath != "" {
		args = append(args, "-c", configPath)
	}
	_, err := runRoot(t, info, args...)
	require.NoError(t, err)
	out, err := runRoot(t, info, "apply", planFile)
	require.NoError(t, err)
	return out
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
	apply := applyVia(t, info, "")
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
	_ = applyVia(t, info, "")
	_, err := runRoot(t, info, "output", "missing")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no output")
}

func TestOutputBeforeApply(t *testing.T) {
	info := testInfo(t, `outputs: { x: 'y' }`)
	_, err := runRoot(t, info, "output")
	require.Error(t, err)
}

func TestPlanParseError(t *testing.T) {
	info := testInfo(t, `not valid syntax {{`)
	_, err := runRoot(t, info, "plan")
	require.Error(t, err)
}

func TestApplyWithConfigInputs(t *testing.T) {
	src := `
inputs: {
  greeting: { type: string }
}
actions: {
  core: {
    echo: { hi: { echo: var.greeting } }
  }
}
outputs: {
  said: action.core.echo.hi.echo
}
`
	info := testInfo(t, src)

	cfg := filepath.Join(t.TempDir(), "prod.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(`
inputs: {
  greeting: 'from-config'
}
`), 0o644))

	out := applyVia(t, info, cfg)
	require.Contains(t, out, "said = from-config")
}

func TestEnvVarOverridesConfig(t *testing.T) {
	src := `
inputs: {
  greeting: { type: string }
}
actions: {
  core: {
    echo: { hi: { echo: var.greeting } }
  }
}
outputs: {
  said: action.core.echo.hi.echo
}
`
	info := testInfo(t, src)

	cfg := filepath.Join(t.TempDir(), "prod.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(`
inputs: {
  greeting: 'from-config'
}
`), 0o644))

	t.Setenv("UB_VAR_greeting", "from-env")
	out := applyVia(t, info, cfg)
	require.Contains(t, out, "said = from-env")
}

func TestEnvVarUnderscoreToHyphen(t *testing.T) {
	src := `
inputs: {
  cluster-name: { type: string }
}
actions: {
  core: {
    echo: { hi: { echo: var.cluster-name } }
  }
}
outputs: {
  said: action.core.echo.hi.echo
}
`
	info := testInfo(t, src)

	t.Setenv("UB_VAR_cluster_name", "web-prod")
	out := applyVia(t, info, "")
	require.Contains(t, out, "said = web-prod")
}

func TestPlanShowsCreateBeforeApply(t *testing.T) {
	src := `
actions: {
  core: { echo: { hi: { echo: 'hello' } } }
}
`
	info := testInfo(t, src)
	out, err := runRoot(t, info, "plan")
	require.NoError(t, err)
	require.Contains(t, out, "> action.core.echo.hi")
	require.Contains(t, out, `echo: "hello"`)
	require.Contains(t, out, "Plan: 0 to create, 0 to update, 0 to replace, 0 to destroy, 1 to rerun.")
}

func TestPlanHidesSkipAfterApply(t *testing.T) {
	src := `
actions: {
  core: { echo: { hi: { echo: 'hello' } } }
}
`
	info := testInfo(t, src)
	_ = applyVia(t, info, "")

	out, err := runRoot(t, info, "plan")
	require.NoError(t, err)
	require.Contains(t, out, "No changes.")
}

func TestPlanEmpty(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	out, err := runRoot(t, info, "plan")
	require.NoError(t, err)
	require.Contains(t, out, "No changes.")
}

func TestPlanWritesPlanFile(t *testing.T) {
	src := `
actions: {
  core: { echo: { hi: { echo: 'hello' } } }
}
`
	info := testInfo(t, src)
	planFile := filepath.Join(t.TempDir(), "plan.json")

	_, err := runRoot(t, info, "plan", "-o", planFile)
	require.NoError(t, err)

	body, err := os.ReadFile(planFile)
	require.NoError(t, err)
	require.Contains(t, string(body), `"format-version": 1`)
	require.Contains(t, string(body), "action.core.echo.hi")
}

func TestApplyConsumesPlanFile(t *testing.T) {
	src := `
actions: {
  core: { echo: { hi: { echo: 'hello world' } } }
}
outputs: {
  said: action.core.echo.hi.echo
}
`
	info := testInfo(t, src)
	planFile := filepath.Join(t.TempDir(), "plan.json")

	_, err := runRoot(t, info, "plan", "-o", planFile)
	require.NoError(t, err)

	out, err := runRoot(t, info, "apply", planFile)
	require.NoError(t, err)
	require.Contains(t, out, "said = hello world")
}

func TestPlanMissingConfigFile(t *testing.T) {
	info := testInfo(t, "description: 'x'")
	_, err := runRoot(t, info, "plan", "-c", "/no/such/path.ub")
	require.Error(t, err)
}

func TestApplyMissingPlanFile(t *testing.T) {
	info := testInfo(t, "description: 'x'")
	_, err := runRoot(t, info, "apply", "/no/such/plan.json")
	require.Error(t, err)
}

func TestApplyRequiresPlanFile(t *testing.T) {
	info := testInfo(t, "description: 'x'")
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
	require.True(t, subs["plan"])
	require.True(t, subs["apply"])
	require.True(t, subs["output"])
	require.True(t, subs["schema"])
	require.True(t, subs["state"])
}

func TestStateListAndShow(t *testing.T) {
	src := `
actions: {
  core: { echo: { hi: { echo: 'hello' } } }
}
outputs: {
  said: action.core.echo.hi.echo
}
`
	info := testInfo(t, src)
	_ = applyVia(t, info, "")

	listOut, err := runRoot(t, info, "state", "list")
	require.NoError(t, err)
	require.Contains(t, listOut, "* ")

	showOut, err := runRoot(t, info, "state", "show")
	require.NoError(t, err)
	require.Contains(t, showOut, "stack:")
	require.Contains(t, showOut, "test-stack")
	require.Contains(t, showOut, "action.core.echo.hi")
	require.Contains(t, showOut, `said = "hello"`)
}

func TestSchema(t *testing.T) {
	src := `
inputs: {
  greeting: {
    type:        string
    description: 'a friendly word'
  }
  size: {
    type:    optional(integer, 3)
    minimum: 1
  }
  hosts: {
    type: list(string)
  }
}
`
	info := testInfo(t, src)
	out, err := runRoot(t, info, "schema")
	require.NoError(t, err)

	require.Contains(t, out, "greeting: string")
	require.Contains(t, out, "a friendly word")
	require.Contains(t, out, "size: optional(integer, 3)")
	require.Contains(t, out, "hosts: list(string)")
}

func TestSchemaEmpty(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	out, err := runRoot(t, info, "schema")
	require.NoError(t, err)
	require.Contains(t, out, "No inputs declared.")
}

// Ensure t.TempDir is visible to the loadStore call (which writes to
// `.unobin/state` relative to cwd) by chdir-ing in testInfo.
var _ = filepath.Join
