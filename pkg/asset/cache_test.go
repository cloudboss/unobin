package asset

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func TestCacheMaterializesRegularFilePath(t *testing.T) {
	catalog, set := cacheCatalog(t, bundleFileSet(t, "program", "body\n"))
	value, err := set.Value("program", "")
	require.NoError(t, err)
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	cache, err := NewCache(catalog, cacheRoot)
	require.NoError(t, err)
	require.NoDirExists(t, cacheRoot)

	resolved, err := cache.Resolve(string(value.Path))
	require.NoError(t, err)
	path := resolved.(string)

	require.Equal(t, "program", filepath.Base(path))
	require.Equal(t, []byte("body\n"), mustReadFile(t, path))
	require.Equal(t, fs.FileMode(0o644), mustStat(t, path).Mode().Perm())
	marker := filepath.Join(filepath.Dir(filepath.Dir(path)), "complete")
	require.FileExists(t, marker)
	require.Equal(t, fs.FileMode(0o444), mustStat(t, marker).Mode().Perm())
}

func TestCacheMaterializesExecutableFilePath(t *testing.T) {
	captured := bundleCapture(t, fstest.MapFS{
		"factory.ub": &fstest.MapFile{},
		"tool":       &fstest.MapFile{Data: []byte("#!/bin/sh\n"), Mode: 0o755},
	}, []syntax.AssetDecl{
		captureDeclaration("tool", "tool"),
	})
	catalog, set := cacheCatalog(t, captured)
	value, err := set.Value("tool", "")
	require.NoError(t, err)
	cache, err := NewCache(catalog, filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)

	resolved, err := cache.Resolve(string(value.Path))
	require.NoError(t, err)
	path := resolved.(string)

	require.Equal(t, fs.FileMode(0o755), mustStat(t, path).Mode().Perm())
}

func TestCacheMaterializesDirectoryPath(t *testing.T) {
	catalog, set := cacheCatalog(t, bundleDirectorySet(t))
	value, err := set.Value("tree", "")
	require.NoError(t, err)
	cache, err := NewCache(catalog, filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)

	resolved, err := cache.Resolve(string(value.Path))
	require.NoError(t, err)
	tree := resolved.(string)

	require.Equal(t, []byte("body\n"), mustReadFile(t, filepath.Join(tree, "nested", "file")))
	require.DirExists(t, filepath.Join(tree, "empty"))
	require.Equal(t, fs.FileMode(0o755), mustStat(t, tree).Mode().Perm())
	require.Equal(
		t,
		fs.FileMode(0o755),
		mustStat(t, filepath.Join(tree, "nested")).Mode().Perm(),
	)
	require.Equal(
		t,
		fs.FileMode(0o644),
		mustStat(t, filepath.Join(tree, "nested", "file")).Mode().Perm(),
	)
}

func TestCacheMaterializesFileAndDirectoryContent(t *testing.T) {
	fileCatalog, fileSet := cacheCatalog(t, bundleFileSet(t, "program", "body\n"))
	fileValue, err := fileSet.Value("program", "")
	require.NoError(t, err)
	fileCache, err := NewCache(fileCatalog, filepath.Join(t.TempDir(), "file-cache"))
	require.NoError(t, err)

	first, err := fileCache.Resolve(string(fileValue.Content))
	require.NoError(t, err)
	require.Equal(t, []byte("body\n"), first)
	fileReference, ok := fileCatalog.Reference(string(fileValue.Content))
	require.True(t, ok)
	fileDir, err := fileCache.referenceDirectory(fileReference)
	require.NoError(t, err)
	require.Equal(
		t,
		fs.FileMode(0o444),
		mustStat(t, filepath.Join(fileDir, "content.bin")).Mode().Perm(),
	)
	first.([]byte)[0] = 'X'
	second, err := fileCache.Resolve(string(fileValue.Content))
	require.NoError(t, err)
	require.Equal(t, []byte("body\n"), second)

	treeCatalog, treeSet := cacheCatalog(t, bundleDirectorySet(t))
	treeValue, err := treeSet.Value("tree", "")
	require.NoError(t, err)
	treeCache, err := NewCache(treeCatalog, filepath.Join(t.TempDir(), "tree-cache"))
	require.NoError(t, err)

	content, err := treeCache.Resolve(string(treeValue.Content))
	require.NoError(t, err)
	require.Equal(t, treeValue.ContentSHA256, contentSHA256(content.([]byte)))
}

