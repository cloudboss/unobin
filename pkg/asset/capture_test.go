package asset

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestCaptureRegularFiles(t *testing.T) {
	projectFS := fstest.MapFS{
		"factory.ub":    &fstest.MapFile{},
		"files/config":  &fstest.MapFile{Data: []byte("config\n"), Mode: 0600},
		"files/program": &fstest.MapFile{Data: []byte("#!/bin/sh\n"), Mode: 0100},
	}

	set, err := Capture(
		captureProjectSource(projectFS),
		captureProjectFile("factory.ub"),
		[]syntax.AssetDecl{
			captureDeclaration("program", "files/program"),
			captureDeclaration("config", "files/config"),
		},
		"",
	)
	require.NoError(t, err)
	require.True(t, validSHA256(set.ID))
	require.Equal(t, []string{"config", "program"}, capturedAssetNames(set.Assets))

	config := capturedEntry(t, set, "config", "")
	require.Equal(t, EntryKindFile, config.Kind)
	require.Equal(t, "0644", config.Mode)
	require.Equal(t, []byte("config\n"), config.Content)
	require.Equal(t, int64(len(config.Content)), config.ContentSize)
	require.Equal(t, contentSHA256(config.Content), config.ContentSHA256)
	require.Equal(
		t,
		entryIdentity(config.Kind, config.Mode, config.ContentSHA256),
		config.EntryIdentity,
	)

	program := capturedEntry(t, set, "program", "")
	require.Equal(t, "0755", program.Mode)
	require.Equal(t, []byte("#!/bin/sh\n"), program.Content)
}

func TestCaptureDirectoryIncludesNestedHiddenAndEmptyEntries(t *testing.T) {
	projectFS := fstest.MapFS{
		"factory.ub":             &fstest.MapFile{},
		"assets/tree":            &fstest.MapFile{Mode: fs.ModeDir},
		"assets/tree/.env":       &fstest.MapFile{Data: []byte("hidden\n")},
		"assets/tree/_data":      &fstest.MapFile{Data: []byte("data\n")},
		"assets/tree/empty":      &fstest.MapFile{Mode: fs.ModeDir},
		"assets/tree/nested":     &fstest.MapFile{Mode: fs.ModeDir},
		"assets/tree/nested/run": &fstest.MapFile{Data: []byte("run\n"), Mode: 0111},
	}

	set, err := Capture(
		captureProjectSource(projectFS),
		captureProjectFile("factory.ub"),
		[]syntax.AssetDecl{captureDeclaration("tree", "assets/tree")},
		"",
	)
	require.NoError(t, err)

	asset := capturedAsset(t, set, "tree")
	require.Equal(
		t,
		[]string{"", ".env", "_data", "empty", "nested", "nested/run"},
		capturedEntryPaths(asset.Entries),
	)
	require.Equal(t, EntryKindDirectory, capturedEntry(t, set, "tree", "").Kind)
	require.Equal(t, "0755", capturedEntry(t, set, "tree", "empty").Mode)
	require.Equal(t, "0755", capturedEntry(t, set, "tree", "nested/run").Mode)

	root := capturedEntry(t, set, "tree", "")
	archive, err := canonicalDirectoryArchive(asset.Entries)
	require.NoError(t, err)
	require.Equal(t, int64(len(archive)), root.ContentSize)
	require.Equal(t, contentSHA256(archive), root.ContentSHA256)
	require.Equal(
		t,
		entryIdentity(root.Kind, root.Mode, root.ContentSHA256),
		root.EntryIdentity,
	)
}

func TestCaptureSetIDChangesWithTreeContentAndPermissions(t *testing.T) {
	tests := []struct {
		name  string
		files fstest.MapFS
	}{
		{name: "base", files: captureChangeFiles("main", "one\n", 0644, true)},
		{name: "content", files: captureChangeFiles("main", "two\n", 0644, true)},
		{name: "permission", files: captureChangeFiles("main", "one\n", 0755, true)},
		{name: "added", files: captureChangeFiles("main", "one\n", 0644, true)},
		{name: "removed", files: captureChangeFiles("main", "one\n", 0644, false)},
		{name: "renamed", files: captureChangeFiles("renamed", "one\n", 0644, true)},
	}
	tests[3].files["assets/extra"] = &fstest.MapFile{Data: []byte("extra\n")}
	ids := make(map[string]string, len(tests))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, err := Capture(
				captureProjectSource(tt.files),
				captureProjectFile("factory.ub"),
				[]syntax.AssetDecl{captureDeclaration("tree", "assets")},
				"",
			)
			require.NoError(t, err)
			ids[tt.name] = set.ID
		})
	}
	require.Len(t, uniqueStrings(ids), len(tests))
}

