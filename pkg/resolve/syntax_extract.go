package resolve

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

// SyntaxImport is one parsed import from a typed syntax file.
type SyntaxImport struct {
	Scope string
	Alias string
	Ref   ImportRef
}

// ExtractSyntaxImports walks a typed syntax file and parses every import.
func ExtractSyntaxImports(f *syntax.File) ([]SyntaxImport, []error) {
	if f == nil {
		return nil, nil
	}
	var out []SyntaxImport
	var errs []error
	switch f.Kind {
	case syntax.FileFactory:
		if f.Factory != nil {
			out, errs = appendSyntaxImports(out, errs, "", f.Factory.Body.Imports)
		}
	case syntax.FileLibrary:
		if f.Library != nil {
			for _, export := range f.Library.Exports {
				scope := string(export.Kind) + "." + export.Name.Name
				out, errs = appendSyntaxImports(out, errs, scope, export.Body.Imports)
			}
		}
	}
	return out, errs
}

func appendSyntaxImports(
	out []SyntaxImport,
	errs []error,
	scope string,
	decls []syntax.ImportDecl,
) ([]SyntaxImport, []error) {
	for _, decl := range decls {
		if decl.Ref == nil {
			continue
		}
		ref, err := ParseImportRef(decl.Ref.Value)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"import %q: %w", importLabel(scope, decl.Alias.Name), err))
			continue
		}
		out = append(out, SyntaxImport{Scope: scope, Alias: decl.Alias.Name, Ref: ref})
	}
	return out, errs
}

func importLabel(scope, alias string) string {
	if scope == "" {
		return alias
	}
	return scope + "." + alias
}