func TestCacheRootSelectionIsLazy(t *testing.T) {
	explicitRoot := filepath.Join(t.TempDir(), "explicit")
	explicit, err := NewCache(nil, explicitRoot)
	require.NoError(t, err)
	got, err := explicit.Root()
	require.NoError(t, err)
	require.Equal(t, explicitRoot, got)
	require.NoDirExists(t, explicitRoot)

	base := filepath.Join(t.TempDir(), "platform")
	platform, err := newCache(nil, "", func() (string, error) {
		return base, nil
	})
	require.NoError(t, err)
	got, err = platform.Root()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(base, "unobin", "assets"), got)
	require.NoDirExists(t, got)
}

func TestCacheNormalizesRelativeRoot(t *testing.T) {
	cache, err := NewCache(nil, filepath.Join("relative", "asset-cache"))
	require.NoError(t, err)
	got, err := cache.Root()
	require.NoError(t, err)
	want, err := filepath.Abs(filepath.Join("relative", "asset-cache"))
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.NoDirExists(t, got)
}

func TestCacheDefersPlatformRootErrorUntilResolution(t *testing.T) {
	cache, err := newCache(nil, "", func() (string, error) {
		return "", errors.New("no user cache")
	})
	require.NoError(t, err)

	_, err = cache.Root()
	require.EqualError(
		t,
		err,
		"asset cache: find platform cache directory: no user cache; "+
			"pass --asset-cache-dir",
	)
}

func TestCacheConcurrentResolutionUsesOneCompleteResult(t *testing.T) {
	catalog, set := cacheCatalog(t, bundleDirectorySet(t))
	value, err := set.Value("tree", "")
	require.NoError(t, err)
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	first, err := NewCache(catalog, cacheRoot)
	require.NoError(t, err)
	second, err := NewCache(catalog, cacheRoot)
	require.NoError(t, err)

	const count = 24
	results := make([]string, count)
	errs := make([]error, count)
	var wait sync.WaitGroup
	for i := range count {
		wait.Go(func() {
			cache := first
			if i%2 == 1 {
				cache = second
			}
			resolved, resolveErr := cache.Resolve(string(value.Path))
			errs[i] = resolveErr
			if resolveErr == nil {
				results[i] = resolved.(string)
			}
		})
	}
	wait.Wait()

	for _, resolveErr := range errs {
		require.NoError(t, resolveErr)
	}
	for _, result := range results {
		require.Equal(t, results[0], result)
	}
	reference, ok := catalog.Reference(string(value.Path))
	require.True(t, ok)
	finalDir, err := first.referenceDirectory(reference)
	require.NoError(t, err)
	require.DirExists(t, finalDir)
}

func TestCacheConcurrentProcessesPublishOneCompleteResult(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	commands := make([]*exec.Cmd, 2)
	var outputs [2]bytes.Buffer
	for i := range commands {
		command := exec.Command(
			os.Args[0],
			"-test.run=^TestCacheProcessMaterializeHelper$",
		)
		command.Env = append(
			os.Environ(),
			"TEST_ASSET_CACHE_PROCESS_ROOT="+cacheRoot,
		)
		command.Stdout = &outputs[i]
		command.Stderr = &outputs[i]
		require.NoError(t, command.Start())
		commands[i] = command
	}
	for i := range commands {
		require.NoError(t, commands[i].Wait(), outputs[i].String())
	}

	catalog, set := cacheCatalog(t, bundleFileSet(t, "program", "body\n"))
	value, err := set.Value("program", "")
	require.NoError(t, err)
	historical, err := NewCache(nil, cacheRoot)
	require.NoError(t, err)
	resolved, err := historical.Resolve(string(value.Path))
	require.NoError(t, err)
	require.Equal(t, []byte("body\n"), mustReadFile(t, resolved.(string)))
	require.NotNil(t, catalog)
}

func TestCacheProcessMaterializeHelper(t *testing.T) {
	cacheRoot := os.Getenv("TEST_ASSET_CACHE_PROCESS_ROOT")
	if cacheRoot == "" {
		return
	}
	catalog, set := cacheCatalog(t, bundleFileSet(t, "program", "body\n"))
	value, err := set.Value("program", "")
	require.NoError(t, err)
	cache, err := NewCache(catalog, cacheRoot)
	require.NoError(t, err)
	_, err = cache.Resolve(string(value.Path))
	require.NoError(t, err)
}

