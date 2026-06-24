package root

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

func TestLSPTraceFlagWritesInitialize(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "trace.jsonl")
	var input bytes.Buffer
	require.NoError(t, protocol.WriteMessage(&input, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
	)))

	_, err := runLSPCommandWithInput(t, &input, "lsp", "--trace", tracePath)
	require.NoError(t, err)

	trace, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	require.Contains(t, string(trace), "initialize")
	require.Contains(t, string(trace), `"direction":"in"`)
	require.Contains(t, string(trace), `"direction":"out"`)
}

func TestLSPLogFlagWritesLifecycle(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "server.log")

	_, err := runLSPCommandWithInput(t, strings.NewReader(""), "lsp", "--log", logPath)
	require.NoError(t, err)

	logBody, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(logBody), "server started")
	require.Contains(t, string(logBody), "server stopped")
}

func TestLSPTraceOpenErrorDoesNotStartServer(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "missing", "trace.jsonl")
	var input bytes.Buffer
	require.NoError(t, protocol.WriteMessage(&input, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
	)))

	out, err := runLSPCommandWithInput(t, &input, "lsp", "--trace", tracePath)
	require.Error(t, err)
	require.NotContains(t, out, "Content-Length")
	require.Contains(t, err.Error(), "--trace")
}

func TestLSPCommandExists(t *testing.T) {
	out, err := runCommand(t, "lsp", "--help")
	require.NoError(t, err)
	require.Contains(t, out, "lsp")
	require.Contains(t, out, "--trace")
	require.Contains(t, out, "--log")
}

func runLSPCommandWithInput(
	t *testing.T,
	input io.Reader,
	args ...string,
) (string, error) {
	t.Helper()
	resetFlags(LSPCmd)
	root := &cobra.Command{
		Use:          "unobin",
		SilenceUsage: true,
	}
	root.AddCommand(LSPCmd)
	out := &bytes.Buffer{}
	root.SetIn(input)
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}
