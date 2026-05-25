package deps

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheKey(t *testing.T) {
	assert.Equal(t, "go-1.26.2", Dependency{Name: "go", Version: "1.26.2"}.CacheKey())
}

func TestCacheHitBinary(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	binDir := filepath.Join(cache, "unobin", "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	binPath := filepath.Join(binDir, "demo-1.0.0")
	require.NoError(t, os.WriteFile(binPath, []byte("cached"), 0o755))

	got, err := Ensure(Dependency{
		Name:    "demo",
		Version: "1.0.0",
		Format:  Binary,
		URLs:    map[Platform]string{{runtime.GOOS, runtime.GOARCH}: "http://unused"},
		SHA256:  map[Platform]string{{runtime.GOOS, runtime.GOARCH}: "unused"},
	})
	require.NoError(t, err)
	assert.Equal(t, binPath, got)
}

func TestCacheHitTarGz(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	binPath := filepath.Join(cache, "unobin", "demo-1.0.0", "bin", "demo")
	require.NoError(t, os.MkdirAll(filepath.Dir(binPath), 0o755))
	require.NoError(t, os.WriteFile(binPath, []byte("cached"), 0o755))

	got, err := Ensure(Dependency{
		Name:       "demo",
		Version:    "1.0.0",
		Format:     TarGz,
		BinaryPath: "bin/demo",
		URLs:       map[Platform]string{{runtime.GOOS, runtime.GOARCH}: "http://unused"},
		SHA256:     map[Platform]string{{runtime.GOOS, runtime.GOARCH}: "unused"},
	})
	require.NoError(t, err)
	assert.Equal(t, binPath, got)
}

func TestUnsupportedPlatform(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	_, err := Ensure(Dependency{
		Name:    "demo",
		Version: "1.0.0",
		URLs:    map[Platform]string{},
		SHA256:  map[Platform]string{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported platform")
}

func TestChecksumMismatch(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.WriteFile(tmp, []byte("hello"), 0o644))

	err := verifyChecksum(tmp, "0000000000000000000000000000000000000000000000000000000000000000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func TestChecksumMatch(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.WriteFile(tmp, []byte("hello"), 0o644))

	// SHA256("hello")
	err := verifyChecksum(tmp,
		"2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")
	assert.NoError(t, err)
}

func TestExtractTarGz(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "archive.tar.gz")
	writeTarGz(t, archive, map[string]string{
		"go/bin/go":     "i am go",
		"go/pkg/README": "pkg files",
	})

	target := filepath.Join(t.TempDir(), "out")
	require.NoError(t, extractTarGz(archive, target))

	got, err := os.ReadFile(filepath.Join(target, "go", "bin", "go"))
	require.NoError(t, err)
	assert.Equal(t, "i am go", string(got))

	got, err = os.ReadFile(filepath.Join(target, "go", "pkg", "README"))
	require.NoError(t, err)
	assert.Equal(t, "pkg files", string(got))
}

func TestExtractZip(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "archive.zip")
	writeZip(t, archive, map[string]string{
		"models/iam/service.json":      "{\"iam\":true}",
		"models/ec2/service.json":      "{\"ec2\":true}",
		"models/s3/inner/service.json": "{\"s3\":true}",
	})

	target := filepath.Join(t.TempDir(), "out")
	require.NoError(t, extractZip(archive, target))

	got, err := os.ReadFile(filepath.Join(target, "models", "iam", "service.json"))
	require.NoError(t, err)
	assert.Equal(t, "{\"iam\":true}", string(got))

	got, err = os.ReadFile(filepath.Join(target, "models", "s3", "inner", "service.json"))
	require.NoError(t, err)
	assert.Equal(t, "{\"s3\":true}", string(got))
}

func TestExtractZipRejectsPathTraversal(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "evil.zip")
	writeZip(t, archive, map[string]string{
		"../escape.txt": "should not extract",
	})

	target := filepath.Join(t.TempDir(), "out")
	err := extractZip(archive, target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes target")
}

func TestEnsureDownloadsAndExtracts(t *testing.T) {
	contents := map[string]string{"bin/demo": "fresh download"}
	tarBytes := buildTarGz(t, contents)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarBytes)
	}))
	defer srv.Close()

	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	dep := Dependency{
		Name:       "demo",
		Version:    "1.0.0",
		Format:     TarGz,
		BinaryPath: "bin/demo",
		URLs:       map[Platform]string{{runtime.GOOS, runtime.GOARCH}: srv.URL + "/archive.tar.gz"},
		SHA256: map[Platform]string{
			{runtime.GOOS, runtime.GOARCH}: sha256OfBytes(t, tarBytes),
		},
	}

	got, err := Ensure(dep)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cache, "unobin", "demo-1.0.0", "bin", "demo"), got)

	bin, err := os.ReadFile(got)
	require.NoError(t, err)
	assert.Equal(t, "fresh download", string(bin))
}

func TestEnsureRejectsBadChecksum(t *testing.T) {
	tarBytes := buildTarGz(t, map[string]string{"bin/demo": "data"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarBytes)
	}))
	defer srv.Close()

	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	_, err := Ensure(Dependency{
		Name:       "demo",
		Version:    "1.0.0",
		Format:     TarGz,
		BinaryPath: "bin/demo",
		URLs:       map[Platform]string{{runtime.GOOS, runtime.GOARCH}: srv.URL + "/x.tar.gz"},
		SHA256: map[Platform]string{
			{
				runtime.GOOS,
				runtime.GOARCH,
			}: "0000000000000000000000000000000000000000000000000000000000000000",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func TestRegistryHasCurrentPlatform(t *testing.T) {
	platform := Platform{runtime.GOOS, runtime.GOARCH}
	for _, dep := range All {
		_, ok := dep.URLs[platform]
		assert.True(t, ok, "%s missing URL for %s/%s",
			dep.Name, runtime.GOOS, runtime.GOARCH)
		_, ok = dep.SHA256[platform]
		assert.True(t, ok, "%s missing SHA256 for %s/%s",
			dep.Name, runtime.GOOS, runtime.GOARCH)
	}
}

func writeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, buildTarGz(t, files), 0o644))
}

func buildTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func sha256OfBytes(t *testing.T, b []byte) string {
	t.Helper()
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, buildZip(t, files), 0o644))
}

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
