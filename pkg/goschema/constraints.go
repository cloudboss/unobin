package goschema

import (
	"fmt"
	"go/ast"

	"github.com/cloudboss/unobin/pkg/lang"
)

// constraintPkgPath is the import path of the package whose builders a Go
// library type uses to declare its constraints. The extractor recognizes
// calls qualified by whatever local name that import is bound to.
const constraintPkgPath = "github.com/cloudboss/unobin/pkg/constraint"

// setConstraintKinds maps a pkg/constraint set-constraint constructor to
// the kind the checker uses. Predicate constructors (Must, When) are
// absent on purpose: predicates are derived in a later pass.
var setConstraintKinds = map[string]string{
	"ExactlyOneOf":     "exactly-one-of",
	"AtLeastOneOf":     "at-least-one-of",
	"AtMostOneOf":      "at-most-one-of",
	"RequiredTogether": "required-together",
	"RequiredWith":     "required-with",
	"ForbiddenWith":    "forbidden-with",
}

// lookupConstraints resolves a registration's input type and returns the
// constraint entries declared by its Constraints method, each field
// selector mapped to its kebab input name. A type in a subpackage
// (PkgAlias set) is followed the same way lookupFields does.
func (w *walker) lookupConstraints(ref typeRef) []lang.ConstraintEntry {
	cw := w
	if ref.PkgAlias != "" {
		importPath, ok := w.imports[ref.PkgAlias]
		if !ok {
			return nil
		}
		sub := w.sub(importPath)
		if sub == nil {
			return nil
		}
		cw = sub
	}
	return cw.constraintsFromType(ref.TypeName)
}

func (w *walker) constraintsFromType(typeName string) []lang.ConstraintEntry {
	method := findMethod(w.files, typeName, "Constraints")
	if method == nil {
		return nil
	}
	names := w.fieldKebabByGoName(typeName)
	var out []lang.ConstraintEntry
	for _, call := range constraintCalls(method) {
		if entry, ok := w.entryFromCall(call, names); ok {
			out = append(out, entry)
		}
	}
	return out
}

// fieldKebabByGoName maps each struct field's Go name to its kebab input
// name, the reverse of what the input schema keys on. It turns a
// v.FieldName selector inside a constraint into the input name the
// checker expects.
func (w *walker) fieldKebabByGoName(typeName string) map[string]string {
	spec := findTypeSpec(w.files, typeName)
	if spec == nil {
		return nil
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return nil
	}
	out := map[string]string{}
	for _, fld := range st.Fields.List {
		name, skip, _, _ := parseUBFieldTag(fld.Tag)
		if skip {
			continue
		}
		for _, goName := range fld.Names {
			kebab := name
			if kebab == "" {
				kebab = lang.PascalToKebab(goName.Name)
			}
			out[goName.Name] = kebab
		}
	}
	return out
}

// findMethod returns the named method on typeName (pointer or value
// receiver) from the package files, or nil.
func findMethod(files []*ast.File, typeName, methodName string) *ast.FuncDecl {
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != methodName {
				continue
			}
			if receiverType(fn) == typeName {
				return fn
			}
		}
	}
	return nil
}

func receiverType(fn *ast.FuncDecl) string {
	if len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// constraintCalls returns the constructor call expressions in a
// Constraints method's returned slice literal. A body that is not a
// single return of a composite literal yields none.
func constraintCalls(method *ast.FuncDecl) []*ast.CallExpr {
	if method.Body == nil {
		return nil
	}
	var calls []*ast.CallExpr
	for _, stmt := range method.Body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		lit, ok := ret.Results[0].(*ast.CompositeLit)
		if !ok {
			continue
		}
		for _, el := range lit.Elts {
			if call, ok := el.(*ast.CallExpr); ok {
				calls = append(calls, call)
			}
		}
	}
	return calls
}

// entryFromCall turns one constructor call into a constraint entry. It
// peels a trailing .Message("...") for the message, then matches the base
// constructor against the set-constraint kinds; predicate constructors
// return ok=false and are left for a later pass. Each argument is read as
// a v.Field selector and mapped to its input name.
func (w *walker) entryFromCall(
	call *ast.CallExpr, names map[string]string,
) (lang.ConstraintEntry, bool) {
	base, message := peelMessage(call)
	sel, ok := base.Fun.(*ast.SelectorExpr)
	if !ok {
		return lang.ConstraintEntry{}, false
	}
	pkg, ok := identName(sel.X)
	if !ok || w.imports[pkg] != constraintPkgPath {
		return lang.ConstraintEntry{}, false
	}
	kind, ok := setConstraintKinds[sel.Sel.Name]
	if !ok {
		return lang.ConstraintEntry{}, false
	}
	fields := make([]string, 0, len(base.Args))
	for _, arg := range base.Args {
		field, ok := w.selectorField(arg, names)
		if !ok {
			return lang.ConstraintEntry{}, false
		}
		fields = append(fields, field)
	}
	return lang.ConstraintEntry{Kind: kind, Fields: fields, Message: message}, true
}

// peelMessage unwraps a trailing .Message("...") call, returning the inner
// constructor call and the message text (empty when absent).
func peelMessage(call *ast.CallExpr) (*ast.CallExpr, string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Message" {
		return call, ""
	}
	inner, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return call, ""
	}
	msg := ""
	if len(call.Args) == 1 {
		if s, ok := stringLit(call.Args[0]); ok {
			msg = s
		}
	}
	return inner, msg
}

// selectorField reads a v.Field argument and returns the field's kebab
// input name. A non-selector argument, or a field absent from names,
// records an error and returns ok=false.
func (w *walker) selectorField(arg ast.Expr, names map[string]string) (string, bool) {
	sel, ok := arg.(*ast.SelectorExpr)
	if !ok {
		if w.errs != nil {
			*w.errs = append(*w.errs,
				fmt.Errorf("constraint field must be a struct field selector, got %T", arg))
		}
		return "", false
	}
	kebab, ok := names[sel.Sel.Name]
	if !ok {
		if w.errs != nil {
			*w.errs = append(*w.errs,
				fmt.Errorf("constraint references unknown field %q", sel.Sel.Name))
		}
		return "", false
	}
	return kebab, true
}
