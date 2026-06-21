package e2e

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/compile"
)

type exampleCompileConfig struct {
	skip string
}

type exampleSourceRoot struct {
	name string
	path string
}

var exampleCompileCases = map[string]exampleCompileConfig{
	"awscfg": {},
	"comprehensions": {
		skip: "uses github.com/cloudboss/unobin-library-std without a local replacement",
	},
	"constraints": {
		skip: "uses github.com/cloudboss/unobin-library-std without a local replacement",
	},
	"for-each": {
		skip: "uses github.com/cloudboss/unobin-library-std without a local replacement",
	},
	"hello": {
		skip: "uses github.com/cloudboss/unobin-library-std without a local replacement",
	},
	"hello-library": {
		skip: "uses github.com/cloudboss/unobin-library-std without a local replacement",
	},
	"hello-nested-library": {
		skip: "uses github.com/cloudboss/unobin-libraries-scratch without a local replacement",
	},
	"locals": {
		skip: "uses github.com/cloudboss/unobin-library-std without a local replacement",
	},
	"optionals": {
		skip: "uses github.com/cloudboss/unobin-library-std without a local replacement",
	},
	"payloads": {
		skip: "uses github.com/cloudboss/unobin-library-std without a local replacement",
	},
	"splat": {
		skip: "uses github.com/cloudboss/unobin-library-std without a local replacement",
	},
}

func TestExampleSourceRoots(t *testing.T) {
	repoRoot := e2eRepoRoot(t)
	examples, err := discoverExampleSourceRoots(repoRoot)
	require.NoError(t, err)
	require.NotEmpty(t, examples)

	seen := make(map[string]bool, len(examples))
	for _, example := range examples {
		seen[example.name] = true
		cfg, ok := exampleCompileCases[example.name]
		require.True(t, ok, "%s needs an example compile config", example.name)
		t.Run(example.name, func(t *testing.T) {
			if cfg.skip != "" {
				t.Skip(cfg.skip)
			}
			if testing.Short() {
				t.Skip("skipped: builds example factory")
			}
			compileExampleSourceRoot(t, repoRoot, example)
		})
	}
	for name := range exampleCompileCases {
		require.True(t, seen[name], "%s is configured but no source root exists", name)
	}
}

func discoverExampleSourceRoots(repoRoot string) ([]exampleSourceRoot, error) {
	examplesDir := filepath.Join(repoRoot, "examples")
	var examples []exampleSourceRoot
	err := filepath.WalkDir(examplesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != examplesDir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "factory.ub" {
			return nil
		}
		dir := filepath.Dir(path)
		rel, err := filepath.Rel(examplesDir, dir)
		if err != nil {
			return err
		}
		examples = append(examples, exampleSourceRoot{
			name: filepath.ToSlash(rel),
			path: dir,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(examples, func(i, j int) bool {
		return examples[i].name < examples[j].name
	})
	return examples, nil
}

func compileExampleSourceRoot(t *testing.T, repoRoot string, example exampleSourceRoot) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := compile.Run(compile.Options{
		FactoryPath:   example.path,
		OutDir:        t.TempDir(),
		StackName:     example.name,
		LibraryPath:   "github.com/cloudboss/unobin//examples/" + example.name,
		GoVersion:     compile.GoMajorMinor(),
		Version:       "v0.0.0",
		CLIVersion:    "dev",
		ReplaceUnobin: repoRoot,
		Build:         true,
		Stdout:        &stdout,
		Stderr:        &stderr,
	})
	if err != nil {
		t.Fatalf("compile %s: %v\nstdout:\n%s\nstderr:\n%s",
			example.name,
			err,
			stdout.String(),
			stderr.String(),
		)
	}
}

func e2eRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			require.NoError(t, fmt.Errorf("cannot locate repo root"))
		}
		wd = parent
	}
}
