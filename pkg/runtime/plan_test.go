package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPartialValueKeepsListStructure(t *testing.T) {
	expr := parseValue(t, "['lit', resource.one.id]")
	got, refs, err := partialValue(expr, &EvalContext{}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"resource.one.id"}, refs)
	require.Equal(t, []any{
		"lit",
		PendingValue{Refs: []string{"resource.one.id"}},
	}, got)
}

func TestPartialValueKeepsObjectStructure(t *testing.T) {
	expr := parseValue(t, "{ ready: true, id: resource.one.id }")
	got, refs, err := partialValue(expr, &EvalContext{}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"resource.one.id"}, refs)
	require.Equal(t, map[string]any{
		"ready": true,
		"id":    PendingValue{Refs: []string{"resource.one.id"}},
	}, got)
}

func TestPartialValueKeepsStringKeyedFields(t *testing.T) {
	expr := parseValue(t, "{ 'app/role': 'web', id: resource.one.id }")
	got, refs, err := partialValue(expr, &EvalContext{}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"resource.one.id"}, refs)
	require.Equal(t, map[string]any{
		"app/role": "web",
		"id":       PendingValue{Refs: []string{"resource.one.id"}},
	}, got)
}
