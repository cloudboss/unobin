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
		scope[name] = scopeRoot{w: w, typeName: typeName, prefix: "input"}
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
		field, ok := w.defaultField(call.Args[0], scope, seen, rejectPointerDefault)
		if !ok {
			return lang.DefaultSpec{}, false
		}
		val, ok := w.defaultValueString(call.Args[1])
		if !ok {
			return lang.DefaultSpec{}, false
		}
		return lang.DefaultSpec{Field: field, Value: val}, true
	case "NullableValue":
		if len(call.Args) != 2 {
			w.addWarnf("NullableValue takes a field and a default")
			return lang.DefaultSpec{}, false
		}
		field, ok := w.defaultField(call.Args[0], scope, seen, requirePointerDefault)
		if !ok {
			return lang.DefaultSpec{}, false
		}
		val, ok := w.defaultValueString(call.Args[1])
		if !ok {
			return lang.DefaultSpec{}, false
		}
		return lang.DefaultSpec{Field: field, Value: val}, true
	}
	w.addWarnf("unsupported default constructor %q", sel.Sel.Name)
	return lang.DefaultSpec{}, false
}

type defaultPointerPolicy int

const (
	rejectPointerDefault defaultPointerPolicy = iota
	requirePointerDefault
)

// defaultField reads a default's field selector and validates that the
// field can take a default: not an indexed list element, the right pointer
// category for the constructor, and not already declared.
func (w *walker) defaultField(
	arg ast.Expr, scope constraintScope, seen map[string]bool, pointerPolicy defaultPointerPolicy,
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
	name := strings.TrimPrefix(field, "input.")
	if seen[name] {
		w.addErrf("duplicate default for %q", name)
		return "", false
	}
	if ft, ok := fieldFinalType(scope[root], hops); ok {
		_, isPointer := ft.(*ast.StarExpr)
		switch {
		case pointerPolicy == rejectPointerDefault && isPointer:
			w.addErrf("pointer field %q cannot take a default", name)
			return "", false
		case pointerPolicy == requirePointerDefault && !isPointer:
			w.addErrf("field %q is not nullable", name)
			return "", false
		}
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
	value, ok := w.defaultLiteralValue(arg)
	if !ok {
		return "", false
	}
	return lang.Render(value), true
}

func (w *walker) defaultLiteralValue(arg ast.Expr) (any, bool) {
	switch v := arg.(type) {
	case *ast.ParenExpr:
		return w.defaultLiteralValue(v.X)
	case *ast.BasicLit:
		switch v.Kind {
		case token.STRING:
			return sourceString(v)
		case token.INT:
			return sourceInt(v)
		case token.FLOAT:
			return sourceNumber(v)
		}
	case *ast.Ident:
		if b, ok := boolLit(v); ok {
			return b, true
		}
	case *ast.UnaryExpr:
		if v.Op == token.SUB {
			if n, ok := sourceInt(v); ok {
				return n, true
			}
			if n, ok := sourceNumber(v); ok {
				return n, true
			}
		}
	case *ast.SelectorExpr, *ast.BinaryExpr:
		if ns, ok := w.foldDuration(arg); ok {
			return ns, true
		}
	case *ast.CallExpr:
		if len(v.Args) == 1 && w.defaultConversion(v.Fun) {
			return w.defaultLiteralValue(v.Args[0])
		}
	case *ast.CompositeLit:
		return w.defaultCompositeValue(v)
	}
	w.addWarnf("a default must be a literal, got %s", renderExpr(arg))
	return nil, false
}

func (w *walker) defaultCompositeValue(lit *ast.CompositeLit) (any, bool) {
	switch lit.Type.(type) {
	case *ast.ArrayType:
		return w.defaultListValue(lit.Elts)
	case *ast.MapType:
		return w.defaultMapValue(lit.Elts)
	}
	w.addWarnf("a default must be a list or map literal, got %s", renderExpr(lit))
	return nil, false
}

func (w *walker) defaultListValue(items []ast.Expr) (any, bool) {
	out := make([]any, 0, len(items))
	for _, item := range items {
		if _, ok := item.(*ast.KeyValueExpr); ok {
			w.addWarnf("a default list item must not have a key, got %s", renderExpr(item))
			return nil, false
		}
		value, ok := w.defaultLiteralValue(item)
		if !ok {
			return nil, false
		}
		out = append(out, value)
	}
	return out, true
}

func (w *walker) defaultMapValue(items []ast.Expr) (any, bool) {
	out := make(map[string]any, len(items))
	for _, item := range items {
		kv, ok := item.(*ast.KeyValueExpr)
		if !ok {
			w.addWarnf("a default map entry must have a key, got %s", renderExpr(item))
			return nil, false
		}
		key, ok := sourceString(kv.Key)
		if !ok {
			w.addWarnf(
				"a default map key must be a string literal, got %s",
				renderExpr(kv.Key),
			)
			return nil, false
		}
		value, ok := w.defaultLiteralValue(kv.Value)
		if !ok {
			return nil, false
		}
		out[key] = value
	}
	return out, true
}

func (w *walker) defaultConversion(fun ast.Expr) bool {
	switch v := fun.(type) {
	case *ast.Ident:
		switch v.Name {
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64",
			"byte", "rune", "float32", "float64", "string", "bool":
			return true
		}
	case *ast.SelectorExpr:
		pkg, ok := identName(v.X)
		return ok && w.imports[pkg] == "time" && v.Sel.Name == "Duration"
	}
	return false
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
