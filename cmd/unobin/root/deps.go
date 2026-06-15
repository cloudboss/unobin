package root

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/git"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/toolchain"
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
		Short: "Reconcile the manifest and lock with the imports",
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

	depsCleanCmd = &cobra.Command{
		Use:   "clean",
		Short: "Remove the cached dependency sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsClean(cmd)
		},
	}

	depsGetCfg = &depsSyncConfig{}
	depsGetCmd = &cobra.Command{
		Use:   "get <repo>[@version]",
		Short: "Add or update a dependency floor and re-pin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsGet(cmd, depsGetCfg, args[0])
		},
	}
)

// depsListTags lists a repository's tags. It is a package var so tests can
// resolve versions without a network round trip.
var depsListTags = func(url string) ([]string, error) {
	return git.ListTags(context.Background(), resolve.WithDefaultScheme(url))
}

type depsSyncConfig struct {
	stackPath     string
	replaceUnobin string
}

const (
	depsPathHelp    = "Path to the factory source file or project directory."
	depsReplaceHelp = "Local path to substitute for github.com/cloudboss/unobin so the " +
		"resolver reads from a working tree instead of fetching."
)

func init() {
	depsSyncCmd.Flags().StringVarP(&depsSyncCfg.stackPath, "path", "p", ".", depsPathHelp)
	depsSyncCmd.Flags().StringVar(&depsSyncCfg.replaceUnobin, "replace-unobin", "", depsReplaceHelp)
	depsListCmd.Flags().StringVarP(&depsListCfg.stackPath, "path", "p", ".", depsPathHelp)
	depsVerifyCmd.Flags().StringVarP(&depsVerifyCfg.stackPath, "path", "p", ".", depsPathHelp)
	depsVerifyCmd.Flags().StringVar(&depsVerifyCfg.replaceUnobin, "replace-unobin", "", depsReplaceHelp)
	depsGetCmd.Flags().StringVarP(&depsGetCfg.stackPath, "path", "p", ".", depsPathHelp)
	depsGetCmd.Flags().StringVar(&depsGetCfg.replaceUnobin, "replace-unobin", "", depsReplaceHelp)
	DepsCmd.AddCommand(depsSyncCmd, depsListCmd, depsVerifyCmd, depsCleanCmd, depsGetCmd)
}

