package root

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
	depsGetCmd.Flags().StringVarP(&depsGetCfg.stackPath, "path", "p", "main.ub", depsPathHelp)
	depsGetCmd.Flags().StringVar(&depsGetCfg.replaceUnobin, "replace-unobin", "", depsReplaceHelp)
	DepsCmd.AddCommand(depsSyncCmd, depsListCmd, depsVerifyCmd, depsCleanCmd, depsGetCmd)
}

// projectRoot resolves the project root from a --path value. The path
// normally names the factory source file, whose directory is the root,
// but a directory works too: `unobin deps list -p mydir` and `-p mydir/`
// both mean the project in mydir. Without this, filepath.Dir("mydir")
// would treat mydir as a filename and return ".".
func projectRoot(stackPath string) string {
	if info, err := os.Stat(stackPath); err == nil && info.IsDir() {
		return stackPath
	}
	return filepath.Dir(stackPath)
}

// runDepsSync reconciles unobin.manifest and unobin.lock with the
// project's imports. The manifest holds the floors; sync reads it,
// requires a floor for every imported repository, removes floors for
// repositories no longer imported, then selects versions across the
// dependency graph, walks the imports to pin every remote library, and
// writes both files at the project root.
func runDepsSync(cmd *cobra.Command, cfg *depsSyncConfig) error {
	root := projectRoot(cfg.stackPath)
	manifest, err := readManifestOrEmpty(root)
	if err != nil {
		return err
	}
	imported, err := deps.ImportedRepos(root)
	if err != nil {
		return err
	}
	if err := reconcileManifest(manifest, imported); err != nil {
		return err
	}
	return resolveAndWrite(cmd, root, manifest, cfg.replaceUnobin)
}

// runDepsGet resolves a version for one dependency, sets its floor in the
// manifest, and re-pins. The query may be empty or "latest" (the highest
// tag), an exact version, or a partial one (v1, v1.2).
func runDepsGet(cmd *cobra.Command, cfg *depsSyncConfig, arg string) error {
	root := projectRoot(cfg.stackPath)
	dep, query, err := parseGetArg(arg)
	if err != nil {
		return err
	}
	if dep.URL == toolchain.UnobinModulePath {
		return fmt.Errorf(
			"%s is toolchain-versioned; pin it with the manifest's unobin line",
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
	manifest, err := readManifestOrEmpty(root)
	if err != nil {
		return err
	}
	manifest.Requires[dep] = version
	fmt.Fprintf(cmd.ErrOrStderr(), "Using %s %s\n", dep, version)
	return resolveAndWrite(cmd, root, manifest, cfg.replaceUnobin)
}

// readManifestOrEmpty reads unobin.manifest from root, returning an empty
// manifest when the file does not exist yet. There is no `deps init`: the
// manifest is created the first time get or sync writes it.
func readManifestOrEmpty(root string) (*deps.Manifest, error) {
	manifest, err := deps.ReadManifest(os.DirFS(root))
	if errors.Is(err, fs.ErrNotExist) {
		return &deps.Manifest{Requires: map[deps.Dependency]string{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

// reconcileManifest makes the manifest's floors match the set of imported
// repositories. An imported repository with no floor is an error that
// points the author at `deps get`; a floor whose repository is no longer
// imported is removed. The unobin repository takes no floor at all: an
// import from it must be served by a replace, since its source version
// may not float free of the toolchain.
func reconcileManifest(m *deps.Manifest, imported map[deps.Dependency]bool) error {
	var missing []string
	for dep := range imported {
		if _, ok := m.Replace[dep]; ok {
			continue // a replaced dependency reads from a local path, no floor
		}
		if dep.URL == toolchain.UnobinModulePath {
			return fmt.Errorf(
				"%s is toolchain-versioned and cannot be imported at a dependency"+
					" version; replace it locally:\n"+
					"  in unobin.manifest: replace: { '%s': '<path-to-unobin>' }",
				dep.URL, dep.URL)
		}
		if _, ok := m.Requires[dep]; ok {
			continue
		}
		missing = append(missing, dep.String())
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf(
			"imported but missing from %s: %s\n"+
				"add a floor with `unobin deps get <repo>@<version>`",
			deps.ManifestFileName, strings.Join(missing, ", "))
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
	resolver, err := newDepsResolver(projectRoot(cfg.stackPath), cfg.replaceUnobin, nil)
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
	lock, err := deps.ReadLock(os.DirFS(projectRoot(stackPath)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("no %s found; run `unobin deps sync` first", deps.LockFileName)
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
