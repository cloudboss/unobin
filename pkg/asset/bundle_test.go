package asset

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func TestCollectionEncodeIsDeterministic(t *testing.T) {
	first := bundleDirectorySet(t)
	second := bundleFileSet(t, "config", "body\n")

	var forward Collection
	require.NoError(t, forward.Add(first))
	require.NoError(t, forward.Add(second))
	forwardBody, err := forward.Encode()
	require.NoError(t, err)

	var reverse Collection
	require.NoError(t, reverse.Add(second))
	require.NoError(t, reverse.Add(first))
	reverseBody, err := reverse.Encode()
	require.NoError(t, err)

	require.Equal(t, forwardBody, reverseBody)
	require.Equal(
		t,
		"0585f31765a619b514e7d1cdd4b72fd4"+
			"122cff7c1c2b731cc4c1fe863c5cd072",
		contentSHA256(forwardBody),
	)

	parts := decodeBundleParts(t, forwardBody)
	require.True(t, slices.IsSortedFunc(parts.manifest.AssetSets, func(a, b ManifestAssetSet) int {
		return strings.Compare(a.ID, b.ID)
	}))
	names := zipEntryNames(t, forwardBody)
	require.Equal(t, "manifest.json", names[0])
	require.True(t, slices.IsSorted(names[1:]))

	catalog, err := Open(forwardBody)
	require.NoError(t, err)
	require.NoError(t, catalog.Verify())
	require.Len(t, catalog.Sets(), 2)
}

func TestBundleManifestEndsWithNewlineAndUsesOneBlobPerContent(t *testing.T) {
	set := bundleCapture(t, mapFSWithEqualFiles(), []syntax.AssetDecl{
		captureDeclaration("first", "first"),
		captureDeclaration("second", "second"),
	})
	var collection Collection
	require.NoError(t, collection.Add(set))

	body, err := collection.Encode()
	require.NoError(t, err)
	files := unzipBundle(t, body)
	require.True(t, bytes.HasSuffix(files["manifest.json"], []byte("\n")))
	require.Len(t, files, 2)
	require.Contains(t, files, "blobs/"+contentSHA256([]byte("equal\n")))
}

func TestOpenDefersBlobContentValidationUntilVerify(t *testing.T) {
	body := encodedDirectoryBundle(t)
	parts := decodeBundleParts(t, body)
	for name, content := range parts.blobs {
		replacement := bytes.Repeat([]byte("x"), len(content))
		if bytes.Equal(replacement, content) {
			replacement[0] = 'y'
		}
		parts.blobs[name] = replacement
		break
	}
	corrupt := encodeBundleParts(t, parts.manifest, parts.blobs, nil)

	catalog, err := Open(corrupt)
	require.NoError(t, err)
	err = catalog.Verify()
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset bundle")
	require.Contains(t, err.Error(), "SHA-256")
}

func TestVerifyRejectsDirectoryContentSHA256(t *testing.T) {
	parts := decodeBundleParts(t, encodedDirectoryBundle(t))
	root := &parts.manifest.AssetSets[0].Assets[0].Entries[0]
	require.Equal(t, "", root.InternalPath)
	require.Equal(t, EntryKindDirectory, root.Kind)
	root.ContentSHA256 = strings.Repeat("0", 64)
	root.EntryIdentity = entryIdentity(root.Kind, root.Mode, root.ContentSHA256)
	recomputeManifestSetID(t, &parts.manifest.AssetSets[0])
	corrupt := encodeBundleParts(t, parts.manifest, parts.blobs, nil)

	catalog, err := Open(corrupt)
	require.NoError(t, err)
	err = catalog.Verify()
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset bundle")
	require.Contains(t, err.Error(), "directory content SHA-256")
}

