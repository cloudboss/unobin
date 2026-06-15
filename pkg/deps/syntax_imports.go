package deps

import (
	"errors"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func extractSyntaxImportRefs(path string, src []byte) ([]resolve.SyntaxImport, error) {
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
	refs, errs := resolve.ExtractSyntaxImports(sf)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return refs, nil
}
