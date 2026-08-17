package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type resourceReadInputs struct {
	Name    string `ub:"name"`
	capture *resourceReadCapture
}

type resourceReadOutputs struct {
	ID   string `ub:"id"`
	Name string `ub:"name"`
}

type resourceReadConfig struct {
	Region string
}

type resourceReadCapture struct {
	config resourceReadConfig
	prior  *resourceReadOutputs
}

func (r *resourceReadInputs) Read(
	_ context.Context,
	config resourceReadConfig,
	prior *resourceReadOutputs,
) (*resourceReadOutputs, error) {
	r.capture.config = config
	r.capture.prior = prior
	return &resourceReadOutputs{ID: prior.ID, Name: r.Name}, nil
}

func resourceReadFixture(
	t *testing.T,
	definition ruleDefinition,
) (
	resolvedResourceDefinition[ruleInput, *ruleOutput, NoConfig],
	resourceReadRequest,
) {
	t.Helper()
	fixture := newResourcePlanningFixture(t, definition)
	prior := fixture.request.Prior
	require.NotNil(t, prior)
	return fixture.definition, resourceReadRequest{
		Address:       "resource.logs",
		Binding:       prior.Target.Binding,
		Inputs:        prior.Target.Inputs,
		Configuration: prior.Target.Configuration,
		PriorOutputs:  prior.Target.Outputs,
	}
}

func TestResourceReadProducesTypedObservation(t *testing.T) {
	definition, request := resourceReadFixture(t, validRuleDefinition())
	wantOutputs := &ruleOutput{
		ID:     "bucket-1",
		ETag:   "etag-observed",
		Status: &ruleStatus{Code: "ready"},
	}
	var gotInputs ruleInput
	var gotConfig NoConfig
	var gotPrior *ruleOutput

	observation, err := definition.readResourceObservation(
		context.Background(),
		request,
		NoConfig{},
		func(
			_ context.Context,
			inputs ruleInput,
			config NoConfig,
			prior *ruleOutput,
		) (*ruleOutput, error) {
			gotInputs = inputs
			gotConfig = config
			gotPrior = prior
			return wantOutputs, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, ObservationPresent, observation.Status)
	require.Equal(t, ruleInput{
		Name:     "logs",
		Region:   "us-east-1",
		Labels:   map[string]string{"env": "test"},
		Settings: &ruleSettings{Tier: "standard", Replicas: 2},
		Zones:    []string{"a", "b"},
	}, gotInputs)
	require.Equal(t, NoConfig{}, gotConfig)
	require.Equal(t, "etag-1", gotPrior.ETag)
	require.NotNil(t, observation.Outputs)
	wantEncoded := encodeRuleOutput(t, wantOutputs)
	require.True(t, encodedValuesEqual(wantEncoded, *observation.Outputs))
	require.NotNil(t, observation.Identity)
	require.Equal(t, "bucket-1", stringValue(observation.Identity.StableID))
}

func TestResourceReadInvokesConstructedHandwrittenResource(t *testing.T) {
	name := InputField(func(v *resourceReadInputs) *string { return &v.Name })
	resolved, err := resolveResourceDefinition(ResourceDefinition[
		resourceReadInputs,
		*resourceReadOutputs,
		resourceReadConfig,
	]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[resourceReadInputs, *resourceReadOutputs]{
			Version:       1,
			Scope:         IdentityConfiguration,
			AddressInputs: []AnyInputField[resourceReadInputs]{name},
			StableID: func(
				_ resourceReadInputs,
				outputs *resourceReadOutputs,
			) (string, error) {
				return outputs.ID, nil
			},
		},
	})
	require.NoError(t, err)
	encodedInputs, _, err := prepareResourceInputs[resourceReadInputs](map[string]any{
		"name": "logs",
	})
	require.NoError(t, err)
	priorOutputs := &resourceReadOutputs{ID: "bucket-1", Name: "old"}
	encodedOutputs, err := encodeResourceOutputs(priorOutputs)
	require.NoError(t, err)
	configuration := planningConfigurationRecord(
		t,
		configurationLibraryPath,
		"https://api.example",
	)
	request := resourceReadRequest{
		Address:       "resource.logs",
		Binding:       Binding{LibraryPath: configurationLibraryPath, Export: "bucket"},
		Inputs:        encodedInputs,
		Configuration: configuration,
		PriorOutputs:  encodedOutputs,
	}
	capture := &resourceReadCapture{}
	config := resourceReadConfig{Region: "us-east-1"}

	observation, err := resolved.readResourceObservation(
		context.Background(),
		request,
		config,
		newResourceProviderRead[
			resourceReadInputs,
			*resourceReadOutputs,
			resourceReadConfig,
			*resourceReadInputs,
		](func() *resourceReadInputs {
			return &resourceReadInputs{capture: capture}
		}),
	)
	require.NoError(t, err)
	require.Equal(t, config, capture.config)
	require.Equal(t, priorOutputs, capture.prior)
	require.NotNil(t, observation.Outputs)
	observed, err := decodeResourceOutputs[*resourceReadOutputs](*observation.Outputs)
	require.NoError(t, err)
	require.Equal(t, &resourceReadOutputs{ID: "bucket-1", Name: "logs"}, observed)
}

