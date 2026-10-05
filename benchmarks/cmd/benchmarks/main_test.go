package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommandsExplainEvidenceOperations(t *testing.T) {
	command := newCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--help"})
	require.NoError(t, command.ExecuteContext(t.Context()))
	for _, name := range []string{"collect", "compare", "validate", "validate-record"} {
		require.Contains(t, output.String(), name)
	}
	require.Contains(t, output.String(), "--repository")
	require.Contains(t, output.String(), "--tools")
}

func TestCommandsRequireInputsAndResolveRecordPaths(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args []string
		want string
	}{
		{name: "collect", args: []string{"collect"}, want: "required flag(s)"},
		{name: "compare", args: []string{"compare"}, want: "required flag(s)"},
		{name: "validate", args: []string{"validate"}, want: "required flag(s)"},
		{name: "record", args: []string{"validate-record"}, want: "required flag(s)"},
		{name: "unknown flag", args: []string{"collect", "--ignore-dirty"}, want: "unknown flag"},
		{name: "missing record", args: []string{"validate-record", "--record", "missing"},
			want: filepath.Join("missing", "metadata.json")},
		{name: "missing comparison", args: []string{"validate", "--comparison", "missing"},
			want: filepath.Join("missing", "metadata.json")},
		{name: "missing workload", args: []string{"collect", "--spec", "missing.json", "--out", "new"},
			want: "missing.json"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository := t.TempDir()
			command := newCommand()
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&output)
			command.SetArgs(append([]string{"--repository", repository}, testCase.args...))
			err := command.ExecuteContext(t.Context())
			require.ErrorContains(t, err, testCase.want)
			require.NoDirExists(t, filepath.Join(repository, "new"))
		})
	}
}
