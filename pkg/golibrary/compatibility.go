package golibrary

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"strconv"

	"golang.org/x/mod/semver"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/libraryapi"
)

type CompatibilityDeclaration struct {
	RequiredAPI                string
	SuggestedUnobinVersion     string
	Path                       string
	Span                       *diagnostic.Span
	RequiredAPISpan            *diagnostic.Span
	SuggestedUnobinVersionSpan *diagnostic.Span
}

type CompatibilityErrorKind uint8

const (
	MissingDeclaration CompatibilityErrorKind = iota
	InvalidDeclaration
	UnsupportedField
)

type CompatibilityError struct {
	Kind       CompatibilityErrorKind
	diagnostic diagnostic.Diagnostic
}

func (e *CompatibilityError) Error() string {
	return e.diagnostic.Message
}

func (e *CompatibilityError) Diagnostics() []diagnostic.Diagnostic {
	return diagnostic.Normalize([]diagnostic.Diagnostic{e.diagnostic})
}

func ReadCompatibility(moduleRoot, packageDir string) (*CompatibilityDeclaration, error) {
	_, packageDir, err := cleanRoots(moduleRoot, packageDir)
	if err != nil {
		return nil, err
	}
	pkg, err := parsePackage(packageDir)
	if err != nil {
		return nil, err
	}
	return readCompatibilityPackage(pkg)
}

func readCompatibilityPackage(pkg *parsedPackage) (*CompatibilityDeclaration, error) {
	fn, err := libraryFunction(pkg)
	if err != nil {
		kind := InvalidDeclaration
		var functionError *libraryFunctionError
		if errors.As(err, &functionError) && functionError.missing {
			kind = MissingDeclaration
		}
		return nil, compatibilityFailure(pkg, pkg.Files[0].Name, kind, err.Error())
	}
	var file *ast.File
	for _, candidate := range pkg.Files {
		if candidate.Pos() <= fn.Pos() && fn.End() <= candidate.End() {
			file = candidate
			break
		}
	}
	aliases, err := runtimeImportAliases(&parsedPackage{Files: []*ast.File{file}})
	if err != nil {
		return nil, compatibilityFailure(pkg, fn.Name, InvalidDeclaration, err.Error())
	}
	if err := validateSignature(fn, aliases); err != nil {
		return nil, compatibilityFailure(pkg, fn.Name, InvalidDeclaration, err.Error())
	}
	if fn.Body == nil || countReturns(fn.Body) != 1 {
		return nil, compatibilityFailure(pkg, fn.Name, InvalidDeclaration,
			"library function must have exactly one return statement")
	}
	stmt := onlyReturn(fn.Body)
	if stmt == nil || len(stmt.Results) != 1 {
		return nil, compatibilityFailure(pkg, fn.Name, InvalidDeclaration,
			"library function must have exactly one return statement")
	}
	literal, ok := directLibraryLiteral(stmt.Results[0], aliases)
	if !ok {
		return nil, compatibilityFailure(pkg, stmt.Results[0], InvalidDeclaration,
			"library function must return &runtime.Library{...}")
	}
	var compatibility ast.Expr
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return nil, compatibilityFailure(pkg, element, InvalidDeclaration,
				"library record must use keyed fields")
		}
		name, ok := field.Key.(*ast.Ident)
		if !ok {
			return nil, compatibilityFailure(pkg, field.Key, InvalidDeclaration,
				"library record field must be an identifier")
		}
		if name.Name != "Compatibility" {
			continue
		}
		if compatibility != nil {
			return nil, compatibilityFailure(pkg, name, InvalidDeclaration,
				"duplicate Compatibility field")
		}
		compatibility = field.Value
	}
	if compatibility == nil {
		return nil, compatibilityFailure(pkg, literal.Type, MissingDeclaration,
			"library has no Compatibility declaration")
	}
	selector := literal.Type.(*ast.SelectorExpr)
	alias := selector.X.(*ast.Ident).Name
	return readCompatibilityLiteral(pkg, compatibility, alias)
}

