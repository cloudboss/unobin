package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/cloudboss/unobin/pkg/state"
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
		Store:   newStateStore(t),
		Stack:   state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
	}
	return exec.Run(context.Background())
}

func TestExecutorRequiresStore(t *testing.T) {
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, `description: 'x'`)),
		Modules: testModules(),
	}
	_, err := exec.Run(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Store")
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

type resourceCounters struct {
	creates int64
	updates int64
	deletes int64
}

type countingResource struct {
	Name string `mapstructure:"name"`
	Size int64  `mapstructure:"size"`

	counters *resourceCounters
}

func (r *countingResource) Create(_ context.Context) (any, error) {
	atomic.AddInt64(&r.counters.creates, 1)
	return map[string]any{"id": "fake-" + r.Name, "name": r.Name, "size": r.Size}, nil
}

func (r *countingResource) Read(_ context.Context, prior any) (any, error) {
	return prior, nil
}

func (r *countingResource) Update(_ context.Context, prior any) (any, error) {
	atomic.AddInt64(&r.counters.updates, 1)
	m, _ := prior.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	m["name"] = r.Name
	m["size"] = r.Size
	return m, nil
}

func (r *countingResource) Delete(_ context.Context, _ any) error {
	atomic.AddInt64(&r.counters.deletes, 1)
	return nil
}

func (r *countingResource) ReplaceFields() []string {
	return nil
}

func resourceModules(c *resourceCounters) map[string]*Module {
	return map[string]*Module{
		"core": {
			Name: "core",
			Resources: map[string]ResourceType{
				"thing": {
					Name:          "thing",
					SchemaVersion: 1,
					New:           func() Resource { return &countingResource{counters: c} },
				},
			},
		},
	}
}

func TestExecutorCreatesResource(t *testing.T) {
	src := `
resources: {
  core: {
    thing: { one: { name: 'alpha', size: 1 } }
  }
}
outputs: {
  id: resource.core.thing.one.id
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	exec := &Executor{
		DAG:     BuildDAG(parseStack(t, src)),
		Modules: resourceModules(&c),
		Store:   store,
		Stack:   stack,
	}
	res, err := exec.Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, "fake-alpha", res.Outputs["id"])
	require.Equal(t, int64(1), atomic.LoadInt64(&c.creates))
	require.Equal(t, int64(0), atomic.LoadInt64(&c.updates))
}

func TestExecutorSameInputsNoCreateOrUpdate(t *testing.T) {
	src := `
resources: {
  core: {
    thing: { one: { name: 'alpha', size: 1 } }
  }
}
`
	var c resourceCounters
	runExecutorTwice(t, src, resourceModules(&c))
	require.Equal(t, int64(1), atomic.LoadInt64(&c.creates))
	require.Equal(t, int64(0), atomic.LoadInt64(&c.updates))
}

func TestExecutorChangedInputsTriggersUpdate(t *testing.T) {
	first := `
resources: {
  core: {
    thing: { one: { name: 'alpha', size: 1 } }
  }
}
`
	second := `
resources: {
  core: {
    thing: { one: { name: 'alpha', size: 9 } }
  }
}
`
	var c resourceCounters
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	mods := resourceModules(&c)

	_, err := (&Executor{
		DAG: BuildDAG(parseStack(t, first)), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)
	_, err = (&Executor{
		DAG: BuildDAG(parseStack(t, second)), Modules: mods, Store: store, Stack: stack,
	}).Run(context.Background())
	require.NoError(t, err)

	require.Equal(t, int64(1), atomic.LoadInt64(&c.creates))
	require.Equal(t, int64(1), atomic.LoadInt64(&c.updates))
}

func TestExecutorResourceMissingType(t *testing.T) {
	_, err := runExecutor(t, `
resources: {
  core: { not-a-thing: { x: {} } }
}
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not-a-thing")
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

type countingAction struct {
	Echo string `mapstructure:"echo"`
	runs *int64
}

func (a *countingAction) Run(_ context.Context) (any, error) {
	atomic.AddInt64(a.runs, 1)
	return map[string]any{"echo": a.Echo}, nil
}

func newStateStore(t *testing.T) *state.LocalStore {
	t.Helper()
	s, err := state.NewLocalStore(t.TempDir(), "test-stack", "prod", state.NoopEncrypter{})
	require.NoError(t, err)
	return s
}

func runExecutorTwice(t *testing.T, src string, modules map[string]*Module) (*ExecResult, *ExecResult) {
	t.Helper()
	store := newStateStore(t)
	stack := state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"}
	g := BuildDAG(parseStack(t, src))

	first, err := (&Executor{DAG: g, Modules: modules, Store: store, Stack: stack}).Run(context.Background())
	require.NoError(t, err)
	second, err := (&Executor{DAG: g, Modules: modules, Store: store, Stack: stack}).Run(context.Background())
	require.NoError(t, err)
	return first, second
}

func countingModules(runs *int64) map[string]*Module {
	return map[string]*Module{
		"core": {
			Name: "core",
			Actions: map[string]ActionType{
				"echo": {
					Name: "echo",
					New:  func() Action { return &countingAction{runs: runs} },
				},
			},
		},
	}
}

func TestExecutorPersistsSnapshot(t *testing.T) {
	store := newStateStore(t)
	exec := &Executor{
		DAG: BuildDAG(parseStack(t, `
actions: {
  core: {
    echo: { hi: { echo: 'hello' } }
  }
}
`)),
		Modules: testModules(),
		Store:   store,
		Stack:   state.StackInfo{Name: "test-stack", Version: "v0", Commit: "c0"},
	}
	res, err := exec.Run(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, res.WrittenRev)

	gotRev, err := store.CurrentRev()
	require.NoError(t, err)
	require.Equal(t, res.WrittenRev, gotRev)

	snap, err := store.Current()
	require.NoError(t, err)
	require.Len(t, snap.Entries, 1)
	require.Equal(t, "action.core.echo.hi", snap.Entries[0].Address)
	require.Equal(t, state.EntryAction, snap.Entries[0].Type)
	require.NotEmpty(t, snap.Entries[0].TriggerHash)
}

func TestExecutorSkipsActionWhenInputsUnchanged(t *testing.T) {
	src := `
actions: {
  core: {
    echo: { hi: { echo: 'hello' } }
  }
}
`
	var runs int64
	runExecutorTwice(t, src, countingModules(&runs))
	require.Equal(t, int64(1), atomic.LoadInt64(&runs),
		"action should run once across two executions when inputs are unchanged")
}

func TestExecutorAlwaysTriggerReruns(t *testing.T) {
	src := `
actions: {
  core: {
    echo: {
      hi: {
        @trigger: 'always'
        echo:     'hello'
      }
    }
  }
}
`
	var runs int64
	runExecutorTwice(t, src, countingModules(&runs))
	require.Equal(t, int64(2), atomic.LoadInt64(&runs),
		"action with @trigger: 'always' should run on every execution")
}

func TestExecutorExplicitTriggerSkipsWhenSame(t *testing.T) {
	src := `
actions: {
  core: {
    echo: {
      hi: {
        @trigger: 'fixed-key'
        echo:     'hello'
      }
    }
  }
}
`
	var runs int64
	runExecutorTwice(t, src, countingModules(&runs))
	require.Equal(t, int64(1), atomic.LoadInt64(&runs))
}

func TestExecutorPropagatesSkippedOutputs(t *testing.T) {
	src := `
actions: {
  core: {
    echo: { hi: { echo: 'cached-value' } }
  }
}
outputs: {
  said: action.core.echo.hi.echo
}
`
	var runs int64
	first, second := runExecutorTwice(t, src, countingModules(&runs))
	require.Equal(t, "cached-value", first.Outputs["said"])
	require.Equal(t, "cached-value", second.Outputs["said"],
		"skipped action's outputs should still flow to downstream references")
}
