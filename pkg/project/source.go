package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/sourcecheck"
	"github.com/cloudboss/unobin/pkg/toolchain"
)

func sourceCheckOptions(
	options Options,
	projectStart string,
	sourceDir string,
	reporter diagnostic.Reporter,
) (sourcecheck.Options, error) {
	projectDir, err := sourceProjectDir(projectStart)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	project, err := sourceProject(projectDir)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	var replaceMap map[deps.Dependency]string
	if project != nil {
		if err := deps.CheckReplacementSentinels(project); err != nil {
			return sourcecheck.Options{}, err
		}
		replaceMap = project.Replace
	}
	replaceUnobinAbs, err := UnobinReplacement(projectDir, options.ReplaceUnobin, replaceMap)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	compatibility, err := options.Compatibility(
		projectDir, project, replaceUnobinAbs)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	projectLock, err := readProjectLockOrNil(projectDir)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	resolver, err := options.NewResolver(projectDir)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	resolver = compile.WrapProjectLockSources(resolver, projectLock)
	resolver, err = compile.WrapReplaces(resolver, projectDir, options.ReplaceUnobin, replaceMap)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	repoVersions, err := compile.ProjectLockVersions(projectDir)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	repoVersions = replacedVersions(
		repoVersions, options.ReplaceUnobin != "", replaceMap)
	schemaRoots := compile.UnobinSchemaRoots(
		options.toolOutput(), replaceUnobinAbs, options.UnobinVersion)
	return sourcecheck.Options{
		ProjectDir:  projectDir,
		Source:      sourceForProjectDir(projectDir, sourceDir),
		Resolver:    resolver,
		Versions:    repoVersions,
		SchemaCache: sourcecheck.NewSchemaCacheWithCompatibility(compatibility, schemaRoots...),
		Reporter:    reporter,
	}, nil
}

func sourceForDir(path string) *resolve.Source {
	return &resolve.Source{FS: os.DirFS(path), Path: path}
}

func sourceForProjectDir(projectDir, sourceDir string) *resolve.Source {
	absoluteSourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return sourceForDir(sourceDir)
	}
	source := sourceForDir(absoluteSourceDir)
	relative, ok := relativeProjectPath(projectDir, absoluteSourceDir)
	if !ok {
		return source
	}
	if relative == "." {
		relative = ""
	}
	source.ProjectFS = os.DirFS(projectDir)
	source.ProjectPath = projectDir
	source.PackageSubdir = filepath.ToSlash(relative)
	return source
}

func sourceFileForProject(projectDir, sourcePath string) syntax.SourceFileSpec {
	spec := syntax.SourceFileSpec{
		PackageRelPath: filepath.ToSlash(filepath.Base(sourcePath)),
	}
	absoluteSourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return spec
	}
	if relative, ok := relativeProjectPath(projectDir, absoluteSourcePath); ok {
		spec.ProjectRelPath = filepath.ToSlash(relative)
	}
	return spec
}

func relativeProjectPath(projectDir, path string) (string, bool) {
	if projectDir == "" {
		return "", false
	}
	relative, err := filepath.Rel(projectDir, path)
	if err != nil || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relative, true
}

func sourceProjectDir(sourceDir string) (string, error) {
	projectDir, err := deps.FindProjectDir(sourceDir)
	if err == nil {
		return projectDir, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return sourceDir, nil
	}
	return "", err
}

func sourceProject(projectDir string) (*deps.Project, error) {
	project, err := deps.ReadProject(os.DirFS(projectDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return project, nil
}

func replacedVersions(
	versions map[string]string,
	replaceUnobin bool,
	replace map[deps.Dependency]string,
) map[string]string {
	if !replaceUnobin && len(replace) == 0 {
		return versions
	}
	if versions == nil {
		versions = map[string]string{}
	}
	if replaceUnobin {
		versions[toolchain.UnobinModulePath] = deps.ReplacementSentinel
	}
	for dep := range replace {
		if dep.Subdir == "" {
			versions[dep.URL] = deps.ReplacementSentinel
		} else {
			versions[dep.String()] = deps.ReplacementSentinel
		}
	}
	return versions
}
