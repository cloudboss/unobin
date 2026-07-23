package asset

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	pathpkg "path"
	"slices"
	"strings"
	"unicode/utf8"
)

func canonicalDirectoryArchive(entries []CapturedEntry) ([]byte, error) {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b CapturedEntry) int {
		switch {
		case a.InternalPath < b.InternalPath:
			return -1
		case a.InternalPath > b.InternalPath:
			return 1
		default:
			return 0
		}
	})

	byPath := make(map[string]CapturedEntry, len(sorted))
	for _, entry := range sorted {
		if _, ok := byPath[entry.InternalPath]; ok {
			if entry.InternalPath == "" {
				return nil, fmt.Errorf("directory archive has multiple root entries")
			}
			return nil, fmt.Errorf("directory archive has duplicate path %q", entry.InternalPath)
		}
		if entry.InternalPath != "" && !validInternalPath(entry.InternalPath) {
			return nil, fmt.Errorf("directory archive has invalid path %q", entry.InternalPath)
		}
		if err := validateArchiveEntry(entry); err != nil {
			return nil, err
		}
		byPath[entry.InternalPath] = entry
	}

	root, ok := byPath[""]
	if !ok {
		return nil, fmt.Errorf("directory archive has no root entry")
	}
	if root.Kind != EntryKindDirectory {
		return nil, fmt.Errorf("directory archive root is not a directory")
	}
	for _, entry := range sorted {
		if entry.InternalPath == "" {
			continue
		}
		parentPath := pathpkg.Dir(entry.InternalPath)
		if parentPath == "." {
			parentPath = ""
		}
		parent, ok := byPath[parentPath]
		if !ok {
			return nil, fmt.Errorf(
				"directory archive path %q has no parent %q",
				entry.InternalPath,
				parentPath,
			)
		}
		if parent.Kind != EntryKindDirectory {
			return nil, fmt.Errorf(
				"directory archive path %q has non-directory parent %q",
				entry.InternalPath,
				parentPath,
			)
		}
	}

	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range sorted {
		if entry.InternalPath == "" {
			continue
		}
		header := zip.FileHeader{
			Name:         entry.InternalPath,
			Method:       zip.Store,
			ModifiedDate: 0x21,
		}
		mode, err := archiveFileMode(entry)
		if err != nil {
			return nil, err
		}
		if entry.Kind == EntryKindDirectory {
			header.Name += "/"
			mode |= fs.ModeDir
		}
		header.SetMode(mode)
		stream, err := writer.CreateHeader(&header)
		if err != nil {
			return nil, fmt.Errorf("create directory archive entry %q: %w", header.Name, err)
		}
		if entry.Kind == EntryKindFile {
			if _, err := stream.Write(entry.Content); err != nil {
				return nil, fmt.Errorf(
					"write directory archive entry %q: %w",
					header.Name,
					err,
				)
			}
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close directory archive: %w", err)
	}
	return output.Bytes(), nil
}

func validInternalPath(value string) bool {
	return utf8.ValidString(value) &&
		!strings.Contains(value, `\`) &&
		value != "." &&
		fs.ValidPath(value)
}

func validateArchiveEntry(entry CapturedEntry) error {
	if entry.InternalPath == "" && entry.Kind != EntryKindDirectory {
		return nil
	}
	if _, err := archiveFileMode(entry); err != nil {
		return err
	}
	if entry.Kind == EntryKindDirectory && len(entry.Content) != 0 {
		return fmt.Errorf(
			"directory archive path %q has directory content",
			entry.InternalPath,
		)
	}
	return nil
}

func archiveFileMode(entry CapturedEntry) (fs.FileMode, error) {
	switch entry.Kind {
	case EntryKindFile:
		switch entry.Mode {
		case "0644":
			return 0644, nil
		case "0755":
			return 0755, nil
		default:
			return 0, fmt.Errorf(
				"directory archive file %q has invalid mode %q",
				entry.InternalPath,
				entry.Mode,
			)
		}
	case EntryKindDirectory:
		if entry.Mode != "0755" {
			return 0, fmt.Errorf(
				"directory archive directory %q has invalid mode %q",
				entry.InternalPath,
				entry.Mode,
			)
		}
		return 0755, nil
	default:
		return 0, fmt.Errorf(
			"directory archive path %q has invalid kind %q",
			entry.InternalPath,
			entry.Kind,
		)
	}
}
