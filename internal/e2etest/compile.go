package e2etest

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/compile"
)

const e2eLibraryModule = "example.com/unobin/e2elib"

func compileCase(
	_ context.Context,
	repoRoot string,
	e2eLibraryDir string,
	c CompiledCase,
	workspace string,
) (string, error) {
	outDir := filepath.Join(workspace, ".e2e", "build")
	factoryPath := filepath.Join(workspace, filepath.FromSlash(c.FactoryPath))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := compile.Run(compile.Options{
		FactoryPath:   factoryPath,
		OutDir:        outDir,
		StackName:     c.Name,
		LibraryPath:   c.LibraryPath,
		GoVersion:     compile.GoMajorMinor(),
		Version:       "v0.0.0",
		CLIVersion:    "dev",
		ReplaceUnobin: repoRoot,
		ReplaceGoModules: map[string]string{
			e2eLibraryModule: e2eLibraryDir,
		},
		Build:  c.Build,
		Stdout: &stdout,
		Stderr: &stderr,
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
	if !c.Build {
		return "", nil
	}
	return filepath.Join(outDir, c.Name), nil
}
