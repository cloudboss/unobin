package main

import (
	"os"

	"github.com/cloudboss/unobin/cmd/unobin/root"
	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "unobin",
	Short:         "Compile and manage unobin stacks",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.AddCommand(
		root.VersionCmd,
		root.CheckCmd,
		root.CompileCmd,
		root.GenerateCmd,
		root.FmtCmd,
		root.PrintGraphCmd,
		root.DepsCmd,
		root.LSPCmd,
	)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		cmdout.PrintUnreportedError(rootCmd, err)
		os.Exit(1)
	}
}
