package compile

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/module"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/toolchain"
)

// unobinModuleRoot locates the unobin source a factory build will
// link, so schema extraction can read types that live in unobin's own
// packages. A replacement directory serves directly when configured;
// otherwise the module cache holds this CLI's own pinned version, and
// download fetches it on a cache miss. ok is false when no source is
// reachable, and schema extraction degrades to its unchecked-fields
// warning. A development build has no version to look up; compile
// requires a replace for one before this runs.
func unobinModuleRoot(
	replaceAbs, version string, download func(version string) error,
) (goschema.ModuleRoot, bool) {
	if replaceAbs != "" {
		return goschema.ModuleRoot{Path: toolchain.UnobinModulePath, Dir: replaceAbs}, true
	}
	if version == "" || version == "dev" {
		return goschema.ModuleRoot{}, false
	}
	escapedPath, err := module.EscapePath(toolchain.UnobinModulePath)
	if err != nil {
		return goschema.ModuleRoot{}, false
	}
	escapedVersion, err := module.EscapeVersion(version)
	if err != nil {
		return goschema.ModuleRoot{}, false
	}
	cache := goModCacheDir()
	if cache == "" {
		return goschema.ModuleRoot{}, false
	}
	dir := filepath.Join(cache, filepath.FromSlash(escapedPath)+"@"+escapedVersion)
	if !dirExists(dir) {
		if download == nil || download(version) != nil || !dirExists(dir) {
			return goschema.ModuleRoot{}, false
		}
	}
	return goschema.ModuleRoot{Path: toolchain.UnobinModulePath, Dir: dir}, true
}

// goModCacheDir resolves the module cache the way the go command
// does: GOMODCACHE wins, then the first GOPATH entry's pkg/mod,
// then the default GOPATH under the home directory.
func goModCacheDir() string {
	if v := os.Getenv("GOMODCACHE"); v != "" {
		return v
	}
	if v := os.Getenv("GOPATH"); v != "" {
		first := strings.Split(v, string(os.PathListSeparator))[0]
		return filepath.Join(first, "pkg", "mod")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "go", "pkg", "mod")
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// downloadUnobinModule fetches the unobin module into the module
// cache. The go command resolves an explicit module@version download
// only from inside a module, so the command runs in a throwaway one.
func downloadUnobinModule(stderr io.Writer, version string) error {
	goBin, err := toolchain.Ensure(stderr)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "unobin-modroot-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	gomod := "module m\n\ngo " + GoMajorMinor() + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return err
	}
	cmd := exec.Command(goBin, "mod", "download", toolchain.UnobinModulePath+"@"+version)
	cmd.Dir = dir
	cmd.Stderr = stderr
	return cmd.Run()
}
