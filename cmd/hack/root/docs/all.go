package docs

import "github.com/spf13/cobra"

var (
	allOut string
	AllCmd = &cobra.Command{
		Use:   "all [packages...]",
		Short: "Generate CLI and Go API reference Markdown",
		RunE: func(cmd *cobra.Command, args []string) error {
			roots := args
			if len(roots) == 0 {
				roots = []string{"pkg"}
			}
			return runAll(cmd, roots)
		},
	}
)

func init() {
	AllCmd.Flags().StringVarP(&allOut, "out", "o", "docs/reference",
		"Directory to write generated Markdown into.")
}

func runAll(cmd *cobra.Command, roots []string) error {
	oldCLIOut := cliOut
	oldAPIOut := apiOut
	cliOut = allOut
	apiOut = allOut
	defer func() {
		cliOut = oldCLIOut
		apiOut = oldAPIOut
	}()
	if err := runCLI(cmd); err != nil {
		return err
	}
	return runAPI(cmd, roots)
}
