package deps

import (
	"fmt"
	"io/fs"
	pathpkg "path"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/projectmarker"
)

func fsHasManifestFile(fsys fs.FS, dir string) (bool, error) {
	marker, err := projectmarker.Classify(fsys, dir)
	if err != nil {
		return false, err
	}
	return marker.Kind != projectmarker.None, nil
}

func validateManifestSource(name string, b []byte) error {
	f, err := syntax.ParseSource(name, b)
	if err != nil {
		return err
	}
	if f.Kind != syntax.FileManifest || f.Manifest == nil {
		return fmt.Errorf("%s must declare manifest", name)
	}
	if errs := syntax.ValidateFile(f); errs.Len() > 0 {
		return errs.Err()
	}
	return nil
}

func nearestManifestInFS(fsys fs.FS, start string) (string, bool, error) {
	dir := cleanFSPath(start)
	for {
		hasManifest, err := fsHasManifestFile(fsys, dir)
		if err != nil {
			return "", false, err
		}
		if hasManifest {
			return dir, true, nil
		}
		if dir == "." {
			return "", false, nil
		}
		dir = pathpkg.Dir(dir)
	}
}

func cleanFSPath(p string) string {
	p = pathpkg.Clean(strings.TrimPrefix(p, "/"))
	if p == "" {
		return "."
	}
	return p
}

func localImportProjectBoundaryError(importPath string) error {
	return fmt.Errorf(
		"local import %q targets a different project; "+
			"import it by dependency id and add manifest.replace for local development",
		importPath,
	)
}
