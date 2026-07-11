package runner

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

type applyFormatGolden struct {
	Cases []applyFormatCaseGolden `json:"cases"`
}

type applyFormatCaseGolden struct {
	Name       string `json:"name"`
	Format     string `json:"format"`
	Deprecated bool   `json:"deprecated"`
	Conflict   string `json:"conflict"`
	Error      string `json:"error"`
}

func TestApplyFormatPrecedenceGolden(t *testing.T) {
	cases := []struct {
		name   string
		format *string
		output *string
	}{
		{name: "defaults to text"},
		{name: "explicit format", format: new("json")},
		{name: "deprecated output", output: new("unobin")},
		{
			name: "both flags conflict", format: new("json"),
			output: new("unobin"),
		},
		{
			name: "invalid format wins", format: new("yaml"),
			output: new("bogus"),
		},
		{
			name: "invalid output follows valid format", format: new("json"),
			output: new("bogus"),
		},
		{name: "invalid output", output: new("bogus")},
		{
			name: "text format conflict", format: new("text"),
			output: new("json"),
		},
	}
	result := applyFormatGolden{}
	for _, tc := range cases {
		command := newApplyCmd(Info{})
		if tc.format != nil {
			require.NoError(t, command.Flags().Set("format", *tc.format))
		}
		if tc.output != nil {
			require.NoError(t, command.Flags().Set("output", *tc.output))
		}
		output, err := command.Flags().GetString("output")
		require.NoError(t, err)
		format, deprecated, conflict, err := resolveApplyCommandFormat(command, output)
		entry := applyFormatCaseGolden{
			Name: tc.name, Format: string(format), Deprecated: deprecated,
			Conflict: runnerErrorString(conflict), Error: runnerErrorString(err),
		}
		result.Cases = append(result.Cases, entry)
	}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/apply-format-precedence.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}
