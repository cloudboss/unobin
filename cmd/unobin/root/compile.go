package root

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/codegen"
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
	stackPath string
	version   string
	commit    string
	stackName string
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
}

func runCompile(cmd *cobra.Command, cfg *compileConfig) error {
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
	}

	name := cfg.stackName
	if name == "" {
		name = deriveStackName(cfg.stackPath)
	}

	out, err := codegen.Generate(codegen.Input{
		Source:    string(src),
		StackName: name,
		Version:   cfg.version,
		Commit:    cfg.commit,
		GoImports: goImports,
	})
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(out)
	return err
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
