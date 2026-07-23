package asset

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	pathpkg "path"
	"slices"
	"strings"
)

func Open(body []byte) (*Catalog, error) {
	catalog, err := openBundle(body)
	if err != nil {
		return nil, fmt.Errorf("asset bundle: %w", err)
	}
	return catalog, nil
}

func (c *Catalog) Verify() error {
	if err := c.verify(); err != nil {
		return fmt.Errorf("asset bundle: %w", err)
	}
	return nil
}

func openBundle(body []byte) (*Catalog, error) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	var manifestFile *zip.File
	blobFiles := make(map[string]*zip.File)
	for _, file := range reader.File {
		switch {
		case file.Name == "manifest.json":
			if manifestFile != nil {
				return nil, fmt.Errorf("duplicate manifest.json")
			}
			if file.Method != zip.Store {
				return nil, fmt.Errorf("manifest.json is not stored")
			}
			manifestFile = file
		case validBlobPath(file.Name):
			if _, ok := blobFiles[file.Name]; ok {
				return nil, fmt.Errorf("duplicate bundle member %q", file.Name)
			}
			if file.Method != zip.Store && file.Method != zip.Deflate {
				return nil, fmt.Errorf("blob %q uses unsupported compression", file.Name)
			}
			blobFiles[file.Name] = file
		default:
			return nil, fmt.Errorf("unexpected ZIP member %q", file.Name)
		}
	}
	if manifestFile == nil {
		return nil, fmt.Errorf("missing manifest.json")
	}
	manifestBody, err := readZipFile(manifestFile)
	if err != nil {
		return nil, fmt.Errorf("read manifest.json: %w", err)
	}
	manifest, err := decodeManifest(manifestBody)
	if err != nil {
		return nil, err
	}
	expectedBlobs, err := validateManifest(manifest, blobFiles)
	if err != nil {
		return nil, err
	}
	for name := range blobFiles {
		if _, ok := expectedBlobs[name]; !ok {
			return nil, fmt.Errorf("unexpected blob %q", name)
		}
	}

	catalog := catalogFromManifest(manifest)
	catalog.readBlob = func(name string) ([]byte, error) {
		file := blobFiles[name]
		if file == nil {
			return nil, fmt.Errorf("missing blob %q", name)
		}
		return readZipFile(file)
	}
	return catalog, nil
}

func decodeManifest(body []byte) (Manifest, error) {
	if !bytes.HasSuffix(body, []byte("\n")) {
		return Manifest{}, fmt.Errorf("manifest.json does not end with a newline")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest.json: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Manifest{}, fmt.Errorf("decode manifest.json: multiple JSON values")
		}
		return Manifest{}, fmt.Errorf("decode manifest.json: %w", err)
	}
	return manifest, nil
}

func validateManifest(
	manifest Manifest,
	blobFiles map[string]*zip.File,
) (map[string]struct{}, error) {
	if manifest.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("unsupported format version %d", manifest.FormatVersion)
	}
	if len(manifest.AssetSets) == 0 {
		return nil, fmt.Errorf("manifest has no asset sets")
	}
	if !slices.IsSortedFunc(manifest.AssetSets, func(a, b ManifestAssetSet) int {
		return strings.Compare(a.ID, b.ID)
	}) {
		return nil, fmt.Errorf("asset sets are not sorted by ID")
	}

	expectedBlobs := make(map[string]struct{})
	setIDs := make(map[string]struct{}, len(manifest.AssetSets))
	for _, set := range manifest.AssetSets {
		if !validSHA256(set.ID) {
			return nil, fmt.Errorf("invalid asset-set ID %q", set.ID)
		}
		if _, ok := setIDs[set.ID]; ok {
			return nil, fmt.Errorf("duplicate asset-set ID %q", set.ID)
		}
		setIDs[set.ID] = struct{}{}
		if err := validateManifestSet(set, blobFiles, expectedBlobs); err != nil {
			return nil, err
		}
	}
	return expectedBlobs, nil
}

