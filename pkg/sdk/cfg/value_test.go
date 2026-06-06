package cfg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAtomicWrappersCarryValueAndSchema(t *testing.T) {
	s := String{Value: "hello", Description: "a greeting"}
	require.Equal(t, "hello", s.Value)
	require.Equal(t, "a greeting", s.Description)

	i := Integer{Value: 42}
	require.EqualValues(t, 42, i.Value)

	n := Number{Value: 3.14}
	require.InDelta(t, 3.14, n.Value, 0.001)

	b := Boolean{Value: true}
	require.True(t, b.Value)
}

func TestCollectionsHoldWrapperElements(t *testing.T) {
	l := List[String]{
		Value: []String{{Value: "a"}, {Value: "b"}},
	}
	require.Len(t, l.Value, 2)
	require.Equal(t, "a", l.Value[0].Value)

	m := Map[Integer]{
		Value: map[string]Integer{
			"x": {Value: 1},
			"y": {Value: 2},
		},
	}
	require.EqualValues(t, 1, m.Value["x"].Value)
}

func TestObjectWrapsUserStructInsideCollection(t *testing.T) {
	type Server struct {
		Host String
		Port Integer
	}

	l := List[Object[Server]]{
		Value: []Object[Server]{
			{Value: Server{
				Host: String{Value: "a"},
				Port: Integer{Value: 80},
			}},
		},
	}
	require.Equal(t, "a", l.Value[0].Value.Host.Value)
	require.EqualValues(t, 80, l.Value[0].Value.Port.Value)
}

func TestMapOfMapComposesWithoutObject(t *testing.T) {
	mm := Map[Map[String]]{
		Value: map[string]Map[String]{
			"east": {Value: map[string]String{
				"profile": {Value: "prod"},
			}},
		},
	}
	require.Equal(t, "prod", mm.Value["east"].Value["profile"].Value)
}

// Every wrapper must satisfy Value. Because Value's marker is
// unexported, external code cannot satisfy it without a wrapper
// from this package, which keeps List, Map, and Set safe from
// foreign element types.
func TestEveryWrapperSatisfiesValue(t *testing.T) {
	var _ Value = String{}
	var _ Value = Integer{}
	var _ Value = Number{}
	var _ Value = Boolean{}
	var _ Value = Null{}
	var _ Value = Any{}
	var _ Value = List[String]{}
	var _ Value = Map[String]{}
	var _ Value = Object[struct{}]{}
}
