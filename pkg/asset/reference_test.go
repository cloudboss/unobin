package asset

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func TestCatalogBuildsLogicalReferences(t *testing.T) {
	captured := bundleDirectorySet(t)
	var collection Collection
	require.NoError(t, collection.Add(captured))
	catalog := collection.Catalog()
	set, ok := catalog.Set(captured.ID)
	require.True(t, ok)

	root, err := set.Value("tree", "")
	require.NoError(t, err)
	selected, err := set.Value("tree", "nested/file")
	require.NoError(t, err)
	directory, err := set.Value("tree", "nested")
	require.NoError(t, err)

	require.Equal(t, "<asset.tree.path>", DisplayReference(string(root.Path)))
	require.Equal(t, "<asset.tree.content>", DisplayReference(string(root.Content)))
	require.Equal(
		t,
		"<asset.tree['nested/file'].path>",
		DisplayReference(string(selected.Path)),
	)
	require.Equal(
		t,
		"<asset.tree['nested/file'].content>",
		DisplayReference(string(selected.Content)),
	)
	require.Equal(t, "0755", directory.Mode)
	require.Equal(
		t,
		"<asset.tree['nested'].content>",
		DisplayReference(string(directory.Content)),
	)

	pathReference, ok := catalog.Reference(string(selected.Path))
	require.True(t, ok)
	require.Equal(t, ReferenceKindPath, pathReference.Kind)
	require.Equal(t, "tree", pathReference.AssetName)
	require.Equal(t, "nested/file", pathReference.InternalPath)
	require.Equal(t, selected.ContentSHA256, pathReference.Entry.ContentSHA256)

	contentReference, ok := catalog.Reference(string(selected.Content))
	require.True(t, ok)
	require.Equal(t, ReferenceKindContent, contentReference.Kind)
	require.Same(t, pathReference.Entry, contentReference.Entry)
	require.Same(t, pathReference.Asset, contentReference.Asset)
}

func TestLogicalReferenceIdentityTracksOnlyItsSelectedEntry(t *testing.T) {
	first := referenceDirectorySet(t, "first\n", "stable\n")
	second := referenceDirectorySet(t, "second\n", "stable\n")

	var collection Collection
	require.NoError(t, collection.Add(first))
	require.NoError(t, collection.Add(second))
	catalog := collection.Catalog()
	firstSet, ok := catalog.Set(first.ID)
	require.True(t, ok)
	secondSet, ok := catalog.Set(second.ID)
	require.True(t, ok)

	firstRoot, err := firstSet.Value("tree", "")
	require.NoError(t, err)
	secondRoot, err := secondSet.Value("tree", "")
	require.NoError(t, err)
	firstChanged, err := firstSet.Value("tree", "changed.txt")
	require.NoError(t, err)
	secondChanged, err := secondSet.Value("tree", "changed.txt")
	require.NoError(t, err)
	firstStable, err := firstSet.Value("tree", "stable.txt")
	require.NoError(t, err)
	secondStable, err := secondSet.Value("tree", "stable.txt")
	require.NoError(t, err)

	require.NotEqual(t, firstRoot.Path, secondRoot.Path)
	require.NotEqual(t, firstChanged.Content, secondChanged.Content)
	require.Equal(t, firstStable.Path, secondStable.Path)
	require.Equal(t, firstStable.Content, secondStable.Content)
}