// projectRoot resolves the project root from a --path value. When an
// ancestor has manifest.ub, that directory is the project root. Without a
// manifest, the path itself is the root when it is a directory; otherwise its
// parent is used so first-time deps sync can create manifest.ub there.
func projectRoot(stackPath string) (string, error) {
	root, err := deps.FindManifestDir(stackPath)
	if err == nil {
		return root, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if info, err := os.Stat(stackPath); err == nil && info.IsDir() {
		return stackPath, nil
	}
	return filepath.Dir(stackPath), nil
}

// runDepsSync reconciles the project manifest and lock with the
// project's imports. The manifest holds the floors; sync reads it,
// requires a floor for every imported repository, removes floors for
// repositories no longer imported, then selects versions across the
// dependency graph, walks the imports to pin every remote library, and
// writes both files at the project root.
func runDepsSync(cmd *cobra.Command, cfg *depsSyncConfig) error {
	root, err := projectRoot(cfg.stackPath)
	if err != nil {
		return err
	}
	manifest, manifestName, err := readManifestOrEmpty(root)
	if err != nil {
		return err
	}
	imported, err := deps.ImportedRepos(root)
	if err != nil {
		return err
	}
	if err := reconcileManifest(manifestName, manifest, imported); err != nil {
		return err
	}
	return resolveAndWrite(cmd, root, manifest, cfg.replaceUnobin)
}

// runDepsGet resolves a version for one dependency, sets its floor in the
// manifest, and re-pins. The query may be empty or "latest" (the highest
// tag), an exact version, or a partial one (v1, v1.2).
func runDepsGet(cmd *cobra.Command, cfg *depsSyncConfig, arg string) error {
	root, err := projectRoot(cfg.stackPath)
	if err != nil {
		return err
	}
	dep, query, err := parseGetArg(arg)
	if err != nil {
		return err
	}
	if dep.URL == toolchain.UnobinModulePath {
		return fmt.Errorf(
			"%s is toolchain-versioned; pin it with the manifest's unobin-version line",
			dep.URL)
	}
	tags, err := depsListTags(dep.URL)
	if err != nil {
		return err
	}
	version, err := deps.ResolveVersion(dep, query, tags)
	if err != nil {
		return err
	}
	manifest, _, err := readManifestOrEmpty(root)
	if err != nil {
		return err
	}
	manifest.Requires[dep] = version
	fmt.Fprintf(cmd.ErrOrStderr(), "Using %s %s\n", dep, version)
	return resolveAndWrite(cmd, root, manifest, cfg.replaceUnobin)
}

// readManifestOrEmpty reads the project manifest from root, returning an
// empty manifest when the file does not exist yet. There is no `deps init`:
// the manifest is created the first time get or sync writes it.
func readManifestOrEmpty(root string) (*deps.Manifest, string, error) {
	manifest, err := deps.ReadManifest(os.DirFS(root))
	if errors.Is(err, fs.ErrNotExist) {
		return &deps.Manifest{Requires: map[deps.Dependency]string{}}, deps.ManifestFileName, nil
	}
	if err != nil {
		return nil, deps.ManifestFileName, err
	}
	return manifest, deps.ManifestFileName, nil
}

// reconcileManifest makes the manifest's floors match the set of imported
// repositories. An imported repository with no floor is an error that
// points the author at `deps get`; a floor whose repository is no longer
// imported is removed. The unobin repository takes no floor at all: an
// import from it must be served by a replace, since its source version
// may not float free of the toolchain.
func reconcileManifest(
	manifestName string,
	m *deps.Manifest,
	imported map[deps.Dependency]bool,
) error {
	var missing []string
	for dep := range imported {
		if _, ok := m.Replace[dep]; ok {
			continue // a replaced dependency reads from a local path, no floor
		}
		if dep.URL == toolchain.UnobinModulePath {
			return fmt.Errorf(
				"%s is toolchain-versioned and cannot be imported at a dependency"+
					" version; replace it locally:\n"+
					"  in manifest.ub: manifest: { replace: { '%s': '<path-to-unobin>' } }",
				dep.URL, dep.URL)
		}
		if _, ok := m.Requires[dep]; ok {
			continue
		}
		missing = append(missing, dep.String())
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return fmt.Errorf(
			"imported but missing from %s: %s\n"+
				"add a floor with `unobin deps get <repo>@<version>`",
			manifestName, strings.Join(missing, ", "))
	}
	for dep := range m.Requires {
		if !imported[dep] {
			delete(m.Requires, dep)
		}
	}
	return nil
}

func parseGetArg(arg string) (deps.Dependency, string, error) {
	repoPart, query := arg, ""
	if at := strings.LastIndex(arg, "@"); at >= 0 {
		repoPart, query = arg[:at], arg[at+1:]
	}
	dep, err := deps.ParseDependency(repoPart)
	return dep, query, err
}

// resolveAndWrite selects versions across manifest's dependency graph,
// walks the imports to build the lock, and writes both files at root.
func resolveAndWrite(
	cmd *cobra.Command, root string, manifest *deps.Manifest, replaceUnobin string,
) error {
	resolver, err := newDepsResolver(root, replaceUnobin, manifest.Replace)
	if err != nil {
		return err
	}
	selection, err := deps.Resolve(manifest, deps.NewFetcher(resolver))
	if err != nil {
		return err
	}
	lock, err := deps.LockFromImports(os.DirFS(root), selection, resolver, manifest.Replace)
	if err != nil {
		return err
	}
	manifestName, err := writeProjectManifest(root, manifest)
	if err != nil {
		return err
	}
	lock.ToolchainVersion = cliVersion()
	if err := deps.WriteSourceLock(filepath.Join(root, deps.SourceLockFileName), lock); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s (%d direct) and %s (%d locked)\n",
		manifestName, len(manifest.Requires), deps.SourceLockFileName, len(lock.Deps))
	return nil
}

func writeProjectManifest(root string, manifest *deps.Manifest) (string, error) {
	path := filepath.Join(root, deps.ManifestFileName)
	return deps.ManifestFileName, deps.WriteManifest(path, manifest)
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
	root, err := projectRoot(cfg.stackPath)
	if err != nil {
		return err
	}
	resolver, err := newDepsResolver(root, cfg.replaceUnobin, nil)
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

// readProjectLock reads the lock from stackPath's project root, with a
// clear error when it is missing.
func readProjectLock(stackPath string) (*deps.Lock, error) {
	root, rootErr := projectRoot(stackPath)
	if rootErr != nil {
		return nil, rootErr
	}
	lock, err := deps.ReadLock(os.DirFS(root))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("no %s found; run `unobin deps sync` first",
				deps.SourceLockFileName)
		}
		return nil, err
	}
	return lock, nil
}

// runDepsClean removes the cached dependency sources, which are shared
// across projects.
func runDepsClean(cmd *cobra.Command) error {
	resolver, err := resolve.NewRemoteResolver()
	if err != nil {
		return err
	}
	dir, err := resolver.CleanImports()
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Removed the import cache at %s\n", dir)
	return nil
}

func newDepsResolver(
	root, replaceUnobin string, replace map[deps.Dependency]string,
) (resolve.Resolver, error) {
	resolver, err := newCompileResolver(root)
	if err != nil {
		return nil, err
	}
	return compile.WrapReplaces(resolver, root, replaceUnobin, replace)
}
