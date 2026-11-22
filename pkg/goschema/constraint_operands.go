package goschema

import (
	"fmt"
	"go/ast"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

var operandCalls = map[string]string{
	"Length": "length", "ByteLength": "byte-length", "Remainder": "remainder",
	"Lookup": "lookup", "Sum": "sum",
	"HasKey": "has-key", "HasItem": "has-item", "IsUnique": "is-unique",
}

func (w *walker) constraintCallName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := identName(sel.X)
	if !ok || w.importPathFor(pkg, sel.Pos()) != constraintPkgPath {
		return ""
	}
	return sel.Sel.Name
}

func (w *walker) coreOperand(
	call *ast.CallExpr, name string, scope constraintScope,
) (constraintFieldRef, bool) {
	sig := runtime.CoreFunctionSigs()[name]
	if len(call.Args) != len(sig.Params) {
		w.addErrf("%s requires %d arguments", condName(call), len(sig.Params))
		return constraintFieldRef{}, false
	}
	types := make([]typecheck.Type, 0, len(call.Args))
	args := make([]string, 0, len(call.Args))
	for i, arg := range call.Args {
		value, ok := w.operand(arg, scope)
		if !ok {
			w.addErrf("%s argument %d is not a supported expression", condName(call), i+1)
			return constraintFieldRef{}, false
		}
		t := value.valueType
		if value.nullable && t.Kind != typecheck.Null {
			t = typecheck.TOptional(t)
		}
		if !typecheck.Assignable(sig.Params[i], t) {
			w.addErrf("%s argument %d requires %s, got %s", condName(call), i+1, sig.Params[i], t)
			return constraintFieldRef{}, false
		}
		types = append(types, t)
		args = append(args, value.expr)
	}
	t := sig.Result
	if sig.Infer != nil {
		t = sig.Infer(types)
	}
	t, nullable := nullableValueType(t)
	return constraintFieldRef{
		expr:      "@core." + name + "(" + strings.Join(args, ", ") + ")",
		valueType: t, nullable: nullable || t.Kind == typecheck.Null || !t.IsKnown(),
	}, true
}

func (w *walker) operand(arg ast.Expr, scope constraintScope) (constraintFieldRef, bool) {
	arg = ast.Unparen(arg)
	if call, ok := arg.(*ast.CallExpr); ok {
		name := w.constraintCallName(call)
		if core, ok := operandCalls[name]; ok {
			return w.coreOperand(call, core, scope)
		}
		switch name {
		case "Project":
			return w.comprehensionOperand(call, scope, false)
		case "CountWhere":
			return w.comprehensionOperand(call, scope, true)
		}
	}
	if ref, ok := w.constantOperand(arg, scope); ok {
		return ref, true
	}
	if id, ok := arg.(*ast.Ident); ok && id.Name == "nil" {
		return constraintFieldRef{expr: "null", valueType: typecheck.TNull(), nullable: true}, true
	}
	return w.valueFieldRef(arg, scope)
}

