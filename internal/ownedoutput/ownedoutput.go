package ownedoutput

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/filechange"
)

const ManifestName = ".unobin-generated.json"

type Generator struct {
	Name    string
	Version string
}

type File struct {
	Path    string
	Content []byte
	Mode    fs.FileMode
	// Preserve creates a file once and retains later edits and obsolete implementations.
	Preserve bool
	Marker   string
	// RemoveIfUnchanged removes obsolete scaffolds only while their initial content is intact.
	RemoveIfUnchanged bool
	// Managed allows tooling edits and replaces the file on each generation.
	Managed bool
}

func Apply(dir string, generator Generator, files []File) ([]filechange.Change, error) {
	return apply(dir, generator, files, os.Rename)
}

func apply(
	dir string,
	generator Generator,
	files []File,
	rename func(string, string) error,
) ([]filechange.Change, error) {
	dir = filepath.Clean(dir)
	if generator.Name == "" || generator.Version == "" {
		return nil, errors.New("owned outputs require a generator name and version")
	}
	files, err := validateFiles(files)
	if err != nil {
		return nil, err
	}
	old, err := readManifest(dir, generator)
	if err != nil {
		return nil, err
	}
	batch, err := prepareBatch(dir, generator, old, files)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(dir, ".unobin-stage-*")
	if err != nil {
		return nil, err
	}
	retainStage := false
	defer func() {
		if !retainStage {
			_ = os.RemoveAll(stage)
		}
	}()
	for i := range batch.updates {
		update := &batch.updates[i]
		if update.remove {
			continue
		}
		update.staged = filepath.Join(stage, fmt.Sprintf("new-%d", i))
		if err := os.WriteFile(update.staged, update.content, update.mode); err != nil {
			return nil, err
		}
	}
	for _, path := range slices.Sorted(maps.Keys(batch.before)) {
		before := batch.before[path]
		if err := checkPath(dir, path); err != nil {
			return nil, err
		}
		after, err := readOutput(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			return nil, err
		}
		if before.exists != after.exists || !bytes.Equal(before.content, after.content) {
			return nil, fmt.Errorf("output %s changed before publication", path)
		}
	}
	paths := make([]string, 0, len(batch.before))
	for path := range batch.before {
		paths = append(paths, filepath.Join(dir, filepath.FromSlash(path)))
	}
	slices.Sort(paths)
	changes, err := filechange.Observe(paths, func() error {
		return publishBatch(dir, stage, batch.updates, rename)
	})
	var recovery *recoveryError
	retainStage = errors.As(err, &recovery)
	return changes, err
}

type manifest struct {
	FormatVersion int      `json:"format-version"`
	Generator     string   `json:"generator"`
	Version       string   `json:"version"`
	Files         []record `json:"files"`
}

type record struct {
	Path              string      `json:"path"`
	Digest            string      `json:"digest"`
	Mode              fs.FileMode `json:"mode"`
	Preserve          bool        `json:"preserve"`
	Marker            string      `json:"marker,omitempty"`
	RemoveIfUnchanged bool        `json:"remove-if-unchanged,omitempty"`
	Managed           bool        `json:"managed,omitempty"`
}

type output struct {
	exists  bool
	content []byte
}

type update struct {
	path, staged, backup string
	content              []byte
	mode                 fs.FileMode
	remove, installed    bool
}

type batch struct {
	before  map[string]output
	updates []update
}

func validatePath(path string) error {
	if path == "." || path == ManifestName || strings.HasPrefix(path, ManifestName+"/") ||
		!fs.ValidPath(path) ||
		strings.ContainsAny(path, `\:`) || !filepath.IsLocal(filepath.FromSlash(path)) {
		return fmt.Errorf("invalid owned output path %q", path)
	}
	return nil
}

func checkPath(dir, path string) error {
	parts := append([]string{""}, strings.Split(path, "/")...)
	current := dir
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("owned output path contains a symlink: %s", current)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("owned output parent is not a directory: %s", current)
		}
	}
	return nil
}

