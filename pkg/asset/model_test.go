package asset

import (
	"encoding/json"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManifestJSONNames(t *testing.T) {
	manifest := Manifest{
		FormatVersion: FormatVersion,
		AssetSets: []ManifestAssetSet{{
			ID: "set",
			Assets: []ManifestAsset{{
				Name: "program",
				Entries: []ManifestEntry{{
					InternalPath:  "main",
					Kind:          EntryKindFile,
					Mode:          "0755",
					ContentSize:   3,
					ContentSHA256: "content",
					EntryIdentity: "entry",
					BlobPath:      "blobs/content",
				}},
			}},
		}},
	}

	got, err := json.Marshal(manifest)
	require.NoError(t, err)
	expected := `{"format-version":1,"asset-sets":[{"id":"set","assets":[{` +
		`"name":"program","entries":[{"internal-path":"main","kind":"file",` +
		`"mode":"0755","content-size":3,"content-sha256":"content",` +
		`"entry-identity":"entry","blob-path":"blobs/content"}]}]}]}`
	require.Equal(t, expected, string(got))
}

func TestNormalizeMode(t *testing.T) {
	tests := []struct {
		name string
		mode fs.FileMode
		want string
	}{
		{name: "directory", mode: fs.ModeDir | 0700, want: "0755"},
		{name: "readable file", mode: 0600, want: "0644"},
		{name: "executable file", mode: 0100, want: "0755"},
		{name: "group executable file", mode: 0010, want: "0755"},
		{name: "other executable file", mode: 0001, want: "0755"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeMode(tt.mode)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeModeRejectsUnsupportedModes(t *testing.T) {
	tests := []struct {
		name string
		mode fs.FileMode
	}{
		{name: "setuid", mode: fs.ModeSetuid | 0644},
		{name: "setgid", mode: fs.ModeSetgid | 0644},
		{name: "sticky directory", mode: fs.ModeDir | fs.ModeSticky | 0755},
		{name: "symlink", mode: fs.ModeSymlink | 0777},
		{name: "named pipe", mode: fs.ModeNamedPipe | 0644},
		{name: "socket", mode: fs.ModeSocket | 0644},
		{name: "device", mode: fs.ModeDevice | 0644},
		{name: "character device", mode: fs.ModeDevice | fs.ModeCharDevice | 0644},
		{name: "irregular", mode: fs.ModeIrregular | 0644},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeMode(tt.mode)
			require.Error(t, err)
		})
	}
}
