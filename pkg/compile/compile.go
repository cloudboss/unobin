// Package compile turns factory source into a buildable Go module: it
// parses and validates the stack, resolves its imports, reads each Go
// library's schema, runs the compile-time checks, generates main.go
// and one package per UB library, and optionally runs `go build`. The
// CLI's compile command is a thin flag layer over Run.
package compile

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

	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/codegen"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/resolve"
	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/toolchain"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// Options configures one compile run.
type Options struct {
	// StackPath is the factory source to compile.
	StackPath string
	// OutDir receives main.go, go.mod, and the generated UB-library
	// packages; `-` streams main.go to Stdout instead.
	OutDir string
	// StackName overrides the stack name; empty derives it from the
	// stack file's parent directory.
	StackName string
	// LibraryPath is the library-path identity to embed in the binary.
	LibraryPath string
	// GoVersion is the toolchain version the generated go.mod declares.
	GoVersion string
	// Version is the release version stamped into the built binary.
	Version string
	// CLIVersion is the compiling CLI's own version; the generated
	// go.mod pins unobin to it so the factory links the runtime its
	// compile checks ran with. "dev" requires a replace.
	CLIVersion string
	// ReplaceUnobin substitutes a local path for the unobin repository.
	ReplaceUnobin string
	// ReplaceGoModules maps a Go module path to the local path that
	// serves it, for both the import resolver and the generated go.mod.
	ReplaceGoModules map[string]string
	// Build runs `go build` in OutDir after writing the source.
	Build bool
	// NewResolver constructs the import resolver for a stack directory;
	// nil uses NewProjectResolver.
	NewResolver func(stackDir string) (resolve.Resolver, error)
	// Stdout and Stderr receive the run's output; nil defaults to the
	// process streams.
	Stdout io.Writer
	Stderr io.Writer
	// TypeObserver, when set, receives every expression the stack's
	// type checks infer, with its type. The residual-Unknown harness
	// uses it; nil compiles without recording.
	TypeObserver func(e lang.Expr, t typecheck.Type)
}

func (o Options) stdout() io.Writer {
	if o.Stdout != nil {
		return o.Stdout
	}
	return os.Stdout
}

func (o Options) stderr() io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

