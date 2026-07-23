package asset

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

const cacheLayoutVersion = "v1"

type Cache struct {
	catalog *Catalog
	root    string
	rootErr error

	mu       sync.Mutex
	resolved map[string]any
}

type completion struct {
	Token           string        `json:"token"`
	EntryIdentity   string        `json:"entry-identity"`
	ReferenceSHA256 string        `json:"reference-sha256"`
	ReferenceKind   ReferenceKind `json:"reference-kind"`
	FormatVersion   int           `json:"format-version"`
	Payload         string        `json:"payload"`
}

func NewCache(catalog *Catalog, requestedRoot string) (*Cache, error) {
	return newCache(catalog, requestedRoot, os.UserCacheDir)
}

func newCache(
	catalog *Catalog,
	requestedRoot string,
	userCacheDir func() (string, error),
) (*Cache, error) {
	cache := &Cache{
		catalog:  catalog,
		resolved: map[string]any{},
	}
	if requestedRoot != "" {
		root, err := filepath.Abs(requestedRoot)
		if err != nil {
			return nil, fmt.Errorf("asset cache: resolve root: %w", err)
		}
		cache.root = filepath.Clean(root)
		return cache, nil
	}
	base, err := userCacheDir()
	if err != nil {
		cache.rootErr = fmt.Errorf(
			"asset cache: find platform cache directory: %w; pass --asset-cache-dir",
			err,
		)
		return cache, nil
	}
	if base == "" {
		cache.rootErr = errors.New(
			"asset cache: platform cache directory is empty; pass --asset-cache-dir",
		)
		return cache, nil
	}
	cache.root = filepath.Join(base, "unobin", "assets")
	return cache, nil
}

func (c *Cache) Root() (string, error) {
	if c == nil {
		return "", errors.New("asset cache is not configured")
	}
	if c.rootErr != nil {
		return "", c.rootErr
	}
	return c.root, nil
}

func (c *Cache) Resolve(token string) (any, error) {
	reference, ok := ParseReference(token)
	if !ok {
		return nil, fmt.Errorf("asset reference %q is invalid", token)
	}
	root, err := c.Root()
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if value, ok := c.resolved[token]; ok {
		return cloneResolved(value), nil
	}

	var embedded *Reference
	if c.catalog != nil {
		embedded, _ = c.catalog.Reference(token)
	}
	finalDir, err := c.referenceDirectory(&reference)
	if err != nil {
		return nil, err
	}
	hitErr := validateCacheDirectoryChain(root, finalDir)
	var marker completion
	if hitErr == nil {
		marker, hitErr = readCompletion(finalDir, reference)
	}
	if hitErr == nil {
		value, err := readCompletedValue(finalDir, marker, embedded)
		if err != nil {
			return nil, fmt.Errorf("asset %s: read cache: %w", DisplayReference(token), err)
		}
		c.resolved[token] = value
		return cloneResolved(value), nil
	}
	if !errors.Is(hitErr, fs.ErrNotExist) {
		return nil, fmt.Errorf("asset %s: validate cache: %w", DisplayReference(token), hitErr)
	}
	if embedded == nil {
		return nil, fmt.Errorf(
			"asset %s is not present in the embedded bundle or cache %s; "+
				"use the cache directory from the earlier factory run",
			DisplayReference(token),
			root,
		)
	}
	if err := c.materialize(finalDir, embedded); err != nil {
		return nil, err
	}
	marker, err = readCompletion(finalDir, reference)
	if err != nil {
		return nil, fmt.Errorf("asset %s: validate completed cache: %w",
			DisplayReference(token), err)
	}
	value, err := readCompletedValue(finalDir, marker, embedded)
	if err != nil {
		return nil, fmt.Errorf("asset %s: read completed cache: %w",
			DisplayReference(token), err)
	}
	c.resolved[token] = value
	return cloneResolved(value), nil
}

