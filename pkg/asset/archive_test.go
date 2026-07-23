package asset

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCanonicalDirectoryArchive(t *testing.T) {
	entries := archiveFixture()

	body, err := canonicalDirectoryArchive(entries)
	require.NoError(t, err)
	require.Equal(
		t,
		"efa409cac7ba9d16a96cf4ca9935621c"+
			"b7631156c8a5b999803925fddd602e45",
		contentSHA256(body),
	)

	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{".hidden", "bin/", "bin/run", "empty/", "notes.txt"},
		zipNames(reader.File),
	)

	expected := map[string]struct {
		mode    fs.FileMode
		content string
	}{
		".hidden":   {mode: 0644, content: "secret\n"},
		"bin/":      {mode: 0755},
		"bin/run":   {mode: 0755, content: "#!/bin/sh\n"},
		"empty/":    {mode: 0755},
		"notes.txt": {mode: 0644, content: "notes\n"},
	}
	for _, file := range reader.File {
		want := expected[file.Name]
		require.Equal(t, uint16(0x21), file.ModifiedDate)
		require.Zero(t, file.ModifiedTime)
		require.Equal(t, time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC), file.Modified)
		require.Empty(t, file.Extra)
		require.Empty(t, file.Comment)
		require.Equal(t, uint16(zip.Store), file.Method)
		require.Equal(t, want.mode, file.Mode().Perm())
		stream, err := file.Open()
		require.NoError(t, err)
		body, err := io.ReadAll(stream)
		require.NoError(t, err)
		require.NoError(t, stream.Close())
		require.Equal(t, want.content, string(body))
	}
}

func TestCanonicalDirectoryArchiveIsDeterministic(t *testing.T) {
	entries := archiveFixture()
	reversed := slices.Clone(entries)
	slices.Reverse(reversed)

	first, err := canonicalDirectoryArchive(entries)
	require.NoError(t, err)
	second, err := canonicalDirectoryArchive(reversed)
	require.NoError(t, err)

	require.Equal(t, first, second)
}

func TestCanonicalDirectoryArchiveOfEmptyDirectory(t *testing.T) {
	body, err := canonicalDirectoryArchive([]CapturedEntry{{
		Kind: EntryKindDirectory,
		Mode: "0755",
	}})
	require.NoError(t, err)

	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)
	require.Empty(t, reader.File)
}

func TestCanonicalDirectoryArchiveRejectsInvalidEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []CapturedEntry
	}{
		{name: "missing root", entries: []CapturedEntry{archiveFile("main", "body")}},
		{
			name: "duplicate root",
			entries: []CapturedEntry{
				archiveRoot(),
				archiveRoot(),
			},
		},
		{
			name: "file root",
			entries: []CapturedEntry{{
				Kind:    EntryKindFile,
				Mode:    "0644",
				Content: []byte("body"),
			}},
		},
		{
			name:    "absolute path",
			entries: []CapturedEntry{archiveRoot(), archiveFile("/main", "body")},
		},
		{
			name:    "backslash",
			entries: []CapturedEntry{archiveRoot(), archiveFile(`dir\main`, "body")},
		},
		{
			name:    "dot segment",
			entries: []CapturedEntry{archiveRoot(), archiveFile("dir/../main", "body")},
		},
		{
			name:    "empty segment",
			entries: []CapturedEntry{archiveRoot(), archiveFile("dir//main", "body")},
		},
		{
			name:    "dot path",
			entries: []CapturedEntry{archiveRoot(), archiveFile(".", "body")},
		},
		{
			name: "duplicate path",
			entries: []CapturedEntry{
				archiveRoot(),
				archiveFile("main", "one"),
				archiveFile("main", "two"),
			},
		},
		{
			name: "missing parent",
			entries: []CapturedEntry{
				archiveRoot(),
				archiveFile("dir/main", "body"),
			},
		},
		{
			name: "file parent",
			entries: []CapturedEntry{
				archiveRoot(),
				archiveFile("dir", "body"),
				archiveFile("dir/main", "body"),
			},
		},
		{
			name: "invalid kind",
			entries: []CapturedEntry{
				archiveRoot(),
				{InternalPath: "main", Kind: EntryKind("other"), Mode: "0644"},
			},
		},
		{
			name: "invalid file mode",
			entries: []CapturedEntry{
				archiveRoot(),
				{InternalPath: "main", Kind: EntryKindFile, Mode: "0600"},
			},
		},
		{
			name: "invalid directory mode",
			entries: []CapturedEntry{
				archiveRoot(),
				{InternalPath: "dir", Kind: EntryKindDirectory, Mode: "0644"},
			},
		},
		{
			name: "directory content",
			entries: []CapturedEntry{
				archiveRoot(),
				{
					InternalPath: "dir",
					Kind:         EntryKindDirectory,
					Mode:         "0755",
					Content:      []byte("unexpected"),
				},
			},
		},
		{
			name: "invalid UTF-8",
			entries: []CapturedEntry{
				archiveRoot(),
				archiveFile(string([]byte{0xff}), "body"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := canonicalDirectoryArchive(tt.entries)
			require.Error(t, err)
		})
	}
}

func archiveFixture() []CapturedEntry {
	return []CapturedEntry{
		archiveFile("notes.txt", "notes\n"),
		archiveRoot(),
		archiveFile(".hidden", "secret\n"),
		{
			InternalPath: "empty",
			Kind:         EntryKindDirectory,
			Mode:         "0755",
		},
		archiveFile("bin/run", "#!/bin/sh\n", "0755"),
		{
			InternalPath: "bin",
			Kind:         EntryKindDirectory,
			Mode:         "0755",
		},
	}
}

func archiveRoot() CapturedEntry {
	return CapturedEntry{Kind: EntryKindDirectory, Mode: "0755"}
}

func archiveFile(name, content string, mode ...string) CapturedEntry {
	fileMode := "0644"
	if len(mode) == 1 {
		fileMode = mode[0]
	}
	return CapturedEntry{
		InternalPath: name,
		Kind:         EntryKindFile,
		Mode:         fileMode,
		Content:      []byte(content),
	}
}

func zipNames(files []*zip.File) []string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	return names
}
