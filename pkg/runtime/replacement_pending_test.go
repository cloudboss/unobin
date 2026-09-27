package runtime

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPendingCollectionDoesNotInvokeTypedPredicates(t *testing.T) {
	labels := InputField(func(in *replacementTestInput) *map[string]string { return &in.Labels })
	calls := 0
	definition, err := resolveResourceDefinition(ResourceDefinition[
		replacementTestInput, *replacementTestOutput, any,
	]{
		SchemaVersion: 1,
		Equality: []InputEqualityRule[replacementTestInput]{
			EqualBy(labels, func(a, b map[string]string) bool {
				calls++
				return maps.Equal(a, b)
			}),
		},
		Replace: Replacement[replacementTestInput, *replacementTestOutput, any]{
			Rules: []ReplacementRule[replacementTestInput]{
				ReplaceWhen(labels, func(_, _ map[string]string) bool {
					calls++
					return true
				}),
			},
		},
	})
	require.NoError(t, err)
	prior := replacementTestInput{Labels: map[string]string{"id": "recorded"}}
	desired := replacementTestInput{}
	pending := map[string][]string{"labels": {"resource.source.id"}}
	equal, err := definition.knownInputsEqual(prior, desired, pending)
	require.NoError(t, err)
	require.True(t, equal)
	reasons, err := definition.knownReplacementReasons(
		prior, desired, nil, nil, nil, nil, pending, false,
	)
	require.NoError(t, err)
	require.Empty(t, reasons)
	require.Zero(t, calls)

	// An explicit null is concrete and must reach the declared comparison rules.
	equal, err = definition.inputsEqual(prior, desired)
	require.NoError(t, err)
	require.False(t, equal)
	reasons, err = definition.replacementReasons(prior, desired, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"labels"}, reasons)
	require.Equal(t, 3, calls)
}
