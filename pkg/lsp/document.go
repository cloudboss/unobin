package lsp

import (
	"fmt"
	"net/url"
	"path/filepath"
)

// Document is the open-text state for one LSP text document.
type Document struct {
	URI     string
	Path    string
	Version int32
	Text    string
	Lines   []LineInfo
}

// LineInfo records byte offsets for one logical line.
type LineInfo struct {
	Start int
	End   int
}

// DocumentStore tracks open documents by URI.
type DocumentStore struct {
	documents map[string]*Document
}

// NewDocumentStore returns an empty document store.
func NewDocumentStore() *DocumentStore {
	return &DocumentStore{documents: make(map[string]*Document)}
}

// Open stores a full text document snapshot.
func (s *DocumentStore) Open(uri string, version int32, text string) (*Document, error) {
	doc, err := newDocument(uri, version, text)
	if err != nil {
		return nil, err
	}
	s.documents[uri] = doc
	return doc, nil
}

// Change replaces an open document with a full text snapshot.
func (s *DocumentStore) Change(uri string, version int32, text string) (*Document, error) {
	if _, ok := s.documents[uri]; !ok {
		return nil, fmt.Errorf("document is not open: %s", uri)
	}
	doc, err := newDocument(uri, version, text)
	if err != nil {
		return nil, err
	}
	s.documents[uri] = doc
	return doc, nil
}

// Close removes an open document snapshot.
func (s *DocumentStore) Close(uri string) {
	delete(s.documents, uri)
}

// Get returns an open document snapshot by URI.
func (s *DocumentStore) Get(uri string) (*Document, bool) {
	doc, ok := s.documents[uri]
	return doc, ok
}

// FileURIToPath converts a file URI to a local path.
func FileURIToPath(rawURI string) (string, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "", fmt.Errorf("parse document URI: %w", err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("unsupported document URI scheme: %s", u.Scheme)
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("unsupported file URI host: %s", u.Host)
	}
	if u.Path == "" {
		return "", fmt.Errorf("file URI has no path")
	}
	return filepath.FromSlash(u.Path), nil
}

func newDocument(uri string, version int32, text string) (*Document, error) {
	path, err := FileURIToPath(uri)
	if err != nil {
		return nil, err
	}
	return &Document{
		URI:     uri,
		Path:    path,
		Version: version,
		Text:    text,
		Lines:   buildLineInfo(text),
	}, nil
}

func buildLineInfo(text string) []LineInfo {
	lines := make([]LineInfo, 0, 1)
	start := 0
	for i := range len(text) {
		if text[i] != '\n' {
			continue
		}
		end := i
		if end > start && text[end-1] == '\r' {
			end--
		}
		lines = append(lines, LineInfo{Start: start, End: end})
		start = i + 1
	}
	end := len(text)
	if end > start && text[end-1] == '\r' {
		end--
	}
	return append(lines, LineInfo{Start: start, End: end})
}
