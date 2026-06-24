package root

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/pkg/lsp"
)

var lspTracePath string
var lspLogPath string

// LSPCmd runs the Unobin language server.
var LSPCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Run the Unobin language server over stdio",
	RunE:  runLSP,
}

func init() {
	LSPCmd.Flags().StringVar(&lspTracePath, "trace", "", "write JSON-RPC trace log")
	LSPCmd.Flags().StringVar(&lspLogPath, "log", "", "write server log")
}

func runLSP(cmd *cobra.Command, args []string) (err error) {
	_ = args
	options := lsp.Options{Version: Version}
	traceFile, err := openLSPOutput(lspTracePath, "--trace")
	if err != nil {
		return err
	}
	if traceFile != nil {
		defer func() {
			if closeErr := traceFile.Close(); err == nil {
				err = closeErr
			}
		}()
		options.Trace = traceFile
	}
	logFile, err := openLSPOutput(lspLogPath, "--log")
	if err != nil {
		return err
	}
	if logFile != nil {
		defer func() {
			if closeErr := logFile.Close(); err == nil {
				err = closeErr
			}
		}()
		options.Log = logFile
	}
	return lsp.ServeWithOptions(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), options)
}

func openLSPOutput(path string, flag string) (*os.File, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("open %s path: %w", flag, err)
	}
	return file, nil
}
