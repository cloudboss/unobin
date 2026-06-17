package runtime

import (
	"context"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

// endpointConfiguration is the configuration struct of the fake
// configured library.
type endpointConfiguration struct {
	Endpoint *cfg.String
}

type requiredEndpointConfiguration struct {
	Endpoint cfg.String
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
	switch conf := c.(type) {
	case *endpointConfiguration:
		if conf == nil || conf.Endpoint == nil {
			return ""
		}
		return conf.Endpoint.Value
	case *requiredEndpointConfiguration:
		if conf == nil {
			return ""
		}
		return conf.Endpoint.Value
	default:
		return ""
	}
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
	return configuredLibrariesWithConfig(
		func() any { return &endpointConfiguration{} }, readSeen, deleteSeen)
}

func requiredConfiguredLibraries() map[string]*Library {
	return configuredLibrariesWithConfig(
		func() any { return &requiredEndpointConfiguration{} }, nil, nil)
}

func configuredLibrariesWithConfig(
	newConfig func() any,
	readSeen, deleteSeen *[]string,
) map[string]*Library {
	return map[string]*Library{
		"fix": {
			Name: "fix",
			Configuration: &cfg.ConfigurationType{
				New: newConfig,
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

func configurationTestExecutor(
	t *testing.T,
	src string,
	libs map[string]*Library,
) *Executor {
	t.Helper()
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	return &Executor{DAG: dag, SyntaxSource: syntaxSource, Libraries: libs}
}

const internalConfigSrc = `
configurations: {
  fix {}
  cluster: fix { endpoint: resource.src.value }
}
resources: {
  src: fix.echo { value: 'https://cluster.example' }
  app: fix.config-echo { @configuration: configuration.cluster }
}
outputs: { got: { value: resource.app.endpoint } }
`

// An internal configuration evaluates during apply, after the nodes
// it reads and before the nodes that select it, and the decoded value
// reaches the consumer's CRUD call.
func TestApplyEvaluatesInternalConfiguration(t *testing.T) {
	libs := configuredLibraries()
	exec := configurationTestExecutor(t, internalConfigSrc, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	res := applyOnce(t, exec)
	require.Equal(t, "https://cluster.example", res.Outputs["got"])
}

func TestStackConfigurationOverridesFactoryConfiguration(t *testing.T) {
	src := `
configurations: { cluster: fix { endpoint: var.missing } }
resources: { app: fix.config-echo { @configuration: configuration.cluster } }
outputs: { got: { value: resource.app.endpoint } }
`
	libs := requiredConfiguredLibraries()
	exec := configurationTestExecutor(t, src, libs)
	exec.Configurations = ConfigTable{
		{Alias: "fix", Name: "cluster"}: &requiredEndpointConfiguration{
			Endpoint: cfg.String{Value: "https://stack.example"},
		},
	}
	exec.RawConfigurations = ConfigTable{
		{Alias: "fix", Name: "cluster"}: map[string]any{
			"endpoint": "https://stack.example",
		},
	}
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	res := applyOnce(t, exec)
	require.Equal(t, "https://stack.example", res.Outputs["got"])
}

func TestFactoryConfigurationErrorsWithoutStackOverride(t *testing.T) {
	src := `
configurations: { cluster: fix {} }
resources: { app: fix.config-echo { @configuration: configuration.cluster } }
`
	libs := requiredConfiguredLibraries()
	exec := configurationTestExecutor(t, src, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	_, err := exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "configuration.cluster")
	require.Contains(t, err.Error(), "endpoint")
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
	first := configurationTestExecutor(t, internalConfigSrc, libs)
	first.Store = store
	first.Factory = factory
	applyOnce(t, first)

	seen = nil
	fresh := configurationTestExecutor(t, internalConfigSrc, libs)
	fresh.Store = store
	fresh.Factory = factory
	plan, err := fresh.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"https://cluster.example"}, seen)
	require.Equal(t, DecisionNoOp,
		findStep(t, plan, "resource.app").Decision)
}

const expressionConfigSrc = `
configurations: {
  fix {}
  cluster: fix {
    endpoint: @core.join([resource.src.value], '')
  }
}
resources: {
  src: fix.echo { value: 'https://cluster.example' }
  app: fix.config-echo { @configuration: configuration.cluster }
}
outputs: { got: { value: resource.app.endpoint } }
`

// An internal configuration field may be an expression. One joining
// over a resource output is pending at first plan, defers with the
// configuration body, and evaluates live during apply, reaching the
// consumer's CRUD call like a literal field does.
func TestApplyEvaluatesExpressionConfiguration(t *testing.T) {
	libs := configuredLibraries()
	exec := configurationTestExecutor(t, expressionConfigSrc, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	res := applyOnce(t, exec)
	require.Equal(t, "https://cluster.example", res.Outputs["got"])
}

// A static expression field evaluates during the plan like a literal
// field, with the result on the step.
func TestPlanEvaluatesStaticExpressionConfiguration(t *testing.T) {
	src := `
configurations: {
  fix {}
  cluster: fix {
    endpoint: @core.join(['https://m.example'], '')
  }
}
resources: { app: fix.config-echo { @configuration: configuration.cluster } }
`
	libs := configuredLibraries()
	exec := configurationTestExecutor(t, src, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	step := findStep(t, plan, "configuration.cluster")
	require.Equal(t, map[string]any{"endpoint": "https://m.example"}, step.Inputs)
	require.Empty(t, step.UnresolvedInputs)
}

// Configuration declarations use selector bodies; a plain value entry
// is rejected before the runtime builds a graph.
func TestExpressionConfigurationMustEvaluateToObject(t *testing.T) {
	src := `factory: {
configurations: { cluster: 'nope' }
resources: { app: fix.config-echo { @configuration: configuration.cluster } }
}`
	_, err := syntax.ParseSource("factory.ub", []byte(src))
	require.Error(t, err)
	require.Contains(t, err.Error(),
		"configuration must be written as selector { ... } or name: selector { ... }")
}

func TestConfigurationAliasQualifiedReferenceFails(t *testing.T) {
	src := `
configurations: {
  cluster: fix { endpoint: configuration.fix.default.endpoint }
}
resources: { app: fix.config-echo { @configuration: configuration.cluster } }
`
	libs := configuredLibraries()
	exec := configurationTestExecutor(t, src, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	_, err := exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(),
		"configuration.cluster: a configuration reference has the form configuration.<name>")
}

func TestConfigurationReferenceToInternalFails(t *testing.T) {
	src := `
configurations: {
  base: fix { endpoint: 'https://b.example' }
  cluster: fix { endpoint: configuration.base.endpoint }
}
resources: { app: fix.config-echo { @configuration: configuration.cluster } }
`
	libs := configuredLibraries()
	exec := configurationTestExecutor(t, src, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	_, err := exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(),
		"configuration.cluster: references configuration.base, "+
			"which this factory defines; only operator-supplied configurations are referenceable")
}

// A state entry naming a configuration the running factory neither
// defines nor receives must fail with both sides named, not reach the
// library with a nil configuration.
func TestDestroyReadUnknownConfigurationRefFails(t *testing.T) {
	libs := configuredLibraries()
	store := newStateStore(t)
	factory := state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	snap := state.NewSnapshot(factory, store.Stack())
	snap.Entries = []*state.Entry{{
		Address:       "resource.app",
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Selector:      &state.Selector{Alias: "fix", Export: "config-echo"},
		SchemaVersion: 1,
		Configuration: &state.ConfigurationRef{
			Kind:     "named",
			Name:     "ghost",
			Selector: state.Selector{Alias: "fix"},
		},
		Inputs:  map[string]any{},
		Outputs: map[string]any{"endpoint": "x"},
	}}
	rev, err := store.Write(snap)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	exec := configurationTestExecutor(t, `configurations: { fix {} }`, libs)
	exec.Store = store
	exec.Factory = factory
	exec.Destroy = true
	_, err = exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "resource.app")
	require.Contains(t, err.Error(), "configuration.ghost")
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
configurations: {
  fix {}
  cluster: fix { endpoint: resource.src.id }
}
resources: { src: fix.echo { value: 'https://cluster.example' } }
data:      { p: fix.probe { @configuration: configuration.cluster } }
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
	exec := configurationTestExecutor(t, internalConfigDataSrc, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Empty(t, seen, "no read should run while the configuration is pending")
	ds := findStep(t, plan, "data.p")
	require.Equal(t, ConfigRef{Alias: "fix", Name: "cluster"}, ds.DeferredRead)
	require.Equal(t, ConfigRef{Alias: "fix", Name: "cluster"}, ds.Configuration)

	fresh := configurationTestExecutor(t, internalConfigDataSrc, libs)
	fresh.Store = exec.Store
	fresh.Factory = exec.Factory
	_, err = planAndApply(fresh)
	require.NoError(t, err)
	require.Equal(t, []string{"id-https://cluster.example"}, seen)
}

const internalConfigVarSrc = `
configurations: {
  fix {}
  cluster: fix { endpoint: resource.src.id }
}
resources: {
  src: fix.echo { value: var.url }
  app: fix.config-echo { @configuration: configuration.cluster }
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
	first := configurationTestExecutor(t, internalConfigVarSrc, libs)
	first.Inputs = map[string]any{"url": "https://a"}
	first.Store = store
	first.Factory = factory
	applyOnce(t, first)

	seen = nil
	fresh := configurationTestExecutor(t, internalConfigVarSrc, libs)
	fresh.Inputs = map[string]any{"url": "https://b"}
	fresh.Store = store
	fresh.Factory = factory
	plan, err := fresh.Plan(context.Background())
	require.NoError(t, err)
	require.Empty(t, seen, "no read should run while the configuration is pending")
	step := findStep(t, plan, "resource.app")
	require.Equal(t, ConfigRef{Alias: "fix", Name: "cluster"}, step.DeferredRead)
	require.Equal(t, ConfigRef{Alias: "fix", Name: "cluster"}, step.Configuration)
	require.Equal(t, DecisionNoOp, step.Decision)
}

// Destroy deletes run with the configuration the entries were created
// under, evaluated from prior state, since nothing applies this run.
func TestDestroyUsesInternalConfigurationFromState(t *testing.T) {
	var deletes []string
	libs := configuredLibrariesRecording(nil, &deletes)
	store := newStateStore(t)
	factory := state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	first := configurationTestExecutor(t, internalConfigSrc, libs)
	first.Store = store
	first.Factory = factory
	applyOnce(t, first)

	down := configurationTestExecutor(t, internalConfigSrc, libs)
	down.Store = store
	down.Factory = factory
	down.Destroy = true
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
	first := configurationTestExecutor(t, internalConfigSrc, libs)
	first.Store = store
	first.Factory = factory
	applyOnce(t, first)

	reads = nil
	fresh := configurationTestExecutor(t, internalConfigSrc, libs)
	fresh.Store = store
	fresh.Factory = factory
	_, err := fresh.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"https://cluster.example"}, reads)
}
