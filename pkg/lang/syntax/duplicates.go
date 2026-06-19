package syntax

import (
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
)

func validateUniqueObjectFields(root parse.Expr, errs *parse.ErrorList) {
	selectorBodies := map[*parse.ObjectLit]bool{}
	lang.Walk(root, func(expr parse.Expr) {
		obj, ok := expr.(*parse.ObjectLit)
		if !ok {
			return
		}
		for _, fld := range obj.Fields {
			if fld.Decl != nil && fld.Decl.Body != nil {
				selectorBodies[fld.Decl.Body] = true
			}
		}
	})
	lang.Walk(root, func(expr parse.Expr) {
		obj, ok := expr.(*parse.ObjectLit)
		if !ok {
			return
		}
		validateUniqueObjectFieldNames(obj, errs, selectorBodies[obj])
	})
}

func validateUniqueObjectFieldNames(
	obj *parse.ObjectLit,
	errs *parse.ErrorList,
	skipMeta bool,
) {
	seen := make(map[string]parse.Position, len(obj.Fields))
	for _, fld := range obj.Fields {
		name, ok := duplicateObjectFieldName(fld, skipMeta)
		if !ok {
			continue
		}
		if prev, dup := seen[name]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"duplicate object field %q (first defined at %s)", name, prev)
			continue
		}
		seen[name] = fld.Key.S.Start
	}
}

func duplicateObjectFieldName(fld *parse.Field, skipMeta bool) (string, bool) {
	if fld == nil || fld.Decl != nil || (skipMeta && fld.Key.IsMeta()) {
		return "", false
	}
	switch fld.Key.Kind {
	case parse.FieldIdent:
		return fld.Key.Name, true
	case parse.FieldString:
		return fld.Key.String, true
	case parse.FieldPath:
		if len(fld.Key.Path) == 0 {
			return "", false
		}
		return strings.Join(fld.Key.Path, "."), true
	default:
		return "", false
	}
}
