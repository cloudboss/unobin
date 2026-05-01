// Package deps caches pinned tool binary dependencies under
// ~/.cache/unobin so unobin invocations use the same toolchain
// versions across machines and don't depend on whatever happens to be
// installed on ${PATH}. Single file `Binary` dependencies live at
// ~/.cache/unobin/bin/<name>-<version>; `TarGz` archives extract to
// ~/.cache/unobin/<name>-<version>/.
package deps

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Format names how a Dependency is delivered.
type Format int

const (
	// Binary is a single executable file at the URL.
	Binary Format = iota
	// TarGz is a gzipped tar archive containing the executable.
	TarGz
)

// Platform identifies a target OS and architecture.
type Platform struct {
	OS   string
	Arch string
}

// Dependency describes one cacheable tool. URLs and SHA256 are keyed by
// the platforms supported. BinaryPath is the path to the executable
// inside the extracted archive (e.g., "go/bin/go") and is ignored when
// Format is Binary.
type Dependency struct {
	Name       string
	Version    string
	Format     Format
	URLs       map[Platform]string
	SHA256     map[Platform]string
	BinaryPath string
}

// CacheKey returns the on-disk name for this dependency.
func (d Dependency) CacheKey() string {
	return d.Name + "-" + d.Version
}

// Ensure returns the filesystem path to the cached executable for dep,
// downloading and extracting on first use. Subsequent calls hit the
// cache and skip the download.
func Ensure(dep Dependency) (string, error) {
	platform := Platform{runtime.GOOS, runtime.GOARCH}
	url, ok := dep.URLs[platform]
	if !ok {
		return "", fmt.Errorf("deps: %s: unsupported platform %s/%s",
			dep.Name, platform.OS, platform.Arch)
	}
	expectedHash, ok := dep.SHA256[platform]
	if !ok {
		return "", fmt.Errorf("deps: %s: no SHA256 for %s/%s",
			dep.Name, platform.OS, platform.Arch)
	}

	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(installPath(root, dep)), 0o755); err != nil {
		return "", err
	}

	binPath := executablePath(root, dep)
	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil
	}

	fmt.Fprintf(os.Stderr, "Downloading %s v%s...\n", dep.Name, dep.Version)

	archive, err := download(url)
	if err != nil {
		return "", err
	}
	defer os.Remove(archive)

	if err := verifyChecksum(archive, expectedHash); err != nil {
		return "", err
	}

	if err := install(archive, root, dep); err != nil {
		return "", err
	}
	if _, err := os.Stat(binPath); err != nil {
		return "", fmt.Errorf("deps: %s: installed but executable missing at %s",
			dep.Name, binPath)
	}
	fmt.Fprintf(os.Stderr, "Cached %s at %s\n", dep.Name, binPath)
	return binPath, nil
}

func cacheRoot() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("deps: locate cache dir: %w", err)
	}
	return filepath.Join(base, "unobin"), nil
}

// installPath returns the on-disk path for dep's install. Single file
// `Binary` dependencies live under <root>/bin/<key>; `TarGz` archives
// extract to <root>/<key>/.
func installPath(root string, dep Dependency) string {
	if dep.Format == Binary {
		return filepath.Join(root, "bin", dep.CacheKey())
	}
	return filepath.Join(root, dep.CacheKey())
}

func executablePath(root string, dep Dependency) string {
	install := installPath(root, dep)
	if dep.Format == Binary {
		return install
	}
	return filepath.Join(install, dep.BinaryPath)
}

func download(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("deps: download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("deps: download %s: HTTP %d", url, resp.StatusCode)
	}
	f, err := os.CreateTemp("", "unobin-deps-*")
	if err != nil {
		return "", err
	}
	var reader io.Reader = resp.Body
	if size := contentLength(resp); size > 0 {
		reader = &progressReader{reader: resp.Body, total: size}
	}
	if _, err := io.Copy(f, reader); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	fmt.Fprintln(os.Stderr)
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func contentLength(resp *http.Response) int64 {
	cl := resp.Header.Get("Content-Length")
	if cl == "" {
		return 0
	}
	n, err := strconv.ParseInt(cl, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// progressReader wraps an io.Reader and draws a single-line progress
// bar to stderr each time the integer percentage advances. The bar is
// 50 chars wide and uses `\r` to overwrite itself in place.
type progressReader struct {
	reader  io.Reader
	total   int64
	current int64
	lastPct int
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.current += int64(n)
	pct := int(pr.current * 100 / pr.total)
	if pct != pr.lastPct {
		pr.lastPct = pct
		fmt.Fprintf(os.Stderr, "\r  [%-50s] %3d%%",
			strings.Repeat("=", pct/2), pct)
	}
	return n, err
}

func verifyChecksum(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("deps: checksum mismatch: got %s, want %s", got, expected)
	}
	return nil
}

func install(archive, root string, dep Dependency) error {
	target := installPath(root, dep)
	switch dep.Format {
	case Binary:
		return installBinary(archive, target)
	case TarGz:
		return extractTarGz(archive, target)
	default:
		return fmt.Errorf("deps: unknown format %d", dep.Format)
	}
}

func installBinary(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func extractTarGz(archive, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		path := filepath.Join(target, h.Name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(h.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
				os.FileMode(h.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			_ = os.Remove(path)
			if err := os.Symlink(h.Linkname, path); err != nil {
				return err
			}
		}
	}
}
