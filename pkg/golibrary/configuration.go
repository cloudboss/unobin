package golibrary

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/gopackage"
)

type configurationWalker struct {
	context   *CompatibilityContext
	checked   map[string]bool
	active    map[string]bool
	completed map[string]bool
	packages  map[string]*parsedPackage
	metadata  map[string]PackageMetadata
}

func (w *configurationWalker) checkConfigurations(source PackageSource, pkg *parsedPackage) error {
	var failures []error
	fn := packageFunction(pkg, "Library")
	if fn != nil && fn.Body != nil {
		ret := onlyReturn(fn.Body)
		if ret != nil && len(ret.Results) == 1 {
			expression := ret.Results[0]
			if pointer, ok := expression.(*ast.UnaryExpr); ok && pointer.Op == token.AND {
				expression = pointer.X
			}
			if literal, ok := expression.(*ast.CompositeLit); ok {
				for _, element := range literal.Elts {
					field, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := field.Key.(*ast.Ident)
					if !ok || key.Name != "Configuration" {
						continue
					}
					if name, ok := field.Value.(*ast.Ident); ok && name.Name == "nil" {
						continue
					}
					if err := w.checkValue(source, pkg, field.Value); err != nil {
						failures = append(failures, err)
					}
				}
			}
		}
	}
	if packageFunction(pkg, "LibraryConfiguration") != nil {
		if err := w.checkEntryPoint(source, pkg); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
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
	inspection, err := w.context.checkPackageDeclaration(source)
	if inspection == nil {
		return err
	}
	source = inspection.metadata.Source
	w.checked[abs] = true
	w.packages[abs] = inspection.pkg
	w.metadata[abs] = inspection.metadata
	return errors.Join(err, w.checkConfigurations(source, inspection.pkg))
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
	packageErr := w.checkPackage(target)
	if w.packages[target.Dir] == nil {
		return packageErr
	}
	return errors.Join(packageErr,
		w.checkEntryPoint(w.metadata[target.Dir].Source, w.packages[target.Dir]))
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
	roots := append([]ModuleSource{source.Module}, w.context.options.Modules...)
	if core := w.context.options.CoreReplacement; core != "" {
		roots = append(roots, ModuleSource{
			Path: "github.com/cloudboss/unobin", Dir: core, Replacement: core,
		})
	}
	importPath := packageImportPath(pkg, selector, alias.Name, func(importPath string) string {
		root := selectedSourceModule(roots, importPath)
		if root.Dir != "" {
			rel := strings.TrimPrefix(strings.TrimPrefix(importPath, root.Path), "/")
			target, err := gopackage.Load(filepath.Join(root.Dir, filepath.FromSlash(rel)), pkg.target)
			if err == nil {
				return target.Name
			}
		}
		return path.Base(importPath)
	})
	selected := selectedSourceModule(roots, importPath)
	lookupModule := selected.Dir == ""
	if selected.Dir != "" && w.context.options.ResolveModule != nil {
		abs, err := filepath.Abs(selected.Dir)
		if err != nil {
			return PackageSource{}, err
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(importPath, selected.Path), "/")
		moduleRoot, err := FindModuleRoot(filepath.Join(abs, filepath.FromSlash(relative)))
		lookupModule = err == nil && moduleRoot != abs
	}
	if lookupModule && w.context.options.ResolveModule != nil {
		module, err := w.context.options.ResolveModule(importPath)
		if err != nil {
			return PackageSource{}, w.failure(source, pkg, selector,
				"cannot read selected configuration module "+importPath, err)
		}
		if module.Path != "" && module.Dir != "" && len(module.Path) >= len(selected.Path) &&
			(importPath == module.Path || strings.HasPrefix(importPath, module.Path+"/")) {
			selected = module
			w.context.options.Modules = append(w.context.options.Modules, module)
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

func selectedSourceModule(roots []ModuleSource, importPath string) ModuleSource {
	var selected ModuleSource
	for _, root := range roots {
		if root.Path == "" || root.Dir == "" ||
			(importPath != root.Path && !strings.HasPrefix(importPath, root.Path+"/")) {
			continue
		}
		if len(root.Path) > len(selected.Path) {
			selected = root
		}
	}
	return selected
}

func packageImportPath(
	pkg *parsedPackage, node ast.Node, alias string, implicitName func(string) string,
) string {
	for _, file := range pkg.Files {
		if node.Pos() < file.Pos() || node.End() > file.End() {
			continue
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			name := ""
			if spec.Name != nil {
				name = spec.Name.Name
			} else {
				name = implicitName(importPath)
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
	metadata := w.metadata[source.Dir]
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
