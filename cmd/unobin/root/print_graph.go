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

// buildModuleMap turns each top-level import alias into a *runtime.Module
// whose Composites map (for UB modules) is populated from the parsed
// exports. Go modules become empty Module values so the runtime's
// composite check distinguishes "imported but not a composite" from
// "not imported at all". Each composite carries its own Modules map so
// composite-internal lookups stay self-contained.
func buildModuleMap(refs map[string]resolve.ImportRef,
	resolver resolve.Resolver) (map[string]*runtime.Module, error) {
	b := &graphModBuilder{
		resolver:   resolver,
		byKey:      map[string]*runtime.Module{},
		inProgress: map[string]bool{},
	}
	out := make(map[string]*runtime.Module, len(refs))
	for _, alias := range sortedRefAliases(refs) {
		mod, err := b.resolve(refs[alias])
		if err != nil {
			return nil, fmt.Errorf("import %q: %w", alias, err)
		}
		out[alias] = mod
	}
	return out, nil
}

type graphModBuilder struct {
	resolver   resolve.Resolver
	byKey      map[string]*runtime.Module
	inProgress map[string]bool
}

func (b *graphModBuilder) resolve(ref resolve.ImportRef) (*runtime.Module, error) {
	source, err := b.resolver.Resolve(ref)
	if err != nil {
		return nil, err
	}
	if !resolve.IsUBModule(source) {
		return &runtime.Module{}, nil
	}
	return b.buildUB(ref, source)
}

func (b *graphModBuilder) buildUB(ref resolve.ImportRef,
	source *resolve.Source) (*runtime.Module, error) {
	key := ubKey(ref)
	if mod, ok := b.byKey[key]; ok {
		return mod, nil
	}
	if b.inProgress[key] {
		return nil, fmt.Errorf("import cycle through %s", key)
	}
	b.inProgress[key] = true
	defer delete(b.inProgress, key)

	manifestBytes, err := readSourceFile(source, "module.ub")
	if err != nil {
		return nil, fmt.Errorf("read module.ub: %w", err)
	}
	manifest, err := lang.ParseSource("module.ub", manifestBytes)
	if err != nil {
		return nil, err
	}
	manifest.Kind = lang.FileModule
	if errs := lang.ValidateFile(manifest); errs.Len() > 0 {
		return nil, errs.Err()
	}

	exports, err := readManifestExports(manifest)
	if err != nil {
		return nil, err
	}

	composites := make(map[string]*runtime.CompositeType, len(exports))
	for name, path := range exports {
		body, err := readSourceFile(source, path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		bf, err := lang.ParseSource(path, body)
		if err != nil {
			return nil, err
		}
		bf.Kind = lang.FileExportedType
		if errs := lang.ValidateFile(bf); errs.Len() > 0 {
			return nil, errs.Err()
		}

		bodyImports, importErrs := resolve.ExtractImports(bf)
		if len(importErrs) > 0 {
			return nil, errors.Join(importErrs...)
		}
		bodyMods, err := b.bodyMods(bodyImports)
		if err != nil {
			return nil, fmt.Errorf("composite %q: %w", name, err)
		}
		composites[name] = &runtime.CompositeType{
			Name:    name,
			Body:    bf,
			Modules: bodyMods,
		}
	}

	mod := &runtime.Module{Composites: composites}
	b.byKey[key] = mod
	return mod, nil
}

func (b *graphModBuilder) bodyMods(refs map[string]resolve.ImportRef) (
	map[string]*runtime.Module, error) {
	out := make(map[string]*runtime.Module, len(refs))
	for _, alias := range sortedRefAliases(refs) {
		mod, err := b.resolve(refs[alias])
		if err != nil {
			return nil, fmt.Errorf("import %q: %w", alias, err)
		}
		out[alias] = mod
	}
	return out, nil
}
