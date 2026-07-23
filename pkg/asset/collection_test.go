package asset

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func TestCollectionDeduplicatesSetsAndBlobs(t *testing.T) {
	first := bundleCapture(t, fstest.MapFS{
		"factory.ub":  &fstest.MapFile{},
		"files/first": &fstest.MapFile{Data: []byte("same\n")},
		"files/other": &fstest.MapFile{Data: []byte("other\n")},
	}, []syntax.AssetDecl{
		captureDeclaration("first", "files/first"),
		captureDeclaration("other", "files/other"),
	})
	equal := bundleCapture(t, fstest.MapFS{
		"factory.ub":  &fstest.MapFile{},
		"files/first": &fstest.MapFile{Data: []byte("same\n")},
		"files/other": &fstest.MapFile{Data: []byte("other\n")},
	}, []syntax.AssetDecl{
		captureDeclaration("other", "files/other"),
		captureDeclaration("first", "files/first"),
	})
	second := bundleCapture(t, fstest.MapFS{
		"factory.ub": &fstest.MapFile{},
		"file":       &fstest.MapFile{Data: []byte("same\n")},
	}, []syntax.AssetDecl{
		captureDeclaration("shared", "file"),
	})

	var collection Collection
	require.NoError(t, collection.Add(first))
	require.NoError(t, collection.Add(equal))
	require.NoError(t, collection.Add(first))
	require.NoError(t, collection.Add(second))
	require.NoError(t, collection.Add(nil))

	require.Len(t, collection.sets, 2)
	require.Len(t, collection.blobs, 2)
	catalog := collection.Catalog()
	wantIDs := []string{first.ID, second.ID}
	slices.Sort(wantIDs)
	require.Equal(t, wantIDs, catalogSetIDs(catalog.Sets()))
}

func TestCollectionRetainsSetsWithSameAssetName(t *testing.T) {
	first := bundleFileSet(t, "program", "one\n")
	second := bundleFileSet(t, "program", "two\n")

	var collection Collection
	require.NoError(t, collection.Add(first))
	require.NoError(t, collection.Add(second))

	firstSet, ok := collection.Catalog().Set(first.ID)
	require.True(t, ok)
	firstAsset, ok := firstSet.Asset("program")
	require.True(t, ok)
	firstRoot, ok := firstAsset.Entry("")
	require.True(t, ok)

	secondSet, ok := collection.Catalog().Set(second.ID)
	require.True(t, ok)
	secondAsset, ok := secondSet.Asset("program")
	require.True(t, ok)
	secondRoot, ok := secondAsset.Entry("")
	require.True(t, ok)

	require.NotEqual(t, firstRoot.ContentSHA256, secondRoot.ContentSHA256)
}

func TestCollectionCatalogAccessorsReturnSortedValues(t *testing.T) {
	set := bundleCapture(t, fstest.MapFS{
		"factory.ub": &fstest.MapFile{},
		"tree":       &fstest.MapFile{Mode: fs.ModeDir},
		"tree/z":     &fstest.MapFile{Data: []byte("z")},
		"tree/a":     &fstest.MapFile{Data: []byte("a")},
		"file":       &fstest.MapFile{Data: []byte("file")},
	}, []syntax.AssetDecl{
		captureDeclaration("tree", "tree"),
		captureDeclaration("file", "file"),
	})
	var collection Collection
	require.NoError(t, collection.Add(set))

	catalog := collection.Catalog()
	sets := catalog.Sets()
	require.Len(t, sets, 1)
	require.Equal(t, set.ID, sets[0].ID)
	require.Equal(t, []string{"file", "tree"}, catalogAssetNames(sets[0].Assets()))
	tree, ok := sets[0].Asset("tree")
	require.True(t, ok)
	require.Equal(t, []string{"", "a", "z"}, catalogEntryPaths(tree.Entries()))
	_, ok = tree.Entry("missing")
	require.False(t, ok)
	_, ok = sets[0].Asset("missing")
	require.False(t, ok)
	_, ok = catalog.Set(strings.Repeat("0", 64))
	require.False(t, ok)
}

