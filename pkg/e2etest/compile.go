package e2etest

import (
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/compile"
)

func compileCase(
	cfg config,
	c CompiledCase,
	workspace string,
) (string, error) {
	outDir := filepath.Join(workspace, ".e2e", "build")
	return compileCaseTo(
		cfg,
		c,
		workspace,
		outDir,
		c.Build,
	)
}

func compileCaseTo(
	cfg config,
	c CompiledCase,
	workspace string,
	outDir string,
	build bool,
) (string, error) {
	factoryPath := filepath.Join(workspace, filepath.FromSlash(c.FactoryPath))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := compile.Run(compile.Options{
		FactoryPath:      factoryPath,
		OutDir:           outDir,
		StackName:        c.Name,
		LibraryPath:      c.LibraryPath,
		GoVersion:        compile.GoMajorMinor(),
		Version:          "v0.0.0",
		CLIVersion:       "dev",
		ReplaceUnobin:    cfg.repoRoot,
		ReplaceGoModules: cfg.goModules,
		Build:            build,
		Stdout:           &stdout,
		Stderr:           &stderr,
	})
	if err != nil {
		return "", fmt.Errorf(
			"compile %s: %w\nstdout:\n%s\nstderr:\n%s",
			c.Name,
			err,
			stdout.String(),
			stderr.String(),
		)
	}
	if !build {
		return "", nil
	}
	return filepath.Join(outDir, c.Name), nil
}
