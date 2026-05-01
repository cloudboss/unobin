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

func (r *fakeResource) Create(_ context.Context) (any, error) {
	return map[string]any{"id": "fake-" + r.Name}, nil
}

func (r *fakeResource) Read(_ context.Context, prior any) (any, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *fakeResource) Update(_ context.Context, prior any) (any, error) {
	m := prior.(map[string]any)
	m["id"] = "fake-" + r.Name + "-updated"
	return m, nil
}

func (r *fakeResource) Delete(_ context.Context, _ any) error { return nil }

func (r *fakeResource) ReplaceFields() []string { return []string{"name"} }

type fakeDataSource struct {
	Key string
}

func (d *fakeDataSource) Read(_ context.Context) (any, error) {
	return map[string]any{"value": d.Key}, nil
}

type fakeAction struct {
	Echo string
}

func (a *fakeAction) Run(_ context.Context) (any, error) {
	return a.Echo, nil
}

func TestModuleHoldsAllRegistrationKinds(t *testing.T) {
	mod := &Module{
		Name: "fake",
		Resources: map[string]ResourceType{
			"thing": {
				Name:          "thing",
				SchemaVersion: 1,
				New:           func() Resource { return &fakeResource{Name: "x"} },
			},
		},
		DataSources: map[string]DataSourceType{
			"lookup": {
				Name: "lookup",
				New:  func() DataSource { return &fakeDataSource{Key: "k"} },
			},
		},
		Actions: map[string]ActionType{
			"echo": {
				Name: "echo",
				New:  func() Action { return &fakeAction{Echo: "hi"} },
			},
		},
	}
	require.Equal(t, "fake", mod.Name)
	require.Contains(t, mod.Resources, "thing")
	require.Contains(t, mod.DataSources, "lookup")
	require.Contains(t, mod.Actions, "echo")
}

func TestResourceLifecycle(t *testing.T) {
	rt := ResourceType{
		Name: "thing",
		New:  func() Resource { return &fakeResource{Name: "alpha"} },
	}
	r := rt.New()
	ctx := context.Background()

	out, err := r.Create(ctx)
	require.NoError(t, err)
	require.Equal(t, "fake-alpha", out.(map[string]any)["id"])

	got, err := r.Read(ctx, out)
	require.NoError(t, err)
	require.Equal(t, out, got)

	updated, err := r.Update(ctx, out)
	require.NoError(t, err)
	require.Equal(t, "fake-alpha-updated", updated.(map[string]any)["id"])

	require.NoError(t, r.Delete(ctx, updated))

	gone, err := r.Read(ctx, nil)
	require.True(t, errors.Is(err, ErrNotFound))
	require.Nil(t, gone)
}

func TestDataSourceRead(t *testing.T) {
	ds := (&DataSourceType{
		Name: "lookup",
		New:  func() DataSource { return &fakeDataSource{Key: "abc"} },
	}).New()
	out, err := ds.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, "abc", out.(map[string]any)["value"])
}
