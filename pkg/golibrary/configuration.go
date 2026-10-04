package golibrary

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/diagnostic"
)

type configurationWalker struct {
	context   *CompatibilityContext
	checked   map[string]bool
	active    map[string]bool
	completed map[string]bool
	packages  map[string]*parsedPackage
}

// ConfigurationSourceError identifies unavailable or unreadable configuration source.
type ConfigurationSourceError struct {
	Message string
	Cause   error
}

func (e *ConfigurationSourceError) Error() string { return e.Message }
func (e *ConfigurationSourceError) Unwrap() error { return e.Cause }

func (w *configurationWalker) checkPackage(source PackageSource) error {
	if source.Dir == "" {
		return nil
	}
	abs, err := filepath.Abs(source.Dir)
	if err != nil {
		return err
	}
	source.Dir = abs
	if w.checked[abs] {
		return nil
	}
	pkg, err := w.context.checkPackageDeclaration(source)
	if err != nil {
		return err
	}
	source = w.context.entries[abs].Source
	w.checked[abs] = true
	w.packages[abs] = pkg
	fn := packageFunction(pkg, "Library")
	expression := onlyReturn(fn.Body).Results[0].(*ast.UnaryExpr)
	literal := expression.X.(*ast.CompositeLit)
	for _, element := range literal.Elts {
		field := element.(*ast.KeyValueExpr)
		if field.Key.(*ast.Ident).Name == "Configuration" {
			if name, ok := field.Value.(*ast.Ident); ok && name.Name == "nil" {
				continue
			}
			if err := w.checkValue(source, pkg, field.Value); err != nil {
				return err
			}
		}
	}
	if packageFunction(pkg, "LibraryConfiguration") != nil {
		return w.checkEntryPoint(source, pkg)
	}
	return nil
}

func (w *configurationWalker) checkValue(
	source PackageSource, pkg *parsedPackage, expression ast.Expr,
) error {
	if pointer, ok := expression.(*ast.UnaryExpr); ok && pointer.Op == token.AND {
		expression = pointer.X
	}
	if _, ok := expression.(*ast.CompositeLit); ok {
		return nil
	}
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return w.failure(source, pkg, expression, "configuration entry point is unreadable", nil)
	}
	if name, ok := call.Fun.(*ast.Ident); ok && name.Name == "LibraryConfiguration" {
		return w.checkEntryPoint(source, pkg)
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "LibraryConfiguration" {
		return w.failure(source, pkg, call, "configuration entry point is unreadable", nil)
	}
	target, err := w.importedPackage(source, pkg, selector)
	if err != nil {
		return err
	}
	if err := w.checkPackage(target); err != nil {
		return err
	}
	return w.checkEntryPoint(w.context.entries[target.Dir].Source, w.packages[target.Dir])
}

func (w *configurationWalker) checkEntryPoint(source PackageSource, pkg *parsedPackage) error {
	key := source.Dir + "::LibraryConfiguration"
	fn := packageFunction(pkg, "LibraryConfiguration")
	if fn == nil {
		return w.failure(source, pkg, pkg.Files[0], "no single LibraryConfiguration() entry point", nil)
	}
	if w.active[key] {
		return w.failure(source, pkg, fn, "configuration forwarding cycle", nil)
	}
	if w.completed[key] {
		return nil
	}
	w.active[key] = true
	defer delete(w.active, key)
	expression := functionReturn(fn)
	if expression == nil {
		return w.failure(source, pkg, fn, "LibraryConfiguration() is unreadable", nil)
	}
	if err := w.checkValue(source, pkg, expression); err != nil {
		return err
	}
	w.completed[key] = true
	return nil
}

