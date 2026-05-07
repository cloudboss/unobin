package root

import (
	"github.com/cloudboss/unobin/cmd/unobin/root/generate"
	"github.com/spf13/cobra"
)

var GenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate code from external sources",
}

func init() {
	GenerateCmd.AddCommand(generate.GomoduleCmd)
}
