package asset

import (
	"fmt"
)

func (c *Catalog) Content(token string) ([]byte, error) {
	parsed, ok := ParseReference(token)
	if !ok || parsed.Kind != ReferenceKindContent {
		return nil, fmt.Errorf("asset content reference %q is invalid", token)
	}
	display := DisplayReference(token)
	reference, ok := c.Reference(token)
	if !ok {
		return nil, fmt.Errorf(
			"asset content %s: not found in asset catalog",
			display,
		)
	}
	content, err := c.referenceContent(reference)
	if err != nil {
		return nil, fmt.Errorf("asset content %s: %w", display, err)
	}
	return content, nil
}

func (c *Catalog) referenceContent(reference *Reference) ([]byte, error) {
	if c == nil || c.readBlob == nil {
		return nil, fmt.Errorf("catalog has no blob reader")
	}
	if reference == nil || reference.Entry == nil || reference.Asset == nil {
		return nil, fmt.Errorf("catalog reference is incomplete")
	}
	if reference.Entry.Kind == EntryKindFile {
		return c.readEntryContent(reference.Entry)
	}

	entries := make([]CapturedEntry, 0, len(reference.Asset.entries))
	for _, entry := range reference.Asset.Entries() {
		if _, ok := pathWithinDirectory(reference.InternalPath, entry.InternalPath); !ok {
			continue
		}
		captured := CapturedEntry{
			InternalPath:  entry.InternalPath,
			Kind:          entry.Kind,
			Mode:          entry.Mode,
			ContentSize:   entry.ContentSize,
			ContentSHA256: entry.ContentSHA256,
			EntryIdentity: entry.EntryIdentity,
		}
		if entry.Kind == EntryKindFile {
			content, err := c.readEntryContent(entry)
			if err != nil {
				return nil, err
			}
			captured.Content = content
		}
		entries = append(entries, captured)
	}
	archive, err := canonicalDirectoryArchive(
		directoryArchiveEntries(entries, reference.InternalPath),
	)
	if err != nil {
		return nil, err
	}
	if err := verifyEntryContent(reference.Entry, archive); err != nil {
		return nil, err
	}
	return archive, nil
}

func (c *Catalog) readEntryContent(entry *Entry) ([]byte, error) {
	content, err := c.readBlob(entry.BlobPath)
	if err != nil {
		return nil, err
	}
	if err := verifyEntryContent(entry, content); err != nil {
		return nil, err
	}
	return content, nil
}

func verifyEntryContent(entry *Entry, content []byte) error {
	if int64(len(content)) != entry.ContentSize {
		return fmt.Errorf("content size does not match")
	}
	if contentSHA256(content) != entry.ContentSHA256 {
		return fmt.Errorf("content SHA-256 does not match")
	}
	return nil
}