func validateManifestSet(
	set ManifestAssetSet,
	blobFiles map[string]*zip.File,
	expectedBlobs map[string]struct{},
) error {
	if len(set.Assets) == 0 {
		return fmt.Errorf("asset set %q has no assets", set.ID)
	}
	if !slices.IsSortedFunc(set.Assets, func(a, b ManifestAsset) int {
		return strings.Compare(a.Name, b.Name)
	}) {
		return fmt.Errorf("asset set %q assets are not sorted by name", set.ID)
	}
	names := make(map[string]struct{}, len(set.Assets))
	for _, item := range set.Assets {
		if !validAssetName(item.Name) {
			return fmt.Errorf("asset set %q has invalid asset name %q", set.ID, item.Name)
		}
		if _, ok := names[item.Name]; ok {
			return fmt.Errorf("asset set %q has duplicate asset name %q", set.ID, item.Name)
		}
		names[item.Name] = struct{}{}
		if err := validateManifestAsset(set.ID, item, blobFiles, expectedBlobs); err != nil {
			return err
		}
	}
	expectedID, err := manifestSetID(set.Assets)
	if err != nil {
		return err
	}
	if set.ID != expectedID {
		return fmt.Errorf("asset-set ID %q does not match %q", set.ID, expectedID)
	}
	return nil
}

func validateManifestAsset(
	setID string,
	item ManifestAsset,
	blobFiles map[string]*zip.File,
	expectedBlobs map[string]struct{},
) error {
	if len(item.Entries) == 0 {
		return fmt.Errorf("asset set %q asset %q has no entries", setID, item.Name)
	}
	if !slices.IsSortedFunc(item.Entries, func(a, b ManifestEntry) int {
		return strings.Compare(a.InternalPath, b.InternalPath)
	}) {
		return fmt.Errorf(
			"asset set %q asset %q entries are not sorted by internal path",
			setID,
			item.Name,
		)
	}
	byPath := make(map[string]ManifestEntry, len(item.Entries))
	for _, entry := range item.Entries {
		if _, ok := byPath[entry.InternalPath]; ok {
			return fmt.Errorf(
				"asset set %q asset %q has duplicate entry path %q",
				setID,
				item.Name,
				entry.InternalPath,
			)
		}
		if entry.InternalPath != "" && !validInternalPath(entry.InternalPath) {
			return fmt.Errorf(
				"asset set %q asset %q has invalid entry path %q",
				setID,
				item.Name,
				entry.InternalPath,
			)
		}
		if _, err := archiveFileMode(CapturedEntry{
			InternalPath: entry.InternalPath,
			Kind:         entry.Kind,
			Mode:         entry.Mode,
		}); err != nil {
			return err
		}
		if entry.ContentSize < 0 {
			return fmt.Errorf(
				"asset set %q asset %q entry %q has negative content size",
				setID,
				item.Name,
				entry.InternalPath,
			)
		}
		if !validSHA256(entry.ContentSHA256) {
			return fmt.Errorf(
				"asset set %q asset %q entry %q has invalid content SHA-256",
				setID,
				item.Name,
				entry.InternalPath,
			)
		}
		if !validSHA256(entry.EntryIdentity) {
			return fmt.Errorf(
				"asset set %q asset %q entry %q has invalid identity",
				setID,
				item.Name,
				entry.InternalPath,
			)
		}
		expectedIdentity := entryIdentity(entry.Kind, entry.Mode, entry.ContentSHA256)
		if entry.EntryIdentity != expectedIdentity {
			return fmt.Errorf(
				"asset set %q asset %q entry %q identity does not match",
				setID,
				item.Name,
				entry.InternalPath,
			)
		}
		switch entry.Kind {
		case EntryKindFile:
			expectedBlobPath := "blobs/" + entry.ContentSHA256
			if entry.BlobPath != expectedBlobPath {
				return fmt.Errorf(
					"asset set %q asset %q entry %q has invalid blob path %q",
					setID,
					item.Name,
					entry.InternalPath,
					entry.BlobPath,
				)
			}
			blobFile := blobFiles[entry.BlobPath]
			if blobFile == nil {
				return fmt.Errorf("missing blob %q", entry.BlobPath)
			}
			if blobFile.UncompressedSize64 != uint64(entry.ContentSize) {
				return fmt.Errorf("blob %q size does not match manifest", entry.BlobPath)
			}
			expectedBlobs[entry.BlobPath] = struct{}{}
		case EntryKindDirectory:
			if entry.BlobPath != "" {
				return fmt.Errorf(
					"asset set %q asset %q directory %q has a blob path",
					setID,
					item.Name,
					entry.InternalPath,
				)
			}
		}
		byPath[entry.InternalPath] = entry
	}
	if _, ok := byPath[""]; !ok {
		return fmt.Errorf("asset set %q asset %q has no root entry", setID, item.Name)
	}
	for path := range byPath {
		if path == "" {
			continue
		}
		parentPath := pathpkg.Dir(path)
		if parentPath == "." {
			parentPath = ""
		}
		parent, ok := byPath[parentPath]
		if !ok {
			return fmt.Errorf(
				"asset set %q asset %q entry %q has no parent %q",
				setID,
				item.Name,
				path,
				parentPath,
			)
		}
		if parent.Kind != EntryKindDirectory {
			return fmt.Errorf(
				"asset set %q asset %q entry %q has non-directory parent %q",
				setID,
				item.Name,
				path,
				parentPath,
			)
		}
	}
	return nil
}

