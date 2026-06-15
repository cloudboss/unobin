package runtime_test

import (
	"context"
	"errors"
	osexec "os/exec"
	"sync/atomic"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"
)

// applyOnce drives one Plan-then-ApplyPlan cycle through the exec,
// round-tripping the plan bytes the way a real stack binary would. It
// is the only apply entry point; there is no apply-without-plan path.
func applyOnce(t *testing.T, exec *runtime.Executor) *runtime.ExecResult {
	t.Helper()
	ctx := context.Background()
	plan, err := exec.Plan(ctx)
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	pf, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	res, err := exec.ApplyPlan(ctx, pf)
	require.NoError(t, err)
	return res
}

// runStack parses, validates, builds the DAG, and drives one
// Plan-and-ApplyPlan cycle through the executor.
func runStack(t *testing.T, src string, inputs map[string]any) *runtime.ExecResult {
	t.Helper()
	f, err := lang.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)

	errs := lang.ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "validate: %v", errsAsStrings(errs))

	store, err := local.NewStore(t.TempDir(), "demo-stack", "test", encrypters.Noop{})
	require.NoError(t, err)

	libs := map[string]*runtime.Library{
		"core": testCommandLibrary(),
	}
	exec := &runtime.Executor{
		DAG:       runtime.BuildDAG(f, libs),
		Libraries: libs,
		Inputs:    inputs,
		Source:    f,
		Store:     store,
		Factory:   state.FactoryInfo{Name: "demo-stack", Version: "v0", ContentRevision: "c0"},
	}
	return applyOnce(t, exec)
}

// testCommandLibrary builds the action library the integration stacks
// run against: a process runner and a script runner returning raw
// stdout, so plan-and-apply cycles execute real commands without
// depending on a published library.
func testCommandLibrary() *runtime.Library {
	return &runtime.Library{
		Name: "core",
		Actions: map[string]runtime.ActionRegistration{
			"command": runtime.MakeAction[commandAction, any](),
			"script":  runtime.MakeAction[scriptAction, any](),
		},
	}
}

// commandAction execs argv and captures raw stdout.
type commandAction struct {
	Argv []string
}

func (a *commandAction) Run(ctx context.Context, _ any) (any, error) {
	if len(a.Argv) == 0 {
		return nil, errors.New("argv is required")
	}
	out, err := osexec.CommandContext(ctx, a.Argv[0], a.Argv[1:]...).Output()
	if err != nil {
		return nil, err
	}
	return map[string]any{"stdout": string(out)}, nil
}

// scriptAction runs a script through sh -c and captures raw stdout.
type scriptAction struct {
	Script string
}

func (a *scriptAction) Run(ctx context.Context, _ any) (any, error) {
	if a.Script == "" {
		return nil, errors.New("script is required")
	}
	out, err := osexec.CommandContext(ctx, "sh", "-c", a.Script).Output()
	if err != nil {
		return nil, err
	}
	return map[string]any{"stdout": string(out)}, nil
}

func errsAsStrings(l *lang.ErrorList) []string {
	es := l.Errors()
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Error()
	}
	return out
}

func TestStackRunsCoreCommand(t *testing.T) {
	src := `
inputs:  { greeting: { type: string } }
actions: { core.command.hello: { argv: ['echo', var.greeting] } }
outputs: { said: { value: action.core.command.hello.stdout } }
`
	res := runStack(t, src, map[string]any{"greeting": "world"})
	require.Equal(t, "world\n", res.Outputs["said"])
}

func TestStackUsesLocals(t *testing.T) {
	src := `
inputs:  { env: { type: string }, region: { type: string } }
locals:  { cluster: $'{{ var.env }}-{{ var.region }}', greeting: $'hello from {{ local.cluster }}' }
actions: { core.command.hello: { argv: ['echo', local.greeting] } }
outputs: { said: { value: action.core.command.hello.stdout }, cluster: { value: local.cluster } }
`
	res := runStack(t, src, map[string]any{"env": "prod", "region": "us-east-1"})
	require.Equal(t, "hello from prod-us-east-1\n", res.Outputs["said"])
	require.Equal(t, "prod-us-east-1", res.Outputs["cluster"])
}

func TestStackLocalReadsActionOutput(t *testing.T) {
	src := `
locals: { echoed: action.core.command.first.stdout }
actions: {
  core.command.first:  { argv: ['echo', 'one'] }
  core.command.second: { argv: ['echo', local.echoed] }
}
outputs: { result: { value: action.core.command.second.stdout } }
`
	res := runStack(t, src, nil)
	require.Equal(t, "one\n\n", res.Outputs["result"])
}

func TestStackChainsActions(t *testing.T) {
	src := `
actions: {
  core.command.first:  { argv: ['echo', 'one'] }
  core.command.second: { argv: ['echo', action.core.command.first.stdout] }
}
outputs: { result: { value: action.core.command.second.stdout } }
`
	res := runStack(t, src, nil)
	require.Equal(t, "one\n\n", res.Outputs["result"])
}

func TestStackHTTPAndScript(t *testing.T) {
	src := `
actions: { core.script.compute: { script: 'echo computed-value' } }
outputs: { result: { value: action.core.script.compute.stdout } }
`
	res := runStack(t, src, nil)
	require.Equal(t, "computed-value\n", res.Outputs["result"])
}

// stackTwiceCounts re-uses one Store across two apply cycles to verify
// state flows between executions.
func stackTwiceCounts(t *testing.T, src string) (int64, *runtime.ExecResult, *runtime.ExecResult) {
	t.Helper()
	store, err := local.NewStore(t.TempDir(), "demo-stack", "test", encrypters.Noop{})
	require.NoError(t, err)

	var runs int64
	libs := map[string]*runtime.Library{
		"test": {
			Name: "test",
			Actions: map[string]runtime.ActionRegistration{
				"counter": runtime.MakeActionWith[counter, any](
					func() *counter { return &counter{runs: &runs} },
				),
			},
		},
	}
	stack := state.FactoryInfo{Name: "demo-stack", Version: "v0", ContentRevision: "c0"}

	f, err := lang.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)

	first := applyOnce(t, &runtime.Executor{
		DAG: runtime.BuildDAG(f, libs), Libraries: libs, Store: store, Factory: stack,
	})
	second := applyOnce(t, &runtime.Executor{
		DAG: runtime.BuildDAG(f, libs), Libraries: libs, Store: store, Factory: stack,
	})
	return atomic.LoadInt64(&runs), first, second
}

type counter struct {
	Tag  string
	runs *int64
}

func (c *counter) Run(_ context.Context, _ any) (any, error) {
	atomic.AddInt64(c.runs, 1)
	return map[string]any{"tag": c.Tag}, nil
}

func TestStackSkipsUnchangedActionAcrossRuns(t *testing.T) {
	src := `
actions: { test.counter.it: { tag: 'fixed' } }
`
	count, _, _ := stackTwiceCounts(t, src)
	require.Equal(t, int64(1), count)
}
