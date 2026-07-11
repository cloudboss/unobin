package e2etest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type commandNormalizeGolden struct {
	Cases []commandNormalizeCaseGolden `json:"cases"`
}

type commandNormalizeCaseGolden struct {
	Value string `json:"value"`
	Error string `json:"error"`
}

func TestValidateCommandNormalizeGolden(t *testing.T) {
	result := commandNormalizeGolden{}
	for _, value := range []string{"", "json", "yaml"} {
		err := validateCommands([]Command{{Name: "command", Normalize: value}})
		result.Cases = append(result.Cases, commandNormalizeCaseGolden{
			Value: value, Error: e2eErrorString(err),
		})
	}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/command-normalize.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func e2eErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestNormalizeJSONFixtures(t *testing.T) {
	cases := []string{
		"valid",
		"blank-line",
		"top-level-array",
		"duplicate-name",
		"missing-kind",
		"bad-format-version",
		"bad-content-revision",
		"bad-plan-digest",
		"bad-timestamp",
		"bad-elapsed",
		"bad-ui-url",
		"built-diagnostic",
		"extra-value",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("testdata", "json-normalize", name+".jsonl")
			input, err := os.ReadFile(path)
			require.NoError(t, err)
			output, normalizeErr := normalizeJSONOutput(string(input), "/repo/root", "/tmp/work")
			requireJSONNormalizeGolden(t, name, output, normalizeErr)
		})
	}

	t.Run("missing-final-newline", func(t *testing.T) {
		input, err := os.ReadFile("testdata/json-normalize/valid.jsonl")
		require.NoError(t, err)
		output, normalizeErr := normalizeJSONOutput(
			strings.TrimSuffix(string(input), "\n"), "/repo/root", "/tmp/work",
		)
		requireJSONNormalizeGolden(t, "missing-final-newline", output, normalizeErr)
	})
}

func requireJSONNormalizeGolden(t *testing.T, name, output string, normalizeErr error) {
	t.Helper()
	suffix := ".stdout"
	actual := output
	if normalizeErr != nil {
		suffix = ".err"
		actual = normalizeErr.Error() + "\n"
	}
	want, err := os.ReadFile(filepath.Join("testdata", "json-normalize", name+suffix))
	require.NoError(t, err)
	require.Equal(t, string(want), actual)
	if suffix == ".stdout" {
		require.NoError(t, normalizeErr)
	} else {
		require.True(t, errors.Is(normalizeErr, errJSONOutput))
	}
}
