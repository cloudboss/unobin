package root

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/compile"
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
			" config.ub asserts the same value under factory.pin.library-path"+
			" and plan, refresh, and validate refuse on mismatch.")

	CompileCmd.Flags().StringVarP(&compileCfg.outDir, "out", "o", "",
		"Directory to write main.go and go.mod into, or `-` to print main.go to stdout.")

	CompileCmd.Flags().StringVar(&compileCfg.goVersion, "go-version", compile.GoMajorMinor(),
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
	replaceGoModules, err := parseReplaceFlags(cfg.replaceGoModule)
	if err != nil {
		return err
	}
	return compile.Run(compile.Options{
		StackPath:        cfg.stackPath,
		OutDir:           cfg.outDir,
		StackName:        cfg.stackName,
		LibraryPath:      cfg.libraryPath,
		GoVersion:        cfg.goVersion,
		Version:          cfg.version,
		CLIVersion:       cliVersion(),
		ReplaceUnobin:    cfg.replaceUnobin,
		ReplaceGoModules: replaceGoModules,
		Build:            cfg.build,
		NewResolver:      newCompileResolver,
		Stdout:           cmd.OutOrStdout(),
		Stderr:           cmd.ErrOrStderr(),
	})
}

// newCompileResolver constructs the resolver the compile, print-graph,
// and deps commands fetch import sources with. Tests override this
// package var to avoid any network access.
var newCompileResolver = compile.NewProjectResolver

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