func (w *configurationWalker) importedPackage(
	source PackageSource, pkg *parsedPackage, selector *ast.SelectorExpr,
) (PackageSource, error) {
	alias, ok := selector.X.(*ast.Ident)
	if !ok {
		return PackageSource{}, w.failure(source, pkg, selector,
			"configuration package alias is unreadable", nil)
	}
	importPath := packageImportPath(pkg, selector, alias.Name)
	var selected ModuleSource
	roots := append([]ModuleSource{source.Module}, w.context.options.Modules...)
	if core := w.context.options.CoreReplacement; core != "" {
		roots = append(roots, ModuleSource{
			Path: "github.com/cloudboss/unobin", Dir: core, Replacement: core,
		})
	}
	for _, root := range roots {
		if root.Path == "" || root.Dir == "" {
			continue
		}
		if importPath != root.Path && !strings.HasPrefix(importPath, root.Path+"/") {
			continue
		}
		if len(root.Path) > len(selected.Path) {
			selected = root
		}
	}
	if selected.Dir == "" {
		return PackageSource{}, w.failure(source, pkg, selector,
			fmt.Sprintf("no selected source for configuration package %q", importPath), nil)
	}
	abs, err := filepath.Abs(selected.Dir)
	if err != nil {
		return PackageSource{}, err
	}
	selected.Dir = abs
	relative := strings.TrimPrefix(strings.TrimPrefix(importPath, selected.Path), "/")
	dir := filepath.Join(abs, filepath.FromSlash(relative))
	info, err := os.Stat(dir)
	if err != nil {
		return PackageSource{}, w.failure(source, pkg, selector,
			"cannot read selected configuration package "+importPath, err)
	}
	if !info.IsDir() {
		return PackageSource{}, w.failure(source, pkg, selector,
			"configuration package is not a directory: "+importPath, nil)
	}
	moduleRoot, err := FindModuleRoot(dir)
	if err != nil {
		return PackageSource{}, w.failure(source, pkg, selector,
			"cannot find selected configuration module "+importPath, err)
	}
	if moduleRoot != abs {
		return PackageSource{}, w.failure(source, pkg, selector,
			"configuration package belongs to an unselected nested module: "+importPath, nil)
	}
	return PackageSource{Module: selected, Dir: dir, Linked: source.Linked}, nil
}

func packageFunction(pkg *parsedPackage, name string) *ast.FuncDecl {
	var found *ast.FuncDecl
	for _, file := range pkg.Files {
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != name {
				continue
			}
			if found != nil {
				return nil
			}
			found = fn
		}
	}
	return found
}

func functionReturn(fn *ast.FuncDecl) ast.Expr {
	if fn == nil || fn.Body == nil {
		return nil
	}
	for _, statement := range fn.Body.List {
		if ret, ok := statement.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			return ret.Results[0]
		}
	}
	return nil
}

func packageImportPath(pkg *parsedPackage, node ast.Node, alias string) string {
	for _, file := range pkg.Files {
		if node.Pos() < file.Pos() || node.End() > file.End() {
			continue
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			name := path.Base(importPath)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			if name != "_" && name != "." && name == alias {
				return importPath
			}
		}
	}
	return ""
}

func (w *configurationWalker) failure(
	source PackageSource, pkg *parsedPackage, node ast.Node, message string, cause error,
) error {
	err := &ConfigurationSourceError{Message: message, Cause: cause}
	metadata := w.context.entries[source.Dir]
	return diagnostic.WithDiagnostics(err, diagnostic.Diagnostic{
		Code: "unobin.library-api.configuration-source", Severity: diagnostic.SeverityError,
		Message: message, Path: pkg.FSet.PositionFor(node.Pos(), false).Filename,
		Span: sourceSpan(pkg.FSet, node),
		Hint: "Provide selected source through a declared schema dependency or an effective replacement.",
		LibraryCompatibility: &diagnostic.LibraryCompatibilityDetails{
			Dependency: source.Module.Dependency, Package: metadata.Package,
			ModulePath: source.Module.Path, Version: source.Module.Version, Commit: source.Module.Commit,
			Replacement: source.Module.Replacement, RequiredAPI: metadata.Declaration.RequiredAPI,
			ImplementedAPIs: w.context.descriptor.ImplementedAPIs,
			UnobinVersion:   w.context.options.UnobinVersion,
		},
	})
}
