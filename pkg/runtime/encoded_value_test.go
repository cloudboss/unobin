package runtime

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodedValuePublicAPI(t *testing.T) {
	name := StringValue("example")
	fields, err := ObjectValue(map[string]EncodedValue{"name": name})
	require.NoError(t, err)
	require.Equal(t, EncodedValueObject, fields.Kind())

	encoded, err := json.Marshal(fields)
	require.NoError(t, err)
	require.Equal(
		t,
		`{"kind":"object","fields":[{"name":"name","value":{"kind":"string","value":"example"}}]}`,
		string(encoded),
	)

	decoded, err := DecodeEncodedValue(encoded)
	require.NoError(t, err)
	require.Equal(t, fields, decoded)
}
