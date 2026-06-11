package runtime

import (
	"context"
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

// endpointConfiguration is the configuration struct of the fake
// configured library.
type endpointConfiguration struct {
	Endpoint *cfg.String
}

// echoResource exposes its input as an output, plus a computed id the
// create produces, so a test can derive a value that is never
// knowable from inputs alone. A changed value forces a replace, which
// regenerates the id.
type echoResource struct {
	Value string
}

func (r *echoResource) SchemaVersion() int { return 1 }
func (r *echoResource) Create(_ context.Context, _ any) (any, error) {
	return map[string]any{"value": r.Value, "id": "id-" + r.Value}, nil
}
func (r *echoResource) Read(_ context.Context, _, prior any) (any, error) { return prior, nil }
func (r *echoResource) Update(_ context.Context, _ any, _ Prior[echoResource, any]) (any, error) {
	return map[string]any{"value": r.Value, "id": "id-" + r.Value}, nil
}
func (r *echoResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *echoResource) ReplaceFields() []string                  { return []string{"value"} }

// configEchoResource records the configuration the runtime handed it,
// so a test can prove which value reached each CRUD call. readSeen
// and deleteSeen, when non-nil, collect the endpoint every Read and
// Delete receive.
type configEchoResource struct {
	readSeen   *[]string
	deleteSeen *[]string
}

func endpointOf(c any) string {
	conf, _ := c.(*endpointConfiguration)
	if conf == nil || conf.Endpoint == nil {
		return ""
	}
	return conf.Endpoint.Value
}

func (r *configEchoResource) SchemaVersion() int { return 1 }
func (r *configEchoResource) Create(_ context.Context, c any) (any, error) {
	return map[string]any{"endpoint": endpointOf(c)}, nil
}
func (r *configEchoResource) Read(_ context.Context, c any, prior any) (any, error) {
	if r.readSeen != nil {
		*r.readSeen = append(*r.readSeen, endpointOf(c))
	}
	return prior, nil
}
func (r *configEchoResource) Update(
	_ context.Context, c any, _ Prior[configEchoResource, any],
) (any, error) {
	return map[string]any{"endpoint": endpointOf(c)}, nil
}
func (r *configEchoResource) Delete(_ context.Context, c any, _ any) error {
	if r.deleteSeen != nil {
		*r.deleteSeen = append(*r.deleteSeen, endpointOf(c))
	}
	return nil
}
func (r *configEchoResource) ReplaceFields() []string { return nil }

func configuredLibraries() map[string]*Library {
	return configuredLibrariesRecording(nil, nil)
}

func configuredLibrariesRecording(readSeen, deleteSeen *[]string) map[string]*Library {
	return map[string]*Library{
		"fix": {
			Name: "fix",
			Configuration: &cfg.ConfigurationType{
				New: func() any { return &endpointConfiguration{} },
			},
			Resources: map[string]ResourceRegistration{
				"echo": MakeResource[echoResource, any](),
				"config-echo": MakeResourceWith[configEchoResource, any](
					func() *configEchoResource {
						return &configEchoResource{readSeen: readSeen, deleteSeen: deleteSeen}
					},
				),
			},
		},
	}
}

const internalConfigSrc = `
configurations: { fix.default: {}, fix.cluster: { endpoint: resource.fix.echo.src.value } }
resources: {
  fix.echo.src:        { value: 'https://cluster.example' }
  fix.config-echo.app: { @configuration: fix.cluster }
}
outputs: { got: { value: resource.fix.config-echo.app.endpoint } }
`

// An internal configuration evaluates during apply, after the nodes
// it reads and before the nodes that select it, and the decoded value
// reaches the consumer's CRUD call.
func TestApplyEvaluatesInternalConfiguration(t *testing.T) {
	libs := configuredLibraries()
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, internalConfigSrc), libs),
		Libraries: libs,
		Store:     newStateStore(t),
		Factory:   state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"},
	}
	res := applyOnce(t, exec)
	require.Equal(t, "https://cluster.example", res.Outputs["got"])
}

// On an unchanged world, the internal configuration evaluates from
// prior state during the plan, so the consumer's drift read runs with
// the real decoded configuration and the plan is a no-op. The second
// plan uses a fresh Executor, the way a separate invocation would.
func TestPlanEvaluatesInternalConfigurationFromState(t *testing.T) {
	var seen []string
	libs := configuredLibrariesRecording(&seen, nil)
	store := newStateStore(t)
	factory := state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG:       BuildDAG(parseStack(t, internalConfigSrc), libs),
		Libraries: libs,
		Store:     store,
		Factory:   factory,
	})

	seen = nil
	fresh := &Executor{
		DAG:       BuildDAG(parseStack(t, internalConfigSrc), libs),
		Libraries: libs,
		Store:     store,
		Factory:   factory,
	}
	plan, err := fresh.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"https://cluster.example"}, seen)
	require.Equal(t, DecisionNoOp,
		findStep(t, plan, "resource.fix.config-echo.app").Decision)
}

