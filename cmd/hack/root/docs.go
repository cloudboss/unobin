package root

import (
	"github.com/cloudboss/unobin/cmd/hack/root/docs"
	"github.com/spf13/cobra"
)

var DocsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Generate reference documentation for the unobin docs site",
}

func init() {
	DocsCmd.AddCommand(docs.CLICmd)
}