func (c *Cache) referenceDirectory(reference *Reference) (string, error) {
	root, err := c.Root()
	if err != nil {
		return "", err
	}
	if reference == nil || !validSHA256(reference.EntryIdentity) {
		return "", errors.New("asset cache: reference has invalid entry identity")
	}
	return filepath.Join(
		root,
		cacheLayoutVersion,
		"sha256",
		reference.EntryIdentity,
		referenceSHA256(reference),
	), nil
}

func referenceSHA256(reference *Reference) string {
	hash := sha256.New()
	writeFrame(hash, "unobin-asset-reference-v1")
	writeFrame(hash, string(reference.Kind))
	writeFrame(hash, reference.AssetName+"\x00"+reference.InternalPath)
	return hex.EncodeToString(hash.Sum(nil))
}

func readCompletion(
	finalDir string,
	reference Reference,
) (completion, error) {
	info, err := os.Lstat(finalDir)
	if err != nil {
		return completion{}, err
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return completion{}, fmt.Errorf("reference directory is not a directory")
	}
	markerPath := filepath.Join(finalDir, "complete")
	info, err = os.Lstat(markerPath)
	if err != nil {
		return completion{}, err
	}
	if !info.Mode().IsRegular() {
		return completion{}, fmt.Errorf("completion marker is not a regular file")
	}
	body, err := os.ReadFile(markerPath)
	if err != nil {
		return completion{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var marker completion
	if err := decoder.Decode(&marker); err != nil {
		return completion{}, fmt.Errorf("decode completion marker: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return completion{}, fmt.Errorf("decode completion marker: %w", err)
		}
		return completion{}, fmt.Errorf("completion marker has multiple JSON values")
	}
	if marker.Token != reference.Token ||
		marker.EntryIdentity != reference.EntryIdentity ||
		marker.ReferenceSHA256 != referenceSHA256(&reference) ||
		marker.ReferenceKind != reference.Kind ||
		marker.FormatVersion != FormatVersion {
		return completion{}, fmt.Errorf("completion marker does not match reference")
	}
	if err := validatePayloadName(marker.Payload); err != nil {
		return completion{}, err
	}
	return marker, nil
}

func readCompletedValue(
	finalDir string,
	marker completion,
	embedded *Reference,
) (any, error) {
	payloadPath, err := safeMaterializeJoin(finalDir, marker.Payload)
	if err != nil {
		return nil, err
	}
	info, err := lstatMaterializedPayload(finalDir, marker.Payload)
	if err != nil {
		return nil, err
	}
	switch marker.ReferenceKind {
	case ReferenceKindPath:
		if marker.Payload == "tree" {
			if !info.IsDir() {
				return nil, fmt.Errorf("tree payload is not a directory")
			}
			if err := validateCompletedTree(payloadPath, embedded); err != nil {
				return nil, err
			}
		} else if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("file payload is not a regular file")
		}
		if embedded != nil && embedded.Entry.Kind == EntryKindFile {
			content, err := os.ReadFile(payloadPath)
			if err != nil {
				return nil, err
			}
			if err := verifyEntryContent(embedded.Entry, content); err != nil {
				return nil, err
			}
		}
		return payloadPath, nil
	case ReferenceKindContent:
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("content payload is not a regular file")
		}
		content, err := os.ReadFile(payloadPath)
		if err != nil {
			return nil, err
		}
		if embedded != nil {
			if err := verifyEntryContent(embedded.Entry, content); err != nil {
				return nil, err
			}
		}
		return content, nil
	default:
		return nil, fmt.Errorf("unsupported reference kind %q", marker.ReferenceKind)
	}
}

func validatePayloadName(value string) error {
	switch {
	case value == "tree":
		return nil
	case value == "content.bin":
		return nil
	case value == "content.zip":
		return nil
	case strings.HasPrefix(value, "file/"):
		return validateMaterializePath(value)
	default:
		return fmt.Errorf("invalid completion payload %q", value)
	}
}

func cloneResolved(value any) any {
	if content, ok := value.([]byte); ok {
		return slices.Clone(content)
	}
	return value
}
