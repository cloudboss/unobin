package runner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const outputsFactorySrc = `
inputs: { region: { type: string } }
outputs: {
  endpoint: { value: var.region, description: 'Public endpoint URL' }
  token:    { value: var.region, @sensitive: true }
  plain:    { value: var.region }
}
`

func TestSchemaShowsOutputs(t *testing.T) {
	info := Info{
		FactoryName:     "test-stack",
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		FactoryBody:     outputsFactorySrc,
	}
	out, err := runRoot(t, info, "schema")
	require.NoError(t, err)
	require.Contains(t, out, "outputs:")
	require.Contains(t, out, "  endpoint  -- Public endpoint URL\n")
	require.Contains(t, out, "  token (sensitive)\n")
	require.Contains(t, out, "  plain\n")
}
