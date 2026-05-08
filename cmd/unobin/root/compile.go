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

	goImports := make(map[string]string, len(refs))
	importVersions := make(map[string]string, len(refs))
	ubImports := make(map[string]string, len(refs))
	ubPackages := make(map[string][]byte, len(refs))

	for alias, ref := range refs {
		source, err := resolver.Resolve(ref)
		if err != nil {
			return fmt.Errorf("import %q: %w", alias, err)
		}
		if resolve.IsUBModule(source) {
			pkg, err := buildUBPackage(alias, source)
			if err != nil {
				return fmt.Errorf("import %q: %w", alias, err)
			}
			ubPackages[alias] = pkg
			ubImports[alias] = name + "/internal/" + alias
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
			goImports[alias] = path
			importVersions[path] = r.Version
		default:
			return fmt.Errorf("import %q: unsupported ref type %T", alias, ref)
		}
	}

	in := codegen.Input{
		Source:    string(src),
		StackName: name,
		Version:   cfg.version,
		Commit:    cfg.commit,
		GoImports: goImports,
		UBImports: ubImports,
	}

	if cfg.outDir == "-" {
		if len(ubPackages) > 0 {
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
		cfg.goVersion, cfg.unobinVersion, importVersions, replaces)
	if err != nil {
		return err
	}
	for alias, pkg := range ubPackages {
		pkgDir := filepath.Join(cfg.outDir, "internal", alias)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(pkgDir, alias+".go"), pkg, 0o644); err != nil {
			return err
		}
	}
	if cfg.build {
		return runGoBuild(cmd, cfg.outDir, name)
	}
	return nil
}

// buildUBPackage reads the UB module's manifest and exported body
// files from source and runs codegen.GenerateUBModule to produce
// the per-module Go package source.
func buildUBPackage(alias string, source *resolve.Source) ([]byte, error) {
	manifestBytes, err := readSourceFile(source, "module.ub")
	if err != nil {
		return nil, fmt.Errorf("read module.ub: %w", err)
	}
	manifest, err := lang.ParseSource("module.ub", manifestBytes)
	if err != nil {
		return nil, err
	}
	if errs := lang.ValidateFile(manifest); errs.Len() > 0 {
		return nil, errs.Err()
	}
	exports, err := readManifestExports(manifest)
	if err != nil {
		return nil, err
	}
	bodies := make(map[string]*lang.File, len(exports))
	for name, path := range exports {
		body, err := readSourceFile(source, path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		f, err := lang.ParseSource(path, body)
		if err != nil {
			return nil, err
		}
		f.Kind = lang.FileExportedType
		if errs := lang.ValidateFile(f); errs.Len() > 0 {
			return nil, errs.Err()
		}
		bodies[name] = f
	}
	return codegen.GenerateUBModule(alias, manifest, bodies)
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