// Run compiles a factory per the options.
func Run(opts Options) error {
	if opts.OutDir == "" {
		return errors.New("--out is required (use `-` for stdout)")
	}
	src, err := os.ReadFile(opts.StackPath)
	if err != nil {
		return err
	}
	f, err := lang.ParseSource(opts.StackPath, src)
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

	name := opts.StackName
	if name == "" {
		name = DeriveStackName(opts.StackPath)
	}

	var replaceUnobinAbs string
	if opts.ReplaceUnobin != "" {
		abs, err := filepath.Abs(opts.ReplaceUnobin)
		if err != nil {
			return err
		}
		replaceUnobinAbs = abs
	}

	stackDir := filepath.Dir(opts.StackPath)
	manifest, err := projectManifest(stackDir)
	if err != nil {
		return err
	}
	var replaceMap map[deps.Dependency]string
	if manifest != nil {
		replaceMap = manifest.Replace
	}
	if replaceUnobinAbs == "" {
		if local, ok := replaceMap[deps.Dependency{URL: toolchain.UnobinModulePath}]; ok {
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
	unobinVersion := opts.CLIVersion
	if unobinVersion == "dev" {
		if replaceUnobinAbs == "" {
			return errors.New(
				"this unobin is a development build with no version to pin; compile with\n" +
					"  --replace-unobin <path-to-unobin-source>\n" +
					"or add to unobin.manifest:\n" +
					"  replace: { '" + toolchain.UnobinModulePath + "': '<path-to-unobin-source>' }")
		}
		unobinVersion = replacedVersion
	}

	// The manifest's unobin-version line pins which CLI compiles the
	// project. A replaced unobin runs the replacement no matter what
	// the line says, so it proceeds with a notice instead.
	if manifest != nil && manifest.UnobinVersion != "" {
		if replaceUnobinAbs != "" {
			fmt.Fprintf(opts.stderr(),
				"notice: unobin.manifest pins unobin %s; the replacement at %s runs instead\n",
				manifest.UnobinVersion, replaceUnobinAbs)
		} else if manifest.UnobinVersion != unobinVersion {
			return fmt.Errorf(
				"this project pins unobin %s but this CLI is %s; install unobin %s",
				manifest.UnobinVersion, unobinVersion, manifest.UnobinVersion)
		}
	}

	schemas := NewSchemaCache(UnobinSchemaRoots(opts.stderr(), replaceUnobinAbs, unobinVersion)...)

	newResolver := opts.NewResolver
	if newResolver == nil {
		newResolver = NewProjectResolver
	}
	resolver, err := newResolver(stackDir)
	if err != nil {
		return err
	}
	// The guard sits under every replace layer, so a replaced import
	// never reaches it and an unreplaced one is refused.
	resolver = &unobinImportGuard{wrapped: resolver}
	if replaceUnobinAbs != "" {
		resolver = &replaceResolver{
			prefix:  toolchain.UnobinModulePath,
			local:   replaceUnobinAbs,
			wrapped: resolver,
		}
	}
	for prefix, local := range opts.ReplaceGoModules {
		resolver = &replaceResolver{
			prefix:  prefix,
			local:   local,
			wrapped: resolver,
		}
	}
	resolver, err = WrapReplaces(resolver, stackDir, "", replaceMap)
	if err != nil {
		return err
	}

	repoVersions, err := LockedVersions(stackDir)
	if err != nil {
		return err
	}
	repoVersions = withReplacedVersions(repoVersions, replaceMap)
	v := newCompileVisitor(name, opts.stderr(), schemas)
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
			schema, warnings, err := schemas.Read(res.SourcePath)
			if err != nil {
				return fmt.Errorf("import %q: %w", res.LocalAlias, err)
			}
			PrintSchemaWarnings(opts.stderr(), res.LocalAlias, warnings)
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
	// Embed only the specs for types the factory declares; a node hits
	// the rules and defaults for its own type alone, so an imported
	// library's other types are dead weight in the generated code.
	used := usedLibraryTypes(f)
	pruneUnusedSpecs(goConstraints, used)
	pruneUnusedSpecs(goDefaults, used)
	checker := check.New(f, libs)
	if errs := checker.References(opts.TypeObserver); errs.Len() > 0 {
		return errs.Err()
	}
	if errs := checker.LiteralConstraints(); errs.Len() > 0 {
		return errs.Err()
	}
	if errs := checker.ForEachNesting(); errs.Len() > 0 {
		return errs.Err()
	}

	in := codegen.Input{
		Body:          string(src),
		LibraryPath:   opts.LibraryPath,
		FactoryName:   name,
		GoImports:     goImports,
		UBImports:     ubImports,
		GoConstraints: goConstraints,
		GoDefaults:    goDefaults,
	}

	if opts.OutDir == "-" {
		if len(v.packages) > 0 {
			return errors.New("compile: cannot stream to stdout when UB libraries are imported")
		}
		out, err := codegen.Generate(in)
		if err != nil {
			return err
		}
		_, err = opts.stdout().Write(out)
		return err
	}

	replaces := codegen.Replaces{}
	if replaceUnobinAbs != "" {
		replaces[toolchain.UnobinModulePath] = replaceUnobinAbs
	}
	maps.Copy(replaces, opts.ReplaceGoModules)
	if err := addManifestReplaces(replaces, stackDir, replaceMap, v.importVersions); err != nil {
		return err
	}

	err = codegen.WriteSource(opts.OutDir, in,
		opts.GoVersion, unobinVersion, v.importVersions, replaces)
	if err != nil {
		return err
	}
	for key, pkgBytes := range v.packages {
		canonical := v.canonicalAlias[key]
		pkgDir := filepath.Join(opts.OutDir, "internal", canonical)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(pkgDir, canonical+".go"), pkgBytes, 0o644); err != nil {
			return err
		}
	}
	if opts.Build {
		return runGoBuild(opts.stdout(), opts.stderr(),
			opts.OutDir, name, opts.Version, unobinVersion)
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
	schemas          *SchemaCache
}

func newCompileVisitor(
	stackName string, warnOut io.Writer, schemas *SchemaCache,
) *compileVisitor {
	return &compileVisitor{
		stackName:        stackName,
		canonicalAlias:   map[string]string{},
		packages:         map[string][]byte{},
		importVersions:   map[string]string{},
		runtimeLibraries: map[string]*ubruntime.Library{},
		warnOut:          warnOut,
		schemas:          schemas,
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
	goSpecs := map[string]codegen.GoLibrarySpecs{}
	runtimeLib := &ubruntime.Library{Name: alias}
	for name, body := range lib.Bodies {
		bodyLibs := make(map[string]*ubruntime.Library, len(lib.BodyImports[name]))
		bodyUsed := usedLibraryTypes(body)
		for _, res := range lib.BodyImports[name] {
			switch res.Kind {
			case resolve.ResolutionGo:
				schema, warnings, err := c.schemas.Read(res.SourcePath)
				if err != nil {
					return fmt.Errorf(
						"composite %q import %q: %w",
						name, res.LocalAlias, err)
				}
				PrintSchemaWarnings(c.warnOut, res.LocalAlias, warnings)
				bodyLibs[res.LocalAlias] = &ubruntime.Library{Schema: schema}
				used := bodyUsed[res.LocalAlias]
				specs := codegen.GoLibrarySpecs{
					Constraints: keepUsedTypes(constraintsFromSchema(schema), used),
					Defaults:    keepUsedTypes(defaultsFromSchema(schema), used),
				}
				if !specs.Empty() {
					goSpecs[res.Path] = specs
				}
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
	src, err := codegen.GenerateUBLibrary(canonical, lib.Bodies, lib.Kinds, composites, goSpecs)
	if err != nil {
		return err
	}
	c.canonicalAlias[canonicalKey] = canonical
	c.packages[canonicalKey] = src
	c.runtimeLibraries[canonicalKey] = runtimeLib
	return nil
}

// decideSelectedUnobin reads `go list -m` output for the unobin module
// after tidy and decides whether the build may proceed. A replaced
// module is the development escape: the build proceeds and the notice
// says the binary runs the replacement. Otherwise the selected version
// must equal the version this CLI pinned; a dependency's go.mod can
// raise the selection above it, and that build would link a runtime
// the compile checks never saw.
func decideSelectedUnobin(listOutput, expected string) (string, error) {
	fields := strings.Fields(listOutput)
	if len(fields) == 0 {
		return "", fmt.Errorf("cannot read the selected version of %s", toolchain.UnobinModulePath)
	}
	selected := fields[0]
	if len(fields) > 1 && fields[1] == "replaced" {
		return fmt.Sprintf(
			"notice: %s is replaced; the factory runs the replacement, not %s",
			toolchain.UnobinModulePath, expected), nil
	}
	if selected != expected {
		return "", fmt.Errorf(
			"the build selected %s %s but this unobin is %s; a dependency requires"+
				" the newer runtime, so upgrade unobin to %s or replace the repo locally",
			toolchain.UnobinModulePath, selected, expected, selected)
	}
	return "", nil
}

// verifySelectedUnobin asks the Go toolchain which unobin version the
// tidied module graph selected and applies decideSelectedUnobin to it,
// writing any notice to the error stream.
func verifySelectedUnobin(stderr io.Writer, goBin, dir, expected string) error {
	list := exec.Command(goBin, "list", "-m",
		"-f", "{{.Version}}{{if .Replace}} replaced{{end}}", toolchain.UnobinModulePath)
	list.Dir = dir
	out, err := list.Output()
	if err != nil {
		return fmt.Errorf("go list -m %s failed: %w", toolchain.UnobinModulePath, err)
	}
	notice, err := decideSelectedUnobin(string(out), expected)
	if err != nil {
		return err
	}
	if notice != "" {
		fmt.Fprintln(stderr, notice)
	}
	return nil
}

func runGoBuild(stdout, stderr io.Writer, dir, binaryName, version, expectedUnobin string) error {
	goBin, err := toolchain.Ensure(stderr)
	if err != nil {
		return err
	}

	tidy := exec.Command(goBin, "mod", "tidy")
	tidy.Dir = dir
	tidy.Stdout = stdout
	tidy.Stderr = stderr
	if err := tidy.Run(); err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}

	if err := verifySelectedUnobin(stderr, goBin, dir, expectedUnobin); err != nil {
		return err
	}

	revision, err := codegen.ContentRevision(dir)
	if err != nil {
		return err
	}

	ldflags := fmt.Sprintf(
		"-X main.factoryVersion=%s -X main.contentRevision=%s -X main.unobinVersion=%s",
		version, revision, expectedUnobin)
	build := exec.Command(goBin, "build", "-ldflags", ldflags, "-o", binaryName, ".")
	build.Dir = dir
	build.Stdout = stdout
	build.Stderr = stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}
	fmt.Fprintf(stderr, "Built %s %s (content-revision %s)\n",
		binaryName, version, revision)
	return nil
}

// LockedVersions reads unobin.lock from dir and returns each repository's
// selected version, or nil when no lock is present, in which case the walk
// uses the version on each import string.
func LockedVersions(dir string) (map[string]string, error) {
	lock, err := deps.ReadLock(os.DirFS(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return lock.RepoVersions()
}

// NewProjectResolver returns the resolver compile uses to fetch import
// sources: a local resolver for relative paths and a remote resolver
// for everything else.
func NewProjectResolver(stackDir string) (resolve.Resolver, error) {
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

// unobinImportGuard refuses an import from the unobin repository when
// no replace serves it. The repo is toolchain-versioned: a
// dependency-versioned import of it could skew from the runtime the
// generated go.mod pins.
type unobinImportGuard struct {
	wrapped resolve.Resolver
}

func (g *unobinImportGuard) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	if ri, ok := ref.(*resolve.RemoteImport); ok && ri.URL == toolchain.UnobinModulePath {
		return nil, fmt.Errorf(
			"the unobin repository is toolchain-versioned and cannot be imported at a"+
				" dependency version; replace it locally for development:\n"+
				"  in unobin.manifest: replace: { '%s': '<path-to-unobin>' }",
			toolchain.UnobinModulePath)
	}
	return g.wrapped.Resolve(ref)
}

// WrapReplaces wraps resolver so that a replaced unobin and each
// manifest replace entry resolve to a local directory instead of
// fetching. Replace paths are taken relative to root.
func WrapReplaces(
	resolver resolve.Resolver, root, replaceUnobin string, replace map[deps.Dependency]string,
) (resolve.Resolver, error) {
	if replaceUnobin != "" {
		abs, err := filepath.Abs(replaceUnobin)
		if err != nil {
			return nil, err
		}
		resolver = &replaceResolver{
			prefix:  toolchain.UnobinModulePath,
			local:   abs,
			wrapped: resolver,
		}
	}
	for dep, path := range replace {
		abs, err := absReplacePath(root, path)
		if err != nil {
			return nil, err
		}
		resolver = &replaceResolver{prefix: dep.URL, local: abs, wrapped: resolver}
	}
	return resolver, nil
}

// projectManifest reads the project's unobin.manifest, returning nil
// when there is no manifest.
func projectManifest(dir string) (*deps.Manifest, error) {
	m, err := deps.ReadManifest(os.DirFS(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return m, nil
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

// usedLibraryTypes returns, per import alias, the set of "<kind>.<type>"
// keys the factory body declares in its resources, data, and actions
// blocks. The keys match the form typeSpecsFromSchema produces, so the
// compiler can omit specs for types the factory never declares.
func usedLibraryTypes(f *lang.File) map[string]map[string]bool {
	used := map[string]map[string]bool{}
	if f == nil || f.Body == nil {
		return used
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != lang.FieldIdent {
			continue
		}
		kind := blockKind(fld.Key.Name)
		if kind == "" {
			continue
		}
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			continue
		}
		for _, entry := range obj.Fields {
			if entry.Key.Kind != lang.FieldPath || len(entry.Key.Path) != 3 {
				continue
			}
			alias := entry.Key.Path[0]
			if used[alias] == nil {
				used[alias] = map[string]bool{}
			}
			used[alias][kind+"."+entry.Key.Path[1]] = true
		}
	}
	return used
}

// blockKind maps a factory declaration block name to the node kind it
// holds, or "" for any other top-level key.
func blockKind(block string) string {
	switch block {
	case "resources":
		return "resource"
	case "data":
		return "data"
	case "actions":
		return "action"
	}
	return ""
}

// pruneUnusedSpecs removes, per alias, the spec entries whose
// "<kind>.<type>" key the factory does not declare, and removes an alias
// left with no entries.
func pruneUnusedSpecs[T any](
	specs map[string]map[string][]T, used map[string]map[string]bool,
) {
	for alias, byType := range specs {
		if kept := keepUsedTypes(byType, used[alias]); kept != nil {
			specs[alias] = kept
		} else {
			delete(specs, alias)
		}
	}
}

// keepUsedTypes returns the entries of m whose "<kind>.<type>" key is in
// used, or nil when none remain. Nil mirrors how typeSpecsFromSchema
// reports an empty result, so the codegen input stays absent rather than
// an empty map.
func keepUsedTypes[T any](m map[string][]T, used map[string]bool) map[string][]T {
	out := map[string][]T{}
	for key, specs := range m {
		if used[key] {
			out[key] = specs
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ReadGoSchema reads a fetched Go library's source from sourcePath
// and returns its schema plus any warnings about registered types
// whose sibling Output struct could not be located. A missing path
// returns nil values with no error, which lets fake resolvers in
// tests fall through without having to write a real library to disk.
// Any other failure mode (missing Library() function, parse error,
// malformed source) is propagated so a broken import fails the
// compile. extra lists module roots beyond the library's own that
// the schema walker may read source from.
func ReadGoSchema(
	sourcePath string, extra ...goschema.ModuleRoot,
) (*ubruntime.LibrarySchema, []string, error) {
	if sourcePath == "" {
		return nil, nil, nil
	}
	return goschema.Read(sourcePath, extra...)
}

// PrintSchemaWarnings emits each warning string to out prefixed with
// the import alias the schema came from.
func PrintSchemaWarnings(out io.Writer, alias string, warnings []string) {
	for _, w := range warnings {
		fmt.Fprintf(out, "warning: import %q: %s\n", alias, w)
	}
}

// GoMajorMinor returns the running Go toolchain's `<major>.<minor>` so
// the generated go.mod's `go` directive matches the current toolchain.
func GoMajorMinor() string {
	v := strings.TrimPrefix(goruntime.Version(), "go")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

// DeriveStackName returns the stack name a source path implies: the
// lowercased basename of the file's directory.
func DeriveStackName(stackPath string) string {
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
