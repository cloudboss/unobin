package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func parseBlocksFile(t *testing.T) *File {
	t.Helper()
	f, err := ParseSource("blocks.ub", []byte(`
inputs:      { who: { type: string } }
constraints: [ { kind: predicate, when: true, require: true } ]
state:       'not an object'
`))
	require.NoError(t, err)
	return f
}

func TestTopLevelBlock(t *testing.T) {
	f := parseBlocksFile(t)

	obj := TopLevelBlock(f, "inputs")
	require.NotNil(t, obj)
	require.Len(t, obj.Fields, 1)
	require.Equal(t, "who", obj.Fields[0].Key.Name)

	require.Nil(t, TopLevelBlock(f, "outputs"), "absent field")
	require.Nil(t, TopLevelBlock(f, "state"), "value of another form")
	require.Nil(t, TopLevelBlock(f, "constraints"), "array, not object")
	require.Nil(t, TopLevelBlock(nil, "inputs"), "nil file")
}

func TestTopLevelArray(t *testing.T) {
	f := parseBlocksFile(t)

	arr := TopLevelArray(f, "constraints")
	require.NotNil(t, arr)
	require.Len(t, arr.Elements, 1)
	require.IsType(t, &ObjectLit{}, arr.Elements[0])

	require.Nil(t, TopLevelArray(f, "outputs"), "absent field")
	require.Nil(t, TopLevelArray(f, "state"), "value of another form")
	require.Nil(t, TopLevelArray(f, "inputs"), "object, not array")
	require.Nil(t, TopLevelArray(nil, "constraints"), "nil file")
}
