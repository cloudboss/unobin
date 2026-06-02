package root

import (
	"errors"
	"fmt"
	"io"
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
  unobin print-graph -p main.ub
  unobin print-graph -p main.ub --format dot | dot -Tsvg > graph.svg`,

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
		"Local path to substitute for github.com/cloudboss/unobin so the "+
			"resolver reads from a working tree.")

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
	f.Kind = lang.FileFactory
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

	repoVersions, err := lockedVersions(filepath.Dir(cfg.stackPath))
	if err != nil {
		return err
	}
	libs, err := buildLibraryMap(refs, resolver, repoVersions, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	if errs := runtime.CheckReferences(f, libs); errs.Len() > 0 {
		return errs.Err()
	}

	dag := runtime.BuildDAG(f, libs)
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

// buildLibraryMap turns each top-level import alias into a *runtime.Library.
// UB-library Composites are populated from the library's kind-prefixed
// body files; Go libraries become empty Library values so the runtime can
// tell "imported but not a composite" apart from "not imported at all". Each
// composite carries its own Libraries map so composite-internal lookups stay
// self-contained.
func buildLibraryMap(refs map[string]resolve.ImportRef, resolver resolve.Resolver,
	versions map[string]string, warnOut io.Writer) (map[string]*runtime.Library, error) {
	v := &graphVisitor{byKey: map[string]*runtime.Library{}, warnOut: warnOut}
	top, err := resolve.WalkUB(refs, resolver, v, versions)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*runtime.Library, len(top))
	for _, res := range top {
		switch res.Kind {
		case resolve.ResolutionGo:
			schema, warnings, err := readGoSchema(res.SourcePath)
			if err != nil {
				return nil, fmt.Errorf("import %q: %w", res.LocalAlias, err)
			}
			printSchemaWarnings(warnOut, res.LocalAlias, warnings)
			out[res.LocalAlias] = &runtime.Library{Schema: schema}
		case resolve.ResolutionUB:
			out[res.LocalAlias] = v.byKey[res.CanonicalKey]
		}
	}
	return out, nil
}

// graphVisitor builds a *runtime.Library per unique UB-library key.
// Go imports contribute nothing to its state because print-graph
// doesn't model their types; the consumer fills in an empty
// *runtime.Library per top-level Go alias.
type graphVisitor struct {
	byKey   map[string]*runtime.Library
	warnOut io.Writer
}

func (g *graphVisitor) OnGoImport(_, _, _ string) error {
	return nil
}

func (g *graphVisitor) OnUBLibrary(
	_, canonicalKey string, _ resolve.ImportRef, lib *resolve.UBLibrary,
) error {
	runtimeLib := &runtime.Library{}
	for name, body := range lib.Bodies {
		bodyLibs := make(map[string]*runtime.Library, len(lib.BodyImports[name]))
		for _, res := range lib.BodyImports[name] {
			switch res.Kind {
			case resolve.ResolutionGo:
				schema, warnings, err := readGoSchema(res.SourcePath)
				if err != nil {
					return fmt.Errorf(
						"composite %q import %q: %w",
						name, res.LocalAlias, err)
				}
				printSchemaWarnings(g.warnOut, res.LocalAlias, warnings)
				bodyLibs[res.LocalAlias] = &runtime.Library{Schema: schema}
			case resolve.ResolutionUB:
				bodyLibs[res.LocalAlias] = g.byKey[res.CanonicalKey]
			}
		}
		runtimeLib.AddComposite(&runtime.CompositeType{
			Name:      name,
			Kind:      runtime.NodeKind(lib.Kinds[name]),
			Body:      body,
			Libraries: bodyLibs,
		})
	}
	g.byKey[canonicalKey] = runtimeLib
	return nil
}
