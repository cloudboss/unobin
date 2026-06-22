package golibrary

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

const runtimeImportPath = "github.com/cloudboss/unobin/pkg/runtime"

type Validation struct {
	ModulePath   string
	PackageName  string
	HasResources bool
	HasData      bool
	HasActions   bool
	HasFunctions bool
}

func FindModuleRoot(packageDir string) (string, error) {
	dir, err := filepath.Abs(packageDir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found in %s or any parent directory", packageDir)
		}
		dir = parent
	}
}

func ValidatePackage(moduleRoot, packageDir string) (*Validation, error) {
	moduleRoot, packageDir, err := cleanRoots(moduleRoot, packageDir)
	if err != nil {
		return nil, err
	}
	modulePath, err := readModulePath(moduleRoot)
	if err != nil {
		return nil, err
	}
	pkg, err := parsePackage(packageDir)
	if err != nil {
		return nil, err
	}
	runtimeAliases, err := runtimeImportAliases(pkg)
	if err != nil {
		return nil, err
	}
	fn, err := libraryFunction(pkg)
	if err != nil {
		return nil, err
	}
	if err := validateSignature(fn, runtimeAliases); err != nil {
		return nil, err
	}
	validation, err := validateBody(fn, runtimeAliases)
	if err != nil {
		return nil, err
	}
	validation.ModulePath = modulePath
	validation.PackageName = pkg.Name
	return validation, nil
}

func cleanRoots(moduleRoot, packageDir string) (string, string, error) {
	moduleRoot, err := filepath.Abs(moduleRoot)
	if err != nil {
		return "", "", err
	}
	packageDir, err = filepath.Abs(packageDir)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(moduleRoot, packageDir)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("package directory %s is not inside module root %s",
			packageDir, moduleRoot)
	}
	return moduleRoot, packageDir, nil
}

func readModulePath(moduleRoot string) (string, error) {
	b, err := os.ReadFile(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		return "", err
	}
	modulePath := modfile.ModulePath(b)
	if modulePath == "" {
		return "", fmt.Errorf("go.mod: missing module path")
	}
	return modulePath, nil
}

type parsedPackage struct {
	Name  string
	Files []*ast.File
}

func parsePackage(packageDir string) (*parsedPackage, error) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		return nil, err
	}
	filesByPackage := map[string][]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(packageDir, name), nil, 0)
		if err != nil {
			return nil, err
		}
		filesByPackage[file.Name.Name] = append(filesByPackage[file.Name.Name], file)
	}
	if len(filesByPackage) == 0 {
		return nil, fmt.Errorf("no Go package in %s", packageDir)
	}
	if len(filesByPackage) > 1 {
		return nil, fmt.Errorf("more than one Go package in %s", packageDir)
	}
	for name, files := range filesByPackage {
		return &parsedPackage{Name: name, Files: files}, nil
	}
	panic("unreachable")
}

func runtimeImportAliases(pkg *parsedPackage) (map[string]bool, error) {
	aliases := map[string]bool{}
	for _, file := range pkg.Files {
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return nil, err
			}
			if path != runtimeImportPath {
				continue
			}
			if spec.Name != nil && spec.Name.Name == "." {
				return nil, fmt.Errorf("dot import of %s is not accepted", runtimeImportPath)
			}
			if spec.Name != nil && spec.Name.Name != "_" {
				aliases[spec.Name.Name] = true
				continue
			}
			aliases["runtime"] = true
		}
	}
	return aliases, nil
}

func libraryFunction(pkg *parsedPackage) (*ast.FuncDecl, error) {
	var found []*ast.FuncDecl
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Library" || fn.Recv != nil {
				continue
			}
			found = append(found, fn)
		}
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no Library() function; no package-level library function")
	}
	if len(found) > 1 {
		return nil, fmt.Errorf("more than one package-level library function")
	}
	return found[0], nil
}

func validateSignature(fn *ast.FuncDecl, runtimeAliases map[string]bool) error {
	if fn.Type.TypeParams != nil && len(fn.Type.TypeParams.List) > 0 {
		return fmt.Errorf("library function must not declare type parameters")
	}
	if fn.Type.Params != nil && len(fn.Type.Params.List) > 0 {
		return fmt.Errorf("library function must not accept parameters")
	}
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return fmt.Errorf("library function must return *runtime.Library")
	}
	if !isRuntimeLibraryPointer(fn.Type.Results.List[0].Type, runtimeAliases) {
		return fmt.Errorf("library function must return *runtime.Library")
	}
	return nil
}

func validateBody(fn *ast.FuncDecl, runtimeAliases map[string]bool) (*Validation, error) {
	if fn.Body == nil || countReturns(fn.Body) != 1 {
		return nil, fmt.Errorf("library function must have exactly one return statement")
	}
	stmt := onlyReturn(fn.Body)
	if stmt == nil || len(stmt.Results) != 1 {
		return nil, fmt.Errorf("library function must have exactly one return statement")
	}
	literal, ok := directLibraryLiteral(stmt.Results[0], runtimeAliases)
	if !ok {
		return nil, fmt.Errorf("library function must return &runtime.Library{...}")
	}
	validation := registeredFields(literal)
	if !validation.HasResources && !validation.HasData && !validation.HasActions &&
		!validation.HasFunctions {
		return nil, fmt.Errorf("library function must register at least one usable type")
	}
	return validation, nil
}

func countReturns(body *ast.BlockStmt) int {
	var count int
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if _, ok := n.(*ast.ReturnStmt); ok {
			count++
		}
		return true
	})
	return count
}

func onlyReturn(body *ast.BlockStmt) *ast.ReturnStmt {
	var stmt *ast.ReturnStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if ret, ok := n.(*ast.ReturnStmt); ok {
			stmt = ret
		}
		return stmt == nil
	})
	return stmt
}

func isRuntimeLibraryPointer(expr ast.Expr, runtimeAliases map[string]bool) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	return isRuntimeLibrarySelector(star.X, runtimeAliases)
}

func directLibraryLiteral(expr ast.Expr, runtimeAliases map[string]bool) (*ast.CompositeLit, bool) {
	unary, ok := expr.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return nil, false
	}
	literal, ok := unary.X.(*ast.CompositeLit)
	if !ok || !isRuntimeLibrarySelector(literal.Type, runtimeAliases) {
		return nil, false
	}
	return literal, true
}

func isRuntimeLibrarySelector(expr ast.Expr, runtimeAliases map[string]bool) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Library" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && runtimeAliases[ident.Name]
}

func registeredFields(literal *ast.CompositeLit) *Validation {
	validation := &Validation{}
	for _, elt := range literal.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || !nonEmptyMapLiteral(kv.Value) {
			continue
		}
		switch key.Name {
		case "Resources":
			validation.HasResources = true
		case "DataSources":
			validation.HasData = true
		case "Actions":
			validation.HasActions = true
		case "Functions":
			validation.HasFunctions = true
		}
	}
	return validation
}

func nonEmptyMapLiteral(expr ast.Expr) bool {
	literal, ok := expr.(*ast.CompositeLit)
	return ok && len(literal.Elts) > 0
}
