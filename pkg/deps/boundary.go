package deps

import (
	"fmt"
	"io/fs"
	pathpkg "path"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/projectmarker"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func fsHasProjectFile(fsys fs.FS, dir string) (bool, error) {
	marker, err := projectmarker.Classify(fsys, dir)
	if err != nil {
		return false, err
	}
	return marker.Kind != projectmarker.None, nil
}

func validateProjectSource(name string, b []byte) error {
	f, err := syntax.ParseSource(name, b)
	if err != nil {
		return err
	}
	if f.Kind != syntax.FileProject || f.Project == nil {
		return fmt.Errorf("%s must declare project", name)
	}
	if errs := syntax.ValidateFile(f); errs.Len() > 0 {
		return errs.Err()
	}
	return nil
}

func nearestProjectInFS(fsys fs.FS, start string) (string, bool, error) {
	dir := cleanFSPath(start)
	for {
		hasProject, err := fsHasProjectFile(fsys, dir)
		if err != nil {
			return "", false, err
		}
		if hasProject {
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

func CheckPackageBoundary(
	src *resolve.Source, owner PackageOwner, pkg RemotePackage,
) error {
	if src == nil || src.ProjectFS == nil || owner.PackageSubdir == "." {
		return nil
	}
	current := "."
	for part := range strings.SplitSeq(owner.PackageSubdir, "/") {
		if part == "" || part == "." {
			continue
		}
		current = pathpkg.Join(current, part)
		marker, err := projectmarker.Classify(src.ProjectFS, current)
		if err != nil {
			return err
		}
		if marker.Kind != projectmarker.None {
			return nestedProjectOwnershipError(owner.Project, pkg, current)
		}
	}
	return nil
}

func nestedProjectOwnershipError(owner ProjectID, pkg RemotePackage, nestedRel string) error {
	nested := ProjectID{URL: owner.URL, Subdir: nestedRel}
	if owner.Subdir != "" {
		nested.Subdir = pathpkg.Join(owner.Subdir, nestedRel)
	}
	return fmt.Errorf(
		"selected project %s does not own package %s; "+
			"the package is inside nested project %s; "+
			"add that project to project.requires or replace it directly",
		owner, pkg, nested)
}

func localImportProjectBoundaryError(importPath string) error {
	return fmt.Errorf(
		"local import %q targets a different project; "+
			"import it by dependency id and add project.replace for local development",
		importPath,
	)
}
