package e2elib

import (
	"archive/zip"
	"bytes"
	"context"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"testing"

	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchiveZIPFileLifecycle(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	writeFixtureFile(t, filepath.Join(sourceDir, ".hidden"), []byte("hidden\n"), 0o644)
	writeFixtureFile(t, filepath.Join(sourceDir, "main.go"), []byte("package main\n"), 0o644)
	writeFixtureFile(
		t,
		filepath.Join(sourceDir, "internal", "helpers.go"),
		[]byte("package internal\n"),
		0o644,
	)
	writeFixtureFile(t, filepath.Join(sourceDir, "run.sh"), []byte("#!/bin/sh\n"), 0o755)
	require.NoError(t, os.Mkdir(filepath.Join(sourceDir, "empty"), 0o755))

	archiveContent := archiveFixture(t, sourceDir)
	mainContent := []byte("package main\n")
	input := ArchiveZIPFile{
		Path:                    "output.zip",
		SourceDir:               sourceDir,
		SelectedPath:            filepath.Join(sourceDir, "main.go"),
		FileContent:             mainContent,
		ArchiveContent:          archiveContent,
		ExpectedFileSHA256:      hashBytes(mainContent),
		ExpectedFileMode:        "0644",
		ExpectedDirectorySHA256: hashBytes(archiveContent),
		ExpectedDirectoryMode:   "0755",
		ExpectedEntries: map[string]string{
			".hidden":             "0644",
			"empty":               "0755",
			"internal":            "0755",
			"internal/helpers.go": "0644",
			"main.go":             "0644",
			"run.sh":              "0755",
		},
	}
	config := &Configuration{BaseDir: root}

	output, err := input.Create(context.Background(), config)
	require.NoError(t, err)
	assert.Equal(t, input.Path, output.Path)
	assert.Equal(t, input.ExpectedEntries, output.Entries)
	written, err := os.ReadFile(filepath.Join(root, input.Path))
	require.NoError(t, err)
	assert.Equal(t, archiveContent, written)

	readOutput, err := input.Read(context.Background(), config, output)
	require.NoError(t, err)
	assert.Equal(t, output, readOutput)

	mismatch := input
	mismatch.FileContent = []byte("different")
	_, err = mismatch.Update(
		context.Background(),
		config,
		ubruntime.Prior[ArchiveZIPFile, *ArchiveZIPFileOutput]{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "selected file content")

	entryMismatch := input
	entryMismatch.ExpectedEntries = maps.Clone(input.ExpectedEntries)
	delete(entryMismatch.ExpectedEntries, "empty")
	_, err = entryMismatch.Create(
		context.Background(),
		config,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source entries")

	archiveMismatch := input
	archiveMismatch.ArchiveContent = []byte("not a ZIP")
	archiveMismatch.ExpectedDirectorySHA256 = hashBytes(archiveMismatch.ArchiveContent)
	_, err = archiveMismatch.Create(
		context.Background(),
		config,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open archive-content")

	require.NoError(t, input.Delete(context.Background(), config, output))
	_, err = input.Read(context.Background(), config, output)
	require.ErrorIs(t, err, ubruntime.ErrNotFound)
}

func archiveFixture(t *testing.T, root string) []byte {
	t.Helper()
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(info.Mode())
		if entry.IsDir() {
			header.Name += "/"
		}
		stream, err := writer.CreateHeader(header)
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = stream.Write(content)
		return err
	})
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return body.Bytes()
}

func writeFixtureFile(t *testing.T, path string, content []byte, mode fs.FileMode) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, content, mode))
}
