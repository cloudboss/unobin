package e2etest

import (
	"context"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"strings"
)

// WithUnobinDir selects a local Unobin checkout or module directory.
func WithUnobinDir(path string) Option {
	return func(c *config) { c.repoRoot = path }
}

// WithGoModule replaces a library module with a local directory when compiling cases.
func WithGoModule(modulePath, path string) Option {
	return func(c *config) { c.goModules[modulePath] = path }
}

// WithSourceDirectory copies a directory into source-case workspaces if absent.
func WithSourceDirectory(path, source string) Option {
	return func(c *config) { c.sourceDirectories[path] = source }
}

// WithEnv adds environment variables to case commands, including automatic pinning.
// Variables declared by an individual command take precedence.
func WithEnv(env map[string]string) Option {
	values := maps.Clone(env)
	return func(c *config) { maps.Copy(c.env, values) }
}

// WithUnobinExecutable supplies the CLI used by source cases that run a process.
func WithUnobinExecutable(path string) Option {
	return func(c *config) { c.unobinExecutable = path }
}

func newConfig(ctx context.Context, opts []Option) (config, error) {
	cfg := config{
		goModules:         map[string]string{},
		sourceDirectories: map[string]string{},
		env:               map[string]string{},
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.repoRoot == "" {
		cmd := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Dir}}",
			"github.com/cloudboss/unobin")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return config{}, fmt.Errorf("locate Unobin module: %w\n%s", err, output)
		}
		cfg.repoRoot = strings.TrimSpace(string(output))
		if cfg.repoRoot == "" {
			return config{}, fmt.Errorf("unobin module has no local directory; use WithUnobinDir")
		}
	}
	root, err := filepath.Abs(cfg.repoRoot)
	if err != nil {
		return config{}, fmt.Errorf("resolve Unobin directory: %w", err)
	}
	cfg.repoRoot = root
	for module, path := range cfg.goModules {
		if module == "" || path == "" {
			return config{}, fmt.Errorf("module replacement requires a module path and directory")
		}
		cfg.goModules[module], err = filepath.Abs(path)
		if err != nil {
			return config{}, fmt.Errorf("resolve module %s: %w", module, err)
		}
	}
	for path, source := range cfg.sourceDirectories {
		if err := checkRelPath("source directory", path); err != nil {
			return config{}, err
		}
		if path == "" || path == "." || source == "" {
			return config{}, fmt.Errorf("source directory requires a workspace subdirectory and source")
		}
		cfg.sourceDirectories[path], err = filepath.Abs(source)
		if err != nil {
			return config{}, fmt.Errorf("resolve source directory %s: %w", path, err)
		}
	}
	return cfg, nil
}

func (c config) command(cmd Command) Command {
	env := maps.Clone(c.env)
	if env == nil {
		env = map[string]string{}
	}
	maps.Copy(env, cmd.Env)
	cmd.Env = env
	return cmd
}
