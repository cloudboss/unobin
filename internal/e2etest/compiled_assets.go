package e2etest

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/asset"
)

func prepareEmptyDirectories(workspace string, paths []string) error {
	for _, relPath := range paths {
		path, err := caseChildPath(workspace, "empty directory", relPath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("create empty directory %s: %w", relPath, err)
		}
	}
	return nil
}

func removeCasePaths(workspace string, paths []string) error {
	for _, relPath := range paths {
		path, err := caseChildPath(workspace, "removal path", relPath)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("%s does not exist", relPath)
			}
			return fmt.Errorf("inspect %s: %w", relPath, err)
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove %s: %w", relPath, err)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			if err == nil {
				return fmt.Errorf("%s still exists", relPath)
			}
			return fmt.Errorf("verify removal of %s: %w", relPath, err)
		}
	}
	return nil
}

func caseChildPath(workspace, field, relPath string) (string, error) {
	if relPath == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if err := checkRelPath(field, relPath); err != nil {
		return "", err
	}
	clean := filepath.Clean(filepath.FromSlash(relPath))
	if clean == "." {
		return "", fmt.Errorf("%s must name a path under the case directory", field)
	}
	return filepath.Join(workspace, clean), nil
}

func checkAssetBundle(workspace string, check *AssetBundleCheck) error {
	if check == nil {
		return nil
	}
	body, err := readAssetBundle(workspace, "build")
	if err != nil {
		return err
	}
	catalog, err := asset.Open(body)
	if err != nil {
		return fmt.Errorf("open asset bundle: %w", err)
	}
	if err := catalog.Verify(); err != nil {
		return fmt.Errorf("verify asset bundle: %w", err)
	}
	if got := len(catalog.Sets()); got != check.SetCount {
		return fmt.Errorf("asset bundle set count is %d, want %d", got, check.SetCount)
	}
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return fmt.Errorf("open asset bundle ZIP: %w", err)
	}
	blobCount := 0
	for _, file := range reader.File {
		if strings.HasPrefix(file.Name, "blobs/") {
			blobCount++
		}
	}
	if blobCount != check.BlobCount {
		return fmt.Errorf(
			"asset bundle blob count is %d, want %d",
			blobCount,
			check.BlobCount,
		)
	}
	return nil
}

func checkAssetCache(workspace string, check *AssetCacheCheck) error {
	if check == nil {
		return nil
	}
	root := filepath.Join(workspace, filepath.FromSlash(check.Path))
	referenceCount := 0
	err := filepath.WalkDir(root, func(
		path string,
		entry fs.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".tmp-") {
			return fmt.Errorf("asset cache has incomplete directory %s", path)
		}
		if entry.Name() != "complete" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("asset cache marker %s is not a regular file", path)
		}
		referenceCount++
		return nil
	})
	if err != nil {
		return fmt.Errorf("inspect asset cache %s: %w", check.Path, err)
	}
	if referenceCount != check.ReferenceCount {
		return fmt.Errorf(
			"asset cache reference count is %d, want %d",
			referenceCount,
			check.ReferenceCount,
		)
	}
	return nil
}

func checkAssetIdentity(
	repoRoot string,
	e2eLibraryDir string,
	c CompiledCase,
	workspace string,
	check *AssetIdentityCheck,
) error {
	if check == nil {
		return nil
	}
	initialBody, err := readAssetBundle(workspace, "build")
	if err != nil {
		return err
	}
	initial, err := asset.Open(initialBody)
	if err != nil {
		return fmt.Errorf("open initial asset bundle: %w", err)
	}

	if err := replaceCaseFile(workspace, check.ChangePath, check.ReplacementPath); err != nil {
		return err
	}
	rebuildDir := filepath.Join(workspace, ".e2e", "rebuild")
	if _, err := compileCaseTo(
		repoRoot,
		e2eLibraryDir,
		c,
		workspace,
		rebuildDir,
		false,
	); err != nil {
		return fmt.Errorf("recompile asset identity fixture: %w", err)
	}
	updatedBody, err := readAssetBundle(workspace, "rebuild")
	if err != nil {
		return err
	}
	updated, err := asset.Open(updatedBody)
	if err != nil {
		return fmt.Errorf("open updated asset bundle: %w", err)
	}
	return compareAssetIdentity(initial, updated, check)
}

func readAssetBundle(workspace, buildDir string) ([]byte, error) {
	path := filepath.Join(workspace, ".e2e", buildDir, "factory.assets")
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read asset bundle: %w", err)
	}
	return body, nil
}

func replaceCaseFile(workspace, targetRel, sourceRel string) error {
	target := filepath.Join(workspace, filepath.FromSlash(targetRel))
	source := filepath.Join(workspace, filepath.FromSlash(sourceRel))
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect replacement %s: %w", sourceRel, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("replacement %s is not a regular file", sourceRel)
	}
	if err := copyFile(source, target, info.Mode().Perm()); err != nil {
		return fmt.Errorf("replace %s: %w", targetRel, err)
	}
	return nil
}

func compareAssetIdentity(
	initialCatalog *asset.Catalog,
	updatedCatalog *asset.Catalog,
	check *AssetIdentityCheck,
) error {
	initial, err := singleAssetSet(initialCatalog, check.Asset)
	if err != nil {
		return fmt.Errorf("initial bundle: %w", err)
	}
	updated, err := singleAssetSet(updatedCatalog, check.Asset)
	if err != nil {
		return fmt.Errorf("updated bundle: %w", err)
	}
	initialRoot, err := initial.Value(check.Asset, "")
	if err != nil {
		return err
	}
	updatedRoot, err := updated.Value(check.Asset, "")
	if err != nil {
		return err
	}
	if initialRoot == updatedRoot {
		return fmt.Errorf("asset %q root identity did not change", check.Asset)
	}
	initialStable, err := initial.Value(check.Asset, check.StableEntry)
	if err != nil {
		return err
	}
	updatedStable, err := updated.Value(check.Asset, check.StableEntry)
	if err != nil {
		return err
	}
	if initialStable != updatedStable {
		return fmt.Errorf(
			"asset %q entry %q identity changed",
			check.Asset,
			check.StableEntry,
		)
	}
	return nil
}

func singleAssetSet(catalog *asset.Catalog, name string) (*asset.Set, error) {
	sets := catalog.Sets()
	if len(sets) != 1 {
		return nil, fmt.Errorf("asset bundle has %d sets, want 1", len(sets))
	}
	if _, ok := sets[0].Asset(name); !ok {
		return nil, fmt.Errorf("asset %q is missing", name)
	}
	return sets[0], nil
}