func (w *walker) comprehensionOperand(
	call *ast.CallExpr, scope constraintScope, count bool,
) (constraintFieldRef, bool) {
	if len(call.Args) != 2 {
		w.addErrf("%s takes a collection and a callback", condName(call))
		return constraintFieldRef{}, false
	}
	list, ok := w.listField(call.Args[0], scope)
	if !ok {
		return constraintFieldRef{}, false
	}
	callback, ok := call.Args[1].(*ast.FuncLit)
	if !ok {
		w.addErrf("%s requires a function literal", condName(call))
		return constraintFieldRef{}, false
	}
	param, paramType, ok := singleParam(callback)
	_, shadow := scope[param]
	if !ok || shadow {
		w.addErrf("%s requires one named callback parameter that does not shadow a field binding",
			condName(call))
		return constraintFieldRef{}, false
	}
	paramSource := sourceTypeRef{w: w, expr: paramType}
	if list.isMap {
		paramSource, ok = paramSource.mapEntryValueType()
	}
	if !ok || !sameSourceType(paramSource, list.elem.source) {
		w.addErrf("%s callback parameter type must match the collection element", condName(call))
		return constraintFieldRef{}, false
	}
	body, ok := expressionReturn(callback)
	if !ok {
		w.addErrf("%s callback must return a single expression", condName(call))
		return constraintFieldRef{}, false
	}
	child := maps.Clone(scope)
	name := "item-" + strconv.Itoa(int(callback.Pos()))
	binding := name
	elem := list.elem
	elem.prefix = name
	if list.isMap {
		binding = name + "-key, " + name
		elem = scopeRoot{w: w, mapEntry: &elem, mapKeyPrefix: name + "-key", mapValuePrefix: name}
	}
	child[param] = elem
	iterable := collectionIterablePath(list.path, list.nullable, list.isMap)
	if count {
		filter, ok := w.condString(body, child)
		if !ok {
			w.addErrf("CountWhere callback must return a supported condition")
			return constraintFieldRef{}, false
		}
		return constraintFieldRef{
			expr:      "@core.length([for " + binding + " in " + iterable + ": true when " + filter + "])",
			valueType: typecheck.TInteger(),
		}, true
	}
	value, ok := w.operand(body, child)
	if !ok {
		w.addErrf("Project callback must return a supported expression")
		return constraintFieldRef{}, false
	}
	t := value.valueType
	if value.nullable {
		t = typecheck.TOptional(t)
	}
	return constraintFieldRef{
		expr:      "[for " + binding + " in " + iterable + ": " + value.expr + "]",
		valueType: typecheck.TList(t),
	}, true
}

func expressionReturn(fn *ast.FuncLit) (ast.Expr, bool) {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return nil, false
	}
	stmt, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(stmt.Results) != 1 {
		return nil, false
	}
	return stmt.Results[0], true
}

func (w *walker) expressionCondition(call *ast.CallExpr, scope constraintScope) (string, bool) {
	if len(call.Args) == 0 {
		w.addErrf("Expr takes a constant expression and its field bindings")
		return "", false
	}
	source, ok := w.constraintString(call.Args[0], scope)
	if !ok {
		w.addErrf("Expr requires a constant string expression")
		return "", false
	}
	expr, err := lang.ParseExpr("Expr", []byte(source))
	if err != nil {
		w.addErrf("Expr: %v", err)
		return "", false
	}
	bindings, fields, ok := w.expressionBindings(call.Args[1:], scope)
	if !ok {
		return "", false
	}
	typeScope := &typecheck.Scope{
		Inputs: fields, MissingAsNull: true,
		LookupFunction: func(library, name string) (typecheck.FuncSig, bool) {
			sig, ok := runtime.CoreFunctionSigs()[name]
			return sig, ok && library == lang.CoreNamespace
		},
	}
	errs := lang.NewErrorList(0)
	typecheck.Check(expr, typecheck.TBoolean(), typeScope, errs)
	paths, refErr := expressionPaths(expr, bindings)
	if errs.Len() != 0 || refErr != nil {
		for _, typeErr := range errs.Errors() {
			w.addErrf("Expr: %s", typeErr.Msg)
		}
		if refErr != nil {
			w.addErrf("Expr: %v", refErr)
		}
		return "", false
	}
	for path, target := range paths {
		path.Root = target.Root
		path.Segments = append(slices.Clone(target.Segments), path.Segments[1:]...)
	}
	out, err := lang.FormatExpr(expr)
	if err != nil {
		w.addErrf("Expr: %v", err)
		return "", false
	}
	return "(" + strings.TrimSpace(string(out)) + ")", true
}