func TestCaptureDirectoryIsIndependentOfCreationOrderAndTimestamps(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeCaptureTree(t, first, []string{"tree/a", "tree/nested/b"})
	writeCaptureTree(t, second, []string{"tree/nested/b", "tree/a"})
	oldTime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	newTime := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
	require.NoError(t, os.Chtimes(filepath.Join(first, "tree", "a"), oldTime, oldTime))
	require.NoError(t, os.Chtimes(filepath.Join(second, "tree", "a"), newTime, newTime))

	firstSet, err := Capture(
		captureOSProjectSource(first),
		captureProjectFile("factory.ub"),
		[]syntax.AssetDecl{captureDeclaration("tree", "tree")},
		"",
	)
	require.NoError(t, err)
	secondSet, err := Capture(
		captureOSProjectSource(second),
		captureProjectFile("factory.ub"),
		[]syntax.AssetDecl{captureDeclaration("tree", "tree")},
		"",
	)
	require.NoError(t, err)

	require.Equal(t, firstSet.ID, secondSet.ID)
	require.Equal(
		t,
		capturedEntry(t, firstSet, "tree", "").ContentSHA256,
		capturedEntry(t, secondSet, "tree", "").ContentSHA256,
	)
}

func TestCaptureResolvesFromDeclaringFile(t *testing.T) {
	projectFS := fstest.MapFS{
		"packages/app/factory.ub": &fstest.MapFile{},
		"packages/shared/data":    &fstest.MapFile{Data: []byte("shared\n")},
	}

	set, err := Capture(
		captureProjectSource(projectFS),
		captureProjectFile("packages/app/factory.ub"),
		[]syntax.AssetDecl{captureDeclaration("data", "../shared/data")},
		"",
	)
	require.NoError(t, err)
	require.Equal(t, []byte("shared\n"), capturedEntry(t, set, "data", "").Content)
}

func TestCaptureUsesPackageBoundaryForVirtualSource(t *testing.T) {
	packageFS := fstest.MapFS{
		"library.ub": &fstest.MapFile{},
		"data/file":  &fstest.MapFile{Data: []byte("virtual\n")},
	}
	source := &resolve.Source{FS: packageFS}
	sourceFile := syntax.SourceFileSpec{PackageRelPath: "library.ub"}

	set, err := Capture(
		source,
		sourceFile,
		[]syntax.AssetDecl{captureDeclaration("data", "data")},
		"",
	)
	require.NoError(t, err)
	require.Equal(t, []byte("virtual\n"), capturedEntry(t, set, "data", "file").Content)
}

func TestCaptureCanSelectVirtualBoundaryRoot(t *testing.T) {
	projectFS := fstest.MapFS{
		"factory.ub": &fstest.MapFile{},
		"data":       &fstest.MapFile{Data: []byte("root\n")},
	}

	set, err := Capture(
		captureProjectSource(projectFS),
		captureProjectFile("factory.ub"),
		[]syntax.AssetDecl{captureDeclaration("project", ".")},
		"",
	)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"", "data", "factory.ub"},
		capturedEntryPaths(capturedAsset(t, set, "project").Entries),
	)
}

func TestCaptureWithoutDeclarationsNeedsNoSource(t *testing.T) {
	set, err := Capture(nil, syntax.SourceFileSpec{}, nil, "")
	require.NoError(t, err)
	require.Nil(t, set)
}

