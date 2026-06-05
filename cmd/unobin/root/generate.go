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
	// Assigned as the function, not its value: the version is stamped
	// before commands run, after this package's vars initialize.
	generate.CLIVersion = cliVersion
	GenerateCmd.AddCommand(generate.GolibraryCmd)
	GenerateCmd.AddCommand(generate.FactoryCmd)
	GenerateCmd.AddCommand(generate.UblibraryCmd)
}