func (w *walker) expressionBindings(
	args []ast.Expr, scope constraintScope,
) (map[string]*lang.DotPath, []typecheck.ObjectField, bool) {
	bindings := make(map[string]*lang.DotPath, len(args))
	fields := make([]typecheck.ObjectField, 0, len(args))
	for _, arg := range args {
		call, ok := arg.(*ast.CallExpr)
		if !ok || w.constraintCallName(call) != "Bind" || len(call.Args) != 2 {
			w.addErrf("Expr bindings must be Bind(name, field) calls")
			return nil, nil, false
		}
		name, ok := w.constraintString(call.Args[0], scope)
		if !ok || name == "" || bindings[name] != nil {
			w.addErrf("Expr binding names must be nonempty and distinct constant strings")
			return nil, nil, false
		}
		field, ok := w.selectorFieldRef(call.Args[1], scope)
		if !ok {
			return nil, nil, false
		}
		target, err := lang.ParseExpr("Bind", []byte(field.expr))
		if err != nil {
			w.addErrf("Expr binding %q: %v", name, err)
			return nil, nil, false
		}
		path, ok := target.(*lang.DotPath)
		if !ok {
			// A projection callback can bind a scalar comprehension variable.
			id, ok := target.(*lang.Ident)
			if !ok {
				w.addErrf("Expr binding %q must refer to a field or iteration value", name)
				return nil, nil, false
			}
			path = &lang.DotPath{Root: id}
		}
		bindings[name] = path
		fields = append(fields, typecheck.ObjectField{
			Name: name, Type: field.valueType, Optional: field.nullable,
		})
	}
	return bindings, fields, true
}

func expressionPaths(
	expr lang.Expr, bindings map[string]*lang.DotPath,
) (map[*lang.DotPath]*lang.DotPath, error) {
	paths := map[*lang.DotPath]*lang.DotPath{}
	var err error
	lang.ScanExpr(expr, lang.ScanCallbacks{
		Expr: func(expr lang.Expr, ctx lang.ScanContext) lang.ScanDecision {
			switch expr := expr.(type) {
			case *lang.Ident:
				if !slices.Contains(ctx.ComprehensionBindings, expr.Name) {
					err = fmt.Errorf("unknown name %q", expr.Name)
				}
			case *lang.Comprehension:
				for _, name := range expr.Names {
					if slices.Contains(ctx.ComprehensionBindings, name) {
						err = fmt.Errorf("a comprehension cannot rebind %q", name)
					}
				}
			}
			if err != nil {
				return lang.ScanStop
			}
			return lang.ScanContinue
		},
		DotPath: func(path *lang.DotPath, ctx lang.ScanContext) lang.ScanDecision {
			if slices.Contains(ctx.ComprehensionBindings, path.Root.Name) {
				return lang.ScanContinue
			}
			if path.Root.Name == "input" && len(path.Segments) > 0 {
				name := path.Segments[0].Name
				if target := bindings[name]; target != nil {
					if slices.Contains(ctx.ComprehensionBindings, target.Root.Name) {
						err = fmt.Errorf("a comprehension shadows input binding %q", name)
						return lang.ScanStop
					}
					paths[path] = target
					return lang.ScanContinue
				}
			}
			err = fmt.Errorf("use Bind to declare the input referenced by %s", path.Root.Name)
			return lang.ScanStop
		},
		Call: func(call *lang.Call, _ lang.ScanContext) lang.ScanDecision {
			if call.Library == nil || call.Func == nil || call.Library.Name != lang.CoreNamespace {
				err = fmt.Errorf("only @core calls are available")
				return lang.ScanStop
			}
			sig, ok := runtime.CoreFunctionSigs()[call.Func.Name]
			if !ok {
				err = fmt.Errorf("unknown @core function %q", call.Func.Name)
				return lang.ScanStop
			}
			if len(call.Args) < len(sig.Params) ||
				sig.Variadic == nil && len(call.Args) != len(sig.Params) {
				err = fmt.Errorf("invalid argument count for @core.%s", call.Func.Name)
				return lang.ScanStop
			}
			return lang.ScanContinue
		},
	})
	return paths, err
}
