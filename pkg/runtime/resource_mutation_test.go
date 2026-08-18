package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var errResourceMutationTest = errors.New("provider mutation failed")

type resourceMutationInputs struct {
	Name    string `ub:"name"`
	capture *resourceMutationCapture
}

type resourceMutationOutputs struct {
	ID   string `ub:"id"`
	Name string `ub:"name"`
}

type resourceMutationConfig struct {
	Region string
}

type defaultResourceMutationInputs struct {
	Name string `ub:"name"`
}

type defaultResourceMutationOutputs struct {
	Name string `ub:"name"`
}

func (r *defaultResourceMutationInputs) Create(
	context.Context,
	NoConfig,
) (*defaultResourceMutationOutputs, error) {
	return &defaultResourceMutationOutputs{Name: r.Name}, nil
}

type resourceMutationCapture struct {
	calls         []string
	context       context.Context
	name          string
	configuration resourceMutationConfig
	prior         Prior[resourceMutationInputs, *resourceMutationOutputs]
	deleted       *resourceMutationOutputs
	createResult  *resourceMutationOutputs
	updateResult  *resourceMutationOutputs
	createErr     error
	updateErr     error
	deleteErr     error
}

func (r *resourceMutationInputs) Create(
	ctx context.Context,
	configuration resourceMutationConfig,
) (*resourceMutationOutputs, error) {
	r.capture.calls = append(r.capture.calls, "create")
	r.capture.context = ctx
	r.capture.name = r.Name
	r.capture.configuration = configuration
	return r.capture.createResult, r.capture.createErr
}

func (r *resourceMutationInputs) Update(
	ctx context.Context,
	configuration resourceMutationConfig,
	prior Prior[resourceMutationInputs, *resourceMutationOutputs],
) (*resourceMutationOutputs, error) {
	r.capture.calls = append(r.capture.calls, "update")
	r.capture.context = ctx
	r.capture.name = r.Name
	r.capture.configuration = configuration
	r.capture.prior = prior
	return r.capture.updateResult, r.capture.updateErr
}

func (r *resourceMutationInputs) Delete(
	ctx context.Context,
	configuration resourceMutationConfig,
	prior *resourceMutationOutputs,
) error {
	r.capture.calls = append(r.capture.calls, "delete")
	r.capture.context = ctx
	r.capture.name = r.Name
	r.capture.configuration = configuration
	r.capture.deleted = prior
	return r.capture.deleteErr
}

func TestResourceProviderCreateUsesConstructedReceiver(t *testing.T) {
	capture := &resourceMutationCapture{
		createResult: &resourceMutationOutputs{ID: "object-1", Name: "logs"},
	}
	create := newResourceProviderCreate[
		resourceMutationInputs,
		*resourceMutationOutputs,
		resourceMutationConfig,
		*resourceMutationInputs,
	](func() *resourceMutationInputs {
		return &resourceMutationInputs{capture: capture}
	})
	ctx := context.WithValue(context.Background(), resourceMutationContextKey{}, "create")
	configuration := resourceMutationConfig{Region: "us-east-1"}

	result, err := create(
		ctx,
		resourceMutationInputs{Name: "logs"},
		configuration,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"create"}, capture.calls)
	require.Same(t, ctx, capture.context)
	require.Equal(t, "logs", capture.name)
	require.Equal(t, configuration, capture.configuration)
	require.Equal(t, capture.createResult, result.Outputs)
	decoded, err := decodeResourceOutputs[*resourceMutationOutputs](result.Encoded)
	require.NoError(t, err)
	require.Equal(t, capture.createResult, decoded)
}

func TestResourceProviderCreateUsesDefaultConstructor(t *testing.T) {
	create := newResourceProviderCreate[
		defaultResourceMutationInputs,
		*defaultResourceMutationOutputs,
		NoConfig,
		*defaultResourceMutationInputs,
	](nil)

	result, err := create(
		context.Background(),
		defaultResourceMutationInputs{Name: "logs"},
		NoConfig{},
	)
	require.NoError(t, err)
	require.Equal(t, &defaultResourceMutationOutputs{Name: "logs"}, result.Outputs)
}

func TestResourceProviderUpdatePassesCompletePrior(t *testing.T) {
	capture := &resourceMutationCapture{
		updateResult: &resourceMutationOutputs{ID: "object-1", Name: "new"},
	}
	update := newResourceProviderUpdate[
		resourceMutationInputs,
		*resourceMutationOutputs,
		resourceMutationConfig,
		*resourceMutationInputs,
	](func() *resourceMutationInputs {
		return &resourceMutationInputs{capture: capture}
	})
	configuration := resourceMutationConfig{Region: "us-west-2"}
	prior := Prior[resourceMutationInputs, *resourceMutationOutputs]{
		Inputs:   resourceMutationInputs{Name: "old"},
		Outputs:  &resourceMutationOutputs{ID: "object-1", Name: "old"},
		Observed: &resourceMutationOutputs{ID: "object-1", Name: "observed"},
	}

	result, err := update(
		context.Background(),
		resourceMutationInputs{Name: "new"},
		configuration,
		prior,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"update"}, capture.calls)
	require.Equal(t, "new", capture.name)
	require.Equal(t, configuration, capture.configuration)
	require.Equal(t, prior, capture.prior)
	require.Equal(t, capture.updateResult, result.Outputs)
	decoded, err := decodeResourceOutputs[*resourceMutationOutputs](result.Encoded)
	require.NoError(t, err)
	require.Equal(t, capture.updateResult, decoded)
}

