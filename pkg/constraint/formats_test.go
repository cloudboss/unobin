package constraint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatBuildersAndMapLoops(t *testing.T) {
	require.Equal(t, KindPredicate, Must(IsFormat(nil, "ip")).kind)
	require.Equal(t, KindPredicate, Must(IsMatch(nil, "^a$")).kind)
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