func TestOpenRejectsCorruptManifestMetadata(t *testing.T) {
	valid := decodeBundleParts(t, encodedDirectoryBundle(t))
	tests := []struct {
		name   string
		mutate func(*bundleParts)
	}{
		{
			name: "format version",
			mutate: func(parts *bundleParts) {
				parts.manifest.FormatVersion++
			},
		},
		{
			name: "no asset sets",
			mutate: func(parts *bundleParts) {
				parts.manifest.AssetSets = nil
			},
		},
		{
			name: "duplicate set",
			mutate: func(parts *bundleParts) {
				parts.manifest.AssetSets = append(
					parts.manifest.AssetSets,
					parts.manifest.AssetSets[0],
				)
			},
		},
		{
			name: "set without assets",
			mutate: func(parts *bundleParts) {
				parts.manifest.AssetSets[0].Assets = nil
			},
		},
		{
			name: "duplicate asset",
			mutate: func(parts *bundleParts) {
				set := &parts.manifest.AssetSets[0]
				set.Assets = append(set.Assets, set.Assets[0])
			},
		},
		{
			name: "invalid asset name",
			mutate: func(parts *bundleParts) {
				parts.manifest.AssetSets[0].Assets[0].Name = "bad_name"
			},
		},
		{
			name: "asset without entries",
			mutate: func(parts *bundleParts) {
				parts.manifest.AssetSets[0].Assets[0].Entries = nil
			},
		},
		{
			name: "missing root",
			mutate: func(parts *bundleParts) {
				entries := parts.manifest.AssetSets[0].Assets[0].Entries
				parts.manifest.AssetSets[0].Assets[0].Entries = entries[1:]
			},
		},
		{
			name: "duplicate entry path",
			mutate: func(parts *bundleParts) {
				asset := &parts.manifest.AssetSets[0].Assets[0]
				asset.Entries = append(asset.Entries, asset.Entries[1])
				slices.SortFunc(asset.Entries, func(a, b ManifestEntry) int {
					return strings.Compare(a.InternalPath, b.InternalPath)
				})
			},
		},
		{
			name: "invalid entry path",
			mutate: func(parts *bundleParts) {
				entry := manifestFileEntryPointer(t, &parts.manifest)
				entry.InternalPath = "../file"
			},
		},
		{
			name: "invalid kind",
			mutate: func(parts *bundleParts) {
				parts.manifest.AssetSets[0].Assets[0].Entries[0].Kind = "other"
			},
		},
		{
			name: "invalid mode",
			mutate: func(parts *bundleParts) {
				parts.manifest.AssetSets[0].Assets[0].Entries[0].Mode = "0777"
			},
		},
		{
			name: "negative content size",
			mutate: func(parts *bundleParts) {
				parts.manifest.AssetSets[0].Assets[0].Entries[0].ContentSize = -1
			},
		},
		{
			name: "uppercase content SHA-256",
			mutate: func(parts *bundleParts) {
				entry := &parts.manifest.AssetSets[0].Assets[0].Entries[0]
				entry.ContentSHA256 = strings.Repeat("A", 64)
			},
		},
		{
			name: "entry identity",
			mutate: func(parts *bundleParts) {
				entry := &parts.manifest.AssetSets[0].Assets[0].Entries[0]
				entry.EntryIdentity = strings.Repeat("0", 64)
			},
		},
		{
			name: "set ID",
			mutate: func(parts *bundleParts) {
				parts.manifest.AssetSets[0].ID = strings.Repeat("0", 64)
			},
		},
		{
			name: "missing parent",
			mutate: func(parts *bundleParts) {
				asset := &parts.manifest.AssetSets[0].Assets[0]
				for i := range asset.Entries {
					if asset.Entries[i].InternalPath == "nested" {
						asset.Entries = slices.Delete(asset.Entries, i, i+1)
						return
					}
				}
			},
		},
		{
			name: "file without blob",
			mutate: func(parts *bundleParts) {
				entry := manifestFileEntry(t, &parts.manifest)
				delete(parts.blobs, entry.BlobPath)
			},
		},
		{
			name: "file with wrong blob path",
			mutate: func(parts *bundleParts) {
				entry := manifestFileEntryPointer(t, &parts.manifest)
				entry.BlobPath = "blobs/" + strings.Repeat("0", 64)
			},
		},
		{
			name: "file size mismatch",
			mutate: func(parts *bundleParts) {
				entry := manifestFileEntryPointer(t, &parts.manifest)
				entry.ContentSize++
			},
		},
		{
			name: "directory with blob",
			mutate: func(parts *bundleParts) {
				entry := &parts.manifest.AssetSets[0].Assets[0].Entries[0]
				entry.BlobPath = "blobs/" + entry.ContentSHA256
				parts.blobs[entry.BlobPath] = []byte("unexpected")
			},
		},
		{
			name: "noncanonical entry order",
			mutate: func(parts *bundleParts) {
				entries := parts.manifest.AssetSets[0].Assets[0].Entries
				slices.Reverse(entries)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := cloneBundleParts(t, valid)
			tt.mutate(&parts)
			body := encodeBundleParts(t, parts.manifest, parts.blobs, nil)
			_, err := Open(body)
			require.Error(t, err)
			require.Contains(t, err.Error(), "asset bundle")
		})
	}
}

