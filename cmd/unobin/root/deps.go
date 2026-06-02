package root

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/spf13/cobra"
)

// DepsCmd is the parent for the dependency-management subcommands.
var DepsCmd = &cobra.Command{
	Use:   "deps",
	Short: "Manage a factory's dependencies",
}

var (
	depsSyncCfg = &depsSyncConfig{}
	depsSyncCmd = &cobra.Command{
		Use:   "sync",
		Short: "Reconcile unobin.manifest and unobin.lock with the imports",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsSync(cmd, depsSyncCfg)
		},
	}
)

type depsSyncConfig struct {
	stackPath     string
	replaceUnobin string
}

func init() {
	depsSyncCmd.Flags().StringVarP(&depsSyncCfg.stackPath, "path", "p", "main.ub",
		"Path to the factory source; its directory is the project root.")
	depsSyncCmd.Flags().StringVar(&depsSyncCfg.replaceUnobin, "replace-unobin", "",
		"Local path to substitute for github.com/cloudboss/unobin so the "+
			"resolver reads from a working tree instead of fetching.")
	DepsCmd.AddCommand(depsSyncCmd)
}

// runDepsSync rebuilds unobin.manifest and unobin.lock from the project's
// imports: it collects the direct requirements into the manifest, selects
// versions across the dependency graph, walks the imports to pin every
// remote library, and writes both files at the project root.
func runDepsSync(cmd *cobra.Command, cfg *depsSyncConfig) error {
	root := filepath.Dir(cfg.stackPath)

	manifest, err := deps.ManifestFromImports(root)
	if err != nil {
		return err
	}
	resolver, err := newDepsResolver(root, cfg.replaceUnobin)
	if err != nil {
		return err
	}
	selection, err := deps.Resolve(manifest, deps.NewFetcher(resolver))
	if err != nil {
		return err
	}
	lock, err := deps.LockFromImports(os.DirFS(root), selection, resolver)
	if err != nil {
		return err
	}
	if err := deps.WriteManifest(filepath.Join(root, deps.ManifestFileName), manifest); err != nil {
		return err
	}
	if err := deps.WriteLock(filepath.Join(root, deps.LockFileName), lock); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s (%d direct) and %s (%d locked)\n",
		deps.ManifestFileName, len(manifest.Requires), deps.LockFileName, len(lock.Deps))
	return nil
}

func newDepsResolver(root, replaceUnobin string) (resolve.Resolver, error) {
	resolver, err := newCompileResolver(root)
	if err != nil {
		return nil, err
	}
	if replaceUnobin != "" {
		abs, err := filepath.Abs(replaceUnobin)
		if err != nil {
			return nil, err
		}
		resolver = &replaceResolver{
			prefix:  "github.com/cloudboss/unobin",
			local:   abs,
			wrapped: resolver,
		}
	}
	return resolver, nil
}
