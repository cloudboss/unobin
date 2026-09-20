package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeResource struct {
	Name string
}

type fakeResourceOutput struct {
	ID string
}

func (r *fakeResource) Create(_ context.Context, _ any) (*fakeResourceOutput, error) {
	return &fakeResourceOutput{ID: "fake-" + r.Name}, nil
}

func (r *fakeResource) Read(
	_ context.Context, _ any, prior Prior[fakeResource, *fakeResourceOutput, any],
) (*fakeResourceOutput, error) {
	if prior.Outputs == nil {
		return nil, ErrNotFound
	}
	return prior.Outputs, nil
}

func (r *fakeResource) Update(
	_ context.Context, _ any, prior Prior[fakeResource, *fakeResourceOutput, any],
) (*fakeResourceOutput, error) {
	prior.Outputs.ID = "fake-" + r.Name + "-updated"
	return prior.Outputs, nil
}

func (r *fakeResource) Delete(
	_ context.Context, _ any, _ Prior[fakeResource, *fakeResourceOutput, any],
) error {
	return nil
}

func fakeResourceDefinition() ResourceDefinition[fakeResource, *fakeResourceOutput, any] {
	return ResourceDefinition[fakeResource, *fakeResourceOutput, any]{
		SchemaVersion: 1,
		Replace: Replacement[fakeResource, *fakeResourceOutput, any]{
			Fields: []AnyInputField[fakeResource]{
				InputField(func(input *fakeResource) *string { return &input.Name }),
			},
		},
	}
}

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

func TestResourceLifecycle(t *testing.T) {
	rt := MakeResourceWith[fakeResource, *fakeResourceOutput, any](
		fakeResourceDefinition(),
		func() *fakeResource { return &fakeResource{Name: "alpha"} },
	)
	r := rt.NewReceiver()
	ctx := context.Background()

	out, err := rt.Create(ctx, r, nil)
	require.NoError(t, err)
	require.Equal(t, "fake-alpha", out.(*fakeResourceOutput).ID)

	prior := resourcePrior{Inputs: map[string]any{"name": "alpha"}, Outputs: mapify(out)}
	got, err := rt.Read(ctx, r, nil, prior)
	require.NoError(t, err)
	require.Equal(t, out, got)

	updated, err := rt.Update(ctx, r, nil, prior)
	require.NoError(t, err)
	require.Equal(t, "fake-alpha-updated", updated.(*fakeResourceOutput).ID)

	prior.Outputs = mapify(updated)
	require.NoError(t, rt.Delete(ctx, r, nil, prior))

	gone, err := rt.Read(ctx, r, nil, resourcePrior{Inputs: map[string]any{"name": "alpha"}})
	require.True(t, errors.Is(err, ErrNotFound))
	require.Nil(t, gone)
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
