package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type echoAction struct {
	Echo string `mapstructure:"echo"`
}

func (a *echoAction) Run(_ context.Context) (any, error) {
	return map[string]any{"echo": a.Echo, "len": int64(len(a.Echo))}, nil
}

type lookupDataSource struct {
	Key string `mapstructure:"key"`
}

func (d *lookupDataSource) Read(_ context.Context) (any, error) {
	return map[string]any{"value": "looked-up:" + d.Key}, nil
}

type failingAction struct{}

func (failingAction) Run(_ context.Context) (any, error) {
	return nil, errors.New("intentional failure")
}

func testModules() map[string]*Module {
	return map[string]*Module{
		"core": {
			Name: "core",
			Actions: map[string]ActionType{
				"echo": {
					Name: "echo",
					New:  func() Action { return &echoAction{} },
				},
				"fail": {
					Name: "fail",
					New:  func() Action { return failingAction{} },
				},
			},
			DataSources: map[string]DataSourceType{
				"lookup": {
					Name: "lookup",
					New:  func() DataSource { return &lookupDataSource{} },
				},
			},
		},
	}
}

func runExecutor(t *testing.T, src string, inputs map[string]any) (*ExecResult, error) {
	t.Helper()
	f := parseStack(t, src)
	g := BuildDAG(f)
	exec := &Executor{
		DAG:     g,
		Modules: testModules(),
		Inputs:  inputs,
	}
	return exec.Run(context.Background())
}

func TestExecutorOutputOnly(t *testing.T) {
	res, err := runExecutor(t, `
outputs: {
  region: var.region
}
`, map[string]any{"region": "us-east-1"})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"region": "us-east-1"}, res.Outputs)
}

func TestExecutorActionRuns(t *testing.T) {
	res, err := runExecutor(t, `
actions: {
  core: {
    echo: {
      hi: { echo: 'hello' }
    }
  }
}
outputs: {
  said:    action.core.echo.hi.echo
  letters: action.core.echo.hi.len
}
`, nil)
	require.NoError(t, err)
	require.Equal(t, "hello", res.Outputs["said"])
	require.Equal(t, int64(5), res.Outputs["letters"])
}

func TestExecutorInputFlowsToAction(t *testing.T) {
	res, err := runExecutor(t, `
actions: {
  core: {
    echo: {
      greet: { echo: var.name }
    }
  }
}
outputs: {
  said: action.core.echo.greet.echo
}
`, map[string]any{"name": "world"})
	require.NoError(t, err)
	require.Equal(t, "world", res.Outputs["said"])
}

func TestExecutorDataSource(t *testing.T) {
	res, err := runExecutor(t, `
data: {
  core: {
    lookup: {
      it: { key: var.key }
    }
  }
}
outputs: {
  found: data.core.lookup.it.value
}
`, map[string]any{"key": "abc"})
	require.NoError(t, err)
	require.Equal(t, "looked-up:abc", res.Outputs["found"])
}

func TestExecutorActionDependsOnAction(t *testing.T) {
	res, err := runExecutor(t, `
actions: {
  core: {
    echo: {
      first:  { echo: 'one' }
      second: { echo: action.core.echo.first.echo }
    }
  }
}
outputs: {
  result: action.core.echo.second.echo
}
`, nil)
	require.NoError(t, err)
	require.Equal(t, "one", res.Outputs["result"])
}

func TestExecutorPropagatesActionError(t *testing.T) {
	_, err := runExecutor(t, `
actions: {
  core: {
    fail: {
      f: {}
    }
  }
}
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "action.core.fail.f")
	require.Contains(t, err.Error(), "intentional failure")
}

func TestExecutorRejectsResources(t *testing.T) {
	_, err := runExecutor(t, `
resources: {
  core: { whatever: { x: {} } }
}
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "resources")
}

func TestExecutorUnknownModule(t *testing.T) {
	_, err := runExecutor(t, `
actions: {
  unknown: { echo: { x: { echo: 'hi' } } }
}
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")
}

func TestExecutorUnknownActionType(t *testing.T) {
	_, err := runExecutor(t, `
actions: {
  core: { not-a-type: { x: {} } }
}
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not-a-type")
}

func TestExecutorEmptyStack(t *testing.T) {
	res, err := runExecutor(t, `description: 'empty'`, nil)
	require.NoError(t, err)
	require.Empty(t, res.Outputs)
}
