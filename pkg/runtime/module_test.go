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

func (r *fakeResource) SchemaVersion() int { return 1 }

func (r *fakeResource) Create(_ context.Context, _ any) (any, error) {
	return map[string]any{"id": "fake-" + r.Name}, nil
}

func (r *fakeResource) Read(_ context.Context, _ any, prior any) (any, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *fakeResource) Update(_ context.Context, _ any, prior any) (any, error) {
	m := prior.(map[string]any)
	m["id"] = "fake-" + r.Name + "-updated"
	return m, nil
}

func (r *fakeResource) Delete(_ context.Context, _ any, _ any) error { return nil }

func (r *fakeResource) ReplaceFields() []string { return []string{"name"} }

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

func TestModuleHoldsAllRegistrationKinds(t *testing.T) {
	mod := &Module{
		Name: "fake",
		Resources: map[string]ResourceRegistration{
			"thing": MakeResourceWith[fakeResource, any](
				func() *fakeResource { return &fakeResource{Name: "x"} },
			),
		},
		DataSources: map[string]DataSourceRegistration{
			"lookup": MakeDataSourceWith[fakeDataSource, any](
				func() *fakeDataSource { return &fakeDataSource{Key: "k"} },
			),
		},
		Actions: map[string]ActionRegistration{
			"echo": MakeActionWith[fakeAction, any](
				func() *fakeAction { return &fakeAction{Echo: "hi"} },
			),
		},
	}
	require.Equal(t, "fake", mod.Name)
	require.Contains(t, mod.Resources, "thing")
	require.Contains(t, mod.DataSources, "lookup")
	require.Contains(t, mod.Actions, "echo")
}

func TestResourceLifecycle(t *testing.T) {
	rt := MakeResourceWith[fakeResource, any](
		func() *fakeResource { return &fakeResource{Name: "alpha"} },
	)
	r := rt.NewReceiver()
	ctx := context.Background()

	out, err := rt.Create(ctx, r, nil)
	require.NoError(t, err)
	require.Equal(t, "fake-alpha", out.(map[string]any)["id"])

	got, err := rt.Read(ctx, r, nil, out)
	require.NoError(t, err)
	require.Equal(t, out, got)

	updated, err := rt.Update(ctx, r, nil, out)
	require.NoError(t, err)
	require.Equal(t, "fake-alpha-updated", updated.(map[string]any)["id"])

	require.NoError(t, rt.Delete(ctx, r, nil, updated))

	gone, err := rt.Read(ctx, r, nil, nil)
	require.True(t, errors.Is(err, ErrNotFound))
	require.Nil(t, gone)
}

func TestDataSourceRead(t *testing.T) {
	dt := MakeDataSourceWith[fakeDataSource, any](
		func() *fakeDataSource { return &fakeDataSource{Key: "abc"} },
	)
	d := dt.NewReceiver()
	out, err := dt.Read(context.Background(), d, nil)
	require.NoError(t, err)
	require.Equal(t, "abc", out.(map[string]any)["value"])
}

func TestModuleHoldsCompositeTypes(t *testing.T) {
	parsed := parseStack(t, "description: 'cluster'\n")
	mod := &Module{
		Name: "net",
		Composites: map[string]*CompositeType{
			"cluster": {Name: "cluster", Body: parsed},
		},
	}
	require.Contains(t, mod.Composites, "cluster")
	require.Same(t, parsed, mod.Composites["cluster"].Body)
}
