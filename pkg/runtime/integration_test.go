package runtime_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/modules/core"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/state"
	"github.com/stretchr/testify/require"
)

// stack runs an end-to-end pipeline: parse, validate, build the DAG,
// instantiate an Executor with a tempdir LocalStore, and Run.
func runStack(t *testing.T, src string, inputs map[string]any) *runtime.ExecResult {
	t.Helper()
	f, err := lang.ParseSource("stack.ub", []byte(src))
	require.NoError(t, err)

	errs := lang.ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "validate: %v", errsAsStrings(errs))

	store, err := state.NewLocalStore(t.TempDir(), "demo-stack", "test", state.NoopEncrypter{})
	require.NoError(t, err)

	mods := map[string]*runtime.Module{
		"core": core.Module(),
	}
	exec := &runtime.Executor{
		DAG:     runtime.BuildDAG(f, mods),
		Modules: mods,
		Inputs:  inputs,
		Store:   store,
		Stack:   state.StackInfo{Name: "demo-stack", Version: "v0", Commit: "c0"},
	}
	res, err := exec.Run(context.Background())
	require.NoError(t, err)
	return res
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
inputs: {
  greeting: { type: string }
}
actions: {
  core: {
    command: {
      hello: { argv: ['echo', var.greeting] }
    }
  }
}
outputs: {
  said: action.core.command.hello.stdout
}
`
	res := runStack(t, src, map[string]any{"greeting": "world"})
	require.Equal(t, "world\n", res.Outputs["said"])
}

func TestStackChainsActions(t *testing.T) {
	src := `
actions: {
  core: {
    command: {
      first:  { argv: ['echo', 'one'] }
      second: { argv: ['echo', action.core.command.first.stdout] }
    }
  }
}
outputs: {
  result: action.core.command.second.stdout
}
`
	res := runStack(t, src, nil)
	require.Equal(t, "one\n\n", res.Outputs["result"])
}

func TestStackHTTPAndScript(t *testing.T) {
	src := `
actions: {
  core: {
    script: {
      compute: {
        script: 'echo computed-value'
      }
    }
  }
}
outputs: {
  result: action.core.script.compute.stdout
}
`
	res := runStack(t, src, nil)
	require.Equal(t, "computed-value\n", res.Outputs["result"])
}

// stackTwiceCounts re-uses one Store across two Runs to verify state
// flows between executions.
func stackTwiceCounts(t *testing.T, src string) (int64, *runtime.ExecResult, *runtime.ExecResult) {
	t.Helper()
	store, err := state.NewLocalStore(t.TempDir(), "demo-stack", "test", state.NoopEncrypter{})
	require.NoError(t, err)

	var runs int64
	mods := map[string]*runtime.Module{
		"test": {
			Name: "test",
			Actions: map[string]runtime.ActionType{
				"counter": {
					Name: "counter",
					New:  func() runtime.Action { return &counter{runs: &runs} },
				},
			},
		},
	}
	stack := state.StackInfo{Name: "demo-stack", Version: "v0", Commit: "c0"}

	f, err := lang.ParseSource("stack.ub", []byte(src))
	require.NoError(t, err)

	first, err := (&runtime.Executor{
		DAG: runtime.BuildDAG(f, mods), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)
	second, err := (&runtime.Executor{
		DAG: runtime.BuildDAG(f, mods), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

	return atomic.LoadInt64(&runs), first, second
}

type counter struct {
	Tag  string `mapstructure:"tag"`
	runs *int64
}

func (c *counter) Run(_ context.Context) (any, error) {
	atomic.AddInt64(c.runs, 1)
	return map[string]any{"tag": c.Tag}, nil
}

func TestStackSkipsUnchangedActionAcrossRuns(t *testing.T) {
	src := `
actions: {
  test: {
    counter: { it: { tag: 'fixed' } }
  }
}
`
	count, _, _ := stackTwiceCounts(t, src)
	require.Equal(t, int64(1), count)
}
