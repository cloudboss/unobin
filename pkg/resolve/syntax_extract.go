package resolve

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

// SyntaxImport is one parsed import from a typed syntax file.
type SyntaxImport struct {
	Scope string
	Alias string
	Ref   ImportRef
}

// SyntaxDependencyKind classifies a typed source dependency.
type SyntaxDependencyKind string

const (
	SyntaxDependencyImport        SyntaxDependencyKind = "import"
	SyntaxDependencyLibraryConfig SyntaxDependencyKind = "library-config"
)

// SyntaxDependency is one dependency reference from typed source.
type SyntaxDependency struct {
	Scope string
	Label string
	Kind  SyntaxDependencyKind
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

// ExtractSyntaxDependencies walks a typed syntax file and parses dependency refs.
func ExtractSyntaxDependencies(f *syntax.File) ([]SyntaxDependency, []error) {
	if f == nil {
		return nil, nil
	}
	var out []SyntaxDependency
	var errs []error
	switch f.Kind {
	case syntax.FileFactory:
		if f.Factory != nil {
			out, errs = appendSyntaxDependencies(out, errs, "", f.Factory.Body)
		}
	case syntax.FileLibrary:
		if f.Library != nil {
			for _, export := range f.Library.Exports {
				scope := string(export.Kind) + "." + export.Name.Name
				out, errs = appendSyntaxDependencies(out, errs, scope, export.Body)
			}
		}
	}
	return out, errs
}

// ExtractSyntaxBodyImports parses the imports declared by a typed factory body.
func ExtractSyntaxBodyImports(body syntax.FactoryBody) (map[string]ImportRef, []error) {
	refs, errs := appendSyntaxImports(nil, nil, "", body.Imports)
	return syntaxImportMap(refs), errs
}

// ExtractSyntaxBodyLibraryConfigDeps parses library-config input type refs in a body.
func ExtractSyntaxBodyLibraryConfigDeps(
	body syntax.FactoryBody,
) ([]SyntaxDependency, []error) {
	return appendSyntaxLibraryConfigDeps(nil, nil, "", body.Inputs)
}

func syntaxImportMap(refs []SyntaxImport) map[string]ImportRef {
	out := make(map[string]ImportRef, len(refs))
	for _, ref := range refs {
		if ref.Scope != "" {
			continue
		}
		out[ref.Alias] = ref.Ref
	}
	return out
}

func appendSyntaxImports(
	out []SyntaxImport,
	errs []error,
	scope string,
	decls []syntax.ImportDecl,
) ([]SyntaxImport, []error) {
	deps, errs := appendSyntaxImportDeps(nil, errs, scope, decls)
	for _, dep := range deps {
		out = append(out, SyntaxImport{
			Scope: dep.Scope,
			Alias: importAliasFromLabel(scope, dep.Label),
			Ref:   dep.Ref,
		})
	}
	return out, errs
}

func appendSyntaxDependencies(
	out []SyntaxDependency,
	errs []error,
	scope string,
	body syntax.FactoryBody,
) ([]SyntaxDependency, []error) {
	out, errs = appendSyntaxImportDeps(out, errs, scope, body.Imports)
	return appendSyntaxLibraryConfigDeps(out, errs, scope, body.Inputs)
}

func appendSyntaxImportDeps(
	out []SyntaxDependency,
	errs []error,
	scope string,
	decls []syntax.ImportDecl,
) ([]SyntaxDependency, []error) {
	for _, decl := range decls {
		if decl.Ref == nil {
			continue
		}
		label := importLabel(scope, decl.Alias.Name)
		ref, err := parseSyntaxDependencyRef(SyntaxDependencyImport, label, decl.Ref.Value)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, SyntaxDependency{
			Scope: scope,
			Label: label,
			Kind:  SyntaxDependencyImport,
			Ref:   ref,
		})
	}
	return out, errs
}

func appendSyntaxLibraryConfigDeps(
	out []SyntaxDependency,
	errs []error,
	scope string,
	decls []syntax.InputDecl,
) ([]SyntaxDependency, []error) {
	for _, decl := range decls {
		lib, ok := decl.Type.(*parse.TypeLibraryConfig)
		if !ok || lib.Path == nil {
			continue
		}
		label := libraryConfigLabel(scope, decl.Name.Name)
		ref, err := parseSyntaxDependencyRef(
			SyntaxDependencyLibraryConfig,
			label,
			lib.Path.Value,
		)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, SyntaxDependency{
			Scope: scope,
			Label: label,
			Kind:  SyntaxDependencyLibraryConfig,
			Ref:   ref,
		})
	}
	return out, errs
}

func parseSyntaxDependencyRef(
	kind SyntaxDependencyKind,
	label string,
	value string,
) (ImportRef, error) {
	ref, err := ParseImportRef(value)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", kind, label, err)
	}
	return ref, nil
}

func importLabel(scope, alias string) string {
	if scope == "" {
		return alias
	}
	return scope + "." + alias
}

func importAliasFromLabel(scope, label string) string {
	if scope == "" {
		return label
	}
	return strings.TrimPrefix(label, scope+".")
}

func libraryConfigLabel(scope, input string) string {
	label := "input." + input + ".type"
	if scope == "" {
		return label
	}
	return scope + "." + label
}
