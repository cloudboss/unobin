package ownedoutput

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/filechange"
)

var testGenerator = Generator{Name: "test", Version: "1"}

func generatedFile(path, value string) File {
	return File{
		Path: path, Content: []byte("// Code generated. DO NOT EDIT.\n" + value),
		Mode: 0o644, Marker: "// Code generated. DO NOT EDIT.",
	}
}

func TestApplyUpdatesOnlyOwnedOutputs(t *testing.T) {
	dir := t.TempDir()
	files := []File{generatedFile("resources/old.go", "old"), generatedFile("library.go", "first")}
	changes, err := Apply(dir, testGenerator, files)
	require.NoError(t, err)
	require.Equal(t, []filechange.Change{
		{Path: filepath.Join(dir, ManifestName), Action: filechange.ActionCreated},
		{Path: filepath.Join(dir, "library.go"), Action: filechange.ActionCreated},
		{Path: filepath.Join(dir, "resources/old.go"), Action: filechange.ActionCreated},
	}, changes)
	authored := filepath.Join(dir, "resources/custom_rsrc.go")
	require.NoError(t, os.WriteFile(authored, []byte("authored implementation"), 0o644))
	files = []File{generatedFile("data/new.go", "new"), generatedFile("library.go", "second")}
	changes, err = Apply(dir, testGenerator, files)
	require.NoError(t, err)
	require.Equal(t, []filechange.Change{
		{Path: filepath.Join(dir, ManifestName), Action: filechange.ActionUpdated},
		{Path: filepath.Join(dir, "data/new.go"), Action: filechange.ActionCreated},
		{Path: filepath.Join(dir, "library.go"), Action: filechange.ActionUpdated},
		{Path: filepath.Join(dir, "resources/old.go"), Action: filechange.ActionRemoved},
	}, changes)
	body, err := os.ReadFile(authored)
	require.NoError(t, err)
	require.Equal(t, "authored implementation", string(body))
	changes, err = Apply(dir, testGenerator, files)
	require.NoError(t, err)
	require.Equal(t, []filechange.Change{
		{Path: filepath.Join(dir, ManifestName), Action: filechange.ActionUnchanged},
		{Path: filepath.Join(dir, "data/new.go"), Action: filechange.ActionUnchanged},
		{Path: filepath.Join(dir, "library.go"), Action: filechange.ActionUnchanged},
	}, changes)
}

func TestApplyPreservesImplementationEdits(t *testing.T) {
	dir := t.TempDir()
	implementation := File{
		Path: "implementation.go", Content: []byte("initial"), Mode: 0o644, Preserve: true,
	}
	_, err := Apply(dir, testGenerator, []File{generatedFile("types.go", "first"), implementation})
	require.NoError(t, err)
	path := filepath.Join(dir, implementation.Path)
	require.NoError(t, os.WriteFile(path, []byte("implemented lifecycle"), 0o644))
	implementation.Content = []byte("new skeleton")
	_, err = Apply(dir, testGenerator, []File{generatedFile("types.go", "second"), implementation})
	require.NoError(t, err)
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "implemented lifecycle", string(body))
	_, err = Apply(dir, testGenerator, nil)
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(dir, "types.go"))
	body, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "implemented lifecycle", string(body))
}

func TestApplyRejectsConflictsBeforePublication(t *testing.T) {
	for _, conflict := range []string{"modified owned", "unknown collision", "legacy directory"} {
		t.Run(conflict, func(t *testing.T) {
			dir := t.TempDir()
			files := []File{generatedFile("a.go", "first")}
			if conflict != "legacy directory" {
				_, err := Apply(dir, testGenerator, files)
				require.NoError(t, err)
			}
			path := filepath.Join(dir, "a.go")
			if conflict == "unknown collision" {
				path = filepath.Join(dir, "new.go")
				files = append(files, generatedFile("new.go", "replacement"))
			}
			require.NoError(t, os.WriteFile(path, []byte("authored"), 0o644))
			before := outputFiles(t, dir)
			files[0] = generatedFile("a.go", "second")
			changes, err := Apply(dir, testGenerator, files)
			require.Error(t, err)
			require.Empty(t, changes)
			require.Equal(t, before, outputFiles(t, dir))
		})
	}
}

func TestApplyRejectsInvalidPathsAndMarkers(t *testing.T) {
	for _, path := range []string{
		"", ".", "../outside.go", "/outside.go", "a/../b.go", `a\b.go`, "C:/outside.go",
		ManifestName, ManifestName + "/outside.go",
	} {
		t.Run(path, func(t *testing.T) {
			dir := t.TempDir()
			changes, err := Apply(dir, testGenerator, []File{generatedFile(path, "invalid")})
			require.Error(t, err)
			require.Empty(t, changes)
			require.Empty(t, outputFiles(t, dir))
		})
	}
	dir := t.TempDir()
	file := generatedFile("types.go", "invalid")
	file.Content = []byte("authored")
	_, err := Apply(dir, testGenerator, []File{file})
	require.ErrorContains(t, err, "marker")
	require.Empty(t, outputFiles(t, dir))
}

