package root

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/cloudboss/unobin/pkg/codegen"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/resolve"
	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
	"github.com/spf13/cobra"
)

var (
	compileCfg = &compileConfig{}
	CompileCmd = &cobra.Command{
		Use:   "compile",
		Short: "Generate a stack binary's main.go from stack source",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompile(cmd, compileCfg)
		},
	}
)

type compileConfig struct {
	stackPath     string
	version       string
	commit        string
	stackName     string
	modulePath    string
	outDir        string
	goVersion     string
	unobinVersion string
	replaceUnobin string
	build         bool
}

func init() {
	CompileCmd.Flags().StringVarP(&compileCfg.stackPath, "path", "p", "stack.ub",
		"Path to the stack source.")

	CompileCmd.Flags().StringVar(&compileCfg.version, "version", "dev",
		"Stack version to embed in the generated binary.")

	CompileCmd.Flags().StringVar(&compileCfg.commit, "commit", "",
		"Git commit to embed in the generated binary.")

	CompileCmd.Flags().StringVar(&compileCfg.stackName, "name", "",
		"Stack name. Defaults to the parent directory's basename.")

	CompileCmd.Flags().StringVar(&compileCfg.modulePath, "module-path", "",
		"Module-path identity to embed in the binary. The operator's"+
			" config.ub asserts the same value under stack.module-path"+
			" and plan, refresh, and validate refuse on mismatch.")

	CompileCmd.Flags().StringVarP(&compileCfg.outDir, "out", "o", "",
		"Directory to write main.go and go.mod into, or `-` to print main.go to stdout.")

	CompileCmd.Flags().StringVar(&compileCfg.goVersion, "go-version", goMajorMinor(),
		"Go toolchain version to declare in the generated go.mod.")

	CompileCmd.Flags().StringVar(&compileCfg.unobinVersion, "unobin-version", "v0.0.0",
		"Version of github.com/cloudboss/unobin to require in the generated go.mod.")

	CompileCmd.Flags().StringVar(&compileCfg.replaceUnobin, "replace-unobin", "",
		"Local path to substitute for github.com/cloudboss/unobin via a go.mod replace directive.")

	CompileCmd.Flags().BoolVar(&compileCfg.build, "build", false,
		"After writing the source, run `go build` in the output directory.")
}

func runCompile(cmd *cobra.Command, cfg *compileConfig) error {
	if cfg.outDir == "" {
		return errors.New("--out is required (use `-` for stdout)")
	}
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

	refs, errs := resolve.ExtractImports(f)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	name := cfg.stackName
	if name == "" {
		name = deriveStackName(cfg.stackPath)
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

	v := newCompileVisitor(name)
	top, err := resolve.WalkUB(refs, resolver, v)
	if err != nil {
		return err
	}

	goImports := make(map[string]string, len(top))
	ubImports := make(map[string]string, len(top))
	mods := make(map[string]*ubruntime.Module, len(top))
	for _, res := range top {
		switch res.Kind {
		case resolve.ResolutionGo:
			goImports[res.LocalAlias] = res.Path
			mods[res.LocalAlias] = &ubruntime.Module{}
		case resolve.ResolutionUB:
			ubImports[res.LocalAlias] = name + "/internal/" + v.canonicalAlias[res.CanonicalKey]
			mods[res.LocalAlias] = v.runtimeModules[res.CanonicalKey]
		}
	}
	if errs := ubruntime.CheckReferences(f, mods); errs.Len() > 0 {
		return errs.Err()
	}

	in := codegen.Input{
		Body:       string(src),
		ModulePath: cfg.modulePath,
		StackName:  name,
		Version:    cfg.version,
		Commit:     cfg.commit,
		GoImports:  goImports,
		UBImports:  ubImports,
	}

	if cfg.outDir == "-" {
		if len(v.packages) > 0 {
			return errors.New("compile: cannot stream to stdout when UB modules are imported")
		}
		out, err := codegen.Generate(in)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(out)
		return err
	}

	replaces := codegen.Replaces{}
	if replaceUnobinAbs != "" {
		replaces["github.com/cloudboss/unobin"] = replaceUnobinAbs
	}

	err = codegen.WriteSource(cfg.outDir, in,
		cfg.goVersion, cfg.unobinVersion, v.importVersions, replaces)
	if err != nil {
		return err
	}
	for key, pkgBytes := range v.packages {
		canonical := v.canonicalAlias[key]
		pkgDir := filepath.Join(cfg.outDir, "internal", canonical)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(pkgDir, canonical+".go"), pkgBytes, 0o644); err != nil {
			return err
		}
	}
	if cfg.build {
		return runGoBuild(cmd, cfg.outDir, name)
	}
	return nil
}

// compileVisitor accumulates the per-import state compile needs as the
// walker descends the import graph. canonicalAlias maps each UB
// module's dedup key to the local alias of the first site that
// reached it (used as the `internal/<dir>/` package name). packages
// holds the generated Go source per key. importVersions pins each
// Go-module path to its version for the stack's go.mod, and reports a
// conflict when two sites disagree on a version.
type compileVisitor struct {
	stackName      string
	canonicalAlias map[string]string
	packages       map[string][]byte
	importVersions map[string]string
	runtimeModules map[string]*ubruntime.Module
}

