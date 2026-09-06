package runtime

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"
)

type resourceCounters struct {
	creates int64
	updates int64
	deletes int64
	reads   int64
	// readFn lets a test control what countingResource.Read returns;
	// nil means Read returns prior unchanged (no drift, not gone).
	readFn func(*countingResourceOutput) (*countingResourceOutput, error)

	// gotUpdatePrior captures the Prior the last Update received, so a
	// test can assert what reached the resource through plan and apply.
	gotUpdatePrior *Prior[countingResource, *countingResourceOutput]
}

type countingResource struct {
	Name string
	Size int64

	counters *resourceCounters
}

type countingResourceOutput struct {
	ID   string
	Name string
	Size int64
}

func (r *countingResource) Create(_ context.Context, _ any) (*countingResourceOutput, error) {
	atomic.AddInt64(&r.counters.creates, 1)
	return &countingResourceOutput{ID: "fake-" + r.Name, Name: r.Name, Size: r.Size}, nil
}

func (r *countingResource) Read(
	_ context.Context, _ any, prior *countingResourceOutput,
) (*countingResourceOutput, error) {
	atomic.AddInt64(&r.counters.reads, 1)
	if r.counters.readFn != nil {
		return r.counters.readFn(prior)
	}
	return prior, nil
}

func (r *countingResource) Update(
	_ context.Context, _ any, prior Prior[countingResource, *countingResourceOutput],
) (*countingResourceOutput, error) {
	atomic.AddInt64(&r.counters.updates, 1)
	r.counters.gotUpdatePrior = &prior
	var outputs countingResourceOutput
	if prior.Outputs != nil {
		outputs = *prior.Outputs
	}
	outputs.Name = r.Name
	outputs.Size = r.Size
	return &outputs, nil
}

func (r *countingResource) Delete(_ context.Context, _ any, _ *countingResourceOutput) error {
	atomic.AddInt64(&r.counters.deletes, 1)
	return nil
}

func countingResourceDefinition() ResourceDefinition[
	countingResource,
	*countingResourceOutput,
	any,
] {
	return ResourceDefinition[countingResource, *countingResourceOutput, any]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[countingResource, *countingResourceOutput]{
			Version: 1,
			Scope:   IdentityConfiguration,
		},
		Replacement: ReplacementRules[countingResource, *countingResourceOutput]{
			Inputs: []ReplacementRule[countingResource]{
				ReplaceWhenChanged(InputField(func(v *countingResource) *string { return &v.Name })),
			},
		},
	}
}

func resourceModules(c *resourceCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[countingResource, *countingResourceOutput, any](
					countingResourceDefinition(),

					func() *countingResource { return &countingResource{counters: c} },
				),
			},
			Functions: map[string]FunctionType{
				"all": {Name: "all", ArgCount: 1, Func: fnAllBools},
			},
		},
	}
}

// fnAllBools mirrors core's all function so tests can call a function
// from a constraint predicate without importing the real library.
func fnAllBools(args []any) (any, error) {
	lst, ok := args[0].([]any)
	if !ok {
		return nil, fmt.Errorf("all: expected a list, got %T", args[0])
	}
	for _, el := range lst {
		b, ok := el.(bool)
		if !ok {
			return nil, fmt.Errorf("all: expected booleans, got %T", el)
		}
		if !b {
			return false, nil
		}
	}
	return true, nil
}

func newStateStore(t *testing.T) *local.Store {
	t.Helper()
	s, err := local.NewStore(t.TempDir(), "test-stack", "prod", encrypters.Noop{})
	require.NoError(t, err)
	return s
}

func TestConfigForUsesLibraryConfigNode(t *testing.T) {
	leaf := &Node{Address: "resource.web", Alias: "aws"}
	configNode := &Node{Address: "library-config.aws", Kind: NodeLibraryConfig, Alias: "aws"}
	e := &Executor{DAG: &DAG{Nodes: map[string]*Node{
		leaf.Address:       leaf,
		configNode.Address: configNode,
	}}}
	e.storeInternalConfiguration(configNode.Address, "decoded-cfg")
	require.Equal(t, "decoded-cfg", e.configFor(leaf))
}

func TestConfigForEmptyConfigSynthesizesValue(t *testing.T) {
	leaf := &Node{Address: "resource.web", Alias: "aws"}
	e := &Executor{
		DAG: &DAG{Nodes: map[string]*Node{leaf.Address: leaf}},
		Libraries: map[string]*Library{
			"aws": {
				Configuration: &cfg.ConfigurationType[*struct{}]{
					New: func() *struct{} { return &struct{}{} },
				},
			},
		},
	}
	require.IsType(t, &struct{}{}, e.configFor(leaf))
}

func TestConfigForNoConfigReturnsNil(t *testing.T) {
	leaf := &Node{Address: "resource.web", Alias: "aws"}
	e := &Executor{
		DAG:       &DAG{Nodes: map[string]*Node{leaf.Address: leaf}},
		Libraries: map[string]*Library{"aws": {}},
	}
	require.Nil(t, e.configFor(leaf))
}

func TestMergeAttrs(t *testing.T) {
	tests := []struct {
		name    string
		inputs  map[string]any
		outputs map[string]any
		want    map[string]any
	}{
		{
			name:    "input passes through when no same-named output",
			inputs:  map[string]any{"path": "/tmp/f"},
			outputs: map[string]any{"sha256": "abc"},
			want:    map[string]any{"path": "/tmp/f", "sha256": "abc"},
		},
		{
			name:    "output wins on a name collision",
			inputs:  map[string]any{"name": "Foo"},
			outputs: map[string]any{"name": "foo"},
			want:    map[string]any{"name": "foo"},
		},
		{
			name:    "output-only field is present",
			inputs:  map[string]any{},
			outputs: map[string]any{"id": "x"},
			want:    map[string]any{"id": "x"},
		},
		{
			name:    "nil outputs leaves inputs readable",
			inputs:  map[string]any{"path": "/tmp/f"},
			outputs: nil,
			want:    map[string]any{"path": "/tmp/f"},
		},
		{
			name:    "nil inputs leaves outputs readable",
			inputs:  nil,
			outputs: map[string]any{"id": "x"},
			want:    map[string]any{"id": "x"},
		},
		{
			name:    "both nil yields an empty map",
			inputs:  nil,
			outputs: nil,
			want:    map[string]any{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, mergeAttrs(tt.inputs, tt.outputs))
		})
	}
}
