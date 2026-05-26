package docs

import (
	"fmt"
	"os"
	"path/filepath"

	unobinroot "github.com/cloudboss/unobin/cmd/unobin/root"
	"github.com/cloudboss/unobin/pkg/docgen"
	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/spf13/cobra"
)

var (
	cliOut string
	CLICmd = &cobra.Command{
		Use:   "cli",
		Short: "Generate the unobin CLI reference as Markdown",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCLI(cmd)
		},
	}
)

func init() {
	CLICmd.Flags().StringVarP(&cliOut, "out", "o", "docs/reference",
		"Directory to write the generated Markdown into.")
}

func runCLI(cmd *cobra.Command) error {
	if err := os.MkdirAll(cliOut, 0o755); err != nil {
		return err
	}
	// Mirror the unobin root so the reference covers the whole CLI; this
	// subcommand list must stay in step with cmd/unobin/root.go.
	unobinCmd := &cobra.Command{
		Use:   "unobin",
		Short: "Compile and manage unobin stacks",
	}
	unobinCmd.AddCommand(
		unobinroot.VersionCmd,
		unobinroot.CompileCmd,
		unobinroot.GenerateCmd,
		unobinroot.FetchCmd,
		unobinroot.FmtCmd,
		unobinroot.PrintGraphCmd,
	)
	content := []byte(docgen.CLIReference(unobinCmd))
	path := filepath.Join(cliOut, "cli.md")
	if err := ufs.WriteFileAtomic(path, content, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s\n", path)
	return nil
}