func TestCaptureRejectsInvalidSourcePaths(t *testing.T) {
	projectFS := fstest.MapFS{
		"nested/factory.ub": &fstest.MapFile{},
		"nested/file":       &fstest.MapFile{Data: []byte("body")},
	}
	tests := []struct {
		name       string
		source     *resolve.Source
		sourceFile syntax.SourceFileSpec
		path       string
	}{
		{
			name:       "missing source",
			sourceFile: captureProjectFile("nested/factory.ub"),
			path:       "file",
		},
		{
			name:       "missing filesystem",
			source:     &resolve.Source{},
			sourceFile: syntax.SourceFileSpec{PackageRelPath: "factory.ub"},
			path:       "file",
		},
		{
			name:       "missing project file path",
			source:     captureProjectSource(projectFS),
			sourceFile: syntax.SourceFileSpec{},
			path:       "file",
		},
		{
			name:       "missing package file path",
			source:     &resolve.Source{FS: projectFS},
			sourceFile: syntax.SourceFileSpec{},
			path:       "file",
		},
		{
			name:       "absolute",
			source:     captureProjectSource(projectFS),
			sourceFile: captureProjectFile("nested/factory.ub"),
			path:       "/nested/file",
		},
		{
			name:       "Windows absolute",
			source:     captureProjectSource(projectFS),
			sourceFile: captureProjectFile("nested/factory.ub"),
			path:       "C:/nested/file",
		},
		{
			name:       "backslash",
			source:     captureProjectSource(projectFS),
			sourceFile: captureProjectFile("nested/factory.ub"),
			path:       `nested\file`,
		},
		{
			name:       "project escape",
			source:     captureProjectSource(projectFS),
			sourceFile: captureProjectFile("nested/factory.ub"),
			path:       "../../outside",
		},
		{
			name:       "missing path",
			source:     captureProjectSource(projectFS),
			sourceFile: captureProjectFile("nested/factory.ub"),
			path:       "missing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Capture(
				tt.source,
				tt.sourceFile,
				[]syntax.AssetDecl{captureDeclaration("data", tt.path)},
				"",
			)
			require.Error(t, err)
			if tt.name == "Windows absolute" {
				require.Contains(t, err.Error(), "must be relative")
			}
		})
	}
}

func TestCaptureRejectsInvalidDeclarationNames(t *testing.T) {
	projectFS := fstest.MapFS{
		"factory.ub": &fstest.MapFile{},
		"file":       &fstest.MapFile{Data: []byte("body")},
	}
	tests := []struct {
		name         string
		declarations []syntax.AssetDecl
	}{
		{
			name:         "empty",
			declarations: []syntax.AssetDecl{captureDeclaration("", "file")},
		},
		{
			name: "duplicate",
			declarations: []syntax.AssetDecl{
				captureDeclaration("data", "file"),
				captureDeclaration("data", "file"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Capture(
				captureProjectSource(projectFS),
				captureProjectFile("factory.ub"),
				tt.declarations,
				"",
			)
			require.Error(t, err)
		})
	}
}

func TestCaptureRejectsMissingOrEmptyDeclarationSource(t *testing.T) {
	projectFS := fstest.MapFS{"factory.ub": &fstest.MapFile{}}
	tests := []syntax.AssetDecl{
		{Name: syntax.Ident{Name: "missing"}},
		captureDeclaration("empty", ""),
	}
	for _, declaration := range tests {
		t.Run(declaration.Name.Name, func(t *testing.T) {
			_, err := Capture(
				captureProjectSource(projectFS),
				captureProjectFile("factory.ub"),
				[]syntax.AssetDecl{declaration},
				"",
			)
			require.Error(t, err)
		})
	}
}

func TestCaptureRejectsUnsupportedGenericEntries(t *testing.T) {
	tests := []struct {
		name      string
		projectFS fstest.MapFS
		source    string
	}{
		{
			name: "root symlink",
			projectFS: fstest.MapFS{
				"factory.ub": &fstest.MapFile{},
				"asset":      &fstest.MapFile{Mode: fs.ModeSymlink},
			},
			source: "asset",
		},
		{
			name: "descendant symlink",
			projectFS: fstest.MapFS{
				"factory.ub": &fstest.MapFile{},
				"asset":      &fstest.MapFile{Mode: fs.ModeDir},
				"asset/link": &fstest.MapFile{Mode: fs.ModeSymlink},
			},
			source: "asset",
		},
		{
			name: "special permission bits",
			projectFS: fstest.MapFS{
				"factory.ub": &fstest.MapFile{},
				"asset":      &fstest.MapFile{Mode: fs.ModeSetuid | 0644},
			},
			source: "asset",
		},
		{
			name: "non-regular entry",
			projectFS: fstest.MapFS{
				"factory.ub": &fstest.MapFile{},
				"asset":      &fstest.MapFile{Mode: fs.ModeNamedPipe | 0644},
			},
			source: "asset",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Capture(
				captureProjectSource(tt.projectFS),
				captureProjectFile("factory.ub"),
				[]syntax.AssetDecl{captureDeclaration("data", tt.source)},
				"",
			)
			require.Error(t, err)
		})
	}
}

