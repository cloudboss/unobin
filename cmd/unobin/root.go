package main

import (
	"os"

	"github.com/cloudboss/unobin/cmd/unobin/root"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:          "unobin",
	Short:        "Compile and manage unobin stacks",
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(
		root.VersionCmd,
		root.CompileCmd,
		root.GenerateCmd,
		root.FetchCmd,
		root.FmtCmd,
		root.PrintGraphCmd,
		root.DepsCmd,
	)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
