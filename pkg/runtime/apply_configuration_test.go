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
	Endpoint cfg.String
}

// echoResource exposes its input as an output for other nodes to read.
type echoResource struct {
	Value string
}

func (r *echoResource) SchemaVersion() int { return 1 }
func (r *echoResource) Create(_ context.Context, _ any) (any, error) {
	return map[string]any{"value": r.Value}, nil
}
func (r *echoResource) Read(_ context.Context, _, prior any) (any, error) { return prior, nil }
func (r *echoResource) Update(_ context.Context, _ any, _ Prior[echoResource, any]) (any, error) {
	return map[string]any{"value": r.Value}, nil
}
func (r *echoResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *echoResource) ReplaceFields() []string                  { return nil }

// configEchoResource records the configuration the runtime handed it,
// so a test can prove which value reached the CRUD call.
type configEchoResource struct{}

func (r *configEchoResource) SchemaVersion() int { return 1 }
func (r *configEchoResource) Create(_ context.Context, c any) (any, error) {
	conf, _ := c.(*endpointConfiguration)
	if conf == nil {
		return map[string]any{"endpoint": ""}, nil
	}
	return map[string]any{"endpoint": conf.Endpoint.Value}, nil
}
func (r *configEchoResource) Read(_ context.Context, _, prior any) (any, error) {
	return prior, nil
}
func (r *configEchoResource) Update(
	_ context.Context, c any, _ Prior[configEchoResource, any],
) (any, error) {
	return (&configEchoResource{}).Create(context.Background(), c)
}
func (r *configEchoResource) Delete(_ context.Context, _, _ any) error { return nil }
func (r *configEchoResource) ReplaceFields() []string                  { return nil }

func configuredLibraries() map[string]*Library {
	return map[string]*Library{
		"fix": {
			Name: "fix",
			Configuration: &cfg.ConfigurationType{
				New: func() any { return &endpointConfiguration{} },
			},
			Resources: map[string]ResourceRegistration{
				"echo":        MakeResource[echoResource, any](),
				"config-echo": MakeResource[configEchoResource, any](),
			},
		},
	}
}

const internalConfigSrc = `
configurations: { fix: { cluster: { endpoint: resource.fix.echo.src.value } } }
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
