package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/sourcecheck"
)

type SourceTarget struct {
	Path string
	Type string
}

func CheckSource(
	options Options,
	reporter diagnostic.Reporter,
) (SourceTarget, error) {
	path := options.Path
	if options.NewResolver == nil {
		options.NewResolver = compile.NewProjectResolver
	}
	info, err := os.Stat(path)
	if err != nil {
		return SourceTarget{}, err
	}
	if info.IsDir() {
		return checkSourceDir(options, path, reporter)
	}
	return checkSourceFile(options, path, reporter)
}

func checkSourceDir(
	options Options,
	path string,
	reporter diagnostic.Reporter,
) (SourceTarget, error) {
	factoryPath := filepath.Join(path, "factory.ub")
	if info, err := os.Stat(factoryPath); err == nil && !info.IsDir() {
		return checkSourceFile(options, factoryPath, reporter)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return SourceTarget{}, err
	}

	source := sourceForDir(path)
	if resolve.HasCompositeExports(source) {
		target := SourceTarget{Path: cleanCheckPath(path), Type: "library"}
		opts, err := sourceCheckOptions(options, path, path, reporter)
		if err != nil {
			return target, err
		}
		return target, sourcecheck.CheckUBLibrary(opts.Source, opts)
	}

	target := SourceTarget{Path: cleanCheckPath(path), Type: "directory"}
	checked := false
	for _, name := range []string{"project.ub", "project-lock.ub"} {
		candidate := filepath.Join(path, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			if _, err := parseAndValidateSource(candidate); err != nil {
				return target, err
			}
			checked = true
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return target, err
		}
	}
	if checked {
		return target, nil
	}
	return SourceTarget{}, fmt.Errorf("%s has no checkable Unobin source", path)
}

func checkSourceFile(
	options Options,
	path string,
	reporter diagnostic.Reporter,
) (SourceTarget, error) {
	target := SourceTarget{Path: cleanCheckPath(path), Type: checkTypeFromName(path)}
	file, err := parseAndValidateSource(path)
	if err != nil {
		return target, err
	}
	target.Type = checkTypeFromKind(file.Kind)
	dir := filepath.Dir(path)
	switch file.Kind {
	case syntax.FileFactory:
		opts, err := sourceCheckOptions(options, dir, dir, reporter)
		if err != nil {
			return target, err
		}
		opts.RootSourceFile = sourceFileForProject(opts.ProjectDir, path)
		_, err = sourcecheck.CheckFactoryBody(file.Factory.Body, opts)
		return target, err
	case syntax.FileLibrary:
		opts, err := sourceCheckOptions(options, dir, dir, reporter)
		if err != nil {
			return target, err
		}
		opts.RootSourceFile = sourceFileForProject(opts.ProjectDir, path)
		return target, sourcecheck.CheckLibraryFile(file.Library, opts)
	case syntax.FileStack, syntax.FileProject, syntax.FileProjectLock:
		return target, nil
	default:
		return SourceTarget{}, fmt.Errorf("%s has no checkable Unobin source", path)
	}
}

func cleanCheckPath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func checkTypeFromName(path string) string {
	switch filepath.Base(path) {
	case "factory.ub":
		return "factory"
	case "library.ub":
		return "library"
	case "project.ub":
		return "project"
	case "project-lock.ub":
		return "project-lock"
	default:
		return ""
	}
}

func checkTypeFromKind(kind syntax.FileKind) string {
	switch kind {
	case syntax.FileFactory:
		return "factory"
	case syntax.FileLibrary:
		return "library"
	case syntax.FileStack:
		return "stack"
	case syntax.FileProject:
		return "project"
	case syntax.FileProjectLock:
		return "project-lock"
	default:
		return ""
	}
}

func parseAndValidateSource(path string) (*syntax.File, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	file, err := syntax.ParseSource(path, body)
	if err != nil {
		return nil, err
	}
	if errs := syntax.ValidateFile(file); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return file, nil
}
