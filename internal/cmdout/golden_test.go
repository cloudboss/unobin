package cmdout

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func requireCmdoutGolden(t *testing.T, path string, value any) {
	t.Helper()
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	require.NoError(t, encoder.Encode(value))
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), buffer.String())
}

func cmdoutErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