func TestApplyRejectsSymlinks(t *testing.T) {
	for _, target := range []string{"file", "directory", "manifest"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			outside := t.TempDir()
			files := []File{generatedFile("resources/types.go", "first")}
			_, err := Apply(dir, testGenerator, files)
			require.NoError(t, err)
			path := filepath.Join(dir, "resources", "types.go")
			switch target {
			case "directory":
				path = filepath.Dir(path)
			case "manifest":
				path = filepath.Join(dir, ManifestName)
			}
			require.NoError(t, os.RemoveAll(path))
			require.NoError(t, os.Symlink(outside, path))
			changes, err := Apply(dir, testGenerator, files)
			require.ErrorContains(t, err, "symlink")
			require.Empty(t, changes)
			require.Empty(t, outputFiles(t, outside))
		})
	}
}

func TestApplyRejectsInvalidOwnershipRecords(t *testing.T) {
	for _, mutation := range []string{"path", "digest", "generator", "version"} {
		t.Run(mutation, func(t *testing.T) {
			dir := t.TempDir()
			files := []File{generatedFile("types.go", "first")}
			_, err := Apply(dir, testGenerator, files)
			require.NoError(t, err)
			path := filepath.Join(dir, ManifestName)
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			var record map[string]any
			require.NoError(t, json.Unmarshal(body, &record))
			switch mutation {
			case "path":
				record["files"].([]any)[0].(map[string]any)["path"] = "../outside"
			case "digest":
				record["files"].([]any)[0].(map[string]any)["digest"] = "invalid"
			case "generator":
				record["generator"] = "another"
			case "version":
				record["format-version"] = 99
			}
			body, err = json.Marshal(record)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, body, 0o644))
			before := outputFiles(t, dir)
			changes, err := Apply(dir, testGenerator, files)
			require.Error(t, err)
			require.Empty(t, changes)
			require.Equal(t, before, outputFiles(t, dir))
		})
	}
}

func TestApplyRollsBackPublicationFailure(t *testing.T) {
	dir := t.TempDir()
	_, err := Apply(dir, testGenerator, []File{generatedFile("old.go", "old")})
	require.NoError(t, err)
	before := outputFiles(t, dir)
	cause := errors.New("publication failed")
	failed := false
	changes, err := apply(dir, testGenerator, []File{generatedFile("new.go", "new")},
		func(source, target string) error {
			if target == filepath.Join(dir, ManifestName) && !failed {
				failed = true
				return cause
			}
			return os.Rename(source, target)
		})
	require.ErrorIs(t, err, cause)
	for _, change := range changes {
		require.Equal(t, filechange.ActionUnchanged, change.Action)
	}
	require.Equal(t, before, outputFiles(t, dir))
}

func TestApplyRetainsBackupsWhenRollbackFails(t *testing.T) {
	dir := t.TempDir()
	original := generatedFile("old.go", "original")
	_, err := Apply(dir, testGenerator, []File{original})
	require.NoError(t, err)
	cause := errors.New("publication and restore failed")
	changes, err := apply(dir, testGenerator, []File{generatedFile("new.go", "new")},
		func(source, target string) error {
			if target == filepath.Join(dir, ManifestName) || target == filepath.Join(dir, "old.go") {
				return cause
			}
			return os.Rename(source, target)
		})
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "recover outputs from")
	require.Equal(t, []filechange.Change{
		{Path: filepath.Join(dir, ManifestName), Action: filechange.ActionRemoved},
		{Path: filepath.Join(dir, "old.go"), Action: filechange.ActionRemoved},
	}, changes)
	require.NoFileExists(t, filepath.Join(dir, ManifestName))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var backups []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".unobin-stage-") {
			path := filepath.Join(dir, entry.Name(), "backup", "old.go")
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original.Content, body)
			backups = append(backups, "old.go")
		}
	}
	require.Equal(t, []string{"old.go"}, backups)
}

func TestApplyAcceptsRelativeOutputDirectories(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := Apply("./output", testGenerator, []File{generatedFile("nested/types.go", "first")})
	require.NoError(t, err)
	body, err := os.ReadFile("output/nested/types.go")
	require.NoError(t, err)
	require.Equal(t, generatedFile("nested/types.go", "first").Content, body)
}

func TestApplyRejectsConflictingBatchPaths(t *testing.T) {
	for _, paths := range [][]string{{"types.go", "types.go"}, {"parent", "parent/types.go"}} {
		dir := t.TempDir()
		var files []File
		for _, path := range paths {
			files = append(files, generatedFile(path, "invalid"))
		}
		changes, err := Apply(dir, testGenerator, files)
		require.Error(t, err)
		require.Empty(t, changes)
		require.Empty(t, outputFiles(t, dir))
	}
}

func outputFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		files[rel] = string(body)
		return err
	}))
	return files
}
