package lsp

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentStoreLifecycle(t *testing.T) {
	store := NewDocumentStore()
	uri := "file:///tmp/example.ub"

	opened, err := store.Open(uri, 1, "one\n")
	require.NoError(t, err)
	require.Equal(t, uri, opened.URI)
	require.Equal(t, filepath.FromSlash("/tmp/example.ub"), opened.Path)
	require.Equal(t, int32(1), opened.Version)
	require.Equal(t, "one\n", opened.Text)
	require.Len(t, opened.Lines, 2)

	got, ok := store.Get(uri)
	require.True(t, ok)
	require.Equal(t, opened, got)

	changed, err := store.Change(uri, 2, "two")
	require.NoError(t, err)
	require.Equal(t, int32(2), changed.Version)
	require.Equal(t, "two", changed.Text)
	require.Len(t, changed.Lines, 1)

	store.Close(uri)
	_, ok = store.Get(uri)
	require.False(t, ok)
}

func TestDocumentStoreRejectsNonFileURI(t *testing.T) {
	store := NewDocumentStore()
	_, err := store.Open("untitled:example.ub", 1, "")
	require.Error(t, err)
}

func TestFileURIToPathDecodesPath(t *testing.T) {
	got, err := FileURIToPath("file:///tmp/hello%20world.ub")
	require.NoError(t, err)
	require.Equal(t, filepath.FromSlash("/tmp/hello world.ub"), got)
}

func TestOffsetToLSP(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		offset int
		want   Position
	}{
		{name: "start of file", text: "alpha\nbeta", offset: 0, want: Position{Line: 0, Character: 0}},
		{name: "ascii character", text: "alpha\nbeta", offset: 3, want: Position{Line: 0, Character: 3}},
		{name: "next line", text: "alpha\nbeta", offset: 6, want: Position{Line: 1, Character: 0}},
		{
			name: "end of file", text: "alpha\nbeta", offset: len("alpha\nbeta"),
			want: Position{Line: 1, Character: 4},
		},
		{
			name: "multi byte utf8", text: "aé\n世", offset: len("aé\n世"),
			want: Position{Line: 1, Character: 1},
		},
		{name: "surrogate pair", text: "a😀b", offset: len("a😀"), want: Position{Line: 0, Character: 3}},
		{name: "inside multi byte utf8", text: "aé", offset: 2, want: Position{Line: 0, Character: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, OffsetToLSP(tt.text, tt.offset))
		})
	}
}

func TestLSPToOffset(t *testing.T) {
	tests := []struct {
		name string
		text string
		pos  Position
		want int
		ok   bool
	}{
		{
			name: "start of file", text: "alpha\nbeta",
			pos: Position{Line: 0, Character: 0}, want: 0, ok: true,
		},
		{
			name: "ascii character", text: "alpha\nbeta",
			pos: Position{Line: 0, Character: 3}, want: 3, ok: true,
		},
		{
			name: "next line", text: "alpha\nbeta",
			pos: Position{Line: 1, Character: 0}, want: 6, ok: true,
		},
		{
			name: "end of file", text: "alpha\nbeta",
			pos: Position{Line: 1, Character: 4}, want: len("alpha\nbeta"), ok: true,
		},
		{
			name: "after surrogate pair", text: "a😀b",
			pos: Position{Line: 0, Character: 3}, want: len("a😀"), ok: true,
		},
		{name: "inside surrogate pair", text: "a😀b", pos: Position{Line: 0, Character: 2}, ok: false},
		{name: "invalid line", text: "alpha\nbeta", pos: Position{Line: 2, Character: 0}, ok: false},
		{name: "past line end", text: "alpha\nbeta", pos: Position{Line: 1, Character: 5}, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := LSPToOffset(tt.text, tt.pos)
			require.Equal(t, tt.ok, ok)
			if tt.ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
}
