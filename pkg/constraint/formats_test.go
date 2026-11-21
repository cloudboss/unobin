package constraint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatBuildersAndMapLoops(t *testing.T) {
	for _, cond := range []func(any) Condition{
		IsRegex, IsIP, IsCIDR, IsBase64, IsPositiveDuration,
	} {
		require.Equal(t, KindPredicate, Must(cond(nil)).kind)
	}
	require.Equal(t, KindPredicate, Must(Matches(nil, "^a$")).kind)
	var values *map[string]string
	require.Equal(t, KindForEach, ForEach(values, func(MapEntry[string]) []Constraint {
		t.Fatal("declarations must not execute callbacks")
		return nil
	}).kind)
	var list []string
	require.Equal(t, KindForEach, ForEach[string](list, func(string) []Constraint {
		t.Fatal("declarations must not execute callbacks")
		return nil
	}).kind)
}
