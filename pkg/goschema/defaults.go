package goschema

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"time"

	"github.com/cloudboss/unobin/pkg/lang"
)

// defaultsPkgPath is the import path of the package whose constructors a
// Go library type uses to declare its input defaults. The extractor
// recognizes calls qualified by whatever local name that import is
// bound to.
const defaultsPkgPath = "github.com/cloudboss/unobin/pkg/defaults"

// lookupDefaults resolves a registration's input type and returns the
// defaults declared by its Defaults method, each field selector mapped
// to its kebab input name. A type in a subpackage (PkgAlias set) is
// followed the same way lookupFields does.
func (w *walker) lookupDefaults(ref typeRef) []lang.DefaultSpec {
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
	return cw.defaultsFromType(ref.TypeName)
}

func (w *walker) defaultsFromType(typeName string) []lang.DefaultSpec {
	method := findMethod(w.files, typeName, "Defaults")
	if method == nil {
		return nil
	}
	w.subject = typeName
	scope := constraintScope{}
	if name, ok := receiverName(method); ok {
		scope[name] = scopeRoot{w: w, typeName: typeName, prefix: "var"}
	}
	var out []lang.DefaultSpec
	seen := map[string]bool{}
	for _, call := range w.listReturnCalls(method.Body, "Defaults method", "default", "pkg/defaults") {
		if spec, ok := w.defaultFromCall(call, scope, seen); ok {
			out = append(out, spec)
		}
	}
	return out
}

// defaultFromCall turns one constructor call into a default spec. A
// constructor that cannot be extracted warns and returns ok=false; a
// declaration the type cannot take (a pointer field, a duplicate, an
// indexed element) records an error instead, since the declaration is
// wrong rather than unreadable.
func (w *walker) defaultFromCall(
	call *ast.CallExpr, scope constraintScope, seen map[string]bool,
) (lang.DefaultSpec, bool) {
	fun := call.Fun
	if idx, ok := fun.(*ast.IndexExpr); ok {
		fun = idx.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		w.addWarnf("a default must be a pkg/defaults constructor call, got %s", renderExpr(call))
		return lang.DefaultSpec{}, false
	}
	pkg, ok := identName(sel.X)
	if !ok || w.imports[pkg] != defaultsPkgPath {
		w.addWarnf("a default must be a pkg/defaults constructor call, got %s", renderExpr(call))
		return lang.DefaultSpec{}, false
	}
	switch sel.Sel.Name {
	case "Value":
		if len(call.Args) != 2 {
			w.addWarnf("Value takes a field and a default")
			return lang.DefaultSpec{}, false
		}
		field, ok := w.defaultField(call.Args[0], scope, seen, true)
		if !ok {
			return lang.DefaultSpec{}, false
		}
		val, ok := w.defaultValueString(call.Args[1])
		if !ok {
			return lang.DefaultSpec{}, false
		}
		return lang.DefaultSpec{Field: field, Value: val}, true
	case "Optional":
		if len(call.Args) != 1 {
			w.addWarnf("Optional takes one field")
			return lang.DefaultSpec{}, false
		}
		field, ok := w.defaultField(call.Args[0], scope, seen, false)
		if !ok {
			return lang.DefaultSpec{}, false
		}
		return lang.DefaultSpec{Field: field, Optional: true}, true
	}
	w.addWarnf("unsupported default constructor %q", sel.Sel.Name)
	return lang.DefaultSpec{}, false
}

// defaultField reads a default's field selector and validates that the
// field can take a default: not an indexed list element, not a pointer
// (a pointer is the spelling for optional with meaningful absence), and
// not already declared.
func (w *walker) defaultField(
	arg ast.Expr, scope constraintScope, seen map[string]bool, hasValue bool,
) (string, bool) {
	field, ok := w.selectorField(arg, scope)
	if !ok {
		return "", false
	}
	root, hops, _ := flattenSelector(arg.(*ast.SelectorExpr))
	for _, hop := range hops {
		if len(hop.indexes) > 0 {
			w.addErrf("a default cannot index a list element")
			return "", false
		}
	}
	name := strings.TrimPrefix(field, "var.")
	if ft, ok := fieldFinalType(scope[root], hops); ok {
		if _, isPointer := ft.(*ast.StarExpr); isPointer {
			if hasValue {
				w.addErrf("pointer field %q cannot take a default", name)
			} else {
				w.addErrf("pointer field %q is already optional", name)
			}
			return "", false
		}
	}
	if seen[name] {
		w.addErrf("duplicate default for %q", name)
		return "", false
	}
	seen[name] = true
	return field, true
}

