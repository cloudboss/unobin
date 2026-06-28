package runner

import (
	"slices"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func libraryConfigInputResolver(
	body *syntax.FactoryBody,
	libs map[string]*runtime.Library,
) lang.LibraryConfigResolver {
	return func(path string) (lang.LibraryConfigSchema, bool) {
		if body == nil || libs == nil {
			return lang.LibraryConfigSchema{}, false
		}
		var out lang.LibraryConfigSchema
		var digest string
		matched := false
		for _, imp := range body.Imports {
			if imp.Ref == nil || imp.Ref.Value != path {
				continue
			}
			lib := libs[imp.Alias.Name]
			got, gotDigest, ok := libraryConfigInputSchema(lib)
			if !ok {
				return lang.LibraryConfigSchema{}, false
			}
			if !matched {
				out = got
				digest = gotDigest
				matched = true
				continue
			}
			if digest != gotDigest {
				return lang.LibraryConfigSchema{}, false
			}
		}
		return out, matched
	}
}

func libraryConfigInputSchema(
	lib *runtime.Library,
) (lang.LibraryConfigSchema, string, bool) {
	if lib == nil {
		return lang.LibraryConfigSchema{}, "", false
	}
	if lib.Schema != nil && lib.Schema.HasConfiguration {
		fields := lib.Schema.ConfigurationFields
		if fields == nil && lib.Schema.Configuration != nil {
			fields = configFieldsFromMap(lib.Schema.Configuration)
		}
		if fields == nil {
			return lang.LibraryConfigSchema{}, "", false
		}
		digest := lib.Schema.ConfigurationDigest
		if digest == "" {
			digest = cfg.DigestView(
				fields,
				lib.Schema.ConfigurationDefaults,
				lib.Schema.ConfigurationConstraints,
			)
		}
		return lang.LibraryConfigSchema{
			Type:        langTypeObjectFromFields(fields),
			Defaults:    lib.Schema.ConfigurationDefaults,
			Constraints: lib.Schema.ConfigurationConstraints,
		}, digest, true
	}
	view, err := cfg.View(lib.Configuration)
	if err != nil || view.SchemaDigest == "" {
		return lang.LibraryConfigSchema{}, "", false
	}
	return lang.LibraryConfigSchema{
		Type:     langTypeObjectFromFields(view.Fields),
		Defaults: view.Defaults,
	}, view.SchemaDigest, true
}

func configFieldsFromMap(schema map[string]typecheck.Type) []typecheck.ObjectField {
	fields := make([]typecheck.ObjectField, 0, len(schema))
	names := make([]string, 0, len(schema))
	for name := range schema {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		t := schema[name]
		fields = append(fields, typecheck.ObjectField{
			Name:     name,
			Type:     t.Unwrap(),
			Optional: t.Kind == typecheck.Optional,
		})
	}
	return fields
}

func langTypeObjectFromFields(fields []typecheck.ObjectField) *lang.TypeObject {
	out := &lang.TypeObject{Fields: make([]*lang.TypeObjectField, 0, len(fields))}
	for _, field := range fields {
		fieldType := langTypeFromCheck(field.Type)
		if field.Optional {
			fieldType = &lang.TypeOptional{Elem: fieldType}
		}
		out.Fields = append(out.Fields, &lang.TypeObjectField{
			Name: field.Name,
			Type: fieldType,
		})
	}
	return out
}

func langTypeFromCheck(t typecheck.Type) lang.TypeExpr {
	switch t.Kind {
	case typecheck.String:
		return &lang.TypeAtomic{Name: "string"}
	case typecheck.Integer:
		return &lang.TypeAtomic{Name: "integer"}
	case typecheck.Number:
		return &lang.TypeAtomic{Name: "number"}
	case typecheck.Boolean:
		return &lang.TypeAtomic{Name: "boolean"}
	case typecheck.Null:
		return &lang.TypeAtomic{Name: "null"}
	case typecheck.List:
		if t.Elem == nil {
			return &lang.TypeList{Elem: &lang.TypeAtomic{Name: "opaque"}}
		}
		return &lang.TypeList{Elem: langTypeFromCheck(*t.Elem)}
	case typecheck.Map:
		if t.Elem == nil {
			return &lang.TypeMap{Elem: &lang.TypeAtomic{Name: "opaque"}}
		}
		return &lang.TypeMap{Elem: langTypeFromCheck(*t.Elem)}
	case typecheck.Object:
		return langTypeObjectFromFields(t.Fields)
	case typecheck.Optional:
		return &lang.TypeOptional{Elem: langTypeFromCheck(t.Unwrap())}
	case typecheck.Tuple:
		elems := make([]lang.TypeExpr, 0, len(t.Elems))
		for _, elem := range t.Elems {
			elems = append(elems, langTypeFromCheck(elem))
		}
		return &lang.TypeTuple{Elements: elems}
	case typecheck.Opaque, typecheck.Unknown, typecheck.LibraryConfig, typecheck.Union:
		return &lang.TypeAtomic{Name: "opaque"}
	}
	return &lang.TypeAtomic{Name: "opaque"}
}
