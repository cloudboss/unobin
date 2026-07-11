package root

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type formatInventoryGolden struct {
	Commands []formatInventoryCommandGolden `json:"commands"`
}

type formatInventoryCommandGolden struct {
	Path    string                     `json:"path"`
	Payload bool                       `json:"payload"`
	Format  *formatInventoryFlagGolden `json:"format"`
}

type formatInventoryFlagGolden struct {
	Default string `json:"default"`
	Help    string `json:"help"`
}

func TestDeveloperFormatInventoryGolden(t *testing.T) {
	root := developerInventoryRoot()
	cases := []formatInventoryCommandGolden{
		{Path: "version"},
		{Path: "check"},
		{Path: "compile"},
		{Path: "deps list"},
		{Path: "deps sync"},
		{Path: "deps get"},
		{Path: "deps verify"},
		{Path: "deps clean"},
		{Path: "generate factory"},
		{Path: "generate golibrary"},
		{Path: "generate ublibrary"},
		{Path: "print-graph"},
		{Path: "fmt", Payload: true},
		{Path: "lsp", Payload: true},
	}
	for i := range cases {
		command := findInventoryCommand(t, root, cases[i].Path)
		if flag := command.Flags().Lookup("format"); flag != nil {
			cases[i].Format = &formatInventoryFlagGolden{
				Default: flag.DefValue,
				Help:    flag.Usage,
			}
		}
	}
	requireFormatInventoryGolden(t, "testdata/format-inventory.json", formatInventoryGolden{
		Commands: cases,
	})
}

func developerInventoryRoot() *cobra.Command {
	root := &cobra.Command{Use: "unobin"}
	root.AddCommand(
		VersionCmd,
		CheckCmd,
		CompileCmd,
		DepsCmd,
		GenerateCmd,
		PrintGraphCmd,
		FmtCmd,
		LSPCmd,
	)
	return root
}

func findInventoryCommand(t *testing.T, root *cobra.Command, path string) *cobra.Command {
	t.Helper()
	command, remaining, err := root.Find(strings.Fields(path))
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.Equal(t, path, strings.TrimPrefix(command.CommandPath(), root.Name()+" "))
	return command
}

func requireFormatInventoryGolden(t *testing.T, path string, value any) {
	t.Helper()
	var got bytes.Buffer
	encoder := json.NewEncoder(&got)
	encoder.SetIndent("", "  ")
	require.NoError(t, encoder.Encode(value))
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), got.String())
}
