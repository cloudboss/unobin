package projectmarker

import (
	"errors"
	"fmt"
	"io/fs"
	pathpkg "path"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"golang.org/x/mod/modfile"
)

type Kind int

const (
	None Kind = iota
	UB
	Go
)

type Marker struct {
	Kind       Kind
	ModulePath string
}

func Classify(fsys fs.FS, dir string) (Marker, error) {
	dir = cleanDir(dir)
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Marker{}, err
		}
		return Marker{}, err
	}

	manifest, manifestOK := markerEntry(entries, "manifest.ub")
	goMod, goModOK := markerEntry(entries, "go.mod")
	if manifestOK && goModOK {
		return Marker{}, fmt.Errorf("project marker %s has both manifest.ub and go.mod", dir)
	}
	if manifestOK {
		if err := validateEntry(dir, "manifest.ub", manifest); err != nil {
			return Marker{}, err
		}
		if err := validateManifest(fsys, dir); err != nil {
			return Marker{}, err
		}
		return Marker{Kind: UB}, nil
	}
	if goModOK {
		if err := validateEntry(dir, "go.mod", goMod); err != nil {
			return Marker{}, err
		}
		modulePath, err := readModulePath(fsys, dir)
		if err != nil {
			return Marker{}, err
		}
		return Marker{Kind: Go, ModulePath: modulePath}, nil
	}
	return Marker{Kind: None}, nil
}

func ClassifyRoot(fsys fs.FS) (Marker, error) {
	return Classify(fsys, ".")
}

func markerEntry(entries []fs.DirEntry, name string) (fs.DirEntry, bool) {
	for _, entry := range entries {
		if entry.Name() == name {
			return entry, true
		}
	}
	return nil, false
}

func validateEntry(dir, name string, entry fs.DirEntry) error {
	marker := markerPath(dir, name)
	if entry.Type()&fs.ModeSymlink != 0 {
		return fmt.Errorf("project marker %s is a symlink", marker)
	}
	if entry.IsDir() {
		return fmt.Errorf("project marker %s is a directory", marker)
	}
	return nil
}

func validateManifest(fsys fs.FS, dir string) error {
	name := markerPath(dir, "manifest.ub")
	b, err := fs.ReadFile(fsys, fsPath(dir, "manifest.ub"))
	if err != nil {
		return fmt.Errorf("project marker %s: %w", name, err)
	}
	parsed, err := lang.ParseSource(name, b)
	if err != nil {
		return fmt.Errorf("project marker %s: %w", name, err)
	}
	if !declaresManifest(parsed) {
		return fmt.Errorf("project marker %s: must declare manifest", name)
	}
	f, errs := syntax.LowerFile(parsed)
	if errs.Len() > 0 {
		return fmt.Errorf("project marker %s: %w", name, errs.Err())
	}
	if f.Kind != syntax.FileManifest || f.Manifest == nil {
		return fmt.Errorf("project marker %s: must declare manifest", name)
	}
	if errs := syntax.ValidateFile(f); errs.Len() > 0 {
		return fmt.Errorf("project marker %s: %w", name, errs.Err())
	}
	return nil
}

func declaresManifest(f *parse.File) bool {
	if f == nil || f.Body == nil || len(f.Body.Fields) != 1 {
		return false
	}
	field := f.Body.Fields[0]
	return field.Key.Kind == parse.FieldIdent && field.Key.Name == "manifest"
}

func readModulePath(fsys fs.FS, dir string) (string, error) {
	name := markerPath(dir, "go.mod")
	b, err := fs.ReadFile(fsys, fsPath(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("project marker %s: %w", name, err)
	}
	file, err := modfile.Parse(name, b, nil)
	if err != nil {
		return "", fmt.Errorf("project marker %s: %w", name, err)
	}
	if file.Module == nil || file.Module.Mod.Path == "" {
		return "", fmt.Errorf("project marker %s: missing module path", name)
	}
	return file.Module.Mod.Path, nil
}

func cleanDir(dir string) string {
	dir = pathpkg.Clean(strings.TrimPrefix(dir, "/"))
	if dir == "" {
		return "."
	}
	return dir
}

func fsPath(dir, name string) string {
	if dir == "." {
		return name
	}
	return pathpkg.Join(dir, name)
}

func markerPath(dir, name string) string {
	if dir == "." {
		return "./" + name
	}
	return pathpkg.Join(dir, name)
}
