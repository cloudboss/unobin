package runner

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type runnerFormatInventoryGolden struct {
	Commands []runnerFormatCommandGolden `json:"commands"`
}

type runnerFormatCommandGolden struct {
	Path    string                  `json:"path"`
	Payload bool                    `json:"payload"`
	Format  *runnerFormatFlagGolden `json:"format"`
}

type runnerFormatFlagGolden struct {
	Default string `json:"default"`
	Help    string `json:"help"`
}

func TestCompiledFormatInventoryGolden(t *testing.T) {
	root := newRootCmd(Info{FactoryName: "factory"})
	cases := []runnerFormatCommandGolden{
		{Path: "version"},
		{Path: "validate"},
		{Path: "schema show"},
		{Path: "plan"},
		{Path: "apply"},
		{Path: "refresh"},
		{Path: "output"},
		{Path: "print-graph"},
		{Path: "pin"},
		{Path: "state list"},
		{Path: "state show"},
		{Path: "state move"},
		{Path: "state remove"},
		{Path: "state snapshots list"},
		{Path: "state snapshots gc"},
		{Path: "state force-unlock"},
		{Path: "schema template", Payload: true},
		{Path: "state pull", Payload: true},
	}
	for i := range cases {
		command := findRunnerInventoryCommand(t, root, cases[i].Path)
		if flag := command.Flags().Lookup("format"); flag != nil {
			cases[i].Format = &runnerFormatFlagGolden{
				Default: flag.DefValue,
				Help:    flag.Usage,
			}
		}
	}
	requireRunnerInventoryGolden(t, runnerFormatInventoryGolden{Commands: cases})
}

func findRunnerInventoryCommand(t *testing.T, root *cobra.Command, path string) *cobra.Command {
	t.Helper()
	command, remaining, err := root.Find(strings.Fields(path))
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.Equal(t, path, strings.TrimPrefix(command.CommandPath(), root.Name()+" "))
	return command
}

func requireRunnerInventoryGolden(t *testing.T, value any) {
	t.Helper()
	var got bytes.Buffer
	encoder := json.NewEncoder(&got)
	encoder.SetIndent("", "  ")
	require.NoError(t, encoder.Encode(value))
	want, err := os.ReadFile("testdata/format-inventory.json")
	require.NoError(t, err)
	require.Equal(t, string(want), got.String())
}
