package root

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/cloudboss/unobin/pkg/codegen"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/resolve"
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

	builder := newUBBuilder(name, resolver)
	goImports := make(map[string]string, len(refs))
	ubImports := make(map[string]string, len(refs))

	for alias, ref := range refs {
		source, err := resolver.Resolve(ref)
		if err != nil {
			return fmt.Errorf("import %q: %w", alias, err)
		}
		if resolve.IsUBModule(source) {
			canonical, err := builder.build(alias, ref, source)
			if err != nil {
				return fmt.Errorf("import %q: %w", alias, err)
			}
			ubImports[alias] = name + "/internal/" + canonical
			continue
		}
		switch r := ref.(type) {
		case *resolve.LocalImport:
			return fmt.Errorf("import %q: local source at %q has no module.ub", alias, r.Path)
		case *resolve.RemoteImport:
			path := r.URL
			if r.Subdir != "" {
				path += "/" + r.Subdir
			}
			if err := builder.recordGoImport(path, r.Version); err != nil {
				return fmt.Errorf("import %q: %w", alias, err)
			}
			goImports[alias] = path
		default:
			return fmt.Errorf("import %q: unsupported ref type %T", alias, ref)
		}
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
		if len(builder.packages) > 0 {
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
		cfg.goVersion, cfg.unobinVersion, builder.importVersions, replaces)
	if err != nil {
		return err
	}
	for key, pkgBytes := range builder.packages {
		canonical := builder.canonicalAlias[key]
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

// ubBuilder accumulates the state of a recursive UB-module build.
// Compile starts at the stack root and descends through every UB
// module's body imports. Each unique UB module URL is generated as a
// single Go package under `<stack-name>/internal/<canonical-alias>`,
// where the canonical alias is whichever name the module was first
// imported as. Multiple call sites that point at the same URL share
// that package even if they use different local aliases. Go-module
// imports are merged into importVersions for go.mod, with an error
// when two sites disagree on the version. Cycles through UB-module
// imports are detected and reported.
type ubBuilder struct {
	stackName string
	resolver  resolve.Resolver

	canonicalAlias map[string]string
	packages       map[string][]byte
	importVersions map[string]string
	inProgress     map[string]bool
}

func newUBBuilder(stackName string, resolver resolve.Resolver) *ubBuilder {
	return &ubBuilder{
		stackName:      stackName,
		resolver:       resolver,
		canonicalAlias: map[string]string{},
		packages:       map[string][]byte{},
		importVersions: map[string]string{},
		inProgress:     map[string]bool{},
	}
}

// build reads a UB module's manifest, parses each exported body, and
// recursively resolves the body's `imports:` block so per-composite
// Modules maps can be baked into the generated source. localAlias is
// the name this site uses for the module; the first site wins as the
// canonical alias.
func (b *ubBuilder) build(localAlias string, ref resolve.ImportRef,
	source *resolve.Source) (string, error) {
	key := ubKey(ref)
	if existing, ok := b.canonicalAlias[key]; ok {
		return existing, nil
	}
	if b.inProgress[key] {
		return "", fmt.Errorf("import cycle through %s", key)
	}
	b.inProgress[key] = true
	defer delete(b.inProgress, key)

	canonical := localAlias

	manifestBytes, err := readSourceFile(source, "module.ub")
	if err != nil {
		return "", fmt.Errorf("read module.ub: %w", err)
	}
	manifest, err := lang.ParseSource("module.ub", manifestBytes)
	if err != nil {
		return "", err
	}
	if errs := lang.ValidateFile(manifest); errs.Len() > 0 {
		return "", errs.Err()
	}

	exports, err := readManifestExports(manifest)
	if err != nil {
		return "", err
	}

	bodies := make(map[string]*lang.File, len(exports))
	composites := make(map[string]map[string]string, len(exports))

	for name, path := range exports {
		body, err := readSourceFile(source, path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		f, err := lang.ParseSource(path, body)
		if err != nil {
			return "", err
		}
		f.Kind = lang.FileExportedType
		if errs := lang.ValidateFile(f); errs.Len() > 0 {
			return "", errs.Err()
		}
		bodies[name] = f

		bodyImports, importErrs := resolve.ExtractImports(f)
		if len(importErrs) > 0 {
			return "", errors.Join(importErrs...)
		}
		paths, err := b.resolveCompositeImports(bodyImports)
		if err != nil {
			return "", fmt.Errorf("composite %q: %w", name, err)
		}
		if len(paths) > 0 {
			composites[name] = paths
		}
	}

	src, err := codegen.GenerateUBModule(canonical, manifest, bodies, composites)
	if err != nil {
		return "", err
	}
	b.canonicalAlias[key] = canonical
	b.packages[key] = src
	return canonical, nil
}

// resolveCompositeImports resolves each entry of a composite body's
// `imports:` block. Go-module entries register at top level (so the
// stack's go.mod gets a single merged set of pinned versions).
// UB-module entries recurse via build, which dedupes by URL so the
// same module imported from multiple places is generated once.
func (b *ubBuilder) resolveCompositeImports(refs map[string]resolve.ImportRef) (map[string]string, error) {
	out := make(map[string]string, len(refs))
	for alias, ref := range refs {
		source, err := b.resolver.Resolve(ref)
		if err != nil {
			return nil, fmt.Errorf("import %q: %w", alias, err)
		}
		if resolve.IsUBModule(source) {
			canonical, err := b.build(alias, ref, source)
			if err != nil {
				return nil, fmt.Errorf("import %q: %w", alias, err)
			}
			out[alias] = b.stackName + "/internal/" + canonical
			continue
		}
		r, ok := ref.(*resolve.RemoteImport)
		if !ok {
			return nil, fmt.Errorf("import %q: composite imports must be remote", alias)
		}
		path := r.URL
		if r.Subdir != "" {
			path += "/" + r.Subdir
		}
		if err := b.recordGoImport(path, r.Version); err != nil {
			return nil, fmt.Errorf("import %q: %w", alias, err)
		}
		out[alias] = path
	}
	return out, nil
}

// recordGoImport pins a Go-module path to a specific version. When the
// same path has already been pinned to a different version somewhere
// else in the stack, the conflict is reported.
func (b *ubBuilder) recordGoImport(path, version string) error {
	if existing, ok := b.importVersions[path]; ok && existing != version {
		return fmt.Errorf("conflicting versions for %s: %s vs %s",
			path, existing, version)
	}
	b.importVersions[path] = version
	return nil
}

// ubKey is the dedup key for a UB-module import reference. Remote
// imports key on URL, subdir, and version so two stack sites that
// pin the same module the same way share one generated package.
// Local imports key on path; the resolver enforces uniqueness up to
// the parent's working directory.
func ubKey(ref resolve.ImportRef) string {
	switch r := ref.(type) {
	case *resolve.RemoteImport:
		return "remote:" + r.URL + "//" + r.Subdir + "@" + r.Version
	case *resolve.LocalImport:
		return "local:" + r.Path
	}
	return ""
}

func readSourceFile(s *resolve.Source, name string) ([]byte, error) {
	f, err := s.FS.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

func readManifestExports(f *lang.File) (map[string]string, error) {
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "exports" {
			continue
		}
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			return nil, fmt.Errorf("`exports:` must be an object")
		}
		out := make(map[string]string, len(obj.Fields))
		for _, ef := range obj.Fields {
			if ef.Key.Kind != lang.FieldIdent || ef.Key.IsMeta() {
				continue
			}
			s, ok := ef.Value.(*lang.StringLit)
			if !ok {
				return nil, fmt.Errorf("export %q: value must be a string", ef.Key.Name)
			}
			out[ef.Key.Name] = s.Value
		}
		return out, nil
	}
	return map[string]string{}, nil
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
