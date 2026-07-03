package root

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

var ModZipCmd = newModZipCmd()

type modZipOptions struct {
	modPath string
	version string
	repo    string
	outPath string
}

func newModZipCmd() *cobra.Command {
	opts := &modZipOptions{}
	cmd := &cobra.Command{
		Use:     "modzip",
		Aliases: []string{"mkmodzip"},
		Short:   "Create a Go module zip from a local VCS checkout",
		Args:    noModZipArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.validate(); err != nil {
				return err
			}
			return runModZip(opts.modPath, opts.version, opts.repo, opts.outPath)
		},
	}
	cmd.Flags().StringVar(&opts.modPath, "module", "", "Module path to put in the zip.")
	cmd.Flags().StringVar(&opts.version, "version", "", "Module version to put in the zip.")
	cmd.Flags().StringVar(&opts.repo, "repo", "", "VCS checkout to read.")
	cmd.Flags().StringVar(&opts.outPath, "out", "", "Zip file to write.")
	return cmd
}

func noModZipArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf(
		"unexpected positional arguments %q; use --module, --version, --repo, and --out flags",
		args,
	)
}

func (opts modZipOptions) validate() error {
	var missing []string
	if opts.modPath == "" {
		missing = append(missing, "--module")
	}
	if opts.version == "" {
		missing = append(missing, "--version")
	}
	if opts.repo == "" {
		missing = append(missing, "--repo")
	}
	if opts.outPath == "" {
		missing = append(missing, "--out")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	return nil
}

func runModZip(modPath, version, repo, outPath string) error {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create output zip: %w", err)
	}

	zipErr := modzip.CreateFromVCS(out, module.Version{
		Path:    modPath,
		Version: version,
	}, absRepo, version, "")
	closeErr := out.Close()
	if zipErr != nil {
		_ = os.Remove(outPath)
		return fmt.Errorf("create module zip: %w", zipErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close output zip: %w", closeErr)
	}
	return nil
}