func TestCacheIgnoresInterruptedTemporaryDirectory(t *testing.T) {
	catalog, set := cacheCatalog(t, bundleFileSet(t, "program", "body\n"))
	value, err := set.Value("program", "")
	require.NoError(t, err)
	cache, err := NewCache(catalog, filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)
	reference, ok := catalog.Reference(string(value.Path))
	require.True(t, ok)
	finalDir, err := cache.referenceDirectory(reference)
	require.NoError(t, err)
	identityDir := filepath.Dir(finalDir)
	interrupted := filepath.Join(identityDir, ".tmp-interrupted")
	require.NoError(t, os.MkdirAll(interrupted, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(interrupted, "partial"), []byte("bad"), 0o600))

	resolved, err := cache.Resolve(string(value.Path))
	require.NoError(t, err)
	require.Equal(t, []byte("body\n"), mustReadFile(t, resolved.(string)))
	require.DirExists(t, interrupted)
}

func TestCacheRejectsSymlinkInCompletedTree(t *testing.T) {
	catalog, set := cacheCatalog(t, bundleDirectorySet(t))
	value, err := set.Value("tree", "")
	require.NoError(t, err)
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	current, err := NewCache(catalog, cacheRoot)
	require.NoError(t, err)
	resolved, err := current.Resolve(string(value.Path))
	require.NoError(t, err)
	tree := resolved.(string)
	target := filepath.Join(tree, "nested", "file")
	require.NoError(t, os.Remove(target))
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "outside"), target))

	reopened, err := NewCache(catalog, cacheRoot)
	require.NoError(t, err)
	_, err = reopened.Resolve(string(value.Path))
	require.ErrorContains(t, err, "symlink")
}

func TestCacheRejectsSymlinkPayloadParent(t *testing.T) {
	catalog, set := cacheCatalog(t, bundleFileSet(t, "program", "body\n"))
	value, err := set.Value("program", "")
	require.NoError(t, err)
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	current, err := NewCache(catalog, cacheRoot)
	require.NoError(t, err)
	resolved, err := current.Resolve(string(value.Path))
	require.NoError(t, err)
	path := resolved.(string)
	payloadDir := filepath.Dir(path)
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Remove(payloadDir))
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "program"), []byte("body\n"), 0o644))
	require.NoError(t, os.Symlink(outside, payloadDir))

	reopened, err := NewCache(catalog, cacheRoot)
	require.NoError(t, err)
	_, err = reopened.Resolve(string(value.Path))
	require.ErrorContains(t, err, "symlink")
}

func TestCacheResolvesHistoricalReference(t *testing.T) {
	catalog, set := cacheCatalog(t, bundleFileSet(t, "program", "body\n"))
	value, err := set.Value("program", "")
	require.NoError(t, err)
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	current, err := NewCache(catalog, cacheRoot)
	require.NoError(t, err)
	_, err = current.Resolve(string(value.Content))
	require.NoError(t, err)

	historical, err := NewCache(nil, cacheRoot)
	require.NoError(t, err)
	resolved, err := historical.Resolve(string(value.Content))
	require.NoError(t, err)
	require.Equal(t, []byte("body\n"), resolved)
}

func TestCacheReportsMissingHistoricalReference(t *testing.T) {
	captured := bundleFileSet(t, "program", "body\n")
	catalog, set := cacheCatalog(t, captured)
	value, err := set.Value("program", "")
	require.NoError(t, err)
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	cache, err := NewCache(nil, cacheRoot)
	require.NoError(t, err)

	_, err = cache.Resolve(string(value.Path))
	require.EqualError(
		t,
		err,
		"asset <asset.program.path> is not present in the embedded bundle or "+
			"cache "+cacheRoot+"; use the cache directory from the earlier factory run",
	)
	require.NotNil(t, catalog)
}

func TestCacheRejectsInvalidReferencesAndPaths(t *testing.T) {
	cache, err := NewCache(nil, filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)
	_, err = cache.Resolve("unobin-asset:not-valid")
	require.EqualError(t, err, `asset reference "unobin-asset:not-valid" is invalid`)

	for _, path := range []string{
		"",
		".",
		"..",
		"../escape",
		"/absolute",
		`back\slash`,
		"double//slash",
		"trailing/",
	} {
		t.Run(path, func(t *testing.T) {
			require.Error(t, validateMaterializePath(path))
		})
	}
}

func cacheCatalog(t testing.TB, captured *CapturedSet) (*Catalog, *Set) {
	t.Helper()
	var collection Collection
	require.NoError(t, collection.Add(captured))
	catalog := collection.Catalog()
	set, ok := catalog.Set(captured.ID)
	require.True(t, ok)
	return catalog, set
}

func mustReadFile(t testing.TB, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return content
}

func mustStat(t testing.TB, path string) fs.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info
}