func TestResourceProviderDeleteDecodesFreshOutputs(t *testing.T) {
	capture := &resourceMutationCapture{}
	deleteResource := newResourceProviderDelete[
		resourceMutationInputs,
		*resourceMutationOutputs,
		resourceMutationConfig,
		*resourceMutationInputs,
	](func() *resourceMutationInputs {
		return &resourceMutationInputs{capture: capture}
	})
	configuration := resourceMutationConfig{Region: "us-east-2"}
	prior := &resourceMutationOutputs{ID: "object-1", Name: "observed"}
	encoded, err := encodeResourceOutputs(prior)
	require.NoError(t, err)

	err = deleteResource(
		context.Background(),
		resourceMutationInputs{Name: "logs"},
		configuration,
		encoded,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"delete"}, capture.calls)
	require.Equal(t, "logs", capture.name)
	require.Equal(t, configuration, capture.configuration)
	require.Equal(t, prior, capture.deleted)
}

func TestResourceProviderMutationsRejectInvalidValues(t *testing.T) {
	t.Run("nil constructor result", func(t *testing.T) {
		create := newResourceProviderCreate[
			resourceMutationInputs,
			*resourceMutationOutputs,
			resourceMutationConfig,
			*resourceMutationInputs,
		](func() *resourceMutationInputs { return nil })

		_, err := create(
			context.Background(),
			resourceMutationInputs{},
			resourceMutationConfig{},
		)
		require.ErrorContains(t, err, "resource constructor returned nil")
	})

	t.Run("nil create outputs", func(t *testing.T) {
		capture := &resourceMutationCapture{}
		create := newResourceProviderCreate[
			resourceMutationInputs,
			*resourceMutationOutputs,
			resourceMutationConfig,
			*resourceMutationInputs,
		](func() *resourceMutationInputs {
			return &resourceMutationInputs{capture: capture}
		})

		_, err := create(
			context.Background(),
			resourceMutationInputs{},
			resourceMutationConfig{},
		)
		require.ErrorContains(t, err, "resource outputs must not be nil")
	})

	t.Run("nil update outputs", func(t *testing.T) {
		capture := &resourceMutationCapture{}
		update := newResourceProviderUpdate[
			resourceMutationInputs,
			*resourceMutationOutputs,
			resourceMutationConfig,
			*resourceMutationInputs,
		](func() *resourceMutationInputs {
			return &resourceMutationInputs{capture: capture}
		})

		_, err := update(
			context.Background(),
			resourceMutationInputs{},
			resourceMutationConfig{},
			Prior[resourceMutationInputs, *resourceMutationOutputs]{},
		)
		require.ErrorContains(t, err, "resource outputs must not be nil")
	})

	t.Run("invalid delete outputs", func(t *testing.T) {
		capture := &resourceMutationCapture{}
		deleteResource := newResourceProviderDelete[
			resourceMutationInputs,
			*resourceMutationOutputs,
			resourceMutationConfig,
			*resourceMutationInputs,
		](func() *resourceMutationInputs {
			return &resourceMutationInputs{capture: capture}
		})

		err := deleteResource(
			context.Background(),
			resourceMutationInputs{},
			resourceMutationConfig{},
			StringValue("invalid"),
		)
		require.ErrorContains(t, err, "expected object")
		require.Empty(t, capture.calls)
	})
}

func TestResourceProviderMutationsReturnProviderErrors(t *testing.T) {
	capture := &resourceMutationCapture{
		createErr: errResourceMutationTest,
		updateErr: errResourceMutationTest,
		deleteErr: errResourceMutationTest,
	}
	construct := func() *resourceMutationInputs {
		return &resourceMutationInputs{capture: capture}
	}
	create := newResourceProviderCreate[
		resourceMutationInputs,
		*resourceMutationOutputs,
		resourceMutationConfig,
		*resourceMutationInputs,
	](construct)
	update := newResourceProviderUpdate[
		resourceMutationInputs,
		*resourceMutationOutputs,
		resourceMutationConfig,
		*resourceMutationInputs,
	](construct)
	deleteResource := newResourceProviderDelete[
		resourceMutationInputs,
		*resourceMutationOutputs,
		resourceMutationConfig,
		*resourceMutationInputs,
	](construct)

	_, err := create(
		context.Background(),
		resourceMutationInputs{},
		resourceMutationConfig{},
	)
	require.ErrorIs(t, err, errResourceMutationTest)
	_, err = update(
		context.Background(),
		resourceMutationInputs{},
		resourceMutationConfig{},
		Prior[resourceMutationInputs, *resourceMutationOutputs]{},
	)
	require.ErrorIs(t, err, errResourceMutationTest)
	prior, encodeErr := encodeResourceOutputs(&resourceMutationOutputs{ID: "object-1"})
	require.NoError(t, encodeErr)
	err = deleteResource(
		context.Background(),
		resourceMutationInputs{},
		resourceMutationConfig{},
		prior,
	)
	require.ErrorIs(t, err, errResourceMutationTest)
	require.Equal(t, []string{"create", "update", "delete"}, capture.calls)
}

type resourceMutationContextKey struct{}
