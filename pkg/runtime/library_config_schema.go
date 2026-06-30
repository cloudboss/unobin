package runtime

import (
	"maps"
	"slices"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

type LibraryConfigSchema struct {
	Path        string
	Identity    string
	Fields      []typecheck.ObjectField
	Defaults    []lang.DefaultSpec
	Constraints []lang.ConstraintSpec
	Digest      string
	Empty       bool
}

func LibraryConfigSchemaFromLibrarySchema(
	path string,
	schema *LibrarySchema,
) (LibraryConfigSchema, bool) {
	if schema == nil || !schema.HasConfiguration {
		return LibraryConfigSchema{}, false
	}
	fields := schema.ConfigurationFields
	if fields == nil && schema.Configuration != nil {
		fields = libraryConfigFieldsFromMap(schema.Configuration)
	}
	if fields == nil {
		return LibraryConfigSchema{}, false
	}
	digest := schema.ConfigurationDigest
	if digest == "" {
		digest = cfg.DigestView(
			fields,
			schema.ConfigurationDefaults,
			schema.ConfigurationConstraints,
		)
	}
	return LibraryConfigSchema{
		Path:        path,
		Identity:    schema.ConfigurationIdentity,
		Fields:      fields,
		Defaults:    schema.ConfigurationDefaults,
		Constraints: schema.ConfigurationConstraints,
		Digest:      digest,
		Empty:       schema.ConfigurationEmpty,
	}, true
}

func LibraryConfigSchemaFromView(path string, view cfg.LibraryConfigView) LibraryConfigSchema {
	return LibraryConfigSchema{
		Path:     path,
		Identity: view.Identity,
		Fields:   view.Fields,
		Defaults: view.Defaults,
		Digest:   view.SchemaDigest,
		Empty:    view.Empty,
	}
}

func LibraryConfigSchemaFromLibrary(
	path string,
	lib *Library,
) (LibraryConfigSchema, bool, error) {
	if lib == nil {
		return LibraryConfigSchema{}, false, nil
	}
	if schema, ok := LibraryConfigSchemaFromLibrarySchema(path, lib.Schema); ok {
		return schema, true, nil
	}
	if lib.Schema != nil && lib.Schema.HasConfiguration {
		return LibraryConfigSchema{}, false, nil
	}
	view, err := cfg.View(lib.Configuration)
	if err != nil || view.SchemaDigest == "" {
		return LibraryConfigSchema{}, false, err
	}
	return LibraryConfigSchemaFromView(path, view), true, nil
}

func (s LibraryConfigSchema) TypecheckType() typecheck.Type {
	identity := s.Identity
	if identity == "" {
		identity = s.Path
	}
	return typecheck.TLibraryConfig(s.Path, identity, s.Digest, s.Fields)
}

func (s LibraryConfigSchema) LangSchema() lang.LibraryConfigSchema {
	return lang.LibraryConfigSchema{
		Type:        langTypeObjectFromCheckFields(s.Fields),
		Defaults:    s.Defaults,
		Constraints: s.Constraints,
	}
}

func libraryConfigFieldsFromMap(schema map[string]typecheck.Type) []typecheck.ObjectField {
	fields := make([]typecheck.ObjectField, 0, len(schema))
	for _, name := range slices.Sorted(maps.Keys(schema)) {
		t := schema[name]
		fields = append(fields, typecheck.ObjectField{
			Name:     name,
			Type:     t.Unwrap(),
			Optional: t.Kind == typecheck.Optional,
		})
	}
	return fields
}

func langTypeObjectFromCheckFields(fields []typecheck.ObjectField) *lang.TypeObject {
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
		return langTypeObjectFromCheckFields(t.Fields)
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