func validateFiles(files []File) ([]File, error) {
	files = slices.Clone(files)
	seen := map[string]bool{}
	for i := range files {
		file := &files[i]
		if err := validatePath(file.Path); err != nil {
			return nil, err
		}
		if seen[file.Path] {
			return nil, fmt.Errorf("duplicate owned output path %s", file.Path)
		}
		seen[file.Path] = true
		if file.Mode == 0 {
			file.Mode = 0o644
		}
		if file.Mode&^fs.FileMode(0o777) != 0 {
			return nil, fmt.Errorf("invalid owned output mode for %s", file.Path)
		}
		if file.RemoveIfUnchanged && !file.Preserve {
			return nil, fmt.Errorf("scaffold %s must preserve implementation edits", file.Path)
		}
		if file.Managed && (file.Preserve || file.Marker != "") {
			return nil, fmt.Errorf("managed output %s cannot preserve edits or require a marker", file.Path)
		}
		if file.Marker != "" && !bytes.Contains(file.Content, []byte(file.Marker)) {
			return nil, fmt.Errorf("owned output %s is missing its generated marker", file.Path)
		}
		file.Content = bytes.Clone(file.Content)
	}
	for path := range seen {
		for parent := filepath.ToSlash(filepath.Dir(path)); parent != "."; {
			if seen[parent] {
				return nil, fmt.Errorf("owned output paths conflict: %s and %s", parent, path)
			}
			parent = filepath.ToSlash(filepath.Dir(parent))
		}
	}
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

func readOutput(path string) (output, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return output{}, nil
	}
	if err != nil {
		return output{}, err
	}
	if !info.Mode().IsRegular() {
		return output{}, fmt.Errorf("output is not a regular file: %s", path)
	}
	body, err := os.ReadFile(path)
	return output{exists: true, content: body}, err
}

func readManifest(dir string, generator Generator) (manifest, error) {
	if err := checkPath(dir, ManifestName); err != nil {
		return manifest{}, err
	}
	file, err := readOutput(filepath.Join(dir, ManifestName))
	if err != nil {
		return manifest{}, err
	}
	if !file.exists {
		entries, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return manifest{}, err
		}
		if len(entries) > 0 {
			return manifest{}, fmt.Errorf(
				"existing output directory %s has no ownership manifest; "+
					"use a fresh directory or review a migration", dir)
		}
		return manifest{}, nil
	}
	var manifest manifest
	decoder := json.NewDecoder(bytes.NewReader(file.content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("read ownership manifest: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return manifest, errors.New("ownership manifest contains trailing data")
	}
	if manifest.FormatVersion != 1 || manifest.Version == "" || manifest.Generator != generator.Name {
		return manifest, errors.New("ownership manifest has an unsupported format or generator")
	}
	seen := map[string]bool{}
	for _, file := range manifest.Files {
		if err := validatePath(file.Path); err != nil {
			return manifest, err
		}
		digest, err := hex.DecodeString(file.Digest)
		if err != nil || len(digest) != sha256.Size || file.Mode&^fs.FileMode(0o777) != 0 {
			return manifest, fmt.Errorf("invalid ownership record for %s", file.Path)
		}
		if file.RemoveIfUnchanged && !file.Preserve {
			return manifest, fmt.Errorf("invalid scaffold ownership record for %s", file.Path)
		}
		if file.Managed && (file.Preserve || file.Marker != "") {
			return manifest, fmt.Errorf("invalid managed ownership record for %s", file.Path)
		}
		if seen[file.Path] {
			return manifest, fmt.Errorf("duplicate ownership record for %s", file.Path)
		}
		seen[file.Path] = true
	}
	return manifest, nil
}

func contentDigest(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func prepareBatch(dir string, generator Generator, old manifest, files []File) (batch, error) {
	batch := batch{before: map[string]output{}}
	known := map[string]record{}
	for _, file := range old.Files {
		known[file.Path] = file
	}
	paths := maps.Clone(known)
	for _, file := range files {
		paths[file.Path] = record{Path: file.Path}
	}
	paths[ManifestName] = record{Path: ManifestName}
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		if err := checkPath(dir, path); err != nil {
			return batch, err
		}
		before, err := readOutput(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			return batch, err
		}
		batch.before[path] = before
		if file, owned := known[path]; owned && !file.Preserve && !file.Managed && before.exists {
			if contentDigest(before.content) != file.Digest ||
				file.Marker != "" && !bytes.Contains(before.content, []byte(file.Marker)) {
				return batch, fmt.Errorf("owned output %s was modified; preserve it before regenerating", path)
			}
		}
	}
	next := manifest{
		FormatVersion: 1, Generator: generator.Name, Version: generator.Version, Files: []record{},
	}
	desired := map[string]bool{}
	for _, file := range files {
		desired[file.Path] = true
		before := batch.before[file.Path]
		previous, owned := known[file.Path]
		if before.exists && !owned {
			return batch, fmt.Errorf("owned output %s conflicts with an unknown file", file.Path)
		}
		if owned && (file.Preserve != previous.Preserve ||
			file.RemoveIfUnchanged != previous.RemoveIfUnchanged || file.Managed != previous.Managed) {
			return batch, fmt.Errorf("ownership policy changed for %s", file.Path)
		}
		if file.Preserve && before.exists {
			next.Files = append(next.Files, previous)
			continue
		}
		next.Files = append(next.Files, record{
			Path: file.Path, Digest: contentDigest(file.Content), Mode: file.Mode,
			Preserve: file.Preserve, Marker: file.Marker,
			RemoveIfUnchanged: file.RemoveIfUnchanged,
			Managed:           file.Managed,
		})
		if !before.exists || !bytes.Equal(before.content, file.Content) {
			batch.updates = append(batch.updates, update{
				path: file.Path, content: file.Content, mode: file.Mode,
			})
		}
	}
	for _, file := range old.Files {
		if desired[file.Path] {
			continue
		}
		if file.Preserve {
			if batch.before[file.Path].exists {
				if file.RemoveIfUnchanged {
					if contentDigest(batch.before[file.Path].content) != file.Digest {
						return batch, fmt.Errorf("obsolete %s contains edits; review it before removal", file.Path)
					}
					batch.updates = append(batch.updates, update{path: file.Path, remove: true})
				} else {
					next.Files = append(next.Files, file)
				}
			}
		} else if batch.before[file.Path].exists {
			batch.updates = append(batch.updates, update{path: file.Path, remove: true})
		}
	}
	slices.SortFunc(next.Files, func(a, b record) int { return strings.Compare(a.Path, b.Path) })
	body, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return batch, err
	}
	body = append(body, '\n')
	if !bytes.Equal(batch.before[ManifestName].content, body) {
		batch.updates = append(batch.updates, update{path: ManifestName, content: body, mode: 0o644})
	}
	return batch, nil
}