// configProbeData records the configuration its Read receives.
type configProbeData struct {
	readSeen *[]string
}

func (d *configProbeData) Read(_ context.Context, c any) (any, error) {
	if d.readSeen != nil {
		*d.readSeen = append(*d.readSeen, endpointOf(c))
	}
	return map[string]any{"value": "v"}, nil
}

const internalConfigDataSrc = `
configurations: { fix.default: {}, fix.cluster: { endpoint: resource.fix.echo.src.id } }
resources: { fix.echo.src: { value: 'https://cluster.example' } }
data:      { fix.probe.p: { @configuration: fix.cluster } }
`

// A data source whose configuration is still pending at plan defers
// its read to apply instead of reading with a nil configuration; the
// apply-time read then sees the real decoded value.
func TestDataReadDefersWhileConfigurationPending(t *testing.T) {
	var seen []string
	libs := configuredLibrariesRecording(&seen, nil)
	libs["fix"].DataSources = map[string]DataSourceRegistration{
		"probe": MakeDataSourceWith[configProbeData, any](
			func() *configProbeData { return &configProbeData{readSeen: &seen} },
		),
	}
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, internalConfigDataSrc), libs),
		Libraries: libs,
		Store:     newStateStore(t),
		Factory:   state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"},
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Empty(t, seen, "no read should run while the configuration is pending")
	ds := findStep(t, plan, "data.fix.probe.p")
	require.Equal(t, "fix.cluster", ds.DeferredRead)
	require.Equal(t, "fix.cluster", ds.Configuration)

	fresh := &Executor{
		DAG:       BuildDAG(parseStack(t, internalConfigDataSrc), libs),
		Libraries: libs,
		Store:     exec.Store,
		Factory:   exec.Factory,
	}
	_, err = planAndApply(fresh)
	require.NoError(t, err)
	require.Equal(t, []string{"id-https://cluster.example"}, seen)
}

const internalConfigVarSrc = `
configurations: { fix.default: {}, fix.cluster: { endpoint: resource.fix.echo.src.id } }
resources: {
  fix.echo.src:        { value: var.url }
  fix.config-echo.app: { @configuration: fix.cluster }
}
`

// When the node a configuration reads is changing this plan, the
// configuration is pending and consumer drift reads are skipped, not
// run with a nil configuration. The decision falls back to the
// stored state.
func TestDriftReadSkippedWhileConfigurationPending(t *testing.T) {
	var seen []string
	libs := configuredLibrariesRecording(&seen, nil)
	store := newStateStore(t)
	factory := state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG:       BuildDAG(parseStack(t, internalConfigVarSrc), libs),
		Libraries: libs,
		Inputs:    map[string]any{"url": "https://a"},
		Store:     store,
		Factory:   factory,
	})

	seen = nil
	fresh := &Executor{
		DAG:       BuildDAG(parseStack(t, internalConfigVarSrc), libs),
		Libraries: libs,
		Inputs:    map[string]any{"url": "https://b"},
		Store:     store,
		Factory:   factory,
	}
	plan, err := fresh.Plan(context.Background())
	require.NoError(t, err)
	require.Empty(t, seen, "no read should run while the configuration is pending")
	step := findStep(t, plan, "resource.fix.config-echo.app")
	require.Equal(t, "fix.cluster", step.DeferredRead)
	require.Equal(t, "fix.cluster", step.Configuration)
	require.Equal(t, DecisionNoOp, step.Decision)
}

// Destroy deletes run with the configuration the entries were created
// under, evaluated from prior state, since nothing applies this run.
func TestDestroyUsesInternalConfigurationFromState(t *testing.T) {
	var deletes []string
	libs := configuredLibrariesRecording(nil, &deletes)
	store := newStateStore(t)
	factory := state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG:       BuildDAG(parseStack(t, internalConfigSrc), libs),
		Libraries: libs,
		Store:     store,
		Factory:   factory,
	})

	down := &Executor{
		DAG:       BuildDAG(parseStack(t, internalConfigSrc), libs),
		Libraries: libs,
		Store:     store,
		Factory:   factory,
		Destroy:   true,
	}
	_, err := planAndApply(down)
	require.NoError(t, err)
	require.Equal(t, []string{"https://cluster.example"}, deletes)
}

// Refresh reads every entry with the configuration recorded for it,
// evaluated from prior state.
func TestRefreshUsesInternalConfigurationFromState(t *testing.T) {
	var reads []string
	libs := configuredLibrariesRecording(&reads, nil)
	store := newStateStore(t)
	factory := state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG:       BuildDAG(parseStack(t, internalConfigSrc), libs),
		Libraries: libs,
		Store:     store,
		Factory:   factory,
	})

	reads = nil
	fresh := &Executor{
		DAG:       BuildDAG(parseStack(t, internalConfigSrc), libs),
		Libraries: libs,
		Store:     store,
		Factory:   factory,
	}
	_, err := fresh.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"https://cluster.example"}, reads)
}
