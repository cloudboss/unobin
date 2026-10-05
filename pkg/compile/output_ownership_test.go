package compile

import (
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestCompileReusedOutputMatchesCleanOutput(t *testing.T) {
	for _, build := range []bool{false, true} {
		if build && testing.Short() {
			continue
		}
		for _, scenario := range []struct{ before, after string }{
			{before: "factory.ub", after: "cases/removed/factory.ub"},
			{before: "factory.ub", after: "cases/renamed/factory.ub"},
			{before: "cases/collision/factory.ub", after: "cases/collision-removed/factory.ub"},
		} {
			t.Run(fmt.Sprintf("%s/build=%t", scenario.after, build), func(t *testing.T) {
				root := t.TempDir()
				require.NoError(t, os.CopyFS(root,
					os.DirFS("testdata/ub/output-ownership/valid")))
				setFactory := func(path string) {
					content, err := os.ReadFile(filepath.Join(root, path))
					require.NoError(t, err)
					require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), content, 0o644))
				}
				setFactory(scenario.before)
				provider := filepath.Join(t.TempDir(), "provider")
				require.NoError(t, os.CopyFS(provider,
					os.DirFS("testdata/go/output-ownership")))
				core, err := filepath.Abs("../..")
				require.NoError(t, err)
				options := Options{
					FactoryPath: filepath.Join(root, "factory.ub"),
					OutDir:      filepath.Join(root, "reused"), StackName: "cleanup",
					GoVersion: "1.26.2", CLIVersion: "dev", ReplaceUnobin: core,
					ReplaceGoModules: map[string]string{"example.com/cleanup/provider": provider},
					Build:            build, Stdout: io.Discard, Stderr: io.Discard,
					NewResolver: func(project string) (resolve.Resolver, error) {
						return resolve.NewLocalResolver(project), nil
					},
				}
				_, err = RunResult(options)
				require.NoError(t, err)
				before := generatedCompileFiles(t, options.OutDir)
				note := filepath.Join(options.OutDir, "authored-notes.txt")
				require.NoError(t, os.WriteFile(note, []byte("authored"), 0o644))
				setFactory(scenario.after)
				reused, err := RunResult(options)
				require.NoError(t, err)
				reusedFiles := generatedCompileFiles(t, options.OutDir)
				options.OutDir = filepath.Join(root, "clean")
				clean, err := RunResult(options)
				require.NoError(t, err)
				cleanFiles := generatedCompileFiles(t, options.OutDir)
				require.Equal(t, slices.Sorted(maps.Keys(cleanFiles)),
					slices.Sorted(maps.Keys(reusedFiles)))
				require.Equal(t, cleanFiles, reusedFiles)
				require.Equal(t, clean.ContentRevision, reused.ContentRevision)
				for path := range before {
					if _, exists := reusedFiles[path]; !exists {
						require.Contains(t, reused.Files, filechange.Change{
							Path: filepath.Join(root, "reused", path), Action: filechange.ActionRemoved,
						})
					}
				}
				content, err := os.ReadFile(note)
				require.NoError(t, err)
				require.Equal(t, "authored", string(content))
				if build {
					reusedModules, err := readSelectedLibraryModules("go", reused.OutputDir)
					require.NoError(t, err)
					cleanModules, err := readSelectedLibraryModules("go", clean.OutputDir)
					require.NoError(t, err)
					for i := range reusedModules {
						reusedModules[i].Dir = ""
					}
					for i := range cleanModules {
						cleanModules[i].Dir = ""
					}
					require.Equal(t, cleanModules, reusedModules)
				} else {
					require.Empty(t, reused.ContentRevision)
				}
			})
		}
	}
}

func generatedCompileFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") && name != "factory.assets" &&
			name != "go.mod" && name != "go.sum" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		files[rel] = string(content)
		return err
	}))
	return files
}