func publishBatch(dir, stage string, updates []update, rename func(string, string) error) error {
	var created []string
	completed := 0
	fail := func(err error) error {
		if restoreErr := rollbackBatch(dir, updates[:completed], created, rename); restoreErr != nil {
			return &recoveryError{stage: stage, cause: errors.Join(err, restoreErr)}
		}
		return err
	}
	for i := range updates {
		change := &updates[i]
		path := filepath.Join(dir, filepath.FromSlash(change.path))
		parents, err := createParents(dir, filepath.Dir(path))
		created = append(created, parents...)
		if err != nil {
			return fail(err)
		}
		if _, err := os.Lstat(path); err == nil {
			change.backup = filepath.Join(stage, "backup", filepath.FromSlash(change.path))
			if err := os.MkdirAll(filepath.Dir(change.backup), 0o755); err != nil {
				change.backup = ""
				return fail(err)
			}
			if err := rename(path, change.backup); err != nil {
				change.backup = ""
				return fail(err)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fail(err)
		}
		completed++
		if !change.remove {
			if err := rename(change.staged, path); err != nil {
				return fail(err)
			}
			change.installed = true
		}
	}
	return nil
}

type recoveryError struct {
	stage string
	cause error
}

func (e *recoveryError) Error() string {
	return fmt.Sprintf("publication rollback failed; recover outputs from %s: %v", e.stage, e.cause)
}

func (e *recoveryError) Unwrap() error { return e.cause }

func createParents(root, dir string) ([]string, error) {
	if root == dir {
		return nil, nil
	}
	created, err := createParents(root, filepath.Dir(dir))
	if err != nil {
		return created, err
	}
	if err := os.Mkdir(dir, 0o755); err == nil {
		return append(created, dir), nil
	} else if !errors.Is(err, fs.ErrExist) {
		return created, err
	}
	return created, nil
}

func rollbackBatch(
	dir string,
	updates []update,
	created []string,
	rename func(string, string) error,
) error {
	var failures []error
	for i := len(updates) - 1; i >= 0; i-- {
		change := updates[i]
		path := filepath.Join(dir, filepath.FromSlash(change.path))
		if change.installed {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				failures = append(failures, err)
			}
		}
		if change.backup != "" {
			if err := rename(change.backup, path); err != nil {
				failures = append(failures, err)
			}
		}
	}
	for i := len(created) - 1; i >= 0; i-- {
		if err := os.Remove(created[i]); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		removeErr := os.Remove(filepath.Join(dir, ManifestName))
		if removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			failures = append(failures, removeErr)
		}
	}
	return errors.Join(failures...)
}
