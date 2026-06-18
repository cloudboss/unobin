package resolve

import (
	"errors"
	"fmt"
	"strings"
)

// ImportRef is a parsed value from an `imports:` block.
type ImportRef interface {
	isImportRef()
}

// LocalImport names a sibling on the operator's filesystem. Local imports
// do not have pinned versions; their content is whatever the developer has
// at the path now. Compile from a clean checkout to make the result
// reproducible.
type LocalImport struct {
	Path string
}

func (*LocalImport) isImportRef() {}

// RemoteImport names an importable repo by host + owner/name and an optional
// package subdir within the repo. The import string has no version; Version is
// filled in from lock.ub as the walk descends. ProjectSubdir and PackageSubdir
// are set after manifest or lock lookup when the owning project differs from
// the imported package.
type RemoteImport struct {
	URL           string
	Subdir        string
	ProjectSubdir string
	PackageSubdir string
	Version       string
}

func (*RemoteImport) isImportRef() {}

// ErrEmptyImportRef is returned when the source string is empty.
var ErrEmptyImportRef = errors.New("empty import reference")

// ParseImportRef parses a string from an `imports:` block. Local imports
// start with `.` or `/`; remote imports name a repo URL and use the
// Terraform-style `//` separator to denote a subdirectory within the
// repo. Without `//` the whole string is the repo URL and the import has
// no subdir.
func ParseImportRef(raw string) (ImportRef, error) {
	if raw == "" {
		return nil, ErrEmptyImportRef
	}
	if isLocalPath(raw) {
		return &LocalImport{Path: raw}, nil
	}
	return parseRemote(raw)
}

func isLocalPath(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '/' {
		return true
	}
	return strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") ||
		s == "." || s == ".."
}

func parseRemote(raw string) (*RemoteImport, error) {
	for _, r := range raw {
		if !validImportRune(r) {
			return nil, fmt.Errorf("import %q: invalid character %q", raw, r)
		}
	}
	url, subdir, err := SplitRepoSubdir(raw)
	if err != nil {
		return nil, err
	}
	if !strings.ContainsRune(url, '/') {
		return nil, fmt.Errorf("import %q: repo URL must contain a host and a path", raw)
	}
	return &RemoteImport{URL: url, Subdir: subdir}, nil
}

// validImportRune reports whether r may appear in a remote import string:
// the letters, digits, and punctuation that make up a host, a path, and
// the `//` subdir separator.
func validImportRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.', r == '-', r == '_', r == '/':
		return true
	default:
		return false
	}
}

// SplitRepoSubdir separates a repo URL from its optional subdir at the
// `//` separator. Without `//`, the whole input is the URL and there is
// no subdir.
func SplitRepoSubdir(s string) (url, subdir string, err error) {
	left, right, ok := strings.Cut(s, "//")
	if !ok {
		return s, "", nil
	}
	left = strings.TrimSuffix(left, "/")
	right = strings.TrimPrefix(right, "/")
	if left == "" {
		return "", "", fmt.Errorf("import %q: empty repo URL before `//`", s)
	}
	return left, right, nil
}