func TestOpenRejectsNoncanonicalSetAndAssetOrder(t *testing.T) {
	first := bundleFileSet(t, "first", "one")
	second := bundleFileSet(t, "second", "two")
	var multipleSets Collection
	require.NoError(t, multipleSets.Add(first))
	require.NoError(t, multipleSets.Add(second))
	body, err := multipleSets.Encode()
	require.NoError(t, err)
	setParts := decodeBundleParts(t, body)
	slices.Reverse(setParts.manifest.AssetSets)
	_, err = Open(encodeBundleParts(t, setParts.manifest, setParts.blobs, nil))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not sorted")

	multipleAssets := bundleCapture(t, fstest.MapFS{
		"factory.ub": &fstest.MapFile{},
		"first":      &fstest.MapFile{Data: []byte("one")},
		"second":     &fstest.MapFile{Data: []byte("two")},
	}, []syntax.AssetDecl{
		captureDeclaration("first", "first"),
		captureDeclaration("second", "second"),
	})
	var oneSet Collection
	require.NoError(t, oneSet.Add(multipleAssets))
	body, err = oneSet.Encode()
	require.NoError(t, err)
	assetParts := decodeBundleParts(t, body)
	slices.Reverse(assetParts.manifest.AssetSets[0].Assets)
	_, err = Open(encodeBundleParts(t, assetParts.manifest, assetParts.blobs, nil))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not sorted")
}

func TestOpenRejectsInvalidContainerMembers(t *testing.T) {
	valid := decodeBundleParts(t, encodedDirectoryBundle(t))
	tests := []struct {
		name   string
		blobs  map[string][]byte
		extras map[string][]byte
	}{
		{
			name:   "unexpected member",
			blobs:  valid.blobs,
			extras: map[string][]byte{"notes.txt": []byte("unexpected")},
		},
		{
			name:  "unused blob",
			blobs: withBundleBlob(valid.blobs, strings.Repeat("f", 64), []byte("unused")),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := encodeBundleParts(t, valid.manifest, tt.blobs, tt.extras)
			_, err := Open(body)
			require.Error(t, err)
			require.Contains(t, err.Error(), "asset bundle")
		})
	}
}

func TestOpenRejectsMalformedOrMissingManifest(t *testing.T) {
	valid := decodeBundleParts(t, encodedDirectoryBundle(t))
	validJSON, err := json.Marshal(valid.manifest)
	require.NoError(t, err)
	validJSON = append(validJSON, '\n')
	tests := []struct {
		name     string
		manifest []byte
	}{
		{name: "malformed JSON", manifest: []byte("{")},
		{name: "missing newline", manifest: bytes.TrimSuffix(validJSON, []byte("\n"))},
		{
			name: "unknown field",
			manifest: bytes.Replace(
				validJSON,
				[]byte("{"),
				[]byte(`{"unknown":true,`),
				1,
			),
		},
		{
			name:     "multiple JSON values",
			manifest: append(slices.Clone(validJSON), []byte("{}\n")...),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := encodeRawBundle(t, tt.manifest, valid.blobs, nil)
			_, err := Open(body)
			require.Error(t, err)
			require.Contains(t, err.Error(), "asset bundle")
		})
	}

	missing := encodeRawBundle(t, nil, valid.blobs, nil)
	_, err = Open(missing)
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset bundle")

	_, err = Open([]byte("not a ZIP"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset bundle")
}

func TestValidAssetName(t *testing.T) {
	for _, name := range []string{"a", "Program1", "two--words"} {
		t.Run("valid "+name, func(t *testing.T) {
			require.True(t, validAssetName(name))
		})
	}
	for _, name := range []string{"", "1name", "bad_name", "trailing-", "@meta", "na.me"} {
		t.Run("invalid "+name, func(t *testing.T) {
			require.False(t, validAssetName(name))
		})
	}
}

type bundleParts struct {
	manifest Manifest
	blobs    map[string][]byte
}

func mapFSWithEqualFiles() fstest.MapFS {
	return fstest.MapFS{
		"factory.ub": &fstest.MapFile{},
		"first":      &fstest.MapFile{Data: []byte("equal\n")},
		"second":     &fstest.MapFile{Data: []byte("equal\n")},
	}
}

func encodedDirectoryBundle(t testing.TB) []byte {
	t.Helper()
	var collection Collection
	require.NoError(t, collection.Add(bundleDirectorySet(t)))
	body, err := collection.Encode()
	require.NoError(t, err)
	return body
}

func decodeBundleParts(t testing.TB, body []byte) bundleParts {
	t.Helper()
	files := unzipBundle(t, body)
	var manifest Manifest
	require.NoError(t, json.Unmarshal(files["manifest.json"], &manifest))
	delete(files, "manifest.json")
	return bundleParts{manifest: manifest, blobs: files}
}

func cloneBundleParts(t testing.TB, source bundleParts) bundleParts {
	t.Helper()
	body, err := json.Marshal(source.manifest)
	require.NoError(t, err)
	var manifest Manifest
	require.NoError(t, json.Unmarshal(body, &manifest))
	blobs := make(map[string][]byte, len(source.blobs))
	for name, content := range source.blobs {
		blobs[name] = slices.Clone(content)
	}
	return bundleParts{manifest: manifest, blobs: blobs}
}

func encodeBundleParts(
	t testing.TB,
	manifest Manifest,
	blobs map[string][]byte,
	extras map[string][]byte,
) []byte {
	t.Helper()
	body, err := json.Marshal(manifest)
	require.NoError(t, err)
	body = append(body, '\n')
	return encodeRawBundle(t, body, blobs, extras)
}

func encodeRawBundle(
	t testing.TB,
	manifest []byte,
	blobs map[string][]byte,
	extras map[string][]byte,
) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	if manifest != nil {
		writeBundleFixtureEntry(t, writer, "manifest.json", manifest)
	}
	names := make([]string, 0, len(blobs)+len(extras))
	for name := range blobs {
		names = append(names, name)
	}
	for name := range extras {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		content, ok := blobs[name]
		if !ok {
			content = extras[name]
		}
		writeBundleFixtureEntry(t, writer, name, content)
	}
	require.NoError(t, writer.Close())
	return output.Bytes()
}

