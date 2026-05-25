package root

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/spf13/cobra"
)

var (
	fetchCfg = &fetchConfig{}
	FetchCmd = &cobra.Command{
		Use:   "fetch",
		Short: "Resolve a stack's imports into the local cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFetch(cmd, fetchCfg)
		},
	}
)

type fetchConfig struct {
	stackPath     string
	replaceUnobin string
}

func init() {
	FetchCmd.Flags().StringVarP(&fetchCfg.stackPath, "path", "p", "stack.ub",
		"Path to the stack source.")
	FetchCmd.Flags().StringVar(&fetchCfg.replaceUnobin, "replace-unobin", "",
		"Local path to substitute for github.com/cloudboss/unobin so the "+
			"resolver reads from a working tree instead of fetching.")
}

func runFetch(cmd *cobra.Command, cfg *fetchConfig) error {
	src, err := os.ReadFile(cfg.stackPath)
	if err != nil {
		return err
	}
	f, err := lang.ParseSource(cfg.stackPath, src)
	if err != nil {
		return err
	}
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return errs.Err()
	}

	var replaceUnobinAbs string
	if cfg.replaceUnobin != "" {
		abs, err := filepath.Abs(cfg.replaceUnobin)
		if err != nil {
			return err
		}
		replaceUnobinAbs = abs
	}

	stackDir := filepath.Dir(cfg.stackPath)
	resolver, err := newCompileResolver(stackDir)
	if err != nil {
		return err
	}
	if replaceUnobinAbs != "" {
		resolver = &replaceResolver{
			prefix:  "github.com/cloudboss/unobin",
			local:   replaceUnobinAbs,
			wrapped: resolver,
		}
	}

	if _, errs := resolve.ResolveAll(cfg.stackPath, f, resolver); len(errs) > 0 {
		return errors.Join(errs...)
	}

	refs, _ := resolve.ExtractImports(f)
	out := cmd.OutOrStdout()
	if len(refs) == 0 {
		fmt.Fprintln(out, "No imports to resolve.")
		return nil
	}
	for _, alias := range sortedImportAliases(refs) {
		fmt.Fprintf(out, "  %s -> %s\n", alias, importRefString(refs[alias]))
	}
	return nil
}

func sortedImportAliases(refs map[string]resolve.ImportRef) []string {
	out := make([]string, 0, len(refs))
	for k := range refs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func importRefString(ref resolve.ImportRef) string {
	switch r := ref.(type) {
	case *resolve.LocalImport:
		return r.Path + " (local)"
	case *resolve.RemoteImport:
		s := r.URL
		if r.Subdir != "" {
			s += "//" + r.Subdir
		}
		return s + "@" + r.Version + " (remote)"
	}
	return "?"
}
