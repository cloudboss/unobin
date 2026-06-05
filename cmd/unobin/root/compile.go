package root

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	"github.com/cloudboss/unobin/pkg/toolchain"
	"github.com/spf13/cobra"
)

// unobinModulePath is the unobin module's path, the one requirement
// every generated go.mod pins to the compiling CLI's version.
const unobinModulePath = "github.com/cloudboss/unobin"

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
	replaceMap, err := manifestReplace(stackDir)
	if err != nil {
		return err
	}
	if replaceUnobinAbs == "" {
		if local, ok := replaceMap[deps.Dependency{URL: unobinModulePath}]; ok {
			abs, err := absReplacePath(stackDir, local)
			if err != nil {
				return err
			}
			replaceUnobinAbs = abs
		}
	}

	// The generated go.mod requires unobin at this CLI's own version, so
	// the runtime a factory links is the one its compile checks ran
	// with. A development build has no version to pin; the replace is
	// what supplies the runtime then, and the requirement stays at the
	// placeholder the replace serves.
	unobinVersion := cliVersion()
	if unobinVersion == "dev" {
		if replaceUnobinAbs == "" {
			return errors.New(
				"this unobin is a development build with no version to pin; compile with\n" +
					"  --replace-unobin <path-to-unobin-source>\n" +
					"or add to unobin.manifest:\n" +
					"  replace: { '" + unobinModulePath + "': '<path-to-unobin-source>' }")
		}
		unobinVersion = replacedVersion
	}

	resolver, err := newCompileResolver(stackDir)
	if err != nil {
		return err
	}
	if replaceUnobinAbs != "" {
		resolver = &replaceResolver{
			prefix:  unobinModulePath,
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
	resolver, err = wrapReplaces(resolver, stackDir, "", replaceMap)
	if err != nil {
		return err
	}

	repoVersions, err := lockedVersions(stackDir)
	if err != nil {
		return err
	}
	repoVersions = withReplacedVersions(repoVersions, replaceMap)
	v := newCompileVisitor(name, cmd.ErrOrStderr())
	top, err := resolve.WalkUB(refs, resolver, v, repoVersions)
	if err != nil {
		return err
	}

	goImports := make(map[string]string, len(top))
	ubImports := make(map[string]string, len(top))
	goConstraints := make(map[string]map[string][]lang.ConstraintSpec, len(top))
	goDefaults := make(map[string]map[string][]lang.DefaultSpec, len(top))
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
			if d := defaultsFromSchema(schema); len(d) > 0 {
				goDefaults[res.LocalAlias] = d
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
		GoDefaults:    goDefaults,
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
		replaces[unobinModulePath] = replaceUnobinAbs
	}
	maps.Copy(replaces, extraReplaces)
	if err := addManifestReplaces(replaces, stackDir, replaceMap, v.importVersions); err != nil {
		return err
	}

	err = codegen.WriteSource(cfg.outDir, in,
		cfg.goVersion, unobinVersion, v.importVersions, replaces)
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
// Go-library path to its version for the stack's go.mod; the lock
// already gives every site of a path the same version.
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
	goBin, err := toolchain.Ensure(cmd.ErrOrStderr())
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

// lockedVersions reads unobin.lock from dir and returns each repository's
// selected version, or nil when no lock is present, in which case the walk
// uses the version on each import string.
func lockedVersions(dir string) (map[string]string, error) {
	lock, err := deps.ReadLock(os.DirFS(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return lock.RepoVersions()
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

// replacedVersion is the placeholder version a replaced dependency carries
// in the generated go.mod; the replace directive serves it from a local
// path, so the version is never used to fetch anything.
const replacedVersion = "v0.0.0"

// manifestReplace reads the replace block from the project's
// unobin.manifest, returning nil when there is no manifest.
func manifestReplace(dir string) (map[deps.Dependency]string, error) {
	m, err := deps.ReadManifest(os.DirFS(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return m.Replace, nil
}

// withReplacedVersions gives each replaced repository a placeholder version
// so the walk treats its import as pinned; the replace resolver serves it
// locally regardless.
func withReplacedVersions(
	versions map[string]string, replace map[deps.Dependency]string,
) map[string]string {
	if len(replace) == 0 {
		return versions
	}
	if versions == nil {
		versions = map[string]string{}
	}
	for dep := range replace {
		versions[dep.URL] = replacedVersion
	}
	return versions
}

// addManifestReplaces records a go.mod replace for every replaced
// repository that resolved to a Go library, pointing its module at the
// local path. UB libraries are compiled in, so they need no go.mod entry.
func addManifestReplaces(
	replaces codegen.Replaces, root string,
	replace map[deps.Dependency]string, importVersions map[string]string,
) error {
	for dep, path := range replace {
		if !goModuleImported(dep.URL, importVersions) {
			continue
		}
		abs, err := absReplacePath(root, path)
		if err != nil {
			return err
		}
		replaces[dep.URL] = abs
	}
	return nil
}

// goModuleImported reports whether url, or a package under it, appears as a
// Go import path the generated go.mod requires.
func goModuleImported(url string, importVersions map[string]string) bool {
	for path := range importVersions {
		if path == url || strings.HasPrefix(path, url+"/") {
			return true
		}
	}
	return false
}

// absReplacePath resolves a replace target to an absolute path; a relative
// path is taken relative to root (the project root).
func absReplacePath(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return filepath.Abs(path)
}

// constraintsFromSchema flattens a Go library's per-type constraints into
// one map keyed by "<kind>.<type>", the form codegen embeds and the plan
// looks up. Returns nil when no type in the library declares a constraint.
func constraintsFromSchema(schema *ubruntime.LibrarySchema) map[string][]lang.ConstraintSpec {
	return typeSpecsFromSchema(schema, func(ts *ubruntime.TypeSchema) []lang.ConstraintSpec {
		return ts.Constraints
	})
}

// defaultsFromSchema flattens a Go library's per-type declared defaults
// the same way constraintsFromSchema flattens constraints.
func defaultsFromSchema(schema *ubruntime.LibrarySchema) map[string][]lang.DefaultSpec {
	return typeSpecsFromSchema(schema, func(ts *ubruntime.TypeSchema) []lang.DefaultSpec {
		return ts.Defaults
	})
}

// typeSpecsFromSchema flattens one kind of per-type spec into a map
// keyed by "<kind>.<type>". Returns nil when no type declares any.
func typeSpecsFromSchema[T any](
	schema *ubruntime.LibrarySchema, pick func(*ubruntime.TypeSchema) []T,
) map[string][]T {
	if schema == nil {
		return nil
	}
	out := map[string][]T{}
	add := func(kind ubruntime.NodeKind, types map[string]*ubruntime.TypeSchema) {
		for typ, ts := range types {
			if specs := pick(ts); len(specs) > 0 {
				out[string(kind)+"."+typ] = specs
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
