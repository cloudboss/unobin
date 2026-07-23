package asset

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"reflect"
	"slices"
	"strings"
)

type Collection struct {
	sets  map[string]CapturedSet
	blobs map[string][]byte
}

func (c *Collection) Add(set *CapturedSet) error {
	if set == nil {
		return nil
	}
	canonical, blobs, err := canonicalCapturedSet(set)
	if err != nil {
		return fmt.Errorf("add asset set: %w", err)
	}
	if c.sets == nil {
		c.sets = make(map[string]CapturedSet)
		c.blobs = make(map[string][]byte)
	}
	if existing, ok := c.sets[canonical.ID]; ok {
		if !reflect.DeepEqual(existing, canonical) {
			return fmt.Errorf("add asset set: conflicting set ID %q", canonical.ID)
		}
		return nil
	}
	for hash, content := range blobs {
		if existing, ok := c.blobs[hash]; ok && !bytes.Equal(existing, content) {
			return fmt.Errorf("add asset set: conflicting blob SHA-256 %q", hash)
		}
	}
	c.sets[canonical.ID] = canonical
	for hash, content := range blobs {
		if _, ok := c.blobs[hash]; !ok {
			c.blobs[hash] = slices.Clone(content)
		}
	}
	return nil
}

func (c *Collection) Catalog() *Catalog {
	manifest := c.manifest()
	catalog := catalogFromManifest(manifest)
	var blobs map[string][]byte
	if c != nil {
		blobs = c.blobs
	}
	catalog.readBlob = func(name string) ([]byte, error) {
		hash, ok := strings.CutPrefix(name, "blobs/")
		if !ok {
			return nil, fmt.Errorf("invalid blob path %q", name)
		}
		content, ok := blobs[hash]
		if !ok {
			return nil, fmt.Errorf("missing blob %q", name)
		}
		return slices.Clone(content), nil
	}
	return catalog
}

func (c *Collection) Encode() ([]byte, error) {
	if c == nil || len(c.sets) == 0 {
		return nil, fmt.Errorf("encode asset bundle: collection is empty")
	}
	manifestBody, err := json.Marshal(c.manifest())
	if err != nil {
		return nil, fmt.Errorf("encode asset bundle manifest: %w", err)
	}
	manifestBody = append(manifestBody, '\n')

	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	if err := writeBundleEntry(writer, "manifest.json", manifestBody); err != nil {
		return nil, err
	}
	hashes := make([]string, 0, len(c.blobs))
	for hash := range c.blobs {
		hashes = append(hashes, hash)
	}
	slices.Sort(hashes)
	for _, hash := range hashes {
		if err := writeBundleEntry(writer, "blobs/"+hash, c.blobs[hash]); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close asset bundle: %w", err)
	}
	body := output.Bytes()
	catalog, err := Open(body)
	if err != nil {
		return nil, err
	}
	if err := catalog.Verify(); err != nil {
		return nil, err
	}
	return body, nil
}

func canonicalCapturedSet(source *CapturedSet) (CapturedSet, map[string][]byte, error) {
	canonical := CapturedSet{
		ID:     source.ID,
		Assets: make([]CapturedAsset, len(source.Assets)),
	}
	for i, item := range source.Assets {
		canonical.Assets[i] = CapturedAsset{
			Name:    item.Name,
			Entries: make([]CapturedEntry, len(item.Entries)),
		}
		for j, entry := range item.Entries {
			canonical.Assets[i].Entries[j] = entry
			canonical.Assets[i].Entries[j].Content = slices.Clone(entry.Content)
		}
		slices.SortFunc(canonical.Assets[i].Entries, func(a, b CapturedEntry) int {
			return strings.Compare(a.InternalPath, b.InternalPath)
		})
	}
	slices.SortFunc(canonical.Assets, func(a, b CapturedAsset) int {
		return strings.Compare(a.Name, b.Name)
	})

	blobs := make(map[string][]byte)
	names := make(map[string]struct{}, len(canonical.Assets))
	for _, item := range canonical.Assets {
		if !validAssetName(item.Name) {
			return CapturedSet{}, nil, fmt.Errorf("invalid asset name %q", item.Name)
		}
		if _, ok := names[item.Name]; ok {
			return CapturedSet{}, nil, fmt.Errorf("duplicate asset name %q", item.Name)
		}
		names[item.Name] = struct{}{}
		itemBlobs, err := validateCapturedAsset(item)
		if err != nil {
			return CapturedSet{}, nil, fmt.Errorf("asset %q: %w", item.Name, err)
		}
		for hash, content := range itemBlobs {
			if existing, ok := blobs[hash]; ok && !bytes.Equal(existing, content) {
				return CapturedSet{}, nil, fmt.Errorf("conflicting blob SHA-256 %q", hash)
			}
			blobs[hash] = content
		}
	}
	expectedID, err := assetSetID(canonical.Assets)
	if err != nil {
		return CapturedSet{}, nil, err
	}
	if canonical.ID != expectedID {
		return CapturedSet{}, nil, fmt.Errorf(
			"asset-set ID %q does not match %q",
			canonical.ID,
			expectedID,
		)
	}
	return canonical, blobs, nil
}

