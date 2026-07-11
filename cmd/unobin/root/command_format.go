package root

import (
	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/spf13/cobra"
)

func addFormatFlag(command *cobra.Command) {
	command.Flags().String("format", "text", cmdout.FormatHelp())
}
