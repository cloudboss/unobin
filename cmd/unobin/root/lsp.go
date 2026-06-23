package root

import (
	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/pkg/lsp"
)

var lspTracePath string
var lspLogPath string

// LSPCmd runs the Unobin language server.
var LSPCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Run the Unobin language server over stdio",
	RunE: func(cmd *cobra.Command, args []string) error {
		return lsp.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), Version)
	},
}

func init() {
	LSPCmd.Flags().StringVar(&lspTracePath, "trace", "", "write JSON-RPC trace log")
	LSPCmd.Flags().StringVar(&lspLogPath, "log", "", "write server log")
}
