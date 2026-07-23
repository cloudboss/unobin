package asset

import (
	"fmt"
	"io/fs"
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
