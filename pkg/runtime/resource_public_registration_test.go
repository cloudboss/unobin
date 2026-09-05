package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMakeResourceValidatesDefinitionBeforeConstruction(t *testing.T) {
	definition := resourceRegistrationDefinition()
	definition.Identity.Version = 0
	constructed := false
	require.PanicsWithError(t, "resource definition: resource identity version must be greater than zero",
		func() {
			MakeResourceWith[resourceRegistrationInput, *resourceRegistrationOutput](
				definition,
				func() *resourceRegistrationInput {
					constructed = true
					return &resourceRegistrationInput{}
				},
			)
		},
	)
	require.False(t, constructed)
}

func TestMakeResourceRejectsNilProviderOutputs(t *testing.T) {
	capture := &resourceRegistrationCapture{}
	registration := MakeResourceWith[resourceRegistrationInput, *resourceRegistrationOutput](
		resourceRegistrationDefinition(),
		func() *resourceRegistrationInput { return &resourceRegistrationInput{capture: capture} },
	)
	ctx := context.Background()
	tests := []struct {
		name string
		call func() (any, error)
	}{
		{"create", func() (any, error) {
			return registration.Create(ctx, registration.NewReceiver(), resourceRegistrationConfig{})
		}},
		{"read", func() (any, error) {
			return registration.Read(ctx, registration.NewReceiver(), resourceRegistrationConfig{}, nil)
		}},
		{"update", func() (any, error) {
			return registration.Update(
				ctx, registration.NewReceiver(), resourceRegistrationConfig{}, nil, nil, nil,
			)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.call()
			require.ErrorContains(t, err, "resource outputs must not be nil")
		})
	}
}

func TestMakeResourceAcceptsLifecycleOnlyProvider(t *testing.T) {
	definition := resourceRegistrationDefinition()
	var registration ResourceRegistration
	require.NotPanics(t, func() {
		registration = MakeResource[resourceRegistrationInput, *resourceRegistrationOutput](definition)
	})
	require.Equal(t, 1, registration.SchemaVersion())
}
