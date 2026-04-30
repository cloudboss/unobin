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

// RemoteImport names an importable repo by host + owner/name, optional
// subdir within the repo, and a version constraint. Constraint format is
// not parsed here (semver tags, branch names, etc. all flow through as
// strings) and the resolver enforces what's allowed.
type RemoteImport struct {
	URL     string
	Subdir  string
	Version string
}

func (*RemoteImport) isImportRef() {}

// ErrEmptyImportRef is returned when the source string is empty.
var ErrEmptyImportRef = errors.New("empty import reference")

// ParseImportRef parses a string from an `imports:` block. Local imports
// start with `.` or `/`; remote imports name a repo URL plus a required
// `@version` suffix and use the Terraform-style `//` separator to denote
// a subdirectory within the repo. Without `//` the whole path before `@`
// is the repo URL and the import has no subdir.
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
	body, version, ok := splitVersion(raw)
	if !ok {
		return nil, fmt.Errorf("import %q: missing required `@version` suffix", raw)
	}
	if version == "" {
		return nil, fmt.Errorf("import %q: empty version after `@`", raw)
	}
	url, subdir, err := splitRepoSubdir(body)
	if err != nil {
		return nil, err
	}
	if !strings.ContainsRune(url, '/') {
		return nil, fmt.Errorf("import %q: repo URL must contain a host and a path", raw)
	}
	return &RemoteImport{URL: url, Subdir: subdir, Version: version}, nil
}

func splitVersion(s string) (body, version string, ok bool) {
	at := strings.LastIndex(s, "@")
	if at < 0 {
		return "", "", false
	}
	return s[:at], s[at+1:], true
}

// splitRepoSubdir separates a repo URL from its optional subdir at the
// `//` separator. Without `//`, the whole input is the URL and there is
// no subdir.
func splitRepoSubdir(s string) (url, subdir string, err error) {
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
