package root

import (
	"github.com/cloudboss/unobin/cmd/unobin/root/generate"
	"github.com/spf13/cobra"
)

var GenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate code and scaffold libraries",
}

func init() {
	GenerateCmd.AddCommand(generate.GolibraryCmd)
	GenerateCmd.AddCommand(generate.FactoryCmd)
	GenerateCmd.AddCommand(generate.UblibraryCmd)
}
