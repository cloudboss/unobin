package goschema

import (
	"go/ast"
	"go/constant"
	"go/token"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func (w *walker) constraintString(arg ast.Expr, scope constraintScope) (string, bool) {
	v := w.constraintConstant(arg, scope, -1, map[ast.Expr]bool{})
	if v.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(v), true
}

func (w *walker) constantOperand(arg ast.Expr, scope constraintScope) (constraintFieldRef, bool) {
	v := w.constraintConstant(arg, scope, -1, map[ast.Expr]bool{})
	switch v.Kind() {
	case constant.Bool:
		return constraintFieldRef{expr: v.ExactString(), valueType: typecheck.TBoolean()}, true
	case constant.String:
		return constraintFieldRef{
			expr: lang.Render(constant.StringVal(v)), valueType: typecheck.TString(),
		}, true
	case constant.Int:
		n, exact := constant.Int64Val(v)
		if exact {
			return constraintFieldRef{
				expr: strconv.FormatInt(n, 10), valueType: typecheck.TInteger(),
			}, true
		}
	case constant.Float:
		n, _ := constant.Float64Val(v)
		if !math.IsInf(n, 0) {
			out := strconv.FormatFloat(n, 'f', -1, 64)
			if n == math.Trunc(n) {
				out += ".0"
			}
			return constraintFieldRef{expr: out, valueType: typecheck.TNumber()}, true
		}
	}
	return constraintFieldRef{}, false
}

func (w *walker) constraintConstant(
	arg ast.Expr, scope constraintScope, iotaIndex int, active map[ast.Expr]bool,
) (result constant.Value) {
	arg = ast.Unparen(arg)
	if active[arg] {
		return constant.MakeUnknown()
	}
	active[arg] = true
	defer delete(active, arg)
	defer func() {
		if recover() != nil {
			result = constant.MakeUnknown()
		}
	}()
	eval := func(e ast.Expr) constant.Value {
		return w.constraintConstant(e, scope, iotaIndex, active)
	}
	switch x := arg.(type) {
	case *ast.BasicLit:
		return constant.MakeFromLiteral(x.Value, x.Kind, 0)
	case *ast.Ident:
		if _, shadowed := scope[x.Name]; shadowed {
			return constant.MakeUnknown()
		}
		if value, typ, i, ok := findConstraintConstant(w.files, x.Name); ok {
			return w.convertConstraintConstant(w.constraintConstant(value, nil, i, active), typ)
		}
		switch x.Name {
		case "true", "false":
			return constant.MakeBool(x.Name == "true")
		case "iota":
			if iotaIndex >= 0 {
				return constant.MakeInt64(int64(iotaIndex))
			}
		}
	case *ast.SelectorExpr:
		if root, ok := identName(x.X); ok {
			if _, shadowed := scope[root]; shadowed {
				break
			}
			if sub := w.constantPackage(w.importPathFor(root, x.Pos())); sub != nil {
				if value, typ, i, ok := findConstraintConstant(sub.files, x.Sel.Name); ok {
					return sub.convertConstraintConstant(sub.constraintConstant(value, nil, i, active), typ)
				}
			}
		}
	case *ast.UnaryExpr:
		return constant.UnaryOp(x.Op, eval(x.X), 0)
	case *ast.BinaryExpr:
		left, right := eval(x.X), eval(x.Y)
		if left.Kind() == constant.Unknown || right.Kind() == constant.Unknown {
			break
		}
		switch x.Op {
		case token.LAND, token.LOR:
			if left.Kind() == constant.Bool && right.Kind() == constant.Bool {
				a, b := constant.BoolVal(left), constant.BoolVal(right)
				return constant.MakeBool(x.Op == token.LAND && a && b || x.Op == token.LOR && (a || b))
			}
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			return constant.MakeBool(constant.Compare(left, x.Op, right))
		case token.SHL, token.SHR:
			amount, ok := constant.Uint64Val(constant.ToInt(right))
			if ok && amount <= 1024 {
				return constant.Shift(left, x.Op, uint(amount))
			}
		case token.QUO:
			if left.Kind() == constant.Int && right.Kind() == constant.Int {
				return constant.BinaryOp(left, token.QUO_ASSIGN, right)
			}
			return constant.BinaryOp(left, x.Op, right)
		default:
			return constant.BinaryOp(left, x.Op, right)
		}
	case *ast.CallExpr:
		if len(x.Args) != 1 {
			break
		}
		return w.convertConstraintConstant(eval(x.Args[0]), x.Fun)
	}
	return constant.MakeUnknown()
}

func (w *walker) constantPackage(path string) *walker {
	if sub := w.sub(path); sub != nil {
		return sub
	}
	first, _, _ := strings.Cut(path, "/")
	if w.pkg == nil || w.pkg.target.Build.GOROOT == "" ||
		!filepath.IsLocal(path) || strings.Contains(first, ".") {
		return nil
	}
	dir := filepath.Join(w.pkg.target.Build.GOROOT, "src", path)
	pkg, err := parseIndexedPackageWithContext(dir, path, w.pkg.target, w.roots...)
	if err != nil {
		return nil
	}
	w.packageCache[path] = pkg
	return w.sub(path)
}

func (w *walker) convertConstraintConstant(value constant.Value, typ ast.Expr) constant.Value {
	if typ == nil {
		return value
	}
	switch w.typeFromAST(typ).Kind {
	case typecheck.Integer:
		return constant.ToInt(value)
	case typecheck.Number:
		numeric := constant.ToFloat(value)
		if numeric.Kind() == constant.Unknown {
			return numeric
		}
		if id, ok := typ.(*ast.Ident); ok && id.Name == "float32" {
			f, _ := constant.Float32Val(numeric)
			return constant.MakeFloat64(float64(f))
		}
		f, _ := constant.Float64Val(numeric)
		return constant.MakeFloat64(f)
	case typecheck.String:
		if value.Kind() == constant.String {
			return value
		}
		if value.Kind() == constant.Int {
			n, ok := constant.Int64Val(value)
			if !ok || n < 0 || n > utf8.MaxRune {
				return constant.MakeString(string(utf8.RuneError))
			}
			return constant.MakeString(string(rune(n)))
		}
	case typecheck.Boolean:
		if value.Kind() == constant.Bool {
			return value
		}
	}
	return constant.MakeUnknown()
}

func findConstraintConstant(files []*ast.File, name string) (ast.Expr, ast.Expr, int, bool) {
	for _, file := range files {
		for _, decl := range file.Decls {
			group, ok := decl.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			var expressions []ast.Expr
			var typ ast.Expr
			for i, spec := range group.Specs {
				value := spec.(*ast.ValueSpec)
				if len(value.Values) != 0 {
					expressions = value.Values
					typ = value.Type
				}
				for j, ident := range value.Names {
					if ident.Name == name && j < len(expressions) {
						return expressions[j], typ, i, true
					}
				}
			}
		}
	}
	return nil, nil, 0, false
}