func TestLogicalReferenceRejectsInvalidSelections(t *testing.T) {
	captured := bundleDirectorySet(t)
	var collection Collection
	require.NoError(t, collection.Add(captured))
	set, ok := collection.Catalog().Set(captured.ID)
	require.True(t, ok)

	tests := []struct {
		name         string
		assetName    string
		internalPath string
		want         string
	}{
		{
			name:      "unknown asset",
			assetName: "missing",
			want:      `asset "missing" is not in asset set`,
		},
		{
			name:         "unknown entry",
			assetName:    "tree",
			internalPath: "missing",
			want:         `asset "tree" has no entry "missing"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := set.Value(tt.assetName, tt.internalPath)
			require.EqualError(t, err, tt.want)
		})
	}
}

func TestCatalogReadsLogicalContentReferences(t *testing.T) {
	captured := bundleDirectorySet(t)
	var collection Collection
	require.NoError(t, collection.Add(captured))
	catalog := collection.Catalog()
	set, ok := catalog.Set(captured.ID)
	require.True(t, ok)

	file, err := set.Value("tree", "nested/file")
	require.NoError(t, err)
	fileContent, err := catalog.Content(string(file.Content))
	require.NoError(t, err)
	require.Equal(t, []byte("body\n"), fileContent)

	directory, err := set.Value("tree", "nested")
	require.NoError(t, err)
	directoryContent, err := catalog.Content(string(directory.Content))
	require.NoError(t, err)
	require.Equal(t, directory.ContentSHA256, contentSHA256(directoryContent))

	root, err := set.Value("tree", "")
	require.NoError(t, err)
	rootContent, err := catalog.Content(string(root.Content))
	require.NoError(t, err)
	require.Equal(t, root.ContentSHA256, contentSHA256(rootContent))
	require.True(t, bytes.HasPrefix(rootContent, []byte("PK")))
}

func TestCatalogRejectsCorruptReferencedContent(t *testing.T) {
	captured := bundleFileSet(t, "program", "expected\n")
	var collection Collection
	require.NoError(t, collection.Add(captured))
	catalog := collection.Catalog()
	set, ok := catalog.Set(captured.ID)
	require.True(t, ok)
	value, err := set.Value("program", "")
	require.NoError(t, err)

	for hash := range collection.blobs {
		collection.blobs[hash] = []byte("corrupt\n")
	}

	_, err = catalog.Content(string(value.Content))
	require.EqualError(
		t,
		err,
		`asset content <asset.program.content>: content size does not match`,
	)
}

func TestCatalogRejectsUnknownLogicalContentReference(t *testing.T) {
	captured := bundleFileSet(t, "program", "expected\n")
	var collection Collection
	require.NoError(t, collection.Add(captured))
	catalog := collection.Catalog()
	set, ok := catalog.Set(captured.ID)
	require.True(t, ok)
	value, err := set.Value("program", "")
	require.NoError(t, err)
	reference, ok := ParseReference(string(value.Content))
	require.True(t, ok)
	unknown := referenceToken(
		reference.Kind,
		strings.Repeat("0", 64),
		reference.AssetName,
		reference.InternalPath,
	)

	_, err = catalog.Content(unknown)
	require.EqualError(
		t,
		err,
		`asset content <asset.program.content>: not found in asset catalog`,
	)
}

func TestParseReferenceRejectsMalformedTokens(t *testing.T) {
	identity := strings.Repeat("0", 64)
	tests := []string{
		"",
		"plain string",
		"unobin-asset:",
		"unobin-asset:v2:path:" + identity + ":dHJlZQA",
		"unobin-asset:v1:other:" + identity + ":dHJlZQA",
		"unobin-asset:v1:path:short:dHJlZQA",
		"unobin-asset:v1:path:" + identity + ":%%%",
		"unobin-asset:v1:path:" + identity + ":dHJlZQ",
		"unobin-asset:v1:path:" + identity + ":AHRyZWU",
	}
	for _, token := range tests {
		t.Run(token, func(t *testing.T) {
			_, ok := ParseReference(token)
			require.False(t, ok)
			require.Equal(t, token, DisplayReference(token))
		})
	}
}

func referenceDirectorySet(t testing.TB, changed, stable string) *CapturedSet {
	t.Helper()
	return bundleCapture(t, fstest.MapFS{
		"factory.ub":        &fstest.MapFile{},
		"tree":              &fstest.MapFile{Mode: fs.ModeDir},
		"tree/changed.txt":  &fstest.MapFile{Data: []byte(changed)},
		"tree/stable.txt":   &fstest.MapFile{Data: []byte(stable)},
		"tree/subdirectory": &fstest.MapFile{Mode: fs.ModeDir},
	}, []syntax.AssetDecl{
		captureDeclaration("tree", "tree"),
	})
}