func writeBundleFixtureEntry(
	t testing.TB,
	writer *zip.Writer,
	name string,
	content []byte,
) {
	t.Helper()
	header := &zip.FileHeader{
		Name:         name,
		Method:       zip.Store,
		ModifiedDate: 0x21,
	}
	header.SetMode(0444)
	stream, err := writer.CreateHeader(header)
	require.NoError(t, err)
	_, err = stream.Write(content)
	require.NoError(t, err)
}

func unzipBundle(t testing.TB, body []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)
	files := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		stream, err := file.Open()
		require.NoError(t, err)
		content, err := io.ReadAll(stream)
		require.NoError(t, err)
		require.NoError(t, stream.Close())
		files[file.Name] = content
	}
	return files
}

func zipEntryNames(t testing.TB, body []byte) []string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
		require.Equal(t, uint16(zip.Store), file.Method)
		require.Equal(t, fs.FileMode(0444), file.Mode().Perm())
		require.Equal(t, uint16(0x21), file.ModifiedDate)
		require.Zero(t, file.ModifiedTime)
		require.Empty(t, file.Extra)
		require.Empty(t, file.Comment)
	}
	return names
}

func recomputeManifestSetID(t testing.TB, set *ManifestAssetSet) {
	t.Helper()
	assets := make([]CapturedAsset, 0, len(set.Assets))
	for _, item := range set.Assets {
		for _, entry := range item.Entries {
			if entry.InternalPath == "" {
				assets = append(assets, CapturedAsset{
					Name: item.Name,
					Entries: []CapturedEntry{{
						EntryIdentity: entry.EntryIdentity,
					}},
				})
				break
			}
		}
	}
	id, err := assetSetID(assets)
	require.NoError(t, err)
	set.ID = id
}

func manifestFileEntry(t testing.TB, manifest *Manifest) ManifestEntry {
	t.Helper()
	for _, set := range manifest.AssetSets {
		for _, item := range set.Assets {
			for _, entry := range item.Entries {
				if entry.Kind == EntryKindFile {
					return entry
				}
			}
		}
	}
	t.Fatal("manifest has no file entry")
	return ManifestEntry{}
}

func manifestFileEntryPointer(t testing.TB, manifest *Manifest) *ManifestEntry {
	t.Helper()
	for i := range manifest.AssetSets {
		for j := range manifest.AssetSets[i].Assets {
			for k := range manifest.AssetSets[i].Assets[j].Entries {
				entry := &manifest.AssetSets[i].Assets[j].Entries[k]
				if entry.Kind == EntryKindFile {
					return entry
				}
			}
		}
	}
	t.Fatal("manifest has no file entry")
	return nil
}

func withBundleBlob(
	source map[string][]byte,
	hash string,
	content []byte,
) map[string][]byte {
	result := make(map[string][]byte, len(source)+1)
	maps.Copy(result, source)
	result["blobs/"+hash] = content
	return result
}
