package e2etest

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
)

func WithRepoRoot(path string) Option {
	return func(c *config) { c.repoRoot = path }
}

func WithE2ELibraryDir(path string) Option {
	return func(c *config) { c.e2eLibraryDir = path }
}

func newConfig(opts []Option) (config, error) {
	repoRoot, err := defaultRepoRoot()
	if err != nil {
		return config{}, err
	}
	cfg := config{
		repoRoot:      repoRoot,
		e2eLibraryDir: filepath.Join(repoRoot, "tests", "e2e", "testdata", "modules", "e2elib"),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg, nil
}

func defaultRepoRoot() (string, error) {
	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate e2etest package")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("locate repo root: %w", err)
	}
	return root, nil
}