func readCompatibilityLiteral(
	pkg *parsedPackage, expression ast.Expr, runtimeAlias string,
) (*CompatibilityDeclaration, error) {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return nil, compatibilityFailure(pkg, expression, InvalidDeclaration,
			"Compatibility must be a direct runtime.LibraryCompatibility literal")
	}
	selector, ok := literal.Type.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "LibraryCompatibility" {
		return nil, compatibilityFailure(pkg, expression, InvalidDeclaration,
			"Compatibility must be a direct runtime.LibraryCompatibility literal")
	}
	alias, ok := selector.X.(*ast.Ident)
	if !ok || alias.Name != runtimeAlias {
		return nil, compatibilityFailure(pkg, expression, InvalidDeclaration,
			"Compatibility must use the library record's runtime import alias")
	}
	declaration := &CompatibilityDeclaration{
		Path: pkg.FSet.PositionFor(literal.Pos(), false).Filename,
		Span: sourceSpan(pkg.FSet, literal),
	}
	seen := map[string]bool{}
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return nil, compatibilityFailure(pkg, element, InvalidDeclaration,
				"Compatibility must use keyed fields")
		}
		name, ok := field.Key.(*ast.Ident)
		if !ok {
			return nil, compatibilityFailure(pkg, field.Key, InvalidDeclaration,
				"Compatibility field must be an identifier")
		}
		if name.Name != "RequiredAPI" && name.Name != "SuggestedUnobinVersion" {
			return nil, compatibilityFailure(pkg, name, UnsupportedField,
				fmt.Sprintf("unsupported compatibility field %q", name.Name))
		}
		if seen[name.Name] {
			return nil, compatibilityFailure(pkg, name, InvalidDeclaration,
				fmt.Sprintf("duplicate compatibility field %q", name.Name))
		}
		seen[name.Name] = true
		value, ok := field.Value.(*ast.BasicLit)
		if !ok || value.Kind != token.STRING {
			return nil, compatibilityFailure(pkg, field.Value, InvalidDeclaration,
				fmt.Sprintf("Compatibility.%s must be a literal string", name.Name))
		}
		decoded, err := strconv.Unquote(value.Value)
		if err != nil {
			return nil, compatibilityFailure(pkg, value, InvalidDeclaration, err.Error())
		}
		switch name.Name {
		case "RequiredAPI":
			declaration.RequiredAPI = decoded
			declaration.RequiredAPISpan = sourceSpan(pkg.FSet, value)
		case "SuggestedUnobinVersion":
			declaration.SuggestedUnobinVersion = decoded
			declaration.SuggestedUnobinVersionSpan = sourceSpan(pkg.FSet, value)
		}
	}
	if declaration.RequiredAPISpan == nil {
		return nil, compatibilityFailure(pkg, literal, InvalidDeclaration,
			"Compatibility.RequiredAPI is required")
	}
	if _, err := libraryapi.Parse(declaration.RequiredAPI); err != nil {
		return nil, compatibilityValueFailure(declaration, declaration.RequiredAPISpan,
			"Compatibility.RequiredAPI: "+err.Error())
	}
	hint := declaration.SuggestedUnobinVersion
	if hint != "" && (!semver.IsValid(hint) || semver.Canonical(hint) != hint) {
		return nil, compatibilityValueFailure(declaration, declaration.SuggestedUnobinVersionSpan,
			"Compatibility.SuggestedUnobinVersion must be a full Unobin release "+
				"with an optional prerelease and no build metadata")
	}
	return declaration, nil
}

func compatibilityFailure(
	pkg *parsedPackage, node ast.Node, kind CompatibilityErrorKind, message string,
) *CompatibilityError {
	code := "unobin.library-api.invalid-declaration"
	switch kind {
	case MissingDeclaration:
		code = "unobin.library-api.missing-declaration"
	case UnsupportedField:
		code = "unobin.library-api.unsupported-field"
	}
	return &CompatibilityError{
		Kind: kind,
		diagnostic: diagnostic.Diagnostic{
			Code: code, Severity: diagnostic.SeverityError, Message: message,
			Path: pkg.FSet.PositionFor(node.Pos(), false).Filename, Span: sourceSpan(pkg.FSet, node),
			Hint: "Use a library release with a literal Compatibility declaration and RequiredAPI.",
		},
	}
}

func compatibilityValueFailure(
	declaration *CompatibilityDeclaration, span *diagnostic.Span, message string,
) *CompatibilityError {
	return &CompatibilityError{
		Kind: InvalidDeclaration,
		diagnostic: diagnostic.Diagnostic{
			Code: "unobin.library-api.invalid-declaration", Severity: diagnostic.SeverityError,
			Message: message, Path: declaration.Path, Span: span,
			LibraryCompatibility: &diagnostic.LibraryCompatibilityDetails{
				RequiredAPI:            declaration.RequiredAPI,
				SuggestedUnobinVersion: declaration.SuggestedUnobinVersion,
			},
		},
	}
}

func sourceSpan(fset *token.FileSet, node ast.Node) *diagnostic.Span {
	start := fset.PositionFor(node.Pos(), false)
	end := fset.PositionFor(node.End(), false)
	return &diagnostic.Span{
		Start: diagnostic.Position{Line: start.Line, Column: start.Column, Offset: start.Offset},
		End:   &diagnostic.Position{Line: end.Line, Column: end.Column, Offset: end.Offset},
	}
}
