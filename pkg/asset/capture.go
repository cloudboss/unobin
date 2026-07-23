package asset

import (
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func Capture(
	source *resolve.Source,
	sourceFile syntax.SourceFileSpec,
	declarations []syntax.AssetDecl,
	outputPath string,
) (*CapturedSet, error) {
	if len(declarations) == 0 {
		return nil, nil
	}
	boundary, err := captureSourceBoundary(source, sourceFile)
	if err != nil {
		return nil, err
	}
	if err := validateDeclarationNames(declarations); err != nil {
		return nil, err
	}

	assets := make([]CapturedAsset, 0, len(declarations))
	for _, declaration := range declarations {
		if declaration.Source == nil {
			return nil, fmt.Errorf("asset %q has no source", declaration.Name.Name)
		}
		sourcePath, err := resolveAssetSource(boundary.baseDir, declaration.Source.Value)
		if err != nil {
			return nil, fmt.Errorf("asset %q: %w", declaration.Name.Name, err)
		}
		entries, err := captureSourceEntries(boundary, sourcePath, outputPath)
		if err != nil {
			return nil, fmt.Errorf(
				"asset %q source %q: %w",
				declaration.Name.Name,
				declaration.Source.Value,
				err,
			)
		}
		assets = append(assets, CapturedAsset{
			Name:    declaration.Name.Name,
			Entries: entries,
		})
	}
	slices.SortFunc(assets, func(a, b CapturedAsset) int {
		return strings.Compare(a.Name, b.Name)
	})
	id, err := assetSetID(assets)
	if err != nil {
		return nil, err
	}
	return &CapturedSet{ID: id, Assets: assets}, nil
}

type sourceBoundary struct {
	fsys    fs.FS
	baseDir string
	osRoot  string
}

func captureSourceBoundary(
	source *resolve.Source,
	sourceFile syntax.SourceFileSpec,
) (sourceBoundary, error) {
	if source == nil {
		return sourceBoundary{}, fmt.Errorf("capture assets: missing declaring source")
	}
	if source.ProjectFS != nil {
		filePath, err := cleanSourceFilePath(sourceFile.ProjectRelPath)
		if err != nil {
			return sourceBoundary{}, fmt.Errorf("capture assets: project file path: %w", err)
		}
		return sourceBoundary{
			fsys:    source.ProjectFS,
			baseDir: pathpkg.Dir(filePath),
			osRoot:  source.ProjectPath,
		}, nil
	}
	if source.FS == nil {
		return sourceBoundary{}, fmt.Errorf("capture assets: missing declaring filesystem")
	}
	filePath, err := cleanSourceFilePath(sourceFile.PackageRelPath)
	if err != nil {
		return sourceBoundary{}, fmt.Errorf("capture assets: package file path: %w", err)
	}
	return sourceBoundary{
		fsys:    source.FS,
		baseDir: pathpkg.Dir(filePath),
		osRoot:  source.Path,
	}, nil
}

func cleanSourceFilePath(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strings.Contains(value, `\`) || pathpkg.IsAbs(value) {
		return "", fmt.Errorf("path %q is not a relative slash path", value)
	}
	clean := pathpkg.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path %q is outside its filesystem boundary", value)
	}
	return clean, nil
}

func validateDeclarationNames(declarations []syntax.AssetDecl) error {
	names := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		name := declaration.Name.Name
		if name == "" {
			return fmt.Errorf("asset name is empty")
		}
		if _, ok := names[name]; ok {
			return fmt.Errorf("duplicate asset name %q", name)
		}
		names[name] = struct{}{}
	}
	return nil
}

func resolveAssetSource(baseDir, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("source path is empty")
	}
	if strings.Contains(value, `\`) ||
		pathpkg.IsAbs(value) ||
		filepath.IsAbs(value) ||
		filepath.VolumeName(value) != "" ||
		hasDrivePrefix(value) {
		return "", fmt.Errorf("source path %q must be relative", value)
	}
	resolved := pathpkg.Clean(pathpkg.Join(baseDir, value))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", fmt.Errorf("source path %q resolves outside its filesystem boundary", value)
	}
	return resolved, nil
}

func hasDrivePrefix(value string) bool {
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	return value[0] >= 'A' && value[0] <= 'Z' ||
		value[0] >= 'a' && value[0] <= 'z'
}

func captureSourceEntries(
	boundary sourceBoundary,
	sourcePath string,
	outputPath string,
) ([]CapturedEntry, error) {
	if boundary.osRoot != "" {
		absoluteSource := filepath.Join(
			boundary.osRoot,
			filepath.FromSlash(sourcePath),
		)
		if outputPath != "" {
			if err := rejectOutputOverlap(absoluteSource, outputPath); err != nil {
				return nil, err
			}
		}
		return captureOSEntries(absoluteSource)
	}
	return captureFSEntries(boundary.fsys, sourcePath)
}

func rejectOutputOverlap(sourcePath, outputPath string) error {
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	sourceContainsOutput, err := containsOSPath(sourceAbs, outputAbs)
	if err != nil {
		return err
	}
	outputContainsSource, err := containsOSPath(outputAbs, sourceAbs)
	if err != nil {
		return err
	}
	if sourceContainsOutput || outputContainsSource {
		return fmt.Errorf(
			"asset source %s and generated output %s overlap",
			sourceAbs,
			outputAbs,
		)
	}
	return nil
}

func containsOSPath(parent, child string) (bool, error) {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false, err
	}
	return relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))), nil
}

func captureOSEntries(root string) ([]CapturedEntry, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if err := validateCapturedMode(root, rootInfo.Mode()); err != nil {
		return nil, err
	}

	var entries []CapturedEntry
	err = filepath.WalkDir(root, func(
		current string,
		_ os.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		entry, err := capturedEntryFromMode(current, info.Mode())
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		if relative == "." {
			relative = ""
		}
		entry.InternalPath = filepath.ToSlash(relative)
		if entry.Kind == EntryKindFile {
			entry.Content, err = os.ReadFile(current)
			if err != nil {
				return err
			}
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return completeCapturedEntries(entries)
}

func captureFSEntries(fsys fs.FS, root string) ([]CapturedEntry, error) {
	if fsys == nil {
		return nil, fmt.Errorf("missing source filesystem")
	}
	if _, err := readRootDirEntry(fsys, root); err != nil {
		return nil, err
	}

	var entries []CapturedEntry
	err := fs.WalkDir(fsys, root, func(
		current string,
		dirEntry fs.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		if dirEntry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlink is not supported", current)
		}
		info, err := dirEntry.Info()
		if err != nil {
			return err
		}
		entry, err := capturedEntryFromMode(current, info.Mode())
		if err != nil {
			return err
		}
		entry.InternalPath, err = internalPathFromRoot(root, current)
		if err != nil {
			return err
		}
		if entry.Kind == EntryKindFile {
			entry.Content, err = fs.ReadFile(fsys, current)
			if err != nil {
				return err
			}
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return completeCapturedEntries(entries)
}

func readRootDirEntry(fsys fs.FS, root string) (fs.DirEntry, error) {
	if root == "." {
		info, err := fs.Stat(fsys, root)
		if err != nil {
			return nil, err
		}
		if err := validateCapturedMode(root, info.Mode()); err != nil {
			return nil, err
		}
		return fs.FileInfoToDirEntry(info), nil
	}
	parent := pathpkg.Dir(root)
	entries, err := fs.ReadDir(fsys, parent)
	if err != nil {
		return nil, err
	}
	base := pathpkg.Base(root)
	for _, entry := range entries {
		if entry.Name() != base {
			continue
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s: symlink is not supported", root)
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if err := validateCapturedMode(root, info.Mode()); err != nil {
			return nil, err
		}
		return entry, nil
	}
	return nil, &fs.PathError{Op: "capture", Path: root, Err: fs.ErrNotExist}
}

func internalPathFromRoot(root, current string) (string, error) {
	if current == root {
		return "", nil
	}
	if root == "." {
		return current, nil
	}
	relative, ok := strings.CutPrefix(current, root+"/")
	if !ok {
		return "", fmt.Errorf("%s is outside asset root %s", current, root)
	}
	return relative, nil
}

func capturedEntryFromMode(path string, mode fs.FileMode) (CapturedEntry, error) {
	if err := validateCapturedMode(path, mode); err != nil {
		return CapturedEntry{}, err
	}
	normalized, err := normalizeMode(mode)
	if err != nil {
		return CapturedEntry{}, fmt.Errorf("%s: %w", path, err)
	}
	kind := EntryKindFile
	if mode.IsDir() {
		kind = EntryKindDirectory
	}
	return CapturedEntry{Kind: kind, Mode: normalized}, nil
}

func validateCapturedMode(path string, mode fs.FileMode) error {
	if mode&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s: symlink is not supported", path)
	}
	if _, err := normalizeMode(mode); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func completeCapturedEntries(entries []CapturedEntry) ([]CapturedEntry, error) {
	slices.SortFunc(entries, func(a, b CapturedEntry) int {
		return strings.Compare(a.InternalPath, b.InternalPath)
	})
	for i := range entries {
		if entries[i].Kind != EntryKindFile {
			continue
		}
		entries[i].ContentSize = int64(len(entries[i].Content))
		entries[i].ContentSHA256 = contentSHA256(entries[i].Content)
		entries[i].EntryIdentity = entryIdentity(
			entries[i].Kind,
			entries[i].Mode,
			entries[i].ContentSHA256,
		)
	}

	directories := make([]int, 0, len(entries))
	for i := range entries {
		if entries[i].Kind == EntryKindDirectory {
			directories = append(directories, i)
		}
	}
	slices.SortFunc(directories, func(a, b int) int {
		return strings.Compare(entries[b].InternalPath, entries[a].InternalPath)
	})
	for _, index := range directories {
		archiveEntries := directoryArchiveEntries(entries, entries[index].InternalPath)
		archive, err := canonicalDirectoryArchive(archiveEntries)
		if err != nil {
			return nil, err
		}
		entries[index].ContentSize = int64(len(archive))
		entries[index].ContentSHA256 = contentSHA256(archive)
		entries[index].EntryIdentity = entryIdentity(
			entries[index].Kind,
			entries[index].Mode,
			entries[index].ContentSHA256,
		)
	}
	return entries, nil
}

func directoryArchiveEntries(entries []CapturedEntry, directory string) []CapturedEntry {
	selected := make([]CapturedEntry, 0, len(entries))
	for _, entry := range entries {
		internalPath, ok := pathWithinDirectory(directory, entry.InternalPath)
		if !ok {
			continue
		}
		clone := entry
		clone.InternalPath = internalPath
		selected = append(selected, clone)
	}
	return selected
}

func pathWithinDirectory(directory, candidate string) (string, bool) {
	if candidate == directory {
		return "", true
	}
	if directory == "" {
		return candidate, true
	}
	relative, ok := strings.CutPrefix(candidate, directory+"/")
	return relative, ok
}
