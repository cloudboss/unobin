package asset

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"slices"
)

func contentSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func entryIdentity(kind EntryKind, mode, contentSum string) string {
	h := sha256.New()
	writeFrame(h, "unobin-asset-entry-v1")
	writeFrame(h, string(kind))
	writeFrame(h, mode)
	writeFrame(h, contentSum)
	return hex.EncodeToString(h.Sum(nil))
}

func assetSetID(assets []CapturedAsset) (string, error) {
	if len(assets) == 0 {
		return "", fmt.Errorf("asset set is empty")
	}
	type assetRoot struct {
		name     string
		identity string
	}
	roots := make([]assetRoot, 0, len(assets))
	names := make(map[string]struct{}, len(assets))
	for _, item := range assets {
		if item.Name == "" {
			return "", fmt.Errorf("asset name is empty")
		}
		if _, ok := names[item.Name]; ok {
			return "", fmt.Errorf("duplicate asset name %q", item.Name)
		}
		names[item.Name] = struct{}{}

		var root *CapturedEntry
		for i := range item.Entries {
			if item.Entries[i].InternalPath != "" {
				continue
			}
			if root != nil {
				return "", fmt.Errorf("asset %q has multiple root entries", item.Name)
			}
			root = &item.Entries[i]
		}
		if root == nil {
			return "", fmt.Errorf("asset %q has no root entry", item.Name)
		}
		if !validSHA256(root.EntryIdentity) {
			return "", fmt.Errorf("asset %q has invalid root entry identity", item.Name)
		}
		roots = append(roots, assetRoot{name: item.Name, identity: root.EntryIdentity})
	}
	slices.SortFunc(roots, func(a, b assetRoot) int {
		switch {
		case a.name < b.name:
			return -1
		case a.name > b.name:
			return 1
		default:
			return 0
		}
	})

	h := sha256.New()
	writeFrame(h, "unobin-asset-set-v1")
	for _, root := range roots {
		writeFrame(h, root.name)
		writeFrame(h, root.identity)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeFrame(h hash.Hash, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = h.Write(size[:])
	_, _ = h.Write([]byte(value))
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return false
	}
	return value == hex.EncodeToString(decoded)
}
