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
	goImports := make(map[string]string, len(refs))
	importVersions := make(map[string]string, len(refs))
	for alias, ref := range refs {
		rem, ok := ref.(*resolve.RemoteImport)
		if !ok {
			return fmt.Errorf("import %q: local imports are not handled by compile yet", alias)
		}
		path := rem.URL
		if rem.Subdir != "" {
			path += "/" + rem.Subdir
		}
		goImports[alias] = path
		importVersions[path] = rem.Version
	}

	name := cfg.stackName
	if name == "" {
		name = deriveStackName(cfg.stackPath)
	}
	in := codegen.Input{
		Source:    string(src),
		StackName: name,
		Version:   cfg.version,
		Commit:    cfg.commit,
		GoImports: goImports,
	}

	if cfg.outDir == "-" {
		out, err := codegen.Generate(in)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(out)
		return err
	}

	replaces := codegen.Replaces{}
	if cfg.replaceUnobin != "" {
		abs, err := filepath.Abs(cfg.replaceUnobin)
		if err != nil {
			return err
		}
		replaces["github.com/cloudboss/unobin"] = abs
	}

	err = codegen.WriteSource(cfg.outDir, in,
		cfg.goVersion, cfg.unobinVersion, importVersions, replaces)
	if err != nil {
		return err
	}
	if cfg.build {
		return runGoBuild(cmd, cfg.outDir, name)
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
