package runner

import (
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func libraryConfigInputResolver(
	body *syntax.FactoryBody,
	libs map[string]*runtime.Library,
	libraryConfigSchemas map[string]runtime.LibraryConfigSchema,
) lang.LibraryConfigResolver {
	return func(path string) (lang.LibraryConfigSchema, bool) {
		if schema, ok := libraryConfigSchemas[path]; ok {
			return schema.LangSchema(), true
		}
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
			got, gotDigest, ok := libraryConfigInputSchema(path, lib)
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
	path string,
	lib *runtime.Library,
) (lang.LibraryConfigSchema, string, bool) {
	schema, ok, err := runtime.LibraryConfigSchemaFromLibrary(path, lib)
	if err != nil || !ok {
		return lang.LibraryConfigSchema{}, "", false
	}
	return schema.LangSchema(), schema.Digest, true
}