func newCompileVisitor(stackName string) *compileVisitor {
	return &compileVisitor{
		stackName:      stackName,
		canonicalAlias: map[string]string{},
		packages:       map[string][]byte{},
		importVersions: map[string]string{},
		runtimeModules: map[string]*ubruntime.Module{},
	}
}

func (c *compileVisitor) OnGoImport(_, path, version string) error {
	if existing, ok := c.importVersions[path]; ok && existing != version {
		return fmt.Errorf("conflicting versions for %s: %s vs %s",
			path, existing, version)
	}
	c.importVersions[path] = version
	return nil
}

func (c *compileVisitor) OnUBModule(
	alias, canonicalKey string, _ resolve.ImportRef, mod *resolve.UBModule,
) error {
	composites := make(map[string]map[string]string, len(mod.BodyImports))
	runtimeComposites := make(map[string]*ubruntime.CompositeType, len(mod.Bodies))
	for name, body := range mod.Bodies {
		bodyMods := make(map[string]*ubruntime.Module, len(mod.BodyImports[name]))
		for _, res := range mod.BodyImports[name] {
			switch res.Kind {
			case resolve.ResolutionGo:
				bodyMods[res.LocalAlias] = &ubruntime.Module{}
			case resolve.ResolutionUB:
				bodyMods[res.LocalAlias] = c.runtimeModules[res.CanonicalKey]
			}
		}
		runtimeComposites[name] = &ubruntime.CompositeType{
			Name:    name,
			Body:    body,
			Modules: bodyMods,
		}
	}
	for name, resols := range mod.BodyImports {
		composite := make(map[string]string, len(resols))
		for _, res := range resols {
			switch res.Kind {
			case resolve.ResolutionGo:
				composite[res.LocalAlias] = res.Path
			case resolve.ResolutionUB:
				composite[res.LocalAlias] = c.stackName +
					"/internal/" + c.canonicalAlias[res.CanonicalKey]
			}
		}
		if len(composite) > 0 {
			composites[name] = composite
		}
	}
	canonical := alias
	src, err := codegen.GenerateUBModule(canonical, mod.Manifest, mod.Bodies, composites)
	if err != nil {
		return err
	}
	c.canonicalAlias[canonicalKey] = canonical
	c.packages[canonicalKey] = src
	c.runtimeModules[canonicalKey] = &ubruntime.Module{
		Name:       alias,
		Composites: runtimeComposites,
	}
	return nil
}

func runGoBuild(cmd *cobra.Command, dir, binaryName string) error {
	goBin, err := deps.Ensure(deps.Go)
	if err != nil {
		return err
	}

	tidy := exec.Command(goBin, "mod", "tidy")
	tidy.Dir = dir
	tidy.Stdout = cmd.OutOrStdout()
	tidy.Stderr = cmd.ErrOrStderr()
	if err := tidy.Run(); err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}

	build := exec.Command(goBin, "build", "-o", binaryName, ".")
	build.Dir = dir
	build.Stdout = cmd.OutOrStdout()
	build.Stderr = cmd.ErrOrStderr()
	if err := build.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}
	return nil
}

// newCompileResolver returns the resolver compile uses to fetch
// import sources. Production wires up a local resolver for relative
// paths and a remote resolver for everything else; tests override
// this package var to avoid any network access.
var newCompileResolver = func(stackDir string) (resolve.Resolver, error) {
	remote, err := resolve.NewRemoteResolver()
	if err != nil {
		return nil, err
	}
	return &dispatchResolver{
		local:  resolve.NewLocalResolver(stackDir),
		remote: remote,
	}, nil
}

type dispatchResolver struct {
	local  *resolve.LocalResolver
	remote *resolve.RemoteResolver
}

func (r *dispatchResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	switch ref.(type) {
	case *resolve.LocalImport:
		return r.local.Resolve(ref)
	case *resolve.RemoteImport:
		return r.remote.Resolve(ref)
	}
	return nil, fmt.Errorf("unsupported import ref type %T", ref)
}

// replaceResolver short-circuits remote imports whose URL matches a
// configured prefix and serves them from a local directory instead.
// Set up by `--replace-unobin` so a developer can compile a stack that
// imports `github.com/cloudboss/unobin//<subdir>` against a working
// tree without making any network calls.
type replaceResolver struct {
	prefix  string
	local   string
	wrapped resolve.Resolver
}

func (r *replaceResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	ri, ok := ref.(*resolve.RemoteImport)
	if !ok || ri.URL != r.prefix {
		return r.wrapped.Resolve(ref)
	}
	target := r.local
	if ri.Subdir != "" {
		target = filepath.Join(target, ri.Subdir)
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("replace %s: %w", r.prefix, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("replace %s: %s is not a directory", r.prefix, target)
	}
	src := &resolve.Source{Commit: "replace"}
	if _, err := os.Stat(filepath.Join(target, "module.ub")); err == nil {
		src.FS = os.DirFS(target)
	}
	return src, nil
}

// goMajorMinor returns the running Go toolchain's `<major>.<minor>` so
// the generated go.mod's `go` directive matches the current toolchain.
func goMajorMinor() string {
	v := strings.TrimPrefix(goruntime.Version(), "go")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

func deriveStackName(stackPath string) string {
	abs, err := filepath.Abs(stackPath)
	if err != nil {
		return "stack"
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(dir)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "stack"
	}
	return strings.ToLower(base)
}