func TestCaptureRejectsOSSymlinks(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "target"), []byte("body"), 0644))
	require.NoError(t, os.Symlink("target", filepath.Join(project, "root-link")))
	require.NoError(t, os.Mkdir(filepath.Join(project, "tree"), 0755))
	require.NoError(t, os.Symlink("../target", filepath.Join(project, "tree", "child-link")))

	for _, source := range []string{"root-link", "tree"} {
		t.Run(source, func(t *testing.T) {
			_, err := Capture(
				captureOSProjectSource(project),
				captureProjectFile("factory.ub"),
				[]syntax.AssetDecl{captureDeclaration("data", source)},
				"",
			)
			require.Error(t, err)
			require.Contains(t, err.Error(), "symlink")
			require.NotContains(t, err.Error(), project)
		})
	}
}

func TestCaptureOSMissingPathUsesProjectRelativeDiagnostic(t *testing.T) {
	project := t.TempDir()

	_, err := Capture(
		captureOSProjectSource(project),
		captureProjectFile("factory.ub"),
		[]syntax.AssetDecl{captureDeclaration("data", "missing/file")},
		"",
	)
	require.EqualError(t, err,
		`asset "data" source "missing/file": capture missing/file: file does not exist`)
	require.NotContains(t, err.Error(), project)
}

func TestCaptureRejectsOutputOverlap(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(project, "assets", "generated"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(project, "assets", "data"),
		[]byte("body"),
		0644,
	))
	require.NoError(t, os.MkdirAll(filepath.Join(project, "output", "assets"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(project, "output", "assets", "data"),
		[]byte("body"),
		0644,
	))
	tests := []struct {
		name   string
		source string
		output string
	}{
		{
			name:   "output inside asset",
			source: "assets",
			output: filepath.Join(project, "assets", "generated"),
		},
		{
			name:   "asset inside output",
			source: "output/assets",
			output: filepath.Join(project, "output"),
		},
		{
			name:   "overlap checked before missing source",
			source: "missing",
			output: filepath.Join(project, "missing", "generated"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Capture(
				captureOSProjectSource(project),
				captureProjectFile("factory.ub"),
				[]syntax.AssetDecl{captureDeclaration("data", tt.source)},
				tt.output,
			)
			require.Error(t, err)
			require.Contains(t, err.Error(), "overlap")
		})
	}
}

func captureProjectSource(fsys fs.FS) *resolve.Source {
	return &resolve.Source{FS: fsys, ProjectFS: fsys}
}

func captureOSProjectSource(root string) *resolve.Source {
	fsys := os.DirFS(root)
	return &resolve.Source{
		FS:          fsys,
		Path:        root,
		ProjectFS:   fsys,
		ProjectPath: root,
	}
}

func captureProjectFile(path string) syntax.SourceFileSpec {
	return syntax.SourceFileSpec{ProjectRelPath: path}
}

func captureDeclaration(name, source string) syntax.AssetDecl {
	return syntax.AssetDecl{
		Name:   syntax.Ident{Name: name},
		Source: &parse.StringLit{Value: source},
	}
}

func capturedAsset(t testing.TB, set *CapturedSet, name string) CapturedAsset {
	t.Helper()
	for _, item := range set.Assets {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("asset %q not found", name)
	return CapturedAsset{}
}

func capturedEntry(t testing.TB, set *CapturedSet, assetName, internalPath string) CapturedEntry {
	t.Helper()
	for _, entry := range capturedAsset(t, set, assetName).Entries {
		if entry.InternalPath == internalPath {
			return entry
		}
	}
	t.Fatalf("asset %q entry %q not found", assetName, internalPath)
	return CapturedEntry{}
}

func capturedAssetNames(assets []CapturedAsset) []string {
	names := make([]string, 0, len(assets))
	for _, item := range assets {
		names = append(names, item.Name)
	}
	return names
}

func capturedEntryPaths(entries []CapturedEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.InternalPath)
	}
	return paths
}

func captureChangeFiles(
	mainName string,
	mainContent string,
	mainMode fs.FileMode,
	includeHelper bool,
) fstest.MapFS {
	files := fstest.MapFS{
		"factory.ub": &fstest.MapFile{},
		"assets":     &fstest.MapFile{Mode: fs.ModeDir},
		"assets/" + mainName: &fstest.MapFile{
			Data: []byte(mainContent),
			Mode: mainMode,
		},
	}
	if includeHelper {
		files["assets/helper"] = &fstest.MapFile{Data: []byte("helper\n")}
	}
	return files
}

func uniqueStrings(values map[string]string) []string {
	unique := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	slices.Sort(unique)
	return unique
}

func writeCaptureTree(t testing.TB, root string, files []string) {
	t.Helper()
	for _, name := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, os.WriteFile(path, []byte(strings.ToUpper(name)+"\n"), 0644))
	}
}
