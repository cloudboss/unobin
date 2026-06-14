package deps

import (
	"errors"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func extractSyntaxImportRefs(f *lang.File) ([]resolve.SyntaxImport, error) {
	sf, serrs := syntax.LowerFile(f)
	if serrs.Len() > 0 {
		return nil, serrs.Err()
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
