package main

import (
	"os"

	"github.com/cloudboss/unobin/cmd/unobin/root"
	"github.com/spf13/cobra"
)

var (
	RootCmd = &cobra.Command{
		Use:          "unobin",
		Short:        "Compile and manage unobin stacks",
		SilenceUsage: true,
	}
)

func init() {
	RootCmd.AddCommand(root.VersionCmd)
	RootCmd.AddCommand(root.CompileCmd)
	RootCmd.AddCommand(root.GenerateCmd)
	RootCmd.AddCommand(root.FetchCmd)
	RootCmd.AddCommand(root.PrintGraphCmd)
}

func main() {
	if err := RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