func validateCapturedAsset(item CapturedAsset) (map[string][]byte, error) {
	if len(item.Entries) == 0 {
		return nil, fmt.Errorf("has no entries")
	}
	byPath := make(map[string]CapturedEntry, len(item.Entries))
	blobs := make(map[string][]byte)
	for _, entry := range item.Entries {
		if _, ok := byPath[entry.InternalPath]; ok {
			return nil, fmt.Errorf("duplicate entry path %q", entry.InternalPath)
		}
		if entry.InternalPath != "" && !validInternalPath(entry.InternalPath) {
			return nil, fmt.Errorf("invalid entry path %q", entry.InternalPath)
		}
		if _, err := archiveFileMode(entry); err != nil {
			return nil, err
		}
		if entry.ContentSize < 0 {
			return nil, fmt.Errorf("entry %q has negative content size", entry.InternalPath)
		}
		if !validSHA256(entry.ContentSHA256) {
			return nil, fmt.Errorf("entry %q has invalid content SHA-256", entry.InternalPath)
		}
		if !validSHA256(entry.EntryIdentity) {
			return nil, fmt.Errorf("entry %q has invalid identity", entry.InternalPath)
		}
		expectedIdentity := entryIdentity(entry.Kind, entry.Mode, entry.ContentSHA256)
		if entry.EntryIdentity != expectedIdentity {
			return nil, fmt.Errorf("entry %q identity does not match", entry.InternalPath)
		}
		switch entry.Kind {
		case EntryKindFile:
			if int64(len(entry.Content)) != entry.ContentSize {
				return nil, fmt.Errorf("entry %q content size does not match", entry.InternalPath)
			}
			if contentSHA256(entry.Content) != entry.ContentSHA256 {
				return nil, fmt.Errorf("entry %q content SHA-256 does not match", entry.InternalPath)
			}
			blobs[entry.ContentSHA256] = slices.Clone(entry.Content)
		case EntryKindDirectory:
			if len(entry.Content) != 0 {
				return nil, fmt.Errorf("directory %q has content bytes", entry.InternalPath)
			}
		}
		byPath[entry.InternalPath] = entry
	}
	if _, ok := byPath[""]; !ok {
		return nil, fmt.Errorf("has no root entry")
	}
	for internalPath := range byPath {
		if internalPath == "" {
			continue
		}
		parentPath := pathpkg.Dir(internalPath)
		if parentPath == "." {
			parentPath = ""
		}
		parent, ok := byPath[parentPath]
		if !ok {
			return nil, fmt.Errorf("entry %q has no parent %q", internalPath, parentPath)
		}
		if parent.Kind != EntryKindDirectory {
			return nil, fmt.Errorf(
				"entry %q has non-directory parent %q",
				internalPath,
				parentPath,
			)
		}
	}
	for _, entry := range item.Entries {
		if entry.Kind != EntryKindDirectory {
			continue
		}
		archiveEntries := directoryArchiveEntries(item.Entries, entry.InternalPath)
		archive, err := canonicalDirectoryArchive(archiveEntries)
		if err != nil {
			return nil, err
		}
		if int64(len(archive)) != entry.ContentSize {
			return nil, fmt.Errorf("directory %q content size does not match", entry.InternalPath)
		}
		if contentSHA256(archive) != entry.ContentSHA256 {
			return nil, fmt.Errorf(
				"directory %q content SHA-256 does not match",
				entry.InternalPath,
			)
		}
	}
	return blobs, nil
}

func (c *Collection) manifest() Manifest {
	manifest := Manifest{FormatVersion: FormatVersion}
	if c == nil {
		return manifest
	}
	manifest.AssetSets = make([]ManifestAssetSet, 0, len(c.sets))
	for _, set := range c.sets {
		manifestSet := ManifestAssetSet{
			ID:     set.ID,
			Assets: make([]ManifestAsset, 0, len(set.Assets)),
		}
		for _, item := range set.Assets {
			manifestAsset := ManifestAsset{
				Name:    item.Name,
				Entries: make([]ManifestEntry, 0, len(item.Entries)),
			}
			for _, entry := range item.Entries {
				blobPath := ""
				if entry.Kind == EntryKindFile {
					blobPath = "blobs/" + entry.ContentSHA256
				}
				manifestAsset.Entries = append(manifestAsset.Entries, ManifestEntry{
					InternalPath:  entry.InternalPath,
					Kind:          entry.Kind,
					Mode:          entry.Mode,
					ContentSize:   entry.ContentSize,
					ContentSHA256: entry.ContentSHA256,
					EntryIdentity: entry.EntryIdentity,
					BlobPath:      blobPath,
				})
			}
			manifestSet.Assets = append(manifestSet.Assets, manifestAsset)
		}
		manifest.AssetSets = append(manifest.AssetSets, manifestSet)
	}
	slices.SortFunc(manifest.AssetSets, func(a, b ManifestAssetSet) int {
		return strings.Compare(a.ID, b.ID)
	})
	return manifest
}

func writeBundleEntry(writer *zip.Writer, name string, content []byte) error {
	header := &zip.FileHeader{
		Name:         name,
		Method:       zip.Store,
		ModifiedDate: 0x21,
	}
	header.SetMode(0444)
	stream, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create asset bundle entry %q: %w", name, err)
	}
	if _, err := stream.Write(content); err != nil {
		return fmt.Errorf("write asset bundle entry %q: %w", name, err)
	}
	return nil
}
