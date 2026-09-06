package e2etest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLibraryModuleConfiguration(t *testing.T) {
	dir := t.TempDir()
	cfg, err := newConfig(t.Context(), []Option{
		WithUnobinDir(e2eRepoRoot(t)),
		WithGoModule("example.com/library", dir),
		WithGoModule("example.com/other", filepath.Join(dir, "other")),
	})
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"example.com/library": dir,
		"example.com/other":   filepath.Join(dir, "other"),
	}, cfg.goModules)
	require.Empty(t, cfg.sourceDirectories)
}

func TestConfigResolvesRelativeDirectories(t *testing.T) {
	root := e2eRepoRoot(t)
	cfg, err := newConfig(t.Context(), []Option{
		WithUnobinDir("../.."),
		WithGoModule("example.com/library", "."),
		WithSourceDirectory("modules/library", "testdata"),
	})
	require.NoError(t, err)
	require.Equal(t, root, cfg.repoRoot)
	require.Equal(t, filepath.Join(root, "pkg", "e2etest"), cfg.goModules["example.com/library"])
	require.Equal(t, map[string]string{
		"modules/library": filepath.Join(root, "pkg", "e2etest", "testdata"),
	}, cfg.sourceDirectories)
}

func TestConfigRejectsInvalidDirectories(t *testing.T) {
	for _, test := range []struct {
		name string
		opt  Option
	}{
		{"missing module", WithGoModule("", ".")},
		{"missing module directory", WithGoModule("example.com/library", "")},
		{"parent directory", WithSourceDirectory("../outside", ".")},
		{"absolute directory", WithSourceDirectory(t.TempDir(), ".")},
		{"workspace root", WithSourceDirectory(".", ".")},
		{"missing source", WithSourceDirectory("modules/library", "")},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := newConfig(t.Context(), []Option{WithUnobinDir(e2eRepoRoot(t)), test.opt})
			require.Error(t, err)
		})
	}
}

func TestCommandEnvironmentDoesNotChangeCallerMaps(t *testing.T) {
	env := map[string]string{"URL": "http://localhost", "INPUT": "default"}
	opt := WithEnv(env)
	env["URL"] = "changed"
	cfg, err := newConfig(t.Context(), []Option{WithUnobinDir(e2eRepoRoot(t)), opt})
	require.NoError(t, err)
	cmd := Command{Env: map[string]string{"INPUT": "override"}}
	got := cfg.command(cmd)
	require.Equal(t, map[string]string{"URL": "http://localhost", "INPUT": "override"}, got.Env)
	got.Env["URL"] = "changed again"
	require.Equal(t, map[string]string{"URL": "http://localhost", "INPUT": "default"}, cfg.env)
	require.Equal(t, map[string]string{"INPUT": "override"}, cmd.Env)
}
