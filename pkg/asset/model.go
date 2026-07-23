package asset

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

const FormatVersion = 1

type EntryKind string

const (
	EntryKindFile      EntryKind = "file"
	EntryKindDirectory EntryKind = "directory"
)

type CapturedEntry struct {
	InternalPath  string
	Kind          EntryKind
	Mode          string
	Content       []byte
	ContentSize   int64
	ContentSHA256 string
	EntryIdentity string
}

type CapturedAsset struct {
	Name    string
	Entries []CapturedEntry
}

type CapturedSet struct {
	ID     string
	Assets []CapturedAsset
}

type Manifest struct {
	FormatVersion int                `json:"format-version"`
	AssetSets     []ManifestAssetSet `json:"asset-sets"`
}

type ManifestAssetSet struct {
	ID     string          `json:"id"`
	Assets []ManifestAsset `json:"assets"`
}

type ManifestAsset struct {
	Name    string          `json:"name"`
	Entries []ManifestEntry `json:"entries"`
}

type ManifestEntry struct {
	InternalPath  string    `json:"internal-path"`
	Kind          EntryKind `json:"kind"`
	Mode          string    `json:"mode"`
	ContentSize   int64     `json:"content-size"`
	ContentSHA256 string    `json:"content-sha256"`
	EntryIdentity string    `json:"entry-identity"`
	BlobPath      string    `json:"blob-path"`
}

type Catalog struct {
	sets       map[string]*Set
	references map[string]*Reference
	readBlob   func(string) ([]byte, error)
}

type Set struct {
	ID      string
	assets  map[string]*Asset
	catalog *Catalog
}

type Asset struct {
	Name    string
	entries map[string]*Entry
}

type Entry struct {
	ManifestEntry
}

type PathRef string

type ContentRef string

type Value struct {
	Path          PathRef    `json:"path"`
	Content       ContentRef `json:"content"`
	ContentSHA256 string     `json:"content-sha256"`
	Mode          string     `json:"mode"`
}

type ReferenceKind string

const (
	ReferenceKindPath    ReferenceKind = "path"
	ReferenceKindContent ReferenceKind = "content"
)

type Reference struct {
	Token         string
	Kind          ReferenceKind
	EntryIdentity string
	AssetName     string
	InternalPath  string
	Asset         *Asset
	Entry         *Entry
}

func (c *Catalog) Set(id string) (*Set, bool) {
	if c == nil {
		return nil, false
	}
	set, ok := c.sets[id]
	return set, ok
}

func (c *Catalog) Sets() []*Set {
	if c == nil {
		return nil
	}
	sets := make([]*Set, 0, len(c.sets))
	for _, set := range c.sets {
		sets = append(sets, set)
	}
	slices.SortFunc(sets, func(a, b *Set) int {
		return strings.Compare(a.ID, b.ID)
	})
	return sets
}

func (c *Catalog) Reference(token string) (*Reference, bool) {
	if c == nil {
		return nil, false
	}
	reference, ok := c.references[token]
	return reference, ok
}

func (s *Set) Asset(name string) (*Asset, bool) {
	if s == nil {
		return nil, false
	}
	asset, ok := s.assets[name]
	return asset, ok
}

func (s *Set) Assets() []*Asset {
	if s == nil {
		return nil
	}
	assets := make([]*Asset, 0, len(s.assets))
	for _, asset := range s.assets {
		assets = append(assets, asset)
	}
	slices.SortFunc(assets, func(a, b *Asset) int {
		return strings.Compare(a.Name, b.Name)
	})
	return assets
}

func (s *Set) Catalog() *Catalog {
	if s == nil {
		return nil
	}
	return s.catalog
}

func (a *Asset) Entry(internalPath string) (*Entry, bool) {
	if a == nil {
		return nil, false
	}
	entry, ok := a.entries[internalPath]
	return entry, ok
}

func (a *Asset) Entries() []*Entry {
	if a == nil {
		return nil
	}
	entries := make([]*Entry, 0, len(a.entries))
	for _, entry := range a.entries {
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b *Entry) int {
		return strings.Compare(a.InternalPath, b.InternalPath)
	})
	return entries
}

func normalizeMode(mode fs.FileMode) (string, error) {
	if mode&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
		return "", fmt.Errorf("unsupported special permission bits in %s", mode)
	}
	switch {
	case mode.Type() == fs.ModeDir:
		return "0755", nil
	case mode.IsRegular() && mode.Perm()&0111 != 0:
		return "0755", nil
	case mode.IsRegular():
		return "0644", nil
	default:
		return "", fmt.Errorf("unsupported entry mode %s", mode)
	}
}
