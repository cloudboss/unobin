package root

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/graphprint"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/spf13/cobra"
)

var (
	printGraphCfg = &printGraphConfig{}
	PrintGraphCmd = &cobra.Command{
		Use:   "print-graph",
		Short: "Print a stack's dependency graph without compiling it",
		Long: `Print a stack's dependency graph from its source.

Imports are resolved in memory; composite call sites are expanded
into their internal sub-nodes the same way the stack binary's
print-graph subcommand does. The output is intended to match what
the compiled binary would emit.

Examples:
  unobin print-graph -p stack.ub
  unobin print-graph -p stack.ub --format dot | dot -Tsvg > graph.svg`,

		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrintGraph(cmd, printGraphCfg)
		},
	}
)

type printGraphConfig struct {
	stackPath     string
	format        string
	replaceUnobin string
}

func init() {
	PrintGraphCmd.Flags().StringVarP(&printGraphCfg.stackPath, "path", "p", "",
		"Path to the stack source.")
	PrintGraphCmd.Flags().StringVar(&printGraphCfg.format, "format", "plain",
		"Output format: 'plain' for an indented text listing, 'dot' for Graphviz.")
	PrintGraphCmd.Flags().StringVar(&printGraphCfg.replaceUnobin, "replace-unobin", "",
		"Local path to substitute for github.com/cloudboss/unobin so the resolver reads from a working tree.")

	_ = PrintGraphCmd.MarkFlagRequired("path")
}

func runPrintGraph(cmd *cobra.Command, cfg *printGraphConfig) error {
	src, err := os.ReadFile(cfg.stackPath)
	if err != nil {
		return err
	}
	f, err := lang.ParseSource(cfg.stackPath, src)
	if err != nil {
		return err
	}
	f.Kind = lang.FileStack
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return errs.Err()
	}

	refs, errs := resolve.ExtractImports(f)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	resolver, err := newCompileResolver(filepath.Dir(cfg.stackPath))
	if err != nil {
		return err
	}
	if cfg.replaceUnobin != "" {
		abs, err := filepath.Abs(cfg.replaceUnobin)
		if err != nil {
			return err
		}
		resolver = &replaceResolver{
			prefix:  "github.com/cloudboss/unobin",
			local:   abs,
			wrapped: resolver,
		}
	}

	mods, err := buildModuleMap(refs, resolver)
	if err != nil {
		return err
	}

	dag := runtime.BuildDAG(f, mods)
	out := cmd.OutOrStdout()
	switch cfg.format {
	case "plain":
		graphprint.Plain(out, dag)
	case "dot":
		graphprint.DOT(out, dag, deriveStackName(cfg.stackPath))
	default:
		return fmt.Errorf("unknown --format %q (want 'plain' or 'dot')", cfg.format)
	}
	return nil
}

// buildModuleMap turns each top-level import alias into a *runtime.Module.
// UB-module Composites are populated from the parsed exports; Go modules
// become empty Module values so the runtime can tell "imported but not a
// composite" apart from "not imported at all". Each composite carries
// its own Modules map so composite-internal lookups stay self-contained.
func buildModuleMap(refs map[string]resolve.ImportRef,
	resolver resolve.Resolver) (map[string]*runtime.Module, error) {
	v := &graphVisitor{byKey: map[string]*runtime.Module{}}
	top, err := resolve.WalkUB(refs, resolver, v)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*runtime.Module, len(top))
	for _, res := range top {
		switch res.Kind {
		case resolve.ResolutionGo:
			out[res.LocalAlias] = &runtime.Module{}
		case resolve.ResolutionUB:
			out[res.LocalAlias] = v.byKey[res.CanonicalKey]
		}
	}
	return out, nil
}

// graphVisitor builds a *runtime.Module per unique UB-module key.
// Go imports contribute nothing to its state because print-graph
// doesn't model their types; the consumer fills in an empty
// *runtime.Module per top-level Go alias.
type graphVisitor struct {
	byKey map[string]*runtime.Module
}

func (g *graphVisitor) OnGoImport(_, _, _ string) error {
	return nil
}

func (g *graphVisitor) OnUBModule(
	_, canonicalKey string, _ resolve.ImportRef, mod *resolve.UBModule,
) error {
	composites := make(map[string]*runtime.CompositeType, len(mod.Bodies))
	for name, body := range mod.Bodies {
		bodyMods := make(map[string]*runtime.Module, len(mod.BodyImports[name]))
		for _, res := range mod.BodyImports[name] {
			switch res.Kind {
			case resolve.ResolutionGo:
				bodyMods[res.LocalAlias] = &runtime.Module{}
			case resolve.ResolutionUB:
				bodyMods[res.LocalAlias] = g.byKey[res.CanonicalKey]
			}
		}
		composites[name] = &runtime.CompositeType{
			Name:    name,
			Body:    body,
			Modules: bodyMods,
		}
	}
	g.byKey[canonicalKey] = &runtime.Module{Composites: composites}
	return nil
}