func TestResourceReadPropagatesNotFound(t *testing.T) {
	definition, request := resourceReadFixture(t, validRuleDefinition())

	observation, err := definition.readResourceObservation(
		context.Background(),
		request,
		NoConfig{},
		func(
			context.Context,
			ruleInput,
			NoConfig,
			*ruleOutput,
		) (*ruleOutput, error) {
			return nil, ErrNotFound
		},
	)
	require.ErrorIs(t, err, ErrNotFound)
	require.Equal(t, ResourceObservation{}, observation)
}

func TestResourceReadRejectsInvalidRequestBeforeProvider(t *testing.T) {
	definition, request := resourceReadFixture(t, validRuleDefinition())
	pending, err := PendingEncodedValue([]string{"resource.network.id"})
	require.NoError(t, err)
	request.Inputs = pending
	calls := 0

	_, err = definition.readResourceObservation(
		context.Background(),
		request,
		NoConfig{},
		func(
			context.Context,
			ruleInput,
			NoConfig,
			*ruleOutput,
		) (*ruleOutput, error) {
			calls++
			return nil, nil
		},
	)
	require.ErrorContains(t, err, "read inputs")
	require.Zero(t, calls)
}

func TestResourceReadRejectsNilConstructedResource(t *testing.T) {
	name := InputField(func(v *resourceReadInputs) *string { return &v.Name })
	resolved, err := resolveResourceDefinition(ResourceDefinition[
		resourceReadInputs,
		*resourceReadOutputs,
		resourceReadConfig,
	]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[resourceReadInputs, *resourceReadOutputs]{
			Version:       1,
			Scope:         IdentityConfiguration,
			AddressInputs: []AnyInputField[resourceReadInputs]{name},
		},
	})
	require.NoError(t, err)
	encodedInputs, _, err := prepareResourceInputs[resourceReadInputs](map[string]any{
		"name": "logs",
	})
	require.NoError(t, err)
	encodedOutputs, err := encodeResourceOutputs(&resourceReadOutputs{ID: "bucket-1"})
	require.NoError(t, err)
	request := resourceReadRequest{
		Address: "resource.logs",
		Binding: Binding{
			LibraryPath: configurationLibraryPath,
			Export:      "bucket",
		},
		Inputs: encodedInputs,
		Configuration: planningConfigurationRecord(
			t,
			configurationLibraryPath,
			"https://api.example",
		),
		PriorOutputs: encodedOutputs,
	}

	_, err = resolved.readResourceObservation(
		context.Background(),
		request,
		resourceReadConfig{},
		newResourceProviderRead[
			resourceReadInputs,
			*resourceReadOutputs,
			resourceReadConfig,
			*resourceReadInputs,
		](func() *resourceReadInputs { return nil }),
	)
	require.ErrorContains(t, err, "resource constructor returned nil")
}

func TestResourceReadRejectsInvalidProviderOutputs(t *testing.T) {
	tests := []struct {
		name    string
		outputs *ruleOutput
		message string
	}{
		{
			name:    "nil outputs",
			outputs: nil,
			message: "resource outputs must not be nil",
		},
		{
			name: "empty stable ID",
			outputs: &ruleOutput{
				ETag:   "etag-observed",
				Status: &ruleStatus{Code: "ready"},
			},
			message: "stable ID must not be empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition, request := resourceReadFixture(t, validRuleDefinition())

			observation, err := definition.readResourceObservation(
				context.Background(),
				request,
				NoConfig{},
				func(
					context.Context,
					ruleInput,
					NoConfig,
					*ruleOutput,
				) (*ruleOutput, error) {
					return tt.outputs, nil
				},
			)
			require.ErrorContains(t, err, tt.message)
			require.Equal(t, ResourceObservation{}, observation)
		})
	}
}

func TestResourceReadReturnsProviderErrors(t *testing.T) {
	tests := []struct {
		name string
		read resourceProviderReadFunc[ruleInput, *ruleOutput, NoConfig]
		want func(*testing.T, error)
	}{
		{
			name: "error",
			read: func(
				context.Context,
				ruleInput,
				NoConfig,
				*ruleOutput,
			) (*ruleOutput, error) {
				return nil, errResourceApplyTest
			},
			want: func(t *testing.T, err error) {
				t.Helper()
				require.ErrorIs(t, err, errResourceApplyTest)
			},
		},
		{
			name: "panic",
			read: func(
				context.Context,
				ruleInput,
				NoConfig,
				*ruleOutput,
			) (*ruleOutput, error) {
				panic("read failed")
			},
			want: func(t *testing.T, err error) {
				t.Helper()
				var panicErr *PanicError
				require.ErrorAs(t, err, &panicErr)
				require.Equal(t, "read failed", panicErr.Value)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition, request := resourceReadFixture(t, validRuleDefinition())

			observation, err := definition.readResourceObservation(
				context.Background(),
				request,
				NoConfig{},
				tt.read,
			)
			tt.want(t, err)
			require.Equal(t, ResourceObservation{}, observation)
		})
	}
}

func TestResourceReadRequiresContextAndProvider(t *testing.T) {
	definition, request := resourceReadFixture(t, validRuleDefinition())
	var nilContext context.Context

	_, err := definition.readResourceObservation(
		nilContext,
		request,
		NoConfig{},
		func(
			context.Context,
			ruleInput,
			NoConfig,
			*ruleOutput,
		) (*ruleOutput, error) {
			return nil, errors.New("unexpected call")
		},
	)
	require.ErrorContains(t, err, "read context is required")

	_, err = definition.readResourceObservation(
		context.Background(),
		request,
		NoConfig{},
		nil,
	)
	require.ErrorContains(t, err, "resource read callback is required")
}
