package root

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

	depsListCfg = &depsSyncConfig{}
	depsListCmd = &cobra.Command{
		Use:   "list",
		Short: "List the locked dependencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsList(cmd, depsListCfg)
		},
	}

	depsVerifyCfg = &depsSyncConfig{}
	depsVerifyCmd = &cobra.Command{
		Use:   "verify",
		Short: "Check the cached dependencies against the lock",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsVerify(cmd, depsVerifyCfg)
		},
	}
)

type depsSyncConfig struct {
	stackPath     string
	replaceUnobin string
}

const (
	depsPathHelp    = "Path to the factory source; its directory is the project root."
	depsReplaceHelp = "Local path to substitute for github.com/cloudboss/unobin so the " +
		"resolver reads from a working tree instead of fetching."
)

func init() {
	depsSyncCmd.Flags().StringVarP(&depsSyncCfg.stackPath, "path", "p", "main.ub", depsPathHelp)
	depsSyncCmd.Flags().StringVar(&depsSyncCfg.replaceUnobin, "replace-unobin", "", depsReplaceHelp)
	depsListCmd.Flags().StringVarP(&depsListCfg.stackPath, "path", "p", "main.ub", depsPathHelp)
	depsVerifyCmd.Flags().StringVarP(&depsVerifyCfg.stackPath, "path", "p", "main.ub", depsPathHelp)
	depsVerifyCmd.Flags().StringVar(&depsVerifyCfg.replaceUnobin, "replace-unobin", "", depsReplaceHelp)
	DepsCmd.AddCommand(depsSyncCmd, depsListCmd, depsVerifyCmd)
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

// runDepsList prints the locked dependencies, one per line, sorted by id.
func runDepsList(cmd *cobra.Command, cfg *depsSyncConfig) error {
	lock, err := readProjectLock(cfg.stackPath)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	for _, id := range lock.SortedIDs() {
		d := lock.Deps[id]
		fmt.Fprintf(out, "%s %s (%s)\n", id, d.Version, d.Kind)
	}
	return nil
}

// runDepsVerify re-fetches the locked UB dependencies and reports any
// whose content no longer matches the recorded hash.
func runDepsVerify(cmd *cobra.Command, cfg *depsSyncConfig) error {
	lock, err := readProjectLock(cfg.stackPath)
	if err != nil {
		return err
	}
	resolver, err := newDepsResolver(filepath.Dir(cfg.stackPath), cfg.replaceUnobin)
	if err != nil {
		return err
	}
	mismatches, err := deps.Verify(lock, resolver)
	if err != nil {
		return err
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("verification failed:\n  %s", strings.Join(mismatches, "\n  "))
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "all dependencies verified")
	return nil
}

// readProjectLock reads unobin.lock from the project rooted at the
// directory of stackPath, with a clear error when it is missing.
func readProjectLock(stackPath string) (*deps.Lock, error) {
	lock, err := deps.ReadLock(os.DirFS(filepath.Dir(stackPath)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("no %s found; run `unobin deps sync` first", deps.LockFileName)
		}
		return nil, err
	}
	return lock, nil
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