func validAssetName(name string) bool {
	if len(name) == 0 || !asciiLetter(name[0]) || name[len(name)-1] == '-' {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !asciiLetter(name[i]) && (name[i] < '0' || name[i] > '9') && name[i] != '-' {
			return false
		}
	}
	return true
}

func asciiLetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func validBlobPath(name string) bool {
	hash, ok := strings.CutPrefix(name, "blobs/")
	return ok && validSHA256(hash)
}

func manifestSetID(assets []ManifestAsset) (string, error) {
	captured := make([]CapturedAsset, 0, len(assets))
	for _, item := range assets {
		entries := make([]CapturedEntry, 0, 1)
		for _, entry := range item.Entries {
			if entry.InternalPath == "" {
				entries = append(entries, CapturedEntry{
					EntryIdentity: entry.EntryIdentity,
				})
			}
		}
		captured = append(captured, CapturedAsset{Name: item.Name, Entries: entries})
	}
	return assetSetID(captured)
}

func catalogFromManifest(manifest Manifest) *Catalog {
	catalog := &Catalog{sets: make(map[string]*Set, len(manifest.AssetSets))}
	for _, manifestSet := range manifest.AssetSets {
		set := &Set{
			ID:     manifestSet.ID,
			assets: make(map[string]*Asset, len(manifestSet.Assets)),
		}
		for _, manifestAsset := range manifestSet.Assets {
			item := &Asset{
				Name:    manifestAsset.Name,
				entries: make(map[string]*Entry, len(manifestAsset.Entries)),
			}
			for _, manifestEntry := range manifestAsset.Entries {
				entry := manifestEntry
				item.entries[entry.InternalPath] = &Entry{ManifestEntry: entry}
			}
			set.assets[item.Name] = item
		}
		catalog.sets[set.ID] = set
	}
	return catalog
}

func (c *Catalog) verify() error {
	if c == nil {
		return fmt.Errorf("missing catalog")
	}
	if c.readBlob == nil {
		return fmt.Errorf("catalog has no blob reader")
	}
	blobs := make(map[string][]byte)
	for _, set := range c.Sets() {
		for _, item := range set.Assets() {
			captured := make([]CapturedEntry, 0, len(item.entries))
			for _, entry := range item.Entries() {
				capturedEntry := CapturedEntry{
					InternalPath:  entry.InternalPath,
					Kind:          entry.Kind,
					Mode:          entry.Mode,
					ContentSize:   entry.ContentSize,
					ContentSHA256: entry.ContentSHA256,
					EntryIdentity: entry.EntryIdentity,
				}
				if entry.Kind == EntryKindFile {
					content, ok := blobs[entry.BlobPath]
					if !ok {
						var err error
						content, err = c.readBlob(entry.BlobPath)
						if err != nil {
							return err
						}
						if int64(len(content)) != entry.ContentSize {
							return fmt.Errorf("blob %q content size does not match", entry.BlobPath)
						}
						if contentSHA256(content) != entry.ContentSHA256 {
							return fmt.Errorf("blob %q SHA-256 does not match", entry.BlobPath)
						}
						blobs[entry.BlobPath] = content
					}
					capturedEntry.Content = content
				}
				captured = append(captured, capturedEntry)
			}
			for _, entry := range captured {
				if entry.Kind != EntryKindDirectory {
					continue
				}
				archiveEntries := directoryArchiveEntries(captured, entry.InternalPath)
				archive, err := canonicalDirectoryArchive(archiveEntries)
				if err != nil {
					return err
				}
				if int64(len(archive)) != entry.ContentSize {
					return fmt.Errorf(
						"asset %q directory %q content size does not match",
						item.Name,
						entry.InternalPath,
					)
				}
				if contentSHA256(archive) != entry.ContentSHA256 {
					return fmt.Errorf(
						"asset %q directory %q directory content SHA-256 does not match",
						item.Name,
						entry.InternalPath,
					)
				}
			}
		}
	}
	return nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	stream, err := file.Open()
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(stream)
	closeErr := stream.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return body, nil
}
