package runtime

import (
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

func TestMakeResourceAcceptsLifecycleOnlyProvider(t *testing.T) {
	definition := resourceRegistrationDefinition()
	var registration ResourceRegistration
	require.NotPanics(t, func() {
		registration = MakeResource[resourceRegistrationInput, *resourceRegistrationOutput](definition)
	})
	require.NotNil(t, registration.resourceDefinition())
}