func TestCollectionRejectsInvalidCapturedData(t *testing.T) {
	valid := bundleDirectorySet(t)
	tests := []struct {
		name   string
		mutate func(*CapturedSet)
	}{
		{
			name: "set ID",
			mutate: func(set *CapturedSet) {
				set.ID = strings.Repeat("0", 64)
			},
		},
		{
			name: "entry identity",
			mutate: func(set *CapturedSet) {
				set.Assets[0].Entries[0].EntryIdentity = strings.Repeat("0", 64)
			},
		},
		{
			name: "file content",
			mutate: func(set *CapturedSet) {
				for i := range set.Assets[0].Entries {
					if set.Assets[0].Entries[i].Kind == EntryKindFile {
						set.Assets[0].Entries[i].Content = []byte("changed")
						return
					}
				}
			},
		},
		{
			name: "missing parent",
			mutate: func(set *CapturedSet) {
				entries := set.Assets[0].Entries
				for i := range entries {
					if entries[i].InternalPath == "nested" {
						set.Assets[0].Entries = slices.Delete(entries, i, i+1)
						return
					}
				}
			},
		},
		{
			name: "special directory content",
			mutate: func(set *CapturedSet) {
				set.Assets[0].Entries[0].Content = []byte("unexpected")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := cloneCapturedSet(t, valid)
			tt.mutate(set)
			var collection Collection
			require.Error(t, collection.Add(set))
		})
	}
}

func TestCollectionEncodeRejectsEmptyCollection(t *testing.T) {
	var collection Collection
	_, err := collection.Encode()
	require.Error(t, err)
}

func bundleFileSet(t testing.TB, name, content string) *CapturedSet {
	t.Helper()
	return bundleCapture(t, fstest.MapFS{
		"factory.ub": &fstest.MapFile{},
		"file":       &fstest.MapFile{Data: []byte(content)},
	}, []syntax.AssetDecl{
		captureDeclaration(name, "file"),
	})
}

func bundleDirectorySet(t testing.TB) *CapturedSet {
	t.Helper()
	return bundleCapture(t, fstest.MapFS{
		"factory.ub":       &fstest.MapFile{},
		"tree":             &fstest.MapFile{Mode: fs.ModeDir},
		"tree/nested":      &fstest.MapFile{Mode: fs.ModeDir},
		"tree/nested/file": &fstest.MapFile{Data: []byte("body\n")},
		"tree/empty":       &fstest.MapFile{Mode: fs.ModeDir},
	}, []syntax.AssetDecl{
		captureDeclaration("tree", "tree"),
	})
}

func bundleCapture(
	t testing.TB,
	projectFS fstest.MapFS,
	declarations []syntax.AssetDecl,
) *CapturedSet {
	t.Helper()
	set, err := Capture(
		captureProjectSource(projectFS),
		captureProjectFile("factory.ub"),
		declarations,
		"",
	)
	require.NoError(t, err)
	return set
}

func cloneCapturedSet(t testing.TB, source *CapturedSet) *CapturedSet {
	t.Helper()
	clone := &CapturedSet{
		ID:     source.ID,
		Assets: make([]CapturedAsset, len(source.Assets)),
	}
	for i, item := range source.Assets {
		clone.Assets[i] = CapturedAsset{
			Name:    item.Name,
			Entries: make([]CapturedEntry, len(item.Entries)),
		}
		for j, entry := range item.Entries {
			clone.Assets[i].Entries[j] = entry
			clone.Assets[i].Entries[j].Content = slices.Clone(entry.Content)
		}
	}
	return clone
}

func catalogSetIDs(sets []*Set) []string {
	ids := make([]string, 0, len(sets))
	for _, set := range sets {
		ids = append(ids, set.ID)
	}
	return ids
}

func catalogAssetNames(assets []*Asset) []string {
	names := make([]string, 0, len(assets))
	for _, item := range assets {
		names = append(names, item.Name)
	}
	return names
}

func catalogEntryPaths(entries []*Entry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.InternalPath)
	}
	return paths
}
