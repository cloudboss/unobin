package root

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/cmdout"
)

func TestCompileRejectsUnknownProfile(t *testing.T) {
	command := &cobra.Command{Use: "compile"}
	root := &cobra.Command{Use: "unobin"}
	root.AddCommand(command)
	command.Flags().String("format", "json", "")
	var output bytes.Buffer
	command.SetOut(&output)
	directory := filepath.Join(t.TempDir(), "build")
	err := runCompile(command, &compileConfig{profile: "custom", outDir: directory})
	require.EqualError(t, err, "compile arguments are invalid")
	require.True(t, cmdout.IsReported(err))
	var result cmdout.CommandError
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	require.Equal(t, cmdout.CodeInvalidArgs, result.Code)
	require.Equal(t, "compile arguments are invalid", result.Message)
	require.Len(t, result.Diagnostics, 1)
	require.Equal(t, `unknown build profile "custom" (want full or local)`,
		result.Diagnostics[0].Message)
	_, err = os.Stat(directory)
	require.ErrorIs(t, err, os.ErrNotExist)
}
