package root

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/codegen"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/goschema"
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
	stackPath       string
	version         string
	stackName       string
	libraryPath     string
	outDir          string
	goVersion       string
	unobinVersion   string
	replaceUnobin   string
	replaceGoModule []string
	build           bool
}

func init() {
	CompileCmd.Flags().StringVarP(&compileCfg.stackPath, "path", "p", "main.ub",
		"Path to the factory source.")

	CompileCmd.Flags().StringVar(&compileCfg.version, "version", "v0.0.0",
		"Release version to stamp into the built binary.")

	CompileCmd.Flags().StringVar(&compileCfg.stackName, "name", "",
		"Stack name. Defaults to the parent directory's basename.")

	CompileCmd.Flags().StringVar(&compileCfg.libraryPath, "library-path", "",
		"Library path identity to embed in the binary. The operator's"+
			" config.ub asserts the same value under factory.library-path"+
			" and plan, refresh, and validate refuse on mismatch.")

	CompileCmd.Flags().StringVarP(&compileCfg.outDir, "out", "o", "",
		"Directory to write main.go and go.mod into, or `-` to print main.go to stdout.")

	CompileCmd.Flags().StringVar(&compileCfg.goVersion, "go-version", goMajorMinor(),
		"Go toolchain version to declare in the generated go.mod.")

	CompileCmd.Flags().StringVar(&compileCfg.unobinVersion, "unobin-version", "v0.0.0",
		"Version of github.com/cloudboss/unobin to require in the generated go.mod.")

	CompileCmd.Flags().StringVar(&compileCfg.replaceUnobin, "replace-unobin", "",
		"Local path to substitute for github.com/cloudboss/unobin via a go.mod replace directive.")

	CompileCmd.Flags().StringArrayVar(&compileCfg.replaceGoModule, "replace-go-module", nil,
		"Local replace for a Go module, repeatable. Format: `module-path=local-path`. "+
			"Both the import resolver and the generated go.mod use the substitution.")

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
	extraReplaces, err := parseReplaceFlags(cfg.replaceGoModule)
	if err != nil {
		return err
	}
	for prefix, local := range extraReplaces {
		resolver = &replaceResolver{
			prefix:  prefix,
			local:   local,
			wrapped: resolver,
		}
	}

	v := newCompileVisitor(name, cmd.ErrOrStderr())
	top, err := resolve.WalkUB(refs, resolver, v)
	if err != nil {
		return err
	}

	goImports := make(map[string]string, len(top))
	ubImports := make(map[string]string, len(top))
	goConstraints := make(map[string]map[string][]lang.ConstraintSpec, len(top))
	libs := make(map[string]*ubruntime.Library, len(top))
	for _, res := range top {
		switch res.Kind {
		case resolve.ResolutionGo:
			goImports[res.LocalAlias] = res.Path
			schema, warnings, err := readGoSchema(res.SourcePath)
			if err != nil {
				return fmt.Errorf("import %q: %w", res.LocalAlias, err)
			}
			printSchemaWarnings(cmd.ErrOrStderr(), res.LocalAlias, warnings)
			libs[res.LocalAlias] = &ubruntime.Library{Schema: schema}
			if c := constraintsFromSchema(schema); len(c) > 0 {
				goConstraints[res.LocalAlias] = c
			}
		case resolve.ResolutionUB:
			ubImports[res.LocalAlias] = name + "/internal/" + v.canonicalAlias[res.CanonicalKey]
			libs[res.LocalAlias] = v.runtimeLibraries[res.CanonicalKey]
		}
	}
	if errs := ubruntime.CheckReferences(f, libs); errs.Len() > 0 {
		return errs.Err()
	}
	if errs := ubruntime.CheckLiteralConstraints(f, libs); errs.Len() > 0 {
		return errs.Err()
	}

	in := codegen.Input{
		Body:          string(src),
		LibraryPath:   cfg.libraryPath,
		FactoryName:   name,
		GoImports:     goImports,
		UBImports:     ubImports,
		GoConstraints: goConstraints,
	}

	if cfg.outDir == "-" {
		if len(v.packages) > 0 {
			return errors.New("compile: cannot stream to stdout when UB libraries are imported")
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
	maps.Copy(replaces, extraReplaces)

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
		return runGoBuild(cmd, cfg.outDir, name, cfg.version)
	}
	return nil
}

// compileVisitor accumulates the per-import state compile needs as the
// walker descends the import graph. canonicalAlias maps each UB
// library's dedup key to the local alias of the first site that
// reached it (used as the `internal/<dir>/` package name). packages
// holds the generated Go source per key. importVersions pins each
// Go-library path to its version for the stack's go.mod, and reports a
// conflict when two sites disagree on a version.
type compileVisitor struct {
	stackName        string
	canonicalAlias   map[string]string
	packages         map[string][]byte
	importVersions   map[string]string
	runtimeLibraries map[string]*ubruntime.Library
	warnOut          io.Writer
}

func newCompileVisitor(stackName string, warnOut io.Writer) *compileVisitor {
	return &compileVisitor{
		stackName:        stackName,
		canonicalAlias:   map[string]string{},
		packages:         map[string][]byte{},
		importVersions:   map[string]string{},
		runtimeLibraries: map[string]*ubruntime.Library{},
		warnOut:          warnOut,
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

func (c *compileVisitor) OnUBLibrary(
	alias, canonicalKey string, _ resolve.ImportRef, lib *resolve.UBLibrary,
) error {
	var violations []error
	for _, name := range slices.Sorted(maps.Keys(lib.Bodies)) {
		violations = append(violations,
			resolve.ValidateCompositeBody(lib.Kinds[name], name, lib.Bodies[name])...)
	}
	if len(violations) > 0 {
		return errors.Join(violations...)
	}
	composites := make(map[string]map[string]string, len(lib.BodyImports))
	runtimeLib := &ubruntime.Library{Name: alias}
	for name, body := range lib.Bodies {
		bodyLibs := make(map[string]*ubruntime.Library, len(lib.BodyImports[name]))
		for _, res := range lib.BodyImports[name] {
			switch res.Kind {
			case resolve.ResolutionGo:
				schema, warnings, err := readGoSchema(res.SourcePath)
				if err != nil {
					return fmt.Errorf(
						"composite %q import %q: %w",
						name, res.LocalAlias, err)
				}
				printSchemaWarnings(c.warnOut, res.LocalAlias, warnings)
				bodyLibs[res.LocalAlias] = &ubruntime.Library{Schema: schema}
			case resolve.ResolutionUB:
				bodyLibs[res.LocalAlias] = c.runtimeLibraries[res.CanonicalKey]
			}
		}
		runtimeLib.AddComposite(&ubruntime.CompositeType{
			Name:      name,
			Kind:      ubruntime.NodeKind(lib.Kinds[name]),
			Body:      body,
			Libraries: bodyLibs,
		})
	}
	for name, resols := range lib.BodyImports {
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
	src, err := codegen.GenerateUBLibrary(canonical, lib.Bodies, lib.Kinds, composites)
	if err != nil {
		return err
	}
	c.canonicalAlias[canonicalKey] = canonical
	c.packages[canonicalKey] = src
	c.runtimeLibraries[canonicalKey] = runtimeLib
	return nil
}

func runGoBuild(cmd *cobra.Command, dir, binaryName, version string) error {
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

	revision, err := codegen.ContentRevision(dir)
	if err != nil {
		return err
	}

	ldflags := fmt.Sprintf("-X main.factoryVersion=%s -X main.contentRevision=%s",
		version, revision)
	build := exec.Command(goBin, "build", "-ldflags", ldflags, "-o", binaryName, ".")
	build.Dir = dir
	build.Stdout = cmd.OutOrStdout()
	build.Stderr = cmd.ErrOrStderr()
	if err := build.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Built %s %s (content-revision %s)\n",
		binaryName, version, revision)
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
	return &resolve.Source{Commit: "replace", Path: target, FS: os.DirFS(target)}, nil
}

// constraintsFromSchema flattens a Go library's per-type constraints into
// one map keyed by "<kind>.<type>", the form codegen embeds and the plan
// looks up. Returns nil when no type in the library declares a constraint.
func constraintsFromSchema(schema *ubruntime.LibrarySchema) map[string][]lang.ConstraintSpec {
	if schema == nil {
		return nil
	}
	out := map[string][]lang.ConstraintSpec{}
	add := func(kind ubruntime.NodeKind, types map[string]*ubruntime.TypeSchema) {
		for typ, ts := range types {
			if len(ts.Constraints) > 0 {
				out[string(kind)+"."+typ] = ts.Constraints
			}
		}
	}
	add(ubruntime.NodeResource, schema.Resources)
	add(ubruntime.NodeData, schema.DataSources)
	add(ubruntime.NodeAction, schema.Actions)
	if len(out) == 0 {
		return nil
	}
	return out
}

// readGoSchema reads a fetched Go library's source from sourcePath
// and returns its schema plus any warnings about registered types
// whose sibling Output struct could not be located. A missing path
// returns nil values with no error, which lets fake resolvers in
// tests fall through without having to write a real library to disk.
// Any other failure mode (missing Library() function, parse error,
// malformed source) is propagated so a broken import fails the
// compile.
func readGoSchema(sourcePath string) (*ubruntime.LibrarySchema, []string, error) {
	if sourcePath == "" {
		return nil, nil, nil
	}
	return goschema.Read(sourcePath)
}

// printSchemaWarnings emits each warning string to err prefixed with
// the import alias the schema came from.
func printSchemaWarnings(out io.Writer, alias string, warnings []string) {
	for _, w := range warnings {
		fmt.Fprintf(out, "warning: import %q: %s\n", alias, w)
	}
}

// parseReplaceFlags parses each `--replace-go-module module-path=local-path`
// value into the map fed to both the import resolver and the generated
// go.mod's replace directive. Returns an error on malformed entries
// (missing `=`, empty side, or relative paths -- the substitution must
// be unambiguous in go.mod and on disk).
func parseReplaceFlags(values []string) (map[string]string, error) {
	out := map[string]string{}
	for _, raw := range values {
		idx := strings.IndexByte(raw, '=')
		if idx <= 0 || idx == len(raw)-1 {
			return nil, fmt.Errorf(
				"--replace-go-module %q: expected module-path=local-path", raw)
		}
		mod := raw[:idx]
		path := raw[idx+1:]
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("--replace-go-module %q: %w", raw, err)
		}
		out[mod] = abs
	}
	return out, nil
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
