package deps

import (
	"errors"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func extractSyntaxImportRefs(path string, src []byte) ([]resolve.SyntaxImport, error) {
	sf, err := parseDependencySource(path, src)
	if err != nil {
		return nil, err
	}
	refs, errs := resolve.ExtractSyntaxImports(sf)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return refs, nil
}

func extractSyntaxDependencyRefs(path string, src []byte) ([]resolve.SyntaxDependency, error) {
	sf, err := parseDependencySource(path, src)
	if err != nil {
		return nil, err
	}
	refs, errs := resolve.ExtractSyntaxDependencies(sf)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return refs, nil
}

func parseDependencySource(path string, src []byte) (*syntax.File, error) {
	sf, err := syntax.ParseSource(path, src)
	if err != nil {
		return nil, err
	}
	switch sf.Kind {
	case syntax.FileFactory, syntax.FileLibrary:
		if verrs := syntax.ValidateFile(sf); verrs.Len() > 0 {
			return nil, verrs.Err()
		}
	}
	return sf, nil
}
