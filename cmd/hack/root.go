// Command hack is the build-time developer tooling for the unobin repo:
// tasks that run from a source checkout, not features of the shipped
// unobin binary. Today it generates reference documentation.
package main

import (
	"os"

	"github.com/cloudboss/unobin/cmd/hack/root"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:          "hack",
	Short:        "Build-time developer tooling for the unobin repo",
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(root.DocsCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
