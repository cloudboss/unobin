package asset

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
)

func (c *Cache) materialize(finalDir string, reference *Reference) error {
	identityDir := filepath.Dir(finalDir)
	root, err := c.Root()
	if err != nil {
		return err
	}
	if err := ensureCacheDirectories(root, identityDir); err != nil {
		return fmt.Errorf("asset %s: create cache parent: %w",
			DisplayReference(reference.Token), err)
	}
	tempDir, err := os.MkdirTemp(identityDir, ".tmp-")
	if err != nil {
		return fmt.Errorf("asset %s: create temporary cache: %w",
			DisplayReference(reference.Token), err)
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = os.RemoveAll(tempDir)
		}
	}()

	payload, err := c.writePayload(tempDir, reference)
	if err != nil {
		return fmt.Errorf("asset %s: materialize: %w",
			DisplayReference(reference.Token), err)
	}
	marker := completion{
		Token:           reference.Token,
		EntryIdentity:   reference.EntryIdentity,
		ReferenceSHA256: referenceSHA256(reference),
		ReferenceKind:   reference.Kind,
		FormatVersion:   FormatVersion,
		Payload:         payload,
	}
	body, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("asset %s: encode completion marker: %w",
			DisplayReference(reference.Token), err)
	}
	body = append(body, '\n')
	if err := writeMaterializedFile(
		filepath.Join(tempDir, "complete"),
		body,
		0o444,
	); err != nil {
		return fmt.Errorf("asset %s: write completion marker: %w",
			DisplayReference(reference.Token), err)
	}
	if err := os.Chmod(tempDir, 0o755); err != nil {
		return fmt.Errorf("asset %s: finalize temporary cache: %w",
			DisplayReference(reference.Token), err)
	}
	if err := os.Rename(tempDir, finalDir); err == nil {
		renamed = true
		return nil
	}

	parsed, ok := ParseReference(reference.Token)
	if !ok {
		return errors.New("asset cache: generated invalid reference")
	}
	if _, err := readCompletion(finalDir, parsed); err == nil {
		return nil
	}
	return fmt.Errorf("asset %s: publish cache entry: destination already exists",
		DisplayReference(reference.Token))
}

