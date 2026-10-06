package sourcecheck

import (
	"errors"

	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// AnalyzeFactoryBody retains resolved imports and local types when body checks fail.
// Import-resolution failures return no analysis.
func AnalyzeFactoryBody(body syntax.FactoryBody, opts Options) (*program.CheckedBody, error) {
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	imports, err := analyzeFactoryImports(body, refs, opts)
	if err != nil {
		return nil, err
	}
	checker := check.NewSyntaxWithLibraryConfigSchemas(
		body, imports.Libraries, imports.LibraryConfigSchemas,
		imports.Assets.Catalog(), imports.RootAssetSetID,
	)
	analysis := &program.CheckedBody{Imports: imports, DAG: checker.DAG()}
	var observe func(lang.Expr, typecheck.Type)
	if len(body.Locals) > 0 {
		locals := make(map[lang.Expr]string, len(body.Locals))
		analysis.LocalTypes = make(map[string]typecheck.Type, len(body.Locals))
		for _, local := range body.Locals {
			if local.Value != nil {
				locals[local.Value] = local.Name.Name
			}
		}
		observe = func(expr lang.Expr, typ typecheck.Type) {
			if name, ok := locals[expr]; ok {
				analysis.LocalTypes[name] = typ
			}
		}
	}
	if errs := checker.References(observe); errs.Len() > 0 {
		return analysis, errs.Err()
	}
	if errs := checker.LiteralConstraints(); errs.Len() > 0 {
		return analysis, errs.Err()
	}
	if errs := checker.ForEachNesting(); errs.Len() > 0 {
		return analysis, errs.Err()
	}
	return analysis, nil
}
