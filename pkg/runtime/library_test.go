package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeResource struct {
	Name string
}

type fakeResourceOutput struct{ ID string }

func fakeResourceDefinition() ResourceDefinition[fakeResource, *fakeResourceOutput, any] {
	return ResourceDefinition[fakeResource, *fakeResourceOutput, any]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[fakeResource, *fakeResourceOutput]{
			Version: 1,
			Scope:   IdentityConfiguration,
		},
		Replacement: ReplacementRules[fakeResource, *fakeResourceOutput]{
			Inputs: []ReplacementRule[fakeResource]{
				ReplaceWhenChanged(InputField(func(v *fakeResource) *string { return &v.Name })),
			},
		},
	}
}

func (r *fakeResource) Create(_ context.Context, _ any) (*fakeResourceOutput, error) {
	return &fakeResourceOutput{ID: "fake-" + r.Name}, nil
}

func (r *fakeResource) Read(
	_ context.Context,
	_ any,
	prior *fakeResourceOutput,
) (*fakeResourceOutput, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *fakeResource) Update(
	_ context.Context, _ any, prior Prior[fakeResource, *fakeResourceOutput],
) (*fakeResourceOutput, error) {
	out := *prior.Outputs
	out.ID = "fake-" + r.Name + "-updated"
	return &out, nil
}

func (r *fakeResource) Delete(_ context.Context, _ any, _ *fakeResourceOutput) error { return nil }

type fakeDataSource struct {
	Key string
}

func (d *fakeDataSource) Read(_ context.Context, _ any) (any, error) {
	return map[string]any{"value": d.Key}, nil
}

type fakeAction struct {
	Echo string
}

func (a *fakeAction) Run(_ context.Context, _ any) (any, error) {
	return a.Echo, nil
}

func TestLibraryHoldsAllRegistrationKinds(t *testing.T) {
	lib := &Library{
		Name: "fake",
		Resources: map[string]ResourceRegistration{
			"thing": MakeResourceWith[fakeResource, *fakeResourceOutput, any](
				fakeResourceDefinition(),

				func() *fakeResource { return &fakeResource{Name: "x"} },
			),
		},
		DataSources: map[string]DataSourceRegistration{
			"lookup": MakeDataSourceWith[fakeDataSource, any, any](
				func() *fakeDataSource { return &fakeDataSource{Key: "k"} },
			),
		},
		Actions: map[string]ActionRegistration{
			"echo": MakeActionWith[fakeAction, any, any](
				func() *fakeAction { return &fakeAction{Echo: "hi"} },
			),
		},
	}
	require.Equal(t, "fake", lib.Name)
	require.Contains(t, lib.Resources, "thing")
	require.Contains(t, lib.DataSources, "lookup")
	require.Contains(t, lib.Actions, "echo")
}

func TestDataSourceRead(t *testing.T) {
	dt := MakeDataSourceWith[fakeDataSource, any, any](
		func() *fakeDataSource { return &fakeDataSource{Key: "abc"} },
	)
	d := dt.NewReceiver()
	out, err := dt.Read(context.Background(), d, nil)
	require.NoError(t, err)
	require.Equal(t, "abc", out.(map[string]any)["value"])
}

func TestLibraryHoldsCompositeTypes(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", "description: 'cluster'")
	lib := &Library{
		Name: "net",
		ResourceComposites: map[string]*CompositeType{
			"cluster": composite,
		},
	}
	require.Same(t, composite.SyntaxBody, lib.Composite(NodeResource, "cluster").SyntaxBody)
	require.Nil(t, lib.Composite(NodeDataSource, "cluster"), "kind selects the map")
}

func TestLibraryAddComposite(t *testing.T) {
	lib := &Library{}
	lib.AddComposite(&CompositeType{Name: "box", Kind: NodeResource})
	lib.AddComposite(&CompositeType{Name: "box", Kind: NodeDataSource})
	lib.AddComposite(&CompositeType{Name: "run", Kind: NodeAction})

	require.NotNil(t, lib.Composite(NodeResource, "box"))
	require.NotNil(t, lib.Composite(NodeDataSource, "box"),
		"resource.box and data-source.box are independent namespaces")
	require.NotNil(t, lib.Composite(NodeAction, "run"))
	require.Nil(t, lib.Composite(NodeAction, "box"))
}