func (c *Cache) writePayload(tempDir string, reference *Reference) (string, error) {
	switch reference.Kind {
	case ReferenceKindPath:
		if reference.Entry.Kind == EntryKindDirectory {
			if err := c.writeTree(filepath.Join(tempDir, "tree"), reference); err != nil {
				return "", err
			}
			return "tree", nil
		}
		name := reference.AssetName
		if reference.InternalPath != "" {
			name = pathpkg.Base(reference.InternalPath)
		}
		payload := "file/" + name
		target, err := safeMaterializeJoin(tempDir, payload)
		if err != nil {
			return "", err
		}
		if err := os.Mkdir(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		content, err := c.catalog.readEntryContent(reference.Entry)
		if err != nil {
			return "", err
		}
		mode, err := materializeMode(reference.Entry.Mode)
		if err != nil {
			return "", err
		}
		if err := writeMaterializedFile(target, content, mode); err != nil {
			return "", err
		}
		if err := os.Chmod(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		return payload, nil
	case ReferenceKindContent:
		content, err := c.catalog.Content(reference.Token)
		if err != nil {
			return "", err
		}
		payload := "content.bin"
		if reference.Entry.Kind == EntryKindDirectory {
			payload = "content.zip"
		}
		target, err := safeMaterializeJoin(tempDir, payload)
		if err != nil {
			return "", err
		}
		if err := writeMaterializedFile(target, content, 0o444); err != nil {
			return "", err
		}
		return payload, nil
	default:
		return "", fmt.Errorf("unsupported reference kind %q", reference.Kind)
	}
}

func (c *Cache) writeTree(root string, reference *Reference) error {
	rootMode, err := materializeMode(reference.Entry.Mode)
	if err != nil {
		return err
	}
	if err := os.Mkdir(root, rootMode); err != nil {
		return err
	}
	type directoryMode struct {
		path string
		mode fs.FileMode
	}
	directories := []directoryMode{{path: root, mode: rootMode}}
	for _, entry := range reference.Asset.Entries() {
		relative, ok := pathWithinDirectory(reference.InternalPath, entry.InternalPath)
		if !ok || relative == "" {
			continue
		}
		if err := validateMaterializePath(relative); err != nil {
			return fmt.Errorf("entry %q: %w", entry.InternalPath, err)
		}
		target, err := safeMaterializeJoin(root, relative)
		if err != nil {
			return err
		}
		mode, err := materializeMode(entry.Mode)
		if err != nil {
			return err
		}
		switch entry.Kind {
		case EntryKindDirectory:
			if err := os.Mkdir(target, mode); err != nil {
				return err
			}
			directories = append(directories, directoryMode{path: target, mode: mode})
		case EntryKindFile:
			content, err := c.catalog.readEntryContent(entry)
			if err != nil {
				return err
			}
			if err := writeMaterializedFile(target, content, mode); err != nil {
				return err
			}
		default:
			return fmt.Errorf("entry %q has unsupported kind %q", entry.InternalPath, entry.Kind)
		}
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := os.Chmod(directories[i].path, directories[i].mode); err != nil {
			return err
		}
	}
	return nil
}

func validateMaterializePath(value string) error {
	if value == "" ||
		value == "." ||
		pathpkg.IsAbs(value) ||
		filepath.IsAbs(value) ||
		filepath.VolumeName(value) != "" ||
		strings.Contains(value, `\`) ||
		!fs.ValidPath(value) {
		return fmt.Errorf("invalid materialization path %q", value)
	}
	return nil
}

func safeMaterializeJoin(root, relative string) (string, error) {
	if err := validateMaterializePath(relative); err != nil {
		return "", err
	}
	target := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("materialization path %q escapes its root", relative)
	}
	return target, nil
}

func materializeMode(value string) (fs.FileMode, error) {
	mode, err := strconv.ParseUint(value, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid materialization mode %q", value)
	}
	return fs.FileMode(mode), nil
}

func writeMaterializedFile(path string, content []byte, mode fs.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func ensureCacheDirectories(root, target string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("cache root is not a directory")
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if err := validateMaterializePath(filepath.ToSlash(relative)); err != nil {
		return err
	}
	current := root
	for part := range strings.SplitSeq(filepath.ToSlash(relative), "/") {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		switch {
		case err == nil:
			if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
				return fmt.Errorf("cache path %q is not a directory", current)
			}
		case errors.Is(err, fs.ErrNotExist):
			if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
			info, err := os.Lstat(current)
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
				return fmt.Errorf("cache path %q is not a directory", current)
			}
		default:
			return err
		}
	}
	return nil
}

func validateCacheDirectoryChain(root, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if err := validateMaterializePath(filepath.ToSlash(relative)); err != nil {
		return err
	}
	current := root
	parts := append([]string{""}, strings.Split(filepath.ToSlash(relative), "/")...)
	for _, part := range parts {
		if part != "" {
			current = filepath.Join(current, filepath.FromSlash(part))
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("cache path %q is not a directory or is a symlink", current)
		}
	}
	return nil
}

func lstatMaterializedPayload(root, relative string) (fs.FileInfo, error) {
	if err := validateMaterializePath(relative); err != nil {
		return nil, err
	}
	current := root
	parts := strings.Split(relative, "/")
	var info fs.FileInfo
	for i, part := range parts {
		current = filepath.Join(current, filepath.FromSlash(part))
		next, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if next.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("payload path %q is a symlink", current)
		}
		if i < len(parts)-1 && !next.IsDir() {
			return nil, fmt.Errorf("payload parent %q is not a directory", current)
		}
		info = next
	}
	return info, nil
}

func validateCompletedTree(root string, reference *Reference) error {
	var expected map[string]*Entry
	if reference != nil {
		expected = map[string]*Entry{}
		for _, entry := range reference.Asset.Entries() {
			relative, ok := pathWithinDirectory(reference.InternalPath, entry.InternalPath)
			if ok {
				expected[relative] = entry
			}
		}
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if item.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("tree path %q is a symlink", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			relative = ""
		} else {
			relative = filepath.ToSlash(relative)
			if err := validateMaterializePath(relative); err != nil {
				return err
			}
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("tree path %q is not a regular file or directory", path)
		}
		if reference == nil {
			return nil
		}
		entry, ok := expected[relative]
		if !ok {
			return fmt.Errorf("tree has unexpected path %q", relative)
		}
		seen[relative] = true
		if (entry.Kind == EntryKindDirectory) != info.IsDir() {
			return fmt.Errorf("tree path %q has the wrong kind", relative)
		}
		mode, err := materializeMode(entry.Mode)
		if err != nil {
			return err
		}
		if info.Mode().Perm() != mode.Perm() {
			return fmt.Errorf("tree path %q has the wrong mode", relative)
		}
		if entry.Kind == EntryKindFile {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := verifyEntryContent(entry, content); err != nil {
				return fmt.Errorf("tree path %q: %w", relative, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for path := range expected {
		if !seen[path] {
			return fmt.Errorf("tree is missing path %q", path)
		}
	}
	return nil
}
