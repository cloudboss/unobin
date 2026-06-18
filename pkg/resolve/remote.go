package resolve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/git"
	"golang.org/x/mod/semver"
)

// RemoteResolver resolves *RemoteImport refs by fetching the named
// git repo at the requested constraint, caching the working tree
// under CacheRoot, and exposing the requested subdir as a Source.
//
// CacheRoot is the directory holding `imports/<host>/<path>/<commit>/`.
// `NewRemoteResolver` defaults it to `<user-cache-dir>/unobin`.
type RemoteResolver struct {
	CacheRoot string
}

// NewRemoteResolver returns a RemoteResolver with CacheRoot set to
// the user's cache directory (XDG_CACHE_HOME or its platform default)
// joined with `unobin`.
func NewRemoteResolver() (*RemoteResolver, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return &RemoteResolver{CacheRoot: filepath.Join(cache, "unobin")}, nil
}

// GitRef returns the first git ref used to fetch ref.
func GitRef(ref *RemoteImport) string {
	refs := gitRefs(ref)
	if len(refs) == 0 {
		return ""
	}
	return refs[0]
}

func gitRefs(ref *RemoteImport) []string {
	if ref == nil {
		return []string{""}
	}
	version := ref.Version
	projectSubdir := remoteProjectSubdir(ref)
	base, ok := unprefixedVersion(projectSubdir, version)
	if !ok {
		return []string{version}
	}
	return []string{projectTag(projectSubdir, base)}
}

func unprefixedVersion(subdir, version string) (string, bool) {
	if semver.IsValid(version) {
		return version, true
	}
	if subdir == "" {
		return "", false
	}
	trimmed, ok := strings.CutPrefix(version, subdir+"/")
	if ok && semver.IsValid(trimmed) {
		return trimmed, true
	}
	return "", false
}

func projectTag(subdir, version string) string {
	if subdir == "" {
		return version
	}
	return subdir + "/" + version
}

func remoteProjectSubdir(ref *RemoteImport) string {
	if ref.ProjectSubdir != "" || ref.PackageSubdir != "" {
		return ref.ProjectSubdir
	}
	return ref.Subdir
}

func remotePackageSubdir(ref *RemoteImport) string {
	if ref.ProjectSubdir != "" || ref.PackageSubdir != "" {
		return ref.PackageSubdir
	}
	return ref.Subdir
}

// Resolve fetches the repo named by ref, caches it, and returns a
// Source rooted at the import's subdir, with FS and Commit always set.
// A UB library also gets its content Hash set for lock-file integrity.
func (r *RemoteResolver) Resolve(ref ImportRef) (*Source, error) {
	ri, ok := ref.(*RemoteImport)
	if !ok {
		return nil, fmt.Errorf("remote resolver cannot handle %T", ref)
	}
	ctx := context.Background()

	cloneURL := WithDefaultScheme(ri.URL)
	gitRef, commit, err := resolveRemoteRef(ctx, cloneURL, gitRefs(ri))
	if err != nil {
		return nil, err
	}

	dir := r.cacheDir(ri.URL, commit)
	if !dirExists(dir) {
		if err := r.fetchInto(ctx, cloneURL, gitRef, dir); err != nil {
			return nil, err
		}
	}

	subdirPath := dir
	if packageSubdir := remotePackageSubdir(ri); packageSubdir != "" {
		subdirPath = filepath.Join(dir, packageSubdir)
	}

	src := &Source{Commit: commit, Path: subdirPath, FS: os.DirFS(subdirPath)}
	if IsUBLibrary(src) {
		hash, err := hashTree(src.FS)
		if err != nil {
			return nil, err
		}
		src.Hash = hash
	}
	return src, nil
}

func resolveRemoteRef(ctx context.Context, url string, refs []string) (string, string, error) {
	var errs []error
	for _, ref := range refs {
		commit, err := git.LsRemote(ctx, url, ref)
		if err == nil {
			return ref, commit, nil
		}
		errs = append(errs, err)
	}
	return "", "", errors.Join(errs...)
}

func (r *RemoteResolver) fetchInto(ctx context.Context, url, ref, dir string) error {
	tmp := dir + ".tmp"
	_ = os.RemoveAll(tmp)
	if _, err := git.Clone(ctx, url, ref, tmp); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return nil
}

func (r *RemoteResolver) cacheDir(url, commit string) string {
	return filepath.Join(r.ImportsDir(), normalizeURL(url), commit)
}

// ImportsDir is the directory holding cached import sources, a sibling of
// the toolchain cache under CacheRoot.
func (r *RemoteResolver) ImportsDir() string {
	return filepath.Join(r.CacheRoot, "imports")
}

// CleanImports removes the cached import sources and returns the directory
// that was removed. It is a no-op when nothing is cached.
func (r *RemoteResolver) CleanImports() (string, error) {
	dir := r.ImportsDir()
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// WithDefaultScheme prepends `https://` to a bare URL like
// `github.com/owner/repo` so go-git knows to fetch it over HTTPS.
// URLs that already include a scheme (`https://`, `http://`, `ssh://`,
// `file://`, ...) or look like SCP-style ssh (`user@host:path`) or
// look like a filesystem path are left alone.
func WithDefaultScheme(url string) string {
	if strings.Contains(url, "://") {
		return url
	}
	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, ".") {
		return url
	}
	if _, after, ok := strings.Cut(url, "@"); ok {
		if strings.Contains(after, ":") {
			return url
		}
	}
	return "https://" + url
}

func normalizeURL(url string) string {
	u := url
	if _, after, ok := strings.Cut(u, "://"); ok {
		u = after
	}
	if _, after, ok := strings.Cut(u, "@"); ok {
		if before, rest, ok := strings.Cut(after, ":"); ok {
			u = before + "/" + rest
		}
	}
	return strings.Trim(u, "/")
}

// hashTree returns a stable sha256 of the file tree rooted at fsys.
// Files are walked in sorted order; per file, the hash absorbs the
// path, the size, and the body. Directories themselves contribute no
// bytes - the file paths carry the structure.
// HashTree returns a stable sha256 of a source tree.
func HashTree(fsys fs.FS) (string, error) {
	return hashTree(fsys)
}

func hashTree(fsys fs.FS) (string, error) {
	var paths []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		return "", err
	}
	slices.Sort(paths)

	h := sha256.New()
	for _, p := range paths {
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\n%d\n", p, len(body))
		h.Write(body)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
