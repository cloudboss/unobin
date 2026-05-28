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
	"sort"
	"strings"

	"github.com/cloudboss/unobin/pkg/git"
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

// Resolve fetches the repo named by ref, caches it, and returns a
// Source rooted at the import's subdir. UB libraries (a `library.ub` is
// present at the subdir root) get their FS, Commit, and Hash set;
// non-UB imports return a Source with only Commit set.
func (r *RemoteResolver) Resolve(ref ImportRef) (*Source, error) {
	ri, ok := ref.(*RemoteImport)
	if !ok {
		return nil, fmt.Errorf("remote resolver cannot handle %T", ref)
	}
	ctx := context.Background()

	cloneURL := withDefaultScheme(ri.URL)
	commit, err := git.LsRemote(ctx, cloneURL, ri.Version)
	if err != nil {
		return nil, err
	}

	dir := r.cacheDir(ri.URL, commit)
	if !dirExists(dir) {
		if err := r.fetchInto(ctx, cloneURL, ri.Version, dir); err != nil {
			return nil, err
		}
	}

	subdirPath := dir
	if ri.Subdir != "" {
		subdirPath = filepath.Join(dir, ri.Subdir)
	}

	src := &Source{Commit: commit, Path: subdirPath}
	if _, err := os.Stat(filepath.Join(subdirPath, "library.ub")); err == nil {
		src.FS = os.DirFS(subdirPath)
		hash, err := hashTree(src.FS)
		if err != nil {
			return nil, err
		}
		src.Hash = hash
	}
	return src, nil
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
	return filepath.Join(r.CacheRoot, "imports", normalizeURL(url), commit)
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// withDefaultScheme prepends `https://` to a bare URL like
// `github.com/owner/repo` so go-git knows to fetch it over HTTPS.
// URLs that already carry a scheme (`https://`, `http://`, `ssh://`,
// `file://`, ...) or look like SCP-style ssh (`user@host:path`) or
// look like a filesystem path are left alone.
func withDefaultScheme(url string) string {
	if strings.Contains(url, "://") {
		return url
	}
	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, ".") {
		return url
	}
	if _, after, ok := strings.Cut(url, "@"); ok {
		if colon := strings.Index(after, ":"); colon >= 0 {
			return url
		}
	}
	return "https://" + url
}

func normalizeURL(url string) string {
	u := url
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if at := strings.Index(u, "@"); at >= 0 {
		if colon := strings.Index(u[at+1:], ":"); colon >= 0 {
			u = u[at+1:at+1+colon] + "/" + u[at+1+colon+1:]
		}
	}
	return strings.Trim(u, "/")
}

// hashTree returns a stable sha256 of the file tree rooted at fsys.
// Files are walked in sorted order; per file, the hash absorbs the
// path, the size, and the body. Directories themselves contribute no
// bytes - the file paths carry the structure.
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
	sort.Strings(paths)

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

// ErrRemoteNotImplemented is retained for callers that switched on it
// while the resolver was a stub. New callers should not depend on it.
var ErrRemoteNotImplemented = errors.New("remote resolver not implemented")