// fieldFinalType resolves the Go type expression of the field a
// selector chain names, descending nested structs the way fieldPath
// does.
func fieldFinalType(entry scopeRoot, hops []selectorHop) (ast.Expr, bool) {
	cw, typeName := entry.w, entry.typeName
	for i, hop := range hops {
		if i == len(hops)-1 {
			spec := findTypeSpec(cw.files, typeName)
			if spec == nil {
				return nil, false
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok {
				return nil, false
			}
			ft := fieldTypeByGoName(st, hop.name)
			if ft == nil {
				return nil, false
			}
			return ft, true
		}
		var ok bool
		cw, typeName, ok = cw.nestedStruct(typeName, hop)
		if !ok {
			return nil, false
		}
	}
	return nil, false
}

// defaultValueString renders a Value default to unobin literal source.
// Integer literals normalize to decimal, so an octal mode like 0o644
// reads back as 420; an expression of time package duration constants
// folds to its nanosecond count. Anything without a constant value in
// the source warns and returns ok=false.
func (w *walker) defaultValueString(arg ast.Expr) (string, bool) {
	switch v := arg.(type) {
	case *ast.ParenExpr:
		return w.defaultValueString(v.X)
	case *ast.BasicLit:
		switch v.Kind {
		case token.STRING:
			if s, err := unquoteString(v.Value); err == nil {
				return "'" + s + "'", true
			}
		case token.INT:
			if n, err := strconv.ParseInt(v.Value, 0, 64); err == nil {
				return strconv.FormatInt(n, 10), true
			}
		case token.FLOAT:
			return v.Value, true
		}
	case *ast.Ident:
		if v.Name == "true" || v.Name == "false" {
			return v.Name, true
		}
	case *ast.UnaryExpr:
		if v.Op == token.SUB {
			if bl, ok := v.X.(*ast.BasicLit); ok {
				switch bl.Kind {
				case token.INT:
					if n, err := strconv.ParseInt(bl.Value, 0, 64); err == nil {
						return strconv.FormatInt(-n, 10), true
					}
				case token.FLOAT:
					return "-" + bl.Value, true
				}
			}
		}
	case *ast.SelectorExpr, *ast.BinaryExpr:
		if ns, ok := w.foldDuration(arg); ok {
			return strconv.FormatInt(ns, 10), true
		}
	}
	w.addWarnf("a default must be a literal, got %s", renderExpr(arg))
	return "", false
}

// durationConsts maps the time package's duration constants to their
// nanosecond counts for folding defaults like 5 * time.Minute.
var durationConsts = map[string]int64{
	"Nanosecond":  int64(time.Nanosecond),
	"Microsecond": int64(time.Microsecond),
	"Millisecond": int64(time.Millisecond),
	"Second":      int64(time.Second),
	"Minute":      int64(time.Minute),
	"Hour":        int64(time.Hour),
}

// foldDuration folds an expression of time package duration constants
// and whole-number literals to its nanosecond count: time.Second,
// 5 * time.Minute, time.Second * 30.
func (w *walker) foldDuration(e ast.Expr) (int64, bool) {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return w.foldDuration(v.X)
	case *ast.SelectorExpr:
		pkg, ok := identName(v.X)
		if !ok || w.imports[pkg] != "time" {
			return 0, false
		}
		ns, ok := durationConsts[v.Sel.Name]
		return ns, ok
	case *ast.BinaryExpr:
		if v.Op != token.MUL {
			return 0, false
		}
		left, ok := w.foldDurationOperand(v.X)
		if !ok {
			return 0, false
		}
		right, ok := w.foldDurationOperand(v.Y)
		if !ok {
			return 0, false
		}
		return left * right, true
	}
	return 0, false
}

// foldDurationOperand reads one side of a duration product: a
// whole-number literal factor or a duration expression.
func (w *walker) foldDurationOperand(e ast.Expr) (int64, bool) {
	if bl, ok := e.(*ast.BasicLit); ok && bl.Kind == token.INT {
		n, err := strconv.ParseInt(bl.Value, 0, 64)
		return n, err == nil
	}
	return w.foldDuration(e)
}
